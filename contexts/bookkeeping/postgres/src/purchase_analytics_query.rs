use std::collections::{BTreeMap, BTreeSet};

use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, TimeZone, Utc};
use diesel::{ExpressionMethods, JoinOnDsl, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum PurchaseAnalyticsQueryError {
    Pool(String),
    Database(diesel::result::Error),
    HouseholdNotFound,
    InvalidPeriod,
    AmountOutOfRange,
}

impl From<diesel::result::Error> for PurchaseAnalyticsQueryError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseAnalyticsProjection {
    pub dimension: String,
    pub id: String,
    pub name: String,
    pub period: String,
    pub amount_yen: i64,
    pub count: i64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchasePriceObservationProjection {
    pub item_id: String,
    pub occurred_at: DateTime<Utc>,
    pub amount_yen: i64,
    pub remaining_refundable_amount_yen: i64,
    pub effective_unit_price_numerator_yen: Option<String>,
    pub effective_unit_price_denominator: Option<String>,
    pub printed_unit_price_yen: Option<i64>,
    pub printed_unit_price_quantity_numerator: Option<String>,
    pub printed_unit_price_quantity_denominator: Option<String>,
    pub printed_unit_price_unit: Option<String>,
}

type AnalyticsKey = (String, String, String, String);

pub async fn purchase_analytics(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    from: &str,
    to: &str,
) -> Result<Vec<PurchaseAnalyticsProjection>, PurchaseAnalyticsQueryError> {
    let (start, end) = period_range(from, to)?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseAnalyticsQueryError::Pool(error.to_string()))?;
    require_member(&mut conn, actor_id, household_id).await?;
    use public::{
        accounts, pools, purchase_allocations, purchase_corrections, purchase_items,
        purchase_payments, purchases,
    };

    let purchases_in_range = purchases::table
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::occurred_at.ge(start))
        .filter(purchases::occurred_at.lt(end))
        .filter(purchases::status.eq("POSTED"))
        .select((
            purchases::id,
            purchases::occurred_at,
            purchases::merchant,
            purchases::payment_account_id,
        ))
        .load::<(Uuid, DateTime<Utc>, String, Option<Uuid>)>(&mut conn)
        .await?;
    let purchase_ids = purchases_in_range
        .iter()
        .map(|(id, _, _, _)| *id)
        .collect::<Vec<_>>();
    if purchase_ids.is_empty() {
        return Ok(Vec::new());
    }
    let item_rows = purchase_items::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_items::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::id.eq_any(&purchase_ids))
        .select((
            purchase_items::id,
            purchase_items::purchase_id,
            purchase_items::product_variant_id,
            purchase_items::name,
            purchase_items::amount_yen,
            purchase_items::quantity_unit,
        ))
        .load::<(Uuid, Uuid, Option<Uuid>, String, i64, Option<String>)>(&mut conn)
        .await?;
    let correction_rows = purchase_corrections::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_corrections::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::id.eq_any(&purchase_ids))
        .select((
            purchase_corrections::item_id,
            purchase_corrections::amount_delta_yen,
            purchase_corrections::purchase_id,
        ))
        .load::<(Option<Uuid>, i64, Uuid)>(&mut conn)
        .await?;
    let allocation_rows = purchase_allocations::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_allocations::purchase_id)))
        .inner_join(pools::table.on(pools::id.eq(purchase_allocations::pool_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::id.eq_any(&purchase_ids))
        .select((
            purchase_allocations::pool_id,
            pools::name,
            purchase_allocations::amount_yen,
            purchase_allocations::purchase_id,
        ))
        .load::<(Uuid, String, i64, Uuid)>(&mut conn)
        .await?;
    let payment_rows = purchase_payments::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_payments::purchase_id)))
        .inner_join(accounts::table.on(accounts::id.eq(purchase_payments::account_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::id.eq_any(&purchase_ids))
        .select((
            purchase_payments::account_id,
            accounts::name,
            purchase_payments::amount_yen,
            purchase_payments::purchase_id,
        ))
        .load::<(Uuid, String, i64, Uuid)>(&mut conn)
        .await?;
    let legacy_account_ids = purchases_in_range
        .iter()
        .filter_map(|(_, _, _, id)| *id)
        .collect::<BTreeSet<_>>();
    let account_names = if legacy_account_ids.is_empty() {
        BTreeMap::new()
    } else {
        accounts::table
            .filter(accounts::household_id.eq(household_id))
            .filter(accounts::id.eq_any(legacy_account_ids))
            .select((accounts::id, accounts::name))
            .load::<(Uuid, String)>(&mut conn)
            .await?
            .into_iter()
            .collect::<BTreeMap<_, _>>()
    };

    let purchase_periods = purchases_in_range
        .iter()
        .map(|(id, occurred, _, _)| (*id, period_string(*occurred)))
        .collect::<BTreeMap<_, _>>();
    let mut correction_by_item = BTreeMap::<Uuid, i64>::new();
    let mut correction_by_purchase = BTreeMap::<Uuid, i64>::new();
    let mut wide_correction_by_purchase = BTreeMap::<Uuid, i64>::new();
    for (item_id, amount, purchase_id) in correction_rows {
        checked_add(&mut correction_by_purchase, purchase_id, amount)?;
        if let Some(item_id) = item_id {
            checked_add(&mut correction_by_item, item_id, amount)?;
        } else {
            checked_add(&mut wide_correction_by_purchase, purchase_id, amount)?;
        }
    }

    let mut totals = BTreeMap::<AnalyticsKey, (i64, i64)>::new();
    for (purchase_id, _, merchant, _) in &purchases_in_range {
        let period = purchase_periods
            .get(purchase_id)
            .cloned()
            .ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?;
        let item_total = item_rows
            .iter()
            .filter(|item| item.1 == *purchase_id)
            .try_fold(0_i64, |sum, item| {
                sum.checked_add(item.4)
                    .ok_or(PurchaseAnalyticsQueryError::AmountOutOfRange)
            })?;
        let correction = correction_by_purchase
            .get(purchase_id)
            .copied()
            .unwrap_or(0);
        let id = merchant.clone();
        let name = if merchant.is_empty() {
            "店舗未入力"
        } else {
            merchant
        }
        .to_owned();
        add_row(
            &mut totals,
            ("merchant".into(), id, name, period),
            item_total
                .checked_add(correction)
                .ok_or(PurchaseAnalyticsQueryError::AmountOutOfRange)?,
            1,
        )?;
    }
    for (item_id, purchase_id, variant_id, item_name, amount, unit) in &item_rows {
        let period = purchase_periods
            .get(purchase_id)
            .cloned()
            .ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?;
        let id = format!(
            "{}\u{1f}{}",
            variant_id.map_or_else(|| item_name.clone(), |value| value.to_string()),
            unit.as_deref().unwrap_or("")
        );
        let display_name = format!("{} · {}", item_name, unit.as_deref().unwrap_or("単位不明"));
        let adjusted = amount
            .checked_add(correction_by_item.get(item_id).copied().unwrap_or(0))
            .ok_or(PurchaseAnalyticsQueryError::AmountOutOfRange)?;
        add_row(
            &mut totals,
            ("item".into(), id, display_name, period),
            adjusted,
            1,
        )?;
    }
    for (id, name, amount, purchase_id) in allocation_rows {
        let period = purchase_periods
            .get(&purchase_id)
            .cloned()
            .ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?;
        add_row(
            &mut totals,
            ("pool".into(), id.to_string(), name, period),
            amount,
            1,
        )?;
    }
    let purchases_with_payment = payment_rows
        .iter()
        .map(|(_, _, _, purchase_id)| *purchase_id)
        .collect::<BTreeSet<_>>();
    for (id, name, amount, purchase_id) in payment_rows {
        let period = purchase_periods
            .get(&purchase_id)
            .cloned()
            .ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?;
        add_row(
            &mut totals,
            ("account".into(), id.to_string(), name, period),
            amount,
            1,
        )?;
    }
    for (purchase_id, _, _, legacy_account_id) in &purchases_in_range {
        if purchases_with_payment.contains(purchase_id) {
            continue;
        }
        if let Some(account_id) = legacy_account_id {
            let period = purchase_periods
                .get(purchase_id)
                .cloned()
                .ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?;
            let amount = item_rows
                .iter()
                .filter(|item| item.1 == *purchase_id)
                .try_fold(0_i64, |sum, item| {
                    sum.checked_add(item.4)
                        .ok_or(PurchaseAnalyticsQueryError::AmountOutOfRange)
                })?;
            let name = account_names.get(account_id).cloned().unwrap_or_default();
            add_row(
                &mut totals,
                ("account".into(), account_id.to_string(), name, period),
                amount,
                1,
            )?;
        }
    }
    let mut correction_totals = BTreeMap::<String, (i64, i64)>::new();
    let mut wide_correction_totals = BTreeMap::<String, (i64, i64)>::new();
    for (purchase_id, amount) in &correction_by_purchase {
        if *amount == 0 {
            continue;
        }
        let period = purchase_periods
            .get(purchase_id)
            .cloned()
            .ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?;
        add_amount_pair(&mut correction_totals, period.clone(), *amount, 1)?;
    }
    for (purchase_id, wide_amount) in &wide_correction_by_purchase {
        if *wide_amount == 0 {
            continue;
        }
        let period = purchase_periods
            .get(purchase_id)
            .cloned()
            .ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?;
        add_amount_pair(&mut wide_correction_totals, period, *wide_amount, 1)?;
    }
    for (period, (amount, count)) in correction_totals {
        add_row(
            &mut totals,
            (
                "pool".into(),
                "__correction__".into(),
                "プール未配賦の訂正".into(),
                period.clone(),
            ),
            amount,
            count,
        )?;
        add_row(
            &mut totals,
            (
                "account".into(),
                "__correction__".into(),
                "決済元未配賦の訂正".into(),
                period,
            ),
            amount,
            count,
        )?;
    }
    for (period, (amount, count)) in wide_correction_totals {
        add_row(
            &mut totals,
            (
                "item".into(),
                "__correction__".into(),
                "購入全体の訂正".into(),
                period,
            ),
            amount,
            count,
        )?;
    }
    Ok(totals
        .into_iter()
        .map(
            |((dimension, id, name, period), (amount_yen, count))| PurchaseAnalyticsProjection {
                dimension,
                id,
                name,
                period,
                amount_yen,
                count,
            },
        )
        .collect())
}

pub async fn purchase_price_observations(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    from: &str,
    to: &str,
    item_ids: &[String],
) -> Result<Vec<PurchasePriceObservationProjection>, PurchaseAnalyticsQueryError> {
    let (start, end) = period_range(from, to)?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseAnalyticsQueryError::Pool(error.to_string()))?;
    require_member(&mut conn, actor_id, household_id).await?;
    if item_ids.is_empty() {
        return Ok(Vec::new());
    }
    use public::{purchase_corrections, purchase_items, purchases};
    let items = purchase_items::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_items::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::occurred_at.ge(start))
        .filter(purchases::occurred_at.lt(end))
        .filter(purchases::status.eq("POSTED"))
        .select((
            purchase_items::id,
            purchase_items::purchase_id,
            purchase_items::product_variant_id,
            purchase_items::name,
            purchase_items::amount_yen,
            purchase_items::quantity_numerator,
            purchase_items::quantity_denominator,
            purchase_items::quantity_unit,
            purchase_items::printed_unit_price_yen,
            purchase_items::printed_unit_price_quantity_numerator,
            purchase_items::printed_unit_price_quantity_denominator,
            purchase_items::printed_unit_price_unit,
            purchases::occurred_at,
        ))
        .load::<(
            Uuid,
            Uuid,
            Option<Uuid>,
            String,
            i64,
            Option<i64>,
            Option<i64>,
            Option<String>,
            Option<i64>,
            Option<i64>,
            Option<i64>,
            Option<String>,
            DateTime<Utc>,
        )>(&mut conn)
        .await?;
    let matches = items
        .iter()
        .filter(|item| {
            requested_item_id(item.2, &item.3, item.7.as_deref())
                .as_ref()
                .is_some_and(|id| item_ids.iter().any(|requested| requested == id))
        })
        .collect::<Vec<_>>();
    let matched_ids = matches.iter().map(|item| item.0).collect::<BTreeSet<_>>();
    let corrections = if matched_ids.is_empty() {
        Vec::new()
    } else {
        purchase_corrections::table
            .inner_join(purchases::table.on(purchases::id.eq(purchase_corrections::purchase_id)))
            .filter(purchases::household_id.eq(household_id))
            .filter(purchases::occurred_at.ge(start))
            .filter(purchases::occurred_at.lt(end))
            .filter(purchases::status.eq("POSTED"))
            .filter(purchase_corrections::item_id.eq_any(matched_ids))
            .select((
                purchase_corrections::item_id,
                purchase_corrections::amount_delta_yen,
            ))
            .load::<(Option<Uuid>, i64)>(&mut conn)
            .await?
    };
    let mut correction_totals = BTreeMap::<Uuid, i64>::new();
    for (item_id, amount) in corrections {
        if let Some(item_id) = item_id {
            checked_add(&mut correction_totals, item_id, amount)?;
        }
    }
    let mut result = Vec::with_capacity(matches.len());
    for item in matches {
        let numerator = item
            .5
            .zip(item.6)
            .filter(|(_, denominator)| *denominator > 0)
            .and_then(|(n, d)| effective_price(item.4, n, d));
        let item_id = requested_item_id(item.2, &item.3, item.7.as_deref()).unwrap_or_default();
        result.push(PurchasePriceObservationProjection {
            item_id,
            occurred_at: item.12,
            amount_yen: item.4,
            remaining_refundable_amount_yen: item
                .4
                .checked_add(correction_totals.get(&item.0).copied().unwrap_or(0))
                .ok_or(PurchaseAnalyticsQueryError::AmountOutOfRange)?,
            effective_unit_price_numerator_yen: numerator.as_ref().map(|(n, _)| n.clone()),
            effective_unit_price_denominator: numerator.map(|(_, d)| d),
            printed_unit_price_yen: item.8,
            printed_unit_price_quantity_numerator: item.9.map(|value| value.to_string()),
            printed_unit_price_quantity_denominator: item.10.map(|value| value.to_string()),
            printed_unit_price_unit: item.11.clone(),
        });
    }
    result.sort_by_key(|item| item.occurred_at);
    Ok(result)
}

