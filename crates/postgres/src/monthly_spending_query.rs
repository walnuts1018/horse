use std::collections::{BTreeMap, BTreeSet};

use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, TimeZone, Utc};
use diesel::{BoolExpressionMethods, ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum MonthlySpendingQueryError {
    Pool(String),
    Database(diesel::result::Error),
    HouseholdNotFound,
    InvalidPeriod,
    AmountOutOfRange,
}

impl From<diesel::result::Error> for MonthlySpendingQueryError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MonthlyCategorySpendingProjection {
    pub period: String,
    pub category_id: Option<Uuid>,
    pub category_name: Option<String>,
    pub amount_yen: i64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MonthlyMemberSpendingProjection {
    pub period: String,
    pub member_id: Uuid,
    pub member_name: String,
    pub amount_yen: i64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MonthlyMemberSpendingReportProjection {
    pub items: Vec<MonthlyMemberSpendingProjection>,
    pub has_unattributed_purchases: bool,
}

type PurchaseFact = (DateTime<Utc>, Option<Uuid>, i64);
type MemberFact = (DateTime<Utc>, Uuid, i64);

pub async fn monthly_category_spending(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    from: &str,
    to: &str,
) -> Result<Vec<MonthlyCategorySpendingProjection>, MonthlySpendingQueryError> {
    let (start, end) = period_range(from, to)?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| MonthlySpendingQueryError::Pool(error.to_string()))?;
    require_member(&mut conn, actor_id, household_id).await?;

    use public::{categories, purchase_corrections, purchase_items, purchases};
    let items = purchase_items::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_items::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::occurred_at.ge(start))
        .filter(purchases::occurred_at.lt(end))
        .filter(purchases::status.eq_any(["POSTED", "CLOSED"]))
        .select((
            purchases::occurred_at,
            purchase_items::category_id,
            purchase_items::amount_yen,
        ))
        .load::<PurchaseFact>(&mut conn)
        .await?;
    let corrections = purchase_corrections::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_corrections::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::occurred_at.ge(start))
        .filter(purchases::occurred_at.lt(end))
        .filter(purchases::status.eq_any(["POSTED", "CLOSED"]))
        .select((
            purchases::occurred_at,
            purchase_corrections::item_id,
            purchase_corrections::amount_delta_yen,
        ))
        .load::<(DateTime<Utc>, Option<Uuid>, i64)>(&mut conn)
        .await?;

    let corrected_item_ids = corrections
        .iter()
        .filter_map(|(_, item_id, _)| *item_id)
        .collect::<BTreeSet<_>>();
    let corrected_categories = if corrected_item_ids.is_empty() {
        BTreeMap::new()
    } else {
        purchase_items::table
            .filter(purchase_items::household_id.eq(household_id))
            .filter(purchase_items::id.eq_any(corrected_item_ids))
            .select((purchase_items::id, purchase_items::category_id))
            .load::<(Uuid, Option<Uuid>)>(&mut conn)
            .await?
            .into_iter()
            .collect::<BTreeMap<_, _>>()
    };
    let mut totals = BTreeMap::<(String, Option<Uuid>), i64>::new();
    for (occurred_at, category_id, amount) in items {
        add_amount(
            &mut totals,
            (period_string(occurred_at), category_id),
            amount,
        )?;
    }
    for (occurred_at, item_id, amount) in corrections {
        let category_id = item_id.and_then(|id| corrected_categories.get(&id).copied().flatten());
        add_amount(
            &mut totals,
            (period_string(occurred_at), category_id),
            amount,
        )?;
    }
    let category_ids = totals
        .keys()
        .filter_map(|(_, id)| *id)
        .collect::<BTreeSet<_>>();
    let names = if category_ids.is_empty() {
        BTreeMap::new()
    } else {
        categories::table
            .filter(categories::household_id.eq(household_id))
            .filter(categories::id.eq_any(category_ids))
            .select((categories::id, categories::name))
            .load::<(Uuid, String)>(&mut conn)
            .await?
            .into_iter()
            .collect::<BTreeMap<_, _>>()
    };
    Ok(totals
        .into_iter()
        .map(
            |((period, category_id), amount_yen)| MonthlyCategorySpendingProjection {
                period,
                category_name: category_id.and_then(|id| names.get(&id).cloned()),
                category_id,
                amount_yen,
            },
        )
        .collect())
}

