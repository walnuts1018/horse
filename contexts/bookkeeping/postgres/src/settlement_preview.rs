use std::collections::BTreeMap;

use bookkeeping_application::settlement_preview::{
    PreviewAdjustment, PreviewFundingRule, PreviewInput, PreviewMember, PreviewObligation,
    PreviewPurchase, PreviewPurchaseAdjustment, PreviewPurchaseItem, PreviewPurchasePayment,
    PreviewSchedule, SettlementPreviewError, calculate_settlement_preview,
};
use bookkeeping_application::settlement_preview::{PreviewFundingRuleKind, PreviewScheduleCadence};
use bookkeeping_domain::SettlementPolicy;
use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, TimeZone, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::min, result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

pub type FundingScheduleRow = (
    Uuid,
    Uuid,
    Uuid,
    i64,
    String,
    NaiveDate,
    Option<NaiveDate>,
    bool,
    DateTime<Utc>,
);
pub type HouseholdFundingScheduleRow = (
    Uuid,
    Uuid,
    Uuid,
    Uuid,
    i64,
    String,
    NaiveDate,
    Option<NaiveDate>,
    bool,
);
pub type FundingRuleRow = (Uuid, i32, Option<Uuid>, Option<String>);
pub type FundingShareRow = (Uuid, i32, Uuid, i64);

#[async_trait::async_trait]
pub trait PolicySettlementData: Send {
    async fn household_members_for_settlement(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
    ) -> Result<Vec<(Uuid, String, Option<DateTime<Utc>>)>, DieselError>;

    async fn household_members_present_after(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        period_start_utc: DateTime<Utc>,
    ) -> Result<Vec<(Uuid, String)>, DieselError>;

    async fn schedules_for_period(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        start: NaiveDate,
        end: NaiveDate,
    ) -> Result<Vec<FundingScheduleRow>, DieselError>;

    async fn schedules_overlapping(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_ids: &[Uuid],
        start: NaiveDate,
        end: NaiveDate,
    ) -> Result<Vec<HouseholdFundingScheduleRow>, DieselError>;

    async fn earliest_schedule_start(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
    ) -> Result<Option<NaiveDate>, DieselError>;

    async fn rules_and_shares(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
    ) -> Result<(Vec<FundingRuleRow>, Vec<FundingShareRow>), DieselError>;

    async fn historical_rules(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        pool_ids: &[Uuid],
        versions: &[i32],
    ) -> Result<Vec<FundingRuleRow>, DieselError>;

    async fn rule_owner(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        pool_id: Uuid,
        version: i32,
    ) -> Result<Option<Uuid>, DieselError>;
}

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum SettlementPreviewPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid,
    InvalidPeriod,
    InvalidFundingSnapshot,
    Calculation(SettlementPreviewError),
}

impl From<DieselError> for SettlementPreviewPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

impl From<SettlementPreviewError> for SettlementPreviewPersistenceError {
    fn from(error: SettlementPreviewError) -> Self {
        Self::Calculation(error)
    }
}

pub async fn calculate_preview(
    pool: &PgPool,
    household_id: &str,
    period: &str,
    policy: &mut impl PolicySettlementData,
) -> Result<
    Vec<bookkeeping_application::settlement_preview::PreviewAmount>,
    SettlementPreviewPersistenceError,
> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SettlementPreviewPersistenceError::Pool(error.to_string()))?;
    calculate_preview_with_connection(&mut pooled, household_id, period, policy).await
}

pub async fn calculate_preview_with_connection(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: &str,
    period: &str,
    policy: &mut impl PolicySettlementData,
) -> Result<
    Vec<bookkeeping_application::settlement_preview::PreviewAmount>,
    SettlementPreviewPersistenceError,
