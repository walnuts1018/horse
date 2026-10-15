use std::collections::{BTreeMap, BTreeSet};

use bookkeeping_application::settlement_query::{
    GetSettlementQuery, SettlementEvidence, SettlementQueryError, SettlementQueryLine,
    SettlementQueryResult, distinct_queries, validate_queries,
};
use chrono::{DateTime, Datelike, NaiveDate, TimeZone, Utc};
use diesel::{BoolExpressionMethods, ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum SettlementQueryPersistenceError {
    Pool(String),
    Database(diesel::result::Error),
    InvalidQuery(SettlementQueryError),
    InvalidUuid,
    InvalidDate,
    NumericOutOfRange,
    Preview(String),
}

impl From<diesel::result::Error> for SettlementQueryPersistenceError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

pub async fn get_many(
    pool: &PgPool,
    queries: &[GetSettlementQuery],
) -> Result<Vec<SettlementQueryResult>, SettlementQueryPersistenceError> {
    validate_queries(queries).map_err(SettlementQueryPersistenceError::InvalidQuery)?;
    if queries.is_empty() {
        return Ok(Vec::new());
    }
    let unique = distinct_queries(queries);
    let mut household_ids = BTreeSet::new();
    let mut starts = BTreeMap::new();
    for query in &unique {
        let id = Uuid::parse_str(&query.household_id)
            .map_err(|_| SettlementQueryPersistenceError::InvalidUuid)?;
        household_ids.insert(id);
        starts.insert((id, query.period.clone()), period_start(&query.period)?);
    }
    let ids = household_ids.into_iter().collect::<Vec<_>>();
    let min_start = starts
        .values()
        .copied()
        .min()
        .ok_or(SettlementQueryPersistenceError::InvalidDate)?;
    let max_end = starts
        .values()
        .copied()
        .map(next_month)
        .collect::<Result<Vec<_>, _>>()?
        .into_iter()
        .max()
        .ok_or(SettlementQueryPersistenceError::InvalidDate)?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SettlementQueryPersistenceError::Pool(error.to_string()))?;
    pooled
        .build_transaction()
        .repeatable_read()
        .read_only()
        .run(async |conn| {
            use public::settlement_run_lines::dsl as lines;
            use public::settlement_runs::dsl as runs;

            let run_rows = runs::settlement_runs
                .filter(runs::household_id.eq_any(&ids))
                .filter(runs::period_start.ge(min_start))
                .filter(runs::period_start.lt(max_end))
                .filter(runs::status.eq("CLOSED"))
                .order((
                    runs::household_id,
                    runs::period_start,
                    runs::revision.desc(),
                ))
                .select((
                    runs::household_id,
                    runs::period_start,
                    runs::id,
                    runs::revision,
                    runs::created_at,
                ))
                .load::<(Uuid, NaiveDate, Uuid, i64, DateTime<Utc>)>(conn)
                .await?;
            let mut closed_by_key = BTreeMap::new();
            for (household, date, run_id, revision, created_at) in run_rows {
                closed_by_key
                    .entry((household, period_string(date)))
                    .or_insert((run_id, revision, created_at));
            }
            let run_ids = closed_by_key
                .values()
                .map(|value| value.0)
                .collect::<Vec<_>>();
            let closed_lines = if run_ids.is_empty() {
                Vec::new()
            } else {
                lines::settlement_run_lines
                    .filter(lines::household_id.eq_any(&ids))
                    .filter(lines::settlement_run_id.eq_any(&run_ids))
                    .order((lines::member_name, lines::member_id))
                    .select((
                        lines::settlement_run_id,
                        lines::member_id,
                        lines::member_name,
                        lines::scheduled_yen,
                        lines::paid_yen,
                        lines::carry_yen,
                        lines::monthly_adjustment_yen,
                        lines::advance_credit_yen,
                        lines::amount_due_yen,
                    ))
                    .load::<(Uuid, Uuid, String, i64, i64, i64, i64, i64, i64)>(conn)
                    .await?
            };
            let mut lines_by_run = BTreeMap::<Uuid, Vec<SettlementQueryLine>>::new();
            for (
                run_id,
                member_id,
                member_name,
                scheduled_yen,
                paid_yen,
                carry_yen,
                monthly_adjustment_yen,
                advance_credit_yen,
                amount_due_yen,
            ) in closed_lines
            {
                lines_by_run
                    .entry(run_id)
                    .or_default()
                    .push(SettlementQueryLine {
                        member_id: member_id.to_string(),
                        member_name,
                        scheduled_yen,
                        paid_yen,
                        carry_yen,
                        monthly_adjustment_yen,
                        advance_credit_yen,
                        amount_due_yen,
                    });
            }

            let evidence = load_evidence(conn, &ids, &starts, min_start, max_end).await?;
            let mut unique_results = BTreeMap::new();
            for query in &unique {
                let household = Uuid::parse_str(&query.household_id)
                    .map_err(|_| SettlementQueryPersistenceError::InvalidUuid)?;
                let key = (household, query.period.clone());
                let result = if let Some((run_id, revision, created_at)) = closed_by_key.get(&key) {
                    SettlementQueryResult {
                        household_id: query.household_id.clone(),
                        period: query.period.clone(),
                        status: "CLOSED".into(),
                        revision: *revision,
                        created_at_micros: Some(created_at.timestamp_micros()),
                        lines: lines_by_run.remove(run_id).unwrap_or_default(),
                        evidence: evidence.get(&key).cloned().unwrap_or_default(),
                    }
                } else {
                    let preview = super::settlement_preview::calculate_preview_with_connection(
                        conn,
                        &query.household_id,
                        &query.period,
                    )
                    .await
                    .map_err(|error| {
                        SettlementQueryPersistenceError::Preview(format!("{error:?}"))
                    })?;
                    SettlementQueryResult {
                        household_id: query.household_id.clone(),
                        period: query.period.clone(),
                        status: "OPEN".into(),
                        revision: 0,
                        created_at_micros: None,
                        lines: preview
                            .into_iter()
                            .map(|line| SettlementQueryLine {
                                member_id: line.member_id,
                                member_name: line.member_name,
                                scheduled_yen: line.scheduled_yen,
                                paid_yen: line.paid_yen,
                                carry_yen: line.carry_yen,
                                monthly_adjustment_yen: line.monthly_adjustment_yen,
                                advance_credit_yen: line.advance_credit_yen,
                                amount_due_yen: line.amount_due_yen,
                            })
                            .collect(),
                        evidence: evidence.get(&key).cloned().unwrap_or_default(),
                    }
                };
                unique_results.insert(key, result);
            }
            queries
                .iter()
                .map(|query| {
                    let household = Uuid::parse_str(&query.household_id)
                        .map_err(|_| SettlementQueryPersistenceError::InvalidUuid)?;
                    unique_results
                        .get(&(household, query.period.clone()))
                        .cloned()
                        .ok_or(SettlementQueryPersistenceError::InvalidDate)
                })
                .collect()
        })
        .await
}