pub async fn monthly_member_spending(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    from: &str,
    to: &str,
) -> Result<MonthlyMemberSpendingReportProjection, MonthlySpendingQueryError> {
    let (start, end) = period_range(from, to)?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| MonthlySpendingQueryError::Pool(error.to_string()))?;
    require_member(&mut conn, actor_id, household_id).await?;

    use public::{
        correction_member_burdens, purchase_corrections, purchase_member_burdens, purchases,
    };
    let burdens = purchase_member_burdens::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_member_burdens::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::occurred_at.ge(start))
        .filter(purchases::occurred_at.lt(end))
        .filter(purchases::status.eq_any(["POSTED", "CLOSED"]))
        .filter(purchase_member_burdens::household_id.eq(household_id))
        .select((
            purchases::occurred_at,
            purchase_member_burdens::member_id,
            purchase_member_burdens::amount_yen,
        ))
        .load::<MemberFact>(&mut conn)
        .await?;
    let correction_burdens = correction_member_burdens::table
        .inner_join(
            purchase_corrections::table
                .on(purchase_corrections::id.eq(correction_member_burdens::correction_id)),
        )
        .inner_join(purchases::table.on(purchases::id.eq(purchase_corrections::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::occurred_at.ge(start))
        .filter(purchases::occurred_at.lt(end))
        .filter(purchases::status.eq_any(["POSTED", "CLOSED"]))
        .filter(correction_member_burdens::household_id.eq(household_id))
        .select((
            purchases::occurred_at,
            correction_member_burdens::member_id,
            correction_member_burdens::amount_delta_yen,
        ))
        .load::<MemberFact>(&mut conn)
        .await?;

    let mut totals = BTreeMap::<(String, Uuid), i64>::new();
    for (occurred_at, member_id, amount) in burdens.into_iter().chain(correction_burdens) {
        add_amount(&mut totals, (period_string(occurred_at), member_id), amount)?;
    }
    let member_ids = totals.keys().map(|(_, id)| *id).collect::<BTreeSet<_>>();
    let names = if member_ids.is_empty() {
        BTreeMap::new()
    } else {
        public::household_members::table
            .inner_join(
                public::members::table
                    .on(public::members::id.eq(public::household_members::member_id)),
            )
            .filter(public::household_members::household_id.eq(household_id))
            .filter(public::household_members::member_id.eq_any(member_ids))
            .select((
                public::household_members::member_id,
                public::members::display_name,
            ))
            .load::<(Uuid, String)>(&mut conn)
            .await?
            .into_iter()
            .collect::<BTreeMap<_, _>>()
    };
    let items = totals
        .into_iter()
        .filter_map(|((period, member_id), amount_yen)| {
            names
                .get(&member_id)
                .map(|member_name| MonthlyMemberSpendingProjection {
                    period,
                    member_id,
                    member_name: member_name.clone(),
                    amount_yen,
                })
        })
        .collect();
    let has_unattributed_purchases =
        unattributed_purchases(&mut conn, household_id, start, end).await?;
    Ok(MonthlyMemberSpendingReportProjection {
        items,
        has_unattributed_purchases,
    })
}

async fn unattributed_purchases(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    start: DateTime<Utc>,
    end: DateTime<Utc>,
) -> Result<bool, MonthlySpendingQueryError> {
    use public::{
        correction_member_burdens, purchase_allocations, purchase_corrections,
        purchase_member_burdens, purchases,
    };
    let allocations = purchase_allocations::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_allocations::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::occurred_at.ge(start))
        .filter(purchases::occurred_at.lt(end))
        .filter(purchases::status.eq_any(["POSTED", "CLOSED"]))
        .select((
            purchase_allocations::purchase_id,
            purchase_allocations::item_id,
            purchase_allocations::pool_id,
            purchase_allocations::amount_yen,
        ))
        .load::<(Uuid, Uuid, Uuid, i64)>(conn)
        .await?;
    let burdens = purchase_member_burdens::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_member_burdens::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::occurred_at.ge(start))
        .filter(purchases::occurred_at.lt(end))
        .filter(purchases::status.eq_any(["POSTED", "CLOSED"]))
        .filter(purchase_member_burdens::household_id.eq(household_id))
        .select((
            purchase_member_burdens::purchase_id,
            purchase_member_burdens::item_id,
            purchase_member_burdens::pool_id,
            purchase_member_burdens::amount_yen,
        ))
        .load::<(Uuid, Uuid, Uuid, i64)>(conn)
        .await?;
    let mut allocation_totals = BTreeMap::<(Uuid, Uuid, Uuid), i64>::new();
    let mut burden_totals = BTreeMap::<(Uuid, Uuid, Uuid), i64>::new();
    for (purchase, item, pool, amount) in allocations {
        add_amount(&mut allocation_totals, (purchase, item, pool), amount)?;
    }
    for (purchase, item, pool, amount) in burdens {
        add_amount(&mut burden_totals, (purchase, item, pool), amount)?;
    }
    if allocation_totals.iter().any(|(key, amount)| {
        *amount != 0 && burden_totals.get(key).copied().unwrap_or(0) != *amount
    }) {
        return Ok(true);
    }

    let corrections = purchase_corrections::table
        .inner_join(purchases::table.on(purchases::id.eq(purchase_corrections::purchase_id)))
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::occurred_at.ge(start))
        .filter(purchases::occurred_at.lt(end))
        .filter(purchases::status.eq_any(["POSTED", "CLOSED"]))
        .select((
            purchase_corrections::id,
            purchase_corrections::amount_delta_yen,
        ))
        .load::<(Uuid, i64)>(conn)
        .await?;
    let correction_ids = corrections.iter().map(|(id, _)| *id).collect::<Vec<_>>();
    if correction_ids.is_empty() {
        return Ok(false);
    }
    let correction_burden_rows = correction_member_burdens::table
        .filter(correction_member_burdens::household_id.eq(household_id))
        .filter(correction_member_burdens::correction_id.eq_any(correction_ids))
        .select((
            correction_member_burdens::correction_id,
            correction_member_burdens::amount_delta_yen,
        ))
        .load::<(Uuid, i64)>(conn)
        .await?;
    let mut correction_totals = BTreeMap::<Uuid, i64>::new();
    for (id, amount) in correction_burden_rows {
        add_amount(&mut correction_totals, id, amount)?;
    }
    Ok(corrections
        .into_iter()
        .any(|(id, amount)| correction_totals.get(&id).copied().unwrap_or(0) != amount))
}