> {
    let household_id = Uuid::parse_str(household_id)
        .map_err(|_| SettlementPreviewPersistenceError::InvalidUuid)?;
    let (target_start, _) = period_bounds(period)?;

    use public::settlement_run_lines::dsl as lines;
    use public::settlement_runs::dsl as runs;

    let member_rows = policy
        .household_members_for_settlement(conn, household_id)
        .await?;
    let members_data = member_rows
        .iter()
        .map(|(id, name, left_at)| PreviewMember {
            id: id.to_string(),
            name: name.clone(),
            left_at_micros: left_at.map(|value| value.timestamp_micros()),
        })
        .collect::<Vec<_>>();
    let member_index = member_rows
        .iter()
        .enumerate()
        .map(|(index, row)| (row.0, index))
        .collect::<BTreeMap<_, _>>();

    let previous_run = runs::settlement_runs
        .filter(runs::household_id.eq(household_id))
        .filter(runs::status.eq("CLOSED"))
        .filter(runs::period_start.lt(target_start))
        .order((runs::period_start.desc(), runs::revision.desc()))
        .select((runs::id, runs::period_start))
        .first::<(Uuid, NaiveDate)>(conn)
        .await
        .optional()?;
    let mut previous_due_by_member = BTreeMap::<String, i64>::new();
    let previous_period = if let Some((run_id, closed_period)) = previous_run {
        let line_rows = lines::settlement_run_lines
            .filter(lines::household_id.eq(household_id))
            .filter(lines::settlement_run_id.eq(run_id))
            .select((lines::member_id, lines::amount_due_yen))
            .load::<(Uuid, i64)>(conn)
            .await?;
        previous_due_by_member.extend(
            line_rows
                .into_iter()
                .map(|(member_id, due)| (member_id.to_string(), due)),
        );
        Some(closed_period)
    } else {
        None
    };

    let earliest = earliest_period(conn, household_id, target_start, policy).await?;
    let first_period = previous_period
        .map(first_of_next_month)
        .or(earliest)
        .unwrap_or(target_start)
        .min(target_start);

    let mut result = Vec::new();
    let mut month = first_period;
    while month <= target_start {
        let month_end = first_of_next_month(month);
        let period = format!("{:04}-{:02}", month.year(), month.month());
        let (obligations, schedules, adjustments, purchases, funding_rules) =
            load_month(conn, household_id, month, month_end, &member_index, policy).await?;
        let input = PreviewInput {
            period: period.clone(),
            members: members_data.clone(),
            obligations,
            schedules,
            adjustments,
            purchase_adjustments: Vec::<PreviewPurchaseAdjustment>::new(),
            purchases,
            funding_rules,
        };
        result = calculate_settlement_preview(input, &previous_due_by_member)?;
        previous_due_by_member = result
            .iter()
            .map(|line| (line.member_id.clone(), line.amount_due_yen))
            .collect();
        if month == target_start {
            break;
        }
        month = month_end;
    }
    Ok(result)
}

async fn earliest_period(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    target_start: NaiveDate,
    policy: &mut impl PolicySettlementData,
) -> Result<Option<NaiveDate>, SettlementPreviewPersistenceError> {
    use public::funding_obligations::dsl as obligations;
    use public::purchases::dsl as purchases;

    let obligation_period = obligations::funding_obligations
        .filter(obligations::household_id.eq(household_id))
        .select(min(obligations::period_start))
        .first::<Option<NaiveDate>>(conn)
        .await?;
    let schedule_start = policy.earliest_schedule_start(conn, household_id).await?;
    let purchase_start = purchases::purchases
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::status.eq("POSTED"))
        .select(diesel::dsl::min(purchases::occurred_at))
        .first::<Option<DateTime<Utc>>>(conn)
        .await?
        .map(accounting_period_start);
    let _ = target_start;
    Ok([obligation_period, schedule_start, purchase_start]
        .into_iter()
        .flatten()
        .min())
}

async fn load_month(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    start: NaiveDate,
    end: NaiveDate,
    member_index: &BTreeMap<Uuid, usize>,
    policy: &mut impl PolicySettlementData,
) -> Result<
    (
        Vec<PreviewObligation>,
        Vec<PreviewSchedule>,
        Vec<PreviewAdjustment>,
        Vec<PreviewPurchase>,
        Vec<PreviewFundingRule>,
    ),
    SettlementPreviewPersistenceError,