fn requested_item_id(variant_id: Option<Uuid>, name: &str, unit: Option<&str>) -> Option<String> {
    Some(format!(
        "{}\u{1f}{}",
        variant_id.map_or_else(|| name.to_owned(), |id| id.to_string()),
        unit.unwrap_or("")
    ))
}

fn effective_price(amount: i64, numerator: i64, denominator: i64) -> Option<(String, String)> {
    if amount < 0 || numerator <= 0 || denominator <= 0 {
        return None;
    }
    let top = i128::from(amount).checked_mul(i128::from(denominator))?;
    let bottom = i128::from(numerator);
    let divisor = gcd(top, bottom);
    Some(((top / divisor).to_string(), (bottom / divisor).to_string()))
}

fn gcd(mut left: i128, mut right: i128) -> i128 {
    while right != 0 {
        let remainder = left % right;
        left = right;
        right = remainder;
    }
    left.max(1)
}

fn add_row(
    totals: &mut BTreeMap<AnalyticsKey, (i64, i64)>,
    key: AnalyticsKey,
    amount: i64,
    count: i64,
) -> Result<(), PurchaseAnalyticsQueryError> {
    let current = totals.entry(key).or_default();
    current.0 = current
        .0
        .checked_add(amount)
        .ok_or(PurchaseAnalyticsQueryError::AmountOutOfRange)?;
    current.1 = current
        .1
        .checked_add(count)
        .ok_or(PurchaseAnalyticsQueryError::AmountOutOfRange)?;
    Ok(())
}