async fn require_member(
    conn: &mut diesel_async::AsyncPgConnection,
    actor_id: Uuid,
    household_id: Uuid,
) -> Result<(), MonthlySpendingQueryError> {
    use public::household_members;
    let active = diesel::select(diesel::dsl::exists(
        household_members::table
            .filter(household_members::household_id.eq(household_id))
            .filter(household_members::member_id.eq(actor_id))
            .filter(household_members::left_at.is_null()),
    ))
    .get_result::<bool>(conn)
    .await?;
    if active {
        Ok(())
    } else {
        Err(MonthlySpendingQueryError::HouseholdNotFound)
    }
}

fn period_range(
    from: &str,
    to: &str,
) -> Result<(DateTime<Utc>, DateTime<Utc>), MonthlySpendingQueryError> {
    let start_date = NaiveDate::parse_from_str(&format!("{from}-01"), "%Y-%m-%d")
        .map_err(|_| MonthlySpendingQueryError::InvalidPeriod)?;
    let to_date = NaiveDate::parse_from_str(&format!("{to}-01"), "%Y-%m-%d")
        .map_err(|_| MonthlySpendingQueryError::InvalidPeriod)?;
    let upper_bound = start_date
        .checked_add_months(chrono::Months::new(120))
        .ok_or(MonthlySpendingQueryError::InvalidPeriod)?;
    if to_date < start_date || to_date > upper_bound {
        return Err(MonthlySpendingQueryError::InvalidPeriod);
    }
    let next_month = to_date
        .checked_add_months(chrono::Months::new(1))
        .ok_or(MonthlySpendingQueryError::InvalidPeriod)?;
    let tokyo =
        FixedOffset::east_opt(9 * 60 * 60).ok_or(MonthlySpendingQueryError::InvalidPeriod)?;
    let start = tokyo
        .from_local_datetime(
            &start_date
                .and_hms_opt(0, 0, 0)
                .ok_or(MonthlySpendingQueryError::InvalidPeriod)?,
        )
        .single()
        .ok_or(MonthlySpendingQueryError::InvalidPeriod)?;
    let end = tokyo
        .from_local_datetime(
            &next_month
                .and_hms_opt(0, 0, 0)
                .ok_or(MonthlySpendingQueryError::InvalidPeriod)?,
        )
        .single()
        .ok_or(MonthlySpendingQueryError::InvalidPeriod)?;
    Ok((start.with_timezone(&Utc), end.with_timezone(&Utc)))
}

fn period_string(value: DateTime<Utc>) -> String {
    let tokyo = FixedOffset::east_opt(9 * 60 * 60).expect("valid fixed offset");
    let local = value.with_timezone(&tokyo);
    format!("{:04}-{:02}", local.year(), local.month())
}

fn add_amount<K: Ord>(
    totals: &mut BTreeMap<K, i64>,
    key: K,
    amount: i64,
) -> Result<(), MonthlySpendingQueryError> {
    let total = totals.entry(key).or_default();
    *total = total
        .checked_add(amount)
        .ok_or(MonthlySpendingQueryError::AmountOutOfRange)?;
    Ok(())
}
