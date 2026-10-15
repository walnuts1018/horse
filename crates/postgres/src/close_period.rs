use std::collections::BTreeMap;

use bookkeeping_workflows::close_period::ClosePeriodDecision;
use chrono::{DateTime, FixedOffset, NaiveDate, TimeZone, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedClosePeriod {
    pub settlement_run_id: String,
    pub period: String,
    pub revision: u64,
}

#[derive(Debug)]
pub enum ClosePeriodPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidPeriod,
    NumericOutOfRange(&'static str),
    InvalidDecision,
    PeriodNotFound,
    PeriodAlreadyClosed,
    StalePeriod,
    DraftPurchasesExist,
    StalePurchase,
}

impl From<DieselError> for ClosePeriodPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_close_period(
    pool: &PgPool,
    household_id: &str,
    decision: ClosePeriodDecision,
    funding_snapshot: serde_json::Value,
) -> Result<PersistedClosePeriod, ClosePeriodPersistenceError> {
    let household_id = parse_uuid(household_id, "household_id")?;
    let actor_id = parse_uuid(decision.event.actor_id.as_str(), "actor_id")?;
    let (period_start, period_end) = period_bounds(&decision.period)?;
    let revision = i64::try_from(decision.revision)
        .map_err(|_| ClosePeriodPersistenceError::NumericOutOfRange("period revision"))?;
    let expected_revision = revision
        .checked_sub(1)
        .ok_or(ClosePeriodPersistenceError::InvalidDecision)?;
    validate_decision(&decision, &decision.period, revision, actor_id)?;
    let settlement_run_id = Uuid::new_v4();

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| ClosePeriodPersistenceError::Pool(error.to_string()))?;
    let conn = &mut *pooled;
    conn.transaction::<PersistedClosePeriod, ClosePeriodPersistenceError, _>(async |conn| {
        use public::accounting_periods::dsl as periods;
        use public::purchases::dsl as purchases;
        use public::settlement_runs::dsl as runs;

        let period_row = periods::accounting_periods
            .filter(periods::household_id.eq(household_id))
            .filter(periods::period_start.eq(period_start))
            .for_update()
            .select((periods::status, periods::revision))
            .first::<(String, i64)>(&mut *conn)
            .await
            .optional()?;
        let Some((status, current_revision)) = period_row else {
            return Err(ClosePeriodPersistenceError::PeriodNotFound);
        };
        if status == "CLOSED" {
            return Err(ClosePeriodPersistenceError::PeriodAlreadyClosed);
        }
        if status != "OPEN" || current_revision != expected_revision {
            return Err(ClosePeriodPersistenceError::StalePeriod);
        }

        let draft_exists = purchases::purchases
            .filter(purchases::household_id.eq(household_id))
            .filter(purchases::status.eq("DRAFT"))
            .filter(purchases::occurred_at.ge(period_start_utc(period_start)?))
            .filter(purchases::occurred_at.lt(period_start_utc(period_end)?))
            .select(purchases::id)
            .first::<Uuid>(&mut *conn)
            .await
            .optional()?
            .is_some();
        if draft_exists {
            return Err(ClosePeriodPersistenceError::DraftPurchasesExist);
        }

        let posted_purchases = purchases::purchases
            .filter(purchases::household_id.eq(household_id))
            .filter(purchases::status.eq("POSTED"))
            .filter(purchases::occurred_at.ge(period_start_utc(period_start)?))
            .filter(purchases::occurred_at.lt(period_start_utc(period_end)?))
            .for_update()
            .select((purchases::id, purchases::revision))
            .load::<(Uuid, i64)>(&mut *conn)
            .await?;
        validate_purchase_revisions(&decision, posted_purchases)?;

        diesel::insert_into(runs::settlement_runs)
            .values((
                runs::id.eq(settlement_run_id),
                runs::household_id.eq(household_id),
                runs::period_start.eq(period_start),
                runs::revision.eq(revision),
                runs::status.eq("CLOSED"),
                runs::funding_snapshot.eq(funding_snapshot.clone()),
                runs::actor_id.eq(actor_id),
                runs::created_at.eq(now),
            ))
            .execute(&mut *conn)
            .await?;

        for line in &decision.settlements {
            let member_id = parse_uuid(line.member_id.as_str(), "member_id")?;
            diesel::insert_into(public::settlement_run_lines::table)
                .values((
                    public::settlement_run_lines::household_id.eq(household_id),
                    public::settlement_run_lines::settlement_run_id.eq(settlement_run_id),
                    public::settlement_run_lines::member_id.eq(member_id),
                    public::settlement_run_lines::member_name.eq(&line.member_name),
                    public::settlement_run_lines::scheduled_yen.eq(line.scheduled_yen),
                    public::settlement_run_lines::paid_yen.eq(line.paid_yen),
                    public::settlement_run_lines::carry_yen.eq(line.carry_yen),
                    public::settlement_run_lines::monthly_adjustment_yen
                        .eq(line.monthly_adjustment_yen),
                    public::settlement_run_lines::advance_credit_yen.eq(line.advance_credit_yen),
                    public::settlement_run_lines::amount_due_yen.eq(line.amount_due_yen),
                ))
                .execute(&mut *conn)
                .await?;

            if line.monthly_adjustment_yen != 0 {
                diesel::insert_into(public::settlement_run_adjustments::table)
                    .values((
                        public::settlement_run_adjustments::household_id.eq(household_id),
                        public::settlement_run_adjustments::settlement_run_id.eq(settlement_run_id),
                        public::settlement_run_adjustments::member_id.eq(member_id),
                        public::settlement_run_adjustments::adjustment_kind.eq("MONTHLY"),
                        public::settlement_run_adjustments::amount_yen
                            .eq(line.monthly_adjustment_yen),
                    ))
                    .execute(&mut *conn)
                    .await?;
            }
            if line.advance_credit_yen != 0 {
                let credit = i64::try_from(line.advance_credit_yen).map_err(|_| {
                    ClosePeriodPersistenceError::NumericOutOfRange("advance credit")
                })?;
                diesel::insert_into(public::settlement_run_adjustments::table)
                    .values((
                        public::settlement_run_adjustments::household_id.eq(household_id),
                        public::settlement_run_adjustments::settlement_run_id.eq(settlement_run_id),
                        public::settlement_run_adjustments::member_id.eq(member_id),
                        public::settlement_run_adjustments::adjustment_kind.eq("ADVANCE_CREDIT"),
                        public::settlement_run_adjustments::amount_yen.eq(credit),
                    ))
                    .execute(&mut *conn)
                    .await?;
            }
        }

        for closure in &decision.purchase_closures {
            let purchase_id = parse_uuid(closure.purchase_id.as_str(), "purchase_id")?;
            let expected = i64::try_from(closure.expected_revision)
                .map_err(|_| ClosePeriodPersistenceError::NumericOutOfRange("purchase revision"))?;
            let next = i64::try_from(closure.revision)
                .map_err(|_| ClosePeriodPersistenceError::NumericOutOfRange("purchase revision"))?;
            let updated = diesel::update(
                purchases::purchases
                    .filter(purchases::household_id.eq(household_id))
                    .filter(purchases::id.eq(purchase_id))
                    .filter(purchases::status.eq("POSTED"))
                    .filter(purchases::revision.eq(expected)),
            )
            .set((
                purchases::status.eq("CLOSED"),
                purchases::revision.eq(next),
                purchases::updated_at.eq(now),
            ))
            .execute(&mut *conn)
            .await?;
            if updated != 1 {
                return Err(ClosePeriodPersistenceError::StalePurchase);
            }
            diesel::insert_into(public::settlement_run_purchase_snapshots::table)
                .values((
                    public::settlement_run_purchase_snapshots::household_id.eq(household_id),
                    public::settlement_run_purchase_snapshots::settlement_run_id
                        .eq(settlement_run_id),
                    public::settlement_run_purchase_snapshots::purchase_id.eq(purchase_id),
                    public::settlement_run_purchase_snapshots::expected_revision.eq(expected),
                    public::settlement_run_purchase_snapshots::revision.eq(next),
                ))
                .execute(&mut *conn)
                .await?;
        }

        let updated_period = diesel::update(
            periods::accounting_periods
                .filter(periods::household_id.eq(household_id))
                .filter(periods::period_start.eq(period_start))
                .filter(periods::status.eq("OPEN"))
                .filter(periods::revision.eq(expected_revision)),
        )
        .set((
            periods::status.eq("CLOSED"),
            periods::revision.eq(revision),
            periods::closed_by.eq(Some(actor_id)),
            periods::closed_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;
        if updated_period != 1 {
            return Err(ClosePeriodPersistenceError::StalePeriod);
        }

        diesel::insert_into(public::household_events::table)
            .values((
                public::household_events::household_id.eq(household_id),
                public::household_events::event_type.eq("period.closed"),
                public::household_events::entity_kind.eq("settlement_run"),
                public::household_events::entity_id.eq(settlement_run_id),
                public::household_events::revision.eq(revision),
                public::household_events::changed_fields.eq(vec![Some("status".to_owned())]),
                public::household_events::actor_id.eq(actor_id),
                public::household_events::created_at.eq(now),
            ))
            .execute(&mut *conn)
            .await?;

        Ok(PersistedClosePeriod {
            settlement_run_id: settlement_run_id.to_string(),
            period: decision.period.clone(),
            revision: decision.revision,
        })
    })
    .await
}

fn validate_decision(
    decision: &ClosePeriodDecision,
    period: &str,
    revision: i64,
    actor_id: Uuid,
) -> Result<(), ClosePeriodPersistenceError> {
    if decision.period != period
        || decision.event.period != decision.period
        || decision.event.revision != decision.revision
        || parse_uuid(decision.event.actor_id.as_str(), "actor_id")? != actor_id
        || decision.event.changed_fields != ["status"]
        || revision <= 0
    {
        return Err(ClosePeriodPersistenceError::InvalidDecision);
    }
    Ok(())
}

fn validate_purchase_revisions(
    decision: &ClosePeriodDecision,
    persisted: Vec<(Uuid, i64)>,
) -> Result<(), ClosePeriodPersistenceError> {
    let expected = decision
        .purchase_closures
        .iter()
        .map(|closure| {
            let id = parse_uuid(closure.purchase_id.as_str(), "purchase_id")?;
            let revision = i64::try_from(closure.expected_revision)
                .map_err(|_| ClosePeriodPersistenceError::NumericOutOfRange("purchase revision"))?;
            Ok((id, revision))
        })
        .collect::<Result<BTreeMap<_, _>, ClosePeriodPersistenceError>>()?;
    let actual = persisted.into_iter().collect::<BTreeMap<_, _>>();
    if actual != expected {
        return Err(ClosePeriodPersistenceError::StalePurchase);
    }
    Ok(())
}

fn period_bounds(period: &str) -> Result<(NaiveDate, NaiveDate), ClosePeriodPersistenceError> {
    let mut parts = period.split('-');
    let year = parts
        .next()
        .filter(|part| part.len() == 4 && part.bytes().all(|byte| byte.is_ascii_digit()))
        .and_then(|part| part.parse::<i32>().ok())
        .ok_or(ClosePeriodPersistenceError::InvalidPeriod)?;
    let month = parts
        .next()
        .filter(|part| part.len() == 2 && part.bytes().all(|byte| byte.is_ascii_digit()))
        .and_then(|part| part.parse::<u32>().ok())
        .filter(|month| (1..=12).contains(month))
        .ok_or(ClosePeriodPersistenceError::InvalidPeriod)?;
    if parts.next().is_some() {
        return Err(ClosePeriodPersistenceError::InvalidPeriod);
    }
    let start = NaiveDate::from_ymd_opt(year, month, 1)
        .ok_or(ClosePeriodPersistenceError::InvalidPeriod)?;
    let (next_year, next_month) = if month == 12 {
        (
            year.checked_add(1)
                .ok_or(ClosePeriodPersistenceError::InvalidPeriod)?,
            1,
        )
    } else {
        (year, month + 1)
    };
    let end = NaiveDate::from_ymd_opt(next_year, next_month, 1)
        .ok_or(ClosePeriodPersistenceError::InvalidPeriod)?;
    Ok((start, end))
}

fn period_start_utc(date: NaiveDate) -> Result<DateTime<Utc>, ClosePeriodPersistenceError> {
    let offset =
        FixedOffset::east_opt(9 * 60 * 60).ok_or(ClosePeriodPersistenceError::InvalidPeriod)?;
    let local = date
        .and_hms_opt(0, 0, 0)
        .ok_or(ClosePeriodPersistenceError::InvalidPeriod)?;
    offset
        .from_local_datetime(&local)
        .single()
        .map(|datetime| datetime.with_timezone(&Utc))
        .ok_or(ClosePeriodPersistenceError::InvalidPeriod)
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, ClosePeriodPersistenceError> {
    Uuid::parse_str(value).map_err(|_| ClosePeriodPersistenceError::InvalidUuid(field))
}