> {
    use public::funding_obligations::dsl as obligations;
    use public::settlement_adjustments::dsl as adjustments;

    let obligation_rows = obligations::funding_obligations
        .filter(obligations::household_id.eq(household_id))
        .filter(obligations::period_start.eq(start))
        .order((obligations::member_id, obligations::pool_id))
        .select((
            obligations::member_id,
            obligations::pool_id,
            obligations::amount_yen,
            obligations::paid_yen,
        ))
        .load::<(Uuid, Uuid, i64, i64)>(conn)
        .await?;
    let obligation_data = obligation_rows
        .into_iter()
        .map(|(member, pool, scheduled, paid)| {
            let member_id = member_index
                .get(&member)
                .copied()
                .ok_or(SettlementPreviewPersistenceError::InvalidFundingSnapshot)?;
            Ok(PreviewObligation {
                member_id,
                pool_id: pool.to_string(),
                scheduled_yen: scheduled,
                paid_yen: paid,
            })
        })
        .collect::<Result<Vec<_>, SettlementPreviewPersistenceError>>()?;

    let schedule_rows = policy
        .schedules_for_period(conn, household_id, start, end)
        .await?;
    let schedule_data = schedule_rows
        .into_iter()
        .map(
            |(id, member, pool, amount, cadence, starts, ends, active, created)| {
                let member_id = member_index
                    .get(&member)
                    .copied()
                    .ok_or(SettlementPreviewPersistenceError::InvalidFundingSnapshot)?;
                let cadence = match cadence.as_str() {
                    "MONTHLY" => PreviewScheduleCadence::Monthly,
                    "WEEKLY" => PreviewScheduleCadence::Weekly,
                    "CUSTOM" => PreviewScheduleCadence::Custom,
                    _ => return Err(SettlementPreviewPersistenceError::InvalidFundingSnapshot),
                };
                Ok(PreviewSchedule {
                    member_id,
                    pool_id: pool.to_string(),
                    amount_yen: amount,
                    cadence,
                    starts_on_day: starts.num_days_from_ce() - 719_163,
                    ends_on_day: ends.map(|date| date.num_days_from_ce() - 719_163),
                    active,
                    created_at_micros: created.timestamp_micros(),
                    id: id.to_string(),
                })
            },
        )
        .collect::<Result<Vec<_>, SettlementPreviewPersistenceError>>()?;

    let adjustment_rows = adjustments::settlement_adjustments
        .filter(adjustments::household_id.eq(household_id))
        .filter(adjustments::period_start.eq(start))
        .order(adjustments::member_id)
        .select((adjustments::member_id, adjustments::amount_yen))
        .load::<(Uuid, i64)>(conn)
        .await?;
    let adjustment_data = adjustment_rows
        .into_iter()
        .map(|(member, amount_yen)| {
            let member_id = member_index
                .get(&member)
                .copied()
                .ok_or(SettlementPreviewPersistenceError::InvalidFundingSnapshot)?;
            Ok(PreviewAdjustment {
                member_id,
                amount_yen,
            })
        })
        .collect::<Result<Vec<_>, SettlementPreviewPersistenceError>>()?;

    let (purchase_data, funding_rules) =
        load_purchase_expenses(conn, household_id, start, end, policy).await?;
    Ok((
        obligation_data,
        schedule_data,
        adjustment_data,
        purchase_data,
        funding_rules,
    ))
}