fn checked_add<K: Ord>(
    totals: &mut BTreeMap<K, i64>,
    key: K,
    amount: i64,
) -> Result<(), PurchaseAnalyticsQueryError> {
    let total = totals.entry(key).or_default();
    *total = total
        .checked_add(amount)
        .ok_or(PurchaseAnalyticsQueryError::AmountOutOfRange)?;
    Ok(())
}

fn add_amount_pair<K: Ord>(
    totals: &mut BTreeMap<K, (i64, i64)>,
    key: K,
    amount: i64,
    count: i64,
) -> Result<(), PurchaseAnalyticsQueryError> {
    let total = totals.entry(key).or_default();
    total.0 = total
        .0
        .checked_add(amount)
        .ok_or(PurchaseAnalyticsQueryError::AmountOutOfRange)?;
    total.1 = total
        .1
        .checked_add(count)
        .ok_or(PurchaseAnalyticsQueryError::AmountOutOfRange)?;
    Ok(())
}

async fn require_member(
    conn: &mut diesel_async::AsyncPgConnection,
    actor_id: Uuid,
    household_id: Uuid,
) -> Result<(), PurchaseAnalyticsQueryError> {
    if household_postgres::household_access::active_member_role(conn, household_id, actor_id)
        .await?
        .is_some()
    {
        Ok(())
    } else {
        Err(PurchaseAnalyticsQueryError::HouseholdNotFound)
    }
}