async fn load_evidence(
    conn: &mut diesel_async::AsyncPgConnection,
    households: &[Uuid],
    starts: &BTreeMap<(Uuid, String), NaiveDate>,
    min_start: NaiveDate,
    max_end: NaiveDate,
) -> Result<BTreeMap<(Uuid, String), Vec<SettlementEvidence>>, SettlementQueryPersistenceError> {
    use public::accounting_periods::dsl as accounting_periods;
    use public::funding_obligations::dsl as obligations;
    use public::funding_schedules::dsl as schedules;
    use public::ledger_entries::dsl as ledger;
    use public::purchase_items::dsl as items;
    use public::purchase_member_burdens::dsl as burdens;
    use public::purchases::dsl as purchases;
    use public::settlement_adjustments::dsl as adjustments;
    use public::transfers::dsl as transfers;

    let mut result = BTreeMap::<(Uuid, String), Vec<SettlementEvidence>>::new();
    let periods = starts
        .keys()
        .map(|(_, period)| period.clone())
        .collect::<BTreeSet<_>>()
        .into_iter()
        .collect::<Vec<_>>();
    let obligation_rows = obligations::funding_obligations
        .filter(obligations::household_id.eq_any(households))
        .filter(obligations::period_start.ge(min_start))
        .filter(obligations::period_start.lt(max_end))
        .select((
            obligations::household_id,
            obligations::period_start,
            obligations::member_id,
            obligations::pool_id,
            obligations::id,
            obligations::amount_yen,
            obligations::paid_yen,
        ))
        .load::<(Uuid, NaiveDate, Uuid, Uuid, Uuid, i64, i64)>(conn)
        .await?;
    let closed_rows = accounting_periods::accounting_periods
        .filter(accounting_periods::household_id.eq_any(households))
        .filter(accounting_periods::period_start.ge(min_start))
        .filter(accounting_periods::period_start.lt(max_end))
        .select((
            accounting_periods::household_id,
            accounting_periods::period_start,
            accounting_periods::status,
        ))
        .load::<(Uuid, NaiveDate, String)>(conn)
        .await?;
    let closed_keys = closed_rows
        .into_iter()
        .filter(|row| row.2 == "CLOSED")
        .map(|row| (row.0, period_string(row.1)))
        .collect::<BTreeSet<_>>();
    let schedule_rows = schedules::funding_schedules
        .filter(schedules::household_id.eq_any(households))
        .filter(schedules::starts_on.lt(max_end))
        .filter(
            schedules::ends_on
                .is_null()
                .or(schedules::ends_on.ge(min_start)),
        )
        .filter(
            schedules::active
                .eq(true)
                .or(schedules::ends_on.is_not_null()),
        )
        .select((
            schedules::household_id,
            schedules::id,
            schedules::member_id,
            schedules::pool_id,
            schedules::amount_yen,
            schedules::cadence,
            schedules::starts_on,
            schedules::ends_on,
            schedules::active,
        ))
        .load::<(
            Uuid,
            Uuid,
            Uuid,
            Uuid,
            i64,
            String,
            NaiveDate,
            Option<NaiveDate>,
            bool,
        )>(conn)
        .await?;
    let mut schedule_totals = BTreeMap::<((Uuid, String), Uuid, Uuid), i64>::new();
    for ((household, period), start) in starts {
        if closed_keys.contains(&(*household, period.clone())) {
            continue;
        }
        let end = next_month(*start)?;
        for (
            schedule_household,
            id,
            member,
            pool,
            amount,
            cadence,
            schedule_start,
            schedule_end,
            active,
        ) in &schedule_rows
        {
            if *schedule_household != *household
                || *schedule_start >= end
                || schedule_end.is_some_and(|date| date < *start)
                || (!*active && schedule_end.is_none())
            {
                continue;
            }
            let yen = schedule_amount(
                *start,
                end,
                *amount,
                cadence,
                *schedule_start,
                *schedule_end,
            )?;
            if yen > 0 {
                let key = (*household, period.clone());
                let total = schedule_totals
                    .entry((key.clone(), *member, *pool))
                    .or_default();
                *total = total
                    .checked_add(yen)
                    .ok_or(SettlementQueryPersistenceError::NumericOutOfRange)?;
                push_evidence(
                    &mut result,
                    key,
                    SettlementEvidence {
                        member_id: member.to_string(),
                        kind: "SCHEDULED".into(),
                        label: "入金予定".into(),
                        amount_yen: yen,
                        source_id: id.to_string(),
                        purchase_id: None,
                        occurred_at_micros: None,
                    },
                );
            }
        }
    }
    for (household, date, member, pool, id, amount, _) in obligation_rows {
        let key = (household, period_string(date));
        let scheduled = schedule_totals
            .get(&(key.clone(), member, pool))
            .copied()
            .unwrap_or_default();
        let evidence_amount = if closed_keys.contains(&key) {
            amount
        } else {
            amount.saturating_sub(scheduled)
        };
        if evidence_amount > 0 {
            push_evidence(
                &mut result,
                key,
                SettlementEvidence {
                    member_id: member.to_string(),
                    kind: "SCHEDULED".into(),
                    label: "確定済み入金予定".into(),
                    amount_yen: evidence_amount,
                    source_id: id.to_string(),
                    purchase_id: None,
                    occurred_at_micros: None,
                },
            );
        }
    }
    let adjustment_rows = adjustments::settlement_adjustments
        .filter(adjustments::household_id.eq_any(households))
        .filter(adjustments::period_start.ge(min_start))
        .filter(adjustments::period_start.lt(max_end))
        .select((
            adjustments::household_id,
            adjustments::period_start,
            adjustments::member_id,
            adjustments::source_kind,
            adjustments::source_id,
            adjustments::amount_yen,
            adjustments::created_at,
        ))
        .load::<(Uuid, NaiveDate, Uuid, String, Uuid, i64, DateTime<Utc>)>(conn)
        .await?;
    for (household, date, member, kind, source, amount, created) in adjustment_rows {
        push_evidence(
            &mut result,
            (household, period_string(date)),
            SettlementEvidence {
                member_id: member.to_string(),
                kind: "ADJUSTMENT".into(),
                label: kind,
                amount_yen: amount,
                source_id: source.to_string(),
                purchase_id: None,
                occurred_at_micros: Some(created.timestamp_micros()),
            },
        );
    }
    let ledger_rows = ledger::ledger_entries
        .filter(ledger::household_id.eq_any(households))
        .filter(ledger::occurred_at.ge(utc_start(min_start)?))
        .filter(ledger::occurred_at.lt(utc_start(max_end)?))
        .filter(ledger::source_kind.eq_any(vec!["FUNDING", "transfer"]))
        .select((
            ledger::id,
            ledger::journal_id,
            ledger::household_id,
            ledger::source_kind,
            ledger::occurred_at,
            ledger::description,
            ledger::amount_yen,
            ledger::from_member_id,
        ))
        .load::<(
            Uuid,
            Uuid,
            Uuid,
            String,
            DateTime<Utc>,
            String,
            i64,
            Option<Uuid>,
        )>(conn)
        .await?;
    let transfer_entries = ledger_rows
        .iter()
        .filter(|row| row.3 == "transfer")
        .map(|row| row.1)
        .collect::<Vec<_>>();
    for (id, _, household, kind, occurred, description, amount, member) in &ledger_rows {
        if kind == "FUNDING"
            && let Some(member) = member
        {
            for ((target, period), start) in starts {
                if *target == *household && accounting_period_start(*occurred) == *start {
                    push_evidence(
                        &mut result,
                        (*target, period.clone()),
                        SettlementEvidence {
                            member_id: member.to_string(),
                            kind: "PAID".into(),
                            label: description.clone(),
                            amount_yen: *amount,
                            source_id: id.to_string(),
                            purchase_id: None,
                            occurred_at_micros: Some(occurred.timestamp_micros()),
                        },
                    );
                }
            }
        }
    }
    if !transfer_entries.is_empty() {
        let rows = transfers::transfers
            .filter(transfers::journal_id.eq_any(&transfer_entries))
            .filter(transfers::kind.eq("ADVANCE"))
            .select((
                transfers::household_id,
                transfers::journal_id,
                transfers::id,
                transfers::to_member_id,
                transfers::amount_yen,
                transfers::description,
            ))
            .load::<(Uuid, Uuid, Uuid, Uuid, i64, String)>(conn)
            .await?;
        for (household, journal, id, member, amount, description) in rows {
            if let Some((_, _, _, _, occurred, ..)) =
                ledger_rows.iter().find(|row| row.1 == journal)
            {
                for ((target, period), start) in starts {
                    if *target == household && accounting_period_start(*occurred) == *start {
                        push_evidence(
                            &mut result,
                            (*target, period.clone()),
                            SettlementEvidence {
                                member_id: member.to_string(),
                                kind: "ADVANCE".into(),
                                label: description.clone(),
                                amount_yen: amount,
                                source_id: id.to_string(),
                                purchase_id: None,
                                occurred_at_micros: Some(occurred.timestamp_micros()),
                            },
                        );
                    }
                }
            }
        }
    }
    let purchase_rows = purchases::purchases
        .filter(purchases::household_id.eq_any(households))
        .filter(purchases::status.eq("POSTED"))
        .filter(purchases::occurred_at.ge(utc_start(min_start)?))
        .filter(purchases::occurred_at.lt(utc_start(max_end)?))
        .select((
            purchases::id,
            purchases::household_id,
            purchases::occurred_at,
            purchases::merchant,
        ))
        .load::<(Uuid, Uuid, DateTime<Utc>, String)>(conn)
        .await?;
    let purchase_ids = purchase_rows.iter().map(|row| row.0).collect::<Vec<_>>();
    if !purchase_ids.is_empty() {
        let item_rows = items::purchase_items
            .filter(items::purchase_id.eq_any(&purchase_ids))
            .select((items::id, items::purchase_id, items::name))
            .load::<(Uuid, Uuid, String)>(conn)
            .await?;
        let item_map = item_rows
            .iter()
            .map(|row| (row.0, row))
            .collect::<BTreeMap<_, _>>();
        let burden_rows = burdens::purchase_member_burdens
            .filter(burdens::purchase_id.eq_any(&purchase_ids))
            .select((
                burdens::purchase_id,
                burdens::item_id,
                burdens::member_id,
                burdens::amount_yen,
            ))
            .load::<(Uuid, Uuid, Uuid, i64)>(conn)
            .await?;
        for (purchase_id, household, occurred, merchant) in purchase_rows {
            let period = period_string(accounting_period_start(occurred));
            let key = (household, period);
            if !starts.contains_key(&key) {
                continue;
            }
            let mut totals = BTreeMap::<(Uuid, Uuid), i64>::new();
            for (purchase, item, member, amount) in &burden_rows {
                if *purchase == purchase_id {
                    let total = totals.entry((*item, *member)).or_default();
                    *total = total
                        .checked_add(*amount)
                        .ok_or(SettlementQueryPersistenceError::NumericOutOfRange)?;
                }
            }
            for ((item_id, member), amount) in totals {
                if let Some(item) = item_map.get(&item_id) {
                    push_evidence(
                        &mut result,
                        key.clone(),
                        SettlementEvidence {
                            member_id: member.to_string(),
                            kind: "PURCHASE".into(),
                            label: format!("{merchant}・{}", item.2),
                            amount_yen: amount,
                            source_id: item_id.to_string(),
                            purchase_id: Some(purchase_id.to_string()),
                            occurred_at_micros: Some(occurred.timestamp_micros()),
                        },
                    );
                }
            }
        }
    }
    for values in result.values_mut() {
        values.sort_by(|left, right| {
            (
                &left.member_id,
                &left.kind,
                left.occurred_at_micros,
                &left.source_id,
            )
                .cmp(&(
                    &right.member_id,
                    &right.kind,
                    right.occurred_at_micros,
                    &right.source_id,
                ))
        });
    }
    let _ = periods;
    Ok(result)
}