async fn load_purchase_expenses(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    start: NaiveDate,
    end: NaiveDate,
    policy: &mut impl PolicySettlementData,
) -> Result<(Vec<PreviewPurchase>, Vec<PreviewFundingRule>), SettlementPreviewPersistenceError> {
    use public::purchase_items::dsl as items;
    use public::purchase_member_burdens::dsl as burdens;
    use public::purchase_payment_funding_snapshots::dsl as snapshots;
    use public::purchase_payments::dsl as payments;
    use public::purchases::dsl as purchases;

    let (from, until) = timestamp_bounds(start, end);
    let purchase_rows = purchases::purchases
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::status.eq("POSTED"))
        .filter(purchases::occurred_at.ge(from))
        .filter(purchases::occurred_at.lt(until))
        .order(purchases::id)
        .select(purchases::id)
        .load::<Uuid>(conn)
        .await?;
    if purchase_rows.is_empty() {
        return Ok((Vec::new(), Vec::new()));
    }

    let item_rows = items::purchase_items
        .filter(items::household_id.eq(household_id))
        .filter(items::purchase_id.eq_any(&purchase_rows))
        .order((items::purchase_id, items::id))
        .select((items::purchase_id, items::id, items::amount_yen))
        .load::<(Uuid, Uuid, i64)>(conn)
        .await?;
    let payment_rows = payments::purchase_payments
        .filter(payments::household_id.eq(household_id))
        .filter(payments::purchase_id.eq_any(&purchase_rows))
        .order((payments::purchase_id, payments::line_no))
        .select((
            payments::purchase_id,
            payments::line_no,
            payments::amount_yen,
        ))
        .load::<(Uuid, i32, i64)>(conn)
        .await?;
    let snapshot_rows = snapshots::purchase_payment_funding_snapshots
        .filter(snapshots::household_id.eq(household_id))
        .filter(snapshots::purchase_id.eq_any(&purchase_rows))
        .order((
            snapshots::purchase_id,
            snapshots::payment_line_no,
            snapshots::member_id,
        ))
        .select((
            snapshots::purchase_id,
            snapshots::payment_line_no,
            snapshots::pool_id,
            snapshots::member_id,
            snapshots::funding_rule_version,
            snapshots::funding_weight,
            snapshots::settlement_policy,
            snapshots::snapshot_source,
        ))
        .load::<(Uuid, i32, Uuid, Uuid, i32, i64, Option<String>, String)>(conn)
        .await?;
    let burden_rows = burdens::purchase_member_burdens
        .filter(burdens::household_id.eq(household_id))
        .filter(burdens::purchase_id.eq_any(&purchase_rows))
        .order((
            burdens::purchase_id,
            burdens::item_id,
            burdens::pool_id,
            burdens::member_id,
        ))
        .select((
            burdens::purchase_id,
            burdens::item_id,
            burdens::pool_id,
            burdens::member_id,
            burdens::amount_yen,
            burdens::funding_rule_version,
            burdens::funding_weight,
        ))
        .load::<(Uuid, Uuid, Uuid, Uuid, i64, i32, i64)>(conn)
        .await?;

    let (rule_rows, share_rows) = policy.rules_and_shares(conn, household_id).await?;
    let historical_rules = rule_rows
        .into_iter()
        .map(|(pool, version, owner, _)| ((pool, version), owner))
        .collect::<BTreeMap<_, _>>();
    let historical_shares = share_rows
        .into_iter()
        .map(|(pool, version, member, weight)| ((pool, version, member), weight))
        .collect::<BTreeMap<_, _>>();
    let mut snapshots_by_payment = BTreeMap::<(Uuid, i32), Vec<_>>::new();
    for row in snapshot_rows {
        snapshots_by_payment
            .entry((row.0, row.1))
            .or_default()
            .push(row);
    }
    let mut burdens_by_item = BTreeMap::<(Uuid, Uuid), Vec<_>>::new();
    for row in burden_rows {
        burdens_by_item.entry((row.0, row.1)).or_default().push(row);
    }
    let mut items_by_purchase = BTreeMap::<Uuid, Vec<_>>::new();
    for (purchase_id, item_id, amount_yen) in item_rows {
        items_by_purchase
            .entry(purchase_id)
            .or_default()
            .push((item_id, amount_yen));
    }
    let mut payments_by_purchase = BTreeMap::<Uuid, Vec<_>>::new();
    for (purchase_id, line_no, amount_yen) in payment_rows {
        payments_by_purchase
            .entry(purchase_id)
            .or_default()
            .push((line_no, amount_yen));
    }

    let mut rules = BTreeMap::<String, PreviewFundingRule>::new();
    let mut result = Vec::new();
    for purchase_id in purchase_rows {
        let purchase_items = items_by_purchase
            .get(&purchase_id)
            .cloned()
            .unwrap_or_default();
        if purchase_items.is_empty() {
            continue;
        }
        let purchase_payments = payments_by_purchase
            .get(&purchase_id)
            .cloned()
            .unwrap_or_default();
        if purchase_payments.is_empty() {
            return Err(SettlementPreviewPersistenceError::InvalidFundingSnapshot);
        }
        let mut preview_payments = Vec::new();
        for (line_no, amount_yen) in purchase_payments {
            let rows = snapshots_by_payment
                .remove(&(purchase_id, line_no))
                .unwrap_or_default();
            let Some(first) = rows.as_slice().first() else {
                return Err(SettlementPreviewPersistenceError::InvalidFundingSnapshot);
            };
            if rows.iter().any(|row| {
                row.2 != first.2 || row.4 != first.4 || row.6 != first.6 || row.7 != first.7
            }) {
                return Err(SettlementPreviewPersistenceError::InvalidFundingSnapshot);
            }
            let alias = format!("{purchase_id}:payment:{line_no}");
            let (kind, owner_member_id, policy, shares) = if let Some(policy) = &first.6 {
                let policy = match policy.as_str() {
                    "BALANCE_TO_STANDARD" => SettlementPolicy::BalanceToStandard,
                    "REIMBURSE_PERSONAL" => SettlementPolicy::ReimbursePersonal,
                    _ => {
                        return Err(SettlementPreviewPersistenceError::InvalidFundingSnapshot);
                    }
                };
                (
                    PreviewFundingRuleKind::Shared,
                    None,
                    Some(policy),
                    rows.iter().map(|row| (row.3.to_string(), row.5)).collect(),
                )
            } else {
                if rows.len() != 1 || first.5 != 1 {
                    return Err(SettlementPreviewPersistenceError::InvalidFundingSnapshot);
                }
                (
                    PreviewFundingRuleKind::Personal,
                    Some(first.3.to_string()),
                    None,
                    Vec::new(),
                )
            };
            rules.insert(
                alias.clone(),
                PreviewFundingRule {
                    id: alias.clone(),
                    kind,
                    owner_member_id,
                    settlement_policy: policy,
                    shares,
                },
            );
            preview_payments.push(PreviewPurchasePayment {
                payment_rule_id: alias,
                amount_yen,
            });
        }
        if snapshots_by_payment
            .keys()
            .any(|(snapshot_purchase_id, _)| *snapshot_purchase_id == purchase_id)
        {
            return Err(SettlementPreviewPersistenceError::InvalidFundingSnapshot);
        }
        let mut preview_items = Vec::new();
        for (item_id, amount_yen) in purchase_items {
            let item_burdens = burdens_by_item
                .remove(&(purchase_id, item_id))
                .unwrap_or_default();
            if item_burdens.is_empty() {
                return Err(SettlementPreviewPersistenceError::InvalidFundingSnapshot);
            }
            let mut burden_total = 0_i64;
            let mut preview_burdens = Vec::new();
            for (_, _, pool_id, member_id, burden_yen, version, weight) in item_burdens {
                let owner = historical_rules
                    .get(&(pool_id, version))
                    .ok_or(SettlementPreviewPersistenceError::InvalidFundingSnapshot)?;
                let allocation_alias =
                    format!("{purchase_id}:allocation:{item_id}:{pool_id}:{member_id}");
                let allocation_rule = if let Some(owner_id) = owner {
                    if *owner_id != member_id || weight != 1 {
                        return Err(SettlementPreviewPersistenceError::InvalidFundingSnapshot);
                    }
                    PreviewFundingRule {
                        id: allocation_alias.clone(),
                        kind: PreviewFundingRuleKind::Personal,
                        owner_member_id: Some(member_id.to_string()),
                        settlement_policy: None,
                        shares: Vec::new(),
                    }
                } else {
                    if historical_shares
                        .get(&(pool_id, version, member_id))
                        .copied()
                        != Some(weight)
                    {
                        return Err(SettlementPreviewPersistenceError::InvalidFundingSnapshot);
                    }
                    PreviewFundingRule {
                        id: allocation_alias.clone(),
                        kind: PreviewFundingRuleKind::Shared,
                        owner_member_id: None,
                        settlement_policy: None,
                        shares: vec![(member_id.to_string(), 1)],
                    }
                };
                rules.insert(allocation_alias.clone(), allocation_rule);
                burden_total = burden_total
                    .checked_add(burden_yen)
                    .ok_or(SettlementPreviewPersistenceError::InvalidFundingSnapshot)?;
                preview_burdens.push((allocation_alias, member_id.to_string(), burden_yen));
            }
            if burden_total != amount_yen {
                return Err(SettlementPreviewPersistenceError::InvalidFundingSnapshot);
            }
            preview_items.push(PreviewPurchaseItem {
                amount_yen,
                burdens: preview_burdens,
            });
        }
        result.push(PreviewPurchase {
            payments: preview_payments,
            items: preview_items,
        });
    }
    Ok((result, rules.into_values().collect()))
}