fn period_range(
    from: &str,
    to: &str,
) -> Result<(DateTime<Utc>, DateTime<Utc>), PurchaseAnalyticsQueryError> {
    let start_date = NaiveDate::parse_from_str(&format!("{from}-01"), "%Y-%m-%d")
        .map_err(|_| PurchaseAnalyticsQueryError::InvalidPeriod)?;
    let to_date = NaiveDate::parse_from_str(&format!("{to}-01"), "%Y-%m-%d")
        .map_err(|_| PurchaseAnalyticsQueryError::InvalidPeriod)?;
    let upper_bound = start_date
        .checked_add_months(chrono::Months::new(120))
        .ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?;
    if to_date < start_date || to_date > upper_bound {
        return Err(PurchaseAnalyticsQueryError::InvalidPeriod);
    }
    let next_month = to_date
        .checked_add_months(chrono::Months::new(1))
        .ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?;
    let tokyo =
        FixedOffset::east_opt(9 * 60 * 60).ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?;
    let start = tokyo
        .from_local_datetime(
            &start_date
                .and_hms_opt(0, 0, 0)
                .ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?,
        )
        .single()
        .ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?;
    let end = tokyo
        .from_local_datetime(
            &next_month
                .and_hms_opt(0, 0, 0)
                .ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?,
        )
        .single()
        .ok_or(PurchaseAnalyticsQueryError::InvalidPeriod)?;
    Ok((start.with_timezone(&Utc), end.with_timezone(&Utc)))
}

fn period_string(value: DateTime<Utc>) -> String {
    let tokyo = FixedOffset::east_opt(9 * 60 * 60).expect("valid fixed offset");
    let local = value.with_timezone(&tokyo);
    format!("{:04}-{:02}", local.year(), local.month())
}