fn push_evidence(
    result: &mut BTreeMap<(Uuid, String), Vec<SettlementEvidence>>,
    key: (Uuid, String),
    item: SettlementEvidence,
) {
    result.entry(key).or_default().push(item);
}

fn period_start(period: &str) -> Result<NaiveDate, SettlementQueryPersistenceError> {
    let (year, month) = period
        .split_once('-')
        .ok_or(SettlementQueryPersistenceError::InvalidDate)?;
    NaiveDate::from_ymd_opt(
        year.parse()
            .map_err(|_| SettlementQueryPersistenceError::InvalidDate)?,
        month
            .parse()
            .map_err(|_| SettlementQueryPersistenceError::InvalidDate)?,
        1,
    )
    .ok_or(SettlementQueryPersistenceError::InvalidDate)
}

fn period_string(date: NaiveDate) -> String {
    format!("{:04}-{:02}", date.year(), date.month())
}
fn next_month(date: NaiveDate) -> Result<NaiveDate, SettlementQueryPersistenceError> {
    if date.month() == 12 {
        date.with_year(date.year() + 1)
            .and_then(|date| date.with_month(1))
    } else {
        date.with_month(date.month() + 1)
    }
    .ok_or(SettlementQueryPersistenceError::InvalidDate)
}
fn utc_start(date: NaiveDate) -> Result<DateTime<Utc>, SettlementQueryPersistenceError> {
    Ok(Utc
        .from_local_datetime(
            &date
                .and_hms_opt(0, 0, 0)
                .ok_or(SettlementQueryPersistenceError::InvalidDate)?,
        )
        .single()
        .ok_or(SettlementQueryPersistenceError::InvalidDate)?
        - chrono::Duration::hours(9))
}
fn accounting_period_start(instant: DateTime<Utc>) -> NaiveDate {
    (instant + chrono::Duration::hours(9))
        .date_naive()
        .with_day(1)
        .expect("first day of a month exists")
}