fn timestamp_bounds(start: NaiveDate, end: NaiveDate) -> (DateTime<Utc>, DateTime<Utc>) {
    let offset = FixedOffset::east_opt(9 * 60 * 60).expect("JST offset is valid");
    let from = offset
        .from_local_datetime(&start.and_hms_opt(0, 0, 0).expect("midnight is valid"))
        .single()
        .expect("fixed offset is unambiguous")
        .with_timezone(&Utc);
    let until = offset
        .from_local_datetime(&end.and_hms_opt(0, 0, 0).expect("midnight is valid"))
        .single()
        .expect("fixed offset is unambiguous")
        .with_timezone(&Utc);
    (from, until)
}

fn period_bounds(
    period: &str,
) -> Result<(NaiveDate, NaiveDate), SettlementPreviewPersistenceError> {
    let (year, month) = period
        .split_once('-')
        .ok_or(SettlementPreviewPersistenceError::InvalidPeriod)?;
    if year.len() != 4 || month.len() != 2 {
        return Err(SettlementPreviewPersistenceError::InvalidPeriod);
    }
    let year = year
        .parse::<i32>()
        .map_err(|_| SettlementPreviewPersistenceError::InvalidPeriod)?;
    let month = month
        .parse::<u32>()
        .map_err(|_| SettlementPreviewPersistenceError::InvalidPeriod)?;
    if !(1..=12).contains(&month) {
        return Err(SettlementPreviewPersistenceError::InvalidPeriod);
    }
    let start = NaiveDate::from_ymd_opt(year, month, 1)
        .ok_or(SettlementPreviewPersistenceError::InvalidPeriod)?;
    Ok((start, first_of_next_month(start)))
}

fn first_of_next_month(date: NaiveDate) -> NaiveDate {
    let (year, month) = if date.month() == 12 {
        (date.year() + 1, 1)
    } else {
        (date.year(), date.month() + 1)
    };
    NaiveDate::from_ymd_opt(year, month, 1).expect("next calendar month is valid")
}

fn accounting_period_start(value: DateTime<Utc>) -> NaiveDate {
    let offset = FixedOffset::east_opt(9 * 60 * 60).expect("JST offset is valid");
    let date = value.with_timezone(&offset).date_naive();
    NaiveDate::from_ymd_opt(date.year(), date.month(), 1).expect("first day is valid")
}