fn schedule_amount(
    start: NaiveDate,
    end: NaiveDate,
    amount: i64,
    cadence: &str,
    schedule_start: NaiveDate,
    schedule_end: Option<NaiveDate>,
) -> Result<i64, SettlementQueryPersistenceError> {
    let schedule_end = schedule_end.unwrap_or(NaiveDate::MAX);
    match cadence {
        "MONTHLY" if schedule_start < end && schedule_end >= start => Ok(amount),
        "MONTHLY" | "CUSTOM" => Ok(
            if cadence == "CUSTOM"
                && schedule_start >= start
                && schedule_start < end
                && schedule_end >= schedule_start
            {
                amount
            } else {
                0
            },
        ),
        "WEEKLY" => {
            let day_delta = (start - schedule_start).num_days().max(0);
            let weeks = day_delta / 7 + if day_delta % 7 == 0 { 0 } else { 1 };
            let mut occurrence = schedule_start + chrono::Duration::days(weeks * 7);
            let mut total = 0_i64;
            while occurrence < end && occurrence <= schedule_end {
                total = total
                    .checked_add(amount)
                    .ok_or(SettlementQueryPersistenceError::InvalidDate)?;
                occurrence += chrono::Duration::days(7);
            }
            Ok(total)
        }
        _ => Err(SettlementQueryPersistenceError::InvalidDate),
    }
}

#[cfg(test)]
mod tests {
    use super::accounting_period_start;
    use chrono::{NaiveDate, TimeZone, Utc};

    #[test]
    fn evidence_period_uses_japan_month_boundary() {
        let before = Utc
            .with_ymd_and_hms(2026, 9, 30, 14, 59, 59)
            .single()
            .unwrap();
        let boundary = Utc
            .with_ymd_and_hms(2026, 9, 30, 15, 0, 0)
            .single()
            .unwrap();

        assert_eq!(
            accounting_period_start(before),
            NaiveDate::from_ymd_opt(2026, 9, 1).unwrap()
        );
        assert_eq!(
            accounting_period_start(boundary),
            NaiveDate::from_ymd_opt(2026, 10, 1).unwrap()
        );
    }
}
