use std::collections::BTreeMap;

use bookkeeping_application::close_period::{
    ClosePeriodCommand, ClosePeriodDecision, FundingCadence, FundingScheduleInput,
    PurchaseSnapshot, SettlementInput, close_period as close_period_workflow,
    decide_funding_obligations,
};
use bookkeeping_domain::{AccountingPeriod, AccountingPeriodStatus, YearMonth};
use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, TimeZone, Utc};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, JoinOnDsl, OptionalExtension, QueryDsl, dsl::now,
    result::Error as DieselError,
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
    HouseholdNotFound,
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

pub async fn close_period(
    pool: &PgPool,
    household_id: &str,
    actor_id: &str,
    period: &str,
    expected_revision: u64,
) -> Result<PersistedClosePeriod, ClosePeriodPersistenceError> {
    let household_id = parse_uuid(household_id, "household_id")?;
    let actor_id = parse_uuid(actor_id, "actor_id")?;
    let year_month =
        YearMonth::parse(period).map_err(|_| ClosePeriodPersistenceError::InvalidPeriod)?;
    let (period_start, period_end) = period_bounds(year_month)?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| ClosePeriodPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .build_transaction()
        .serializable()
        .run(async |conn| {
            let mut operations = PgClosePeriodOperations {
                conn,
                household_id,
                period_start,
                period_end,
                period: period.to_owned(),
                expected_revision,
                actor_id,
            };
            close_period_workflow(&mut operations)
                .await
                .map_err(|error| match error {
                    bookkeeping_application::close_period::ClosePeriodWorkflowError::Decision(
                        bookkeeping_application::close_period::ClosePeriodError::PeriodAlreadyClosed,
                    ) => ClosePeriodPersistenceError::PeriodAlreadyClosed,
                    bookkeeping_application::close_period::ClosePeriodWorkflowError::Decision(
                        bookkeeping_application::close_period::ClosePeriodError::RevisionMismatch { .. },
                    ) => ClosePeriodPersistenceError::StalePeriod,
                    bookkeeping_application::close_period::ClosePeriodWorkflowError::Decision(
                        bookkeeping_application::close_period::ClosePeriodError::DraftPurchasesExist,
                    ) => ClosePeriodPersistenceError::DraftPurchasesExist,
                    bookkeeping_application::close_period::ClosePeriodWorkflowError::Operations(error) => error,
                    _ => ClosePeriodPersistenceError::InvalidDecision,
                })
        })
        .await
}

struct PgClosePeriodOperations<'a> {
    conn: &'a mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    period_start: NaiveDate,
    period_end: NaiveDate,
    period: String,
    expected_revision: u64,
    actor_id: Uuid,
}

impl bookkeeping_application::close_period::ClosePeriodOperations for PgClosePeriodOperations<'_> {
    type Error = ClosePeriodPersistenceError;
    type Result = PersistedClosePeriod;

    async fn load(&mut self) -> Result<ClosePeriodCommand, Self::Error> {
        use public::{accounting_periods, household_members, households, purchases};

        let household_exists = households::table
            .filter(households::id.eq(self.household_id))
            .select(households::id)
            .first::<Uuid>(&mut *self.conn)
            .await
            .optional()?
            .is_some();
        if !household_exists {
            return Err(ClosePeriodPersistenceError::HouseholdNotFound);
        }
        let actor_is_member = household_members::table
            .filter(household_members::household_id.eq(self.household_id))
            .filter(household_members::member_id.eq(self.actor_id))
            .filter(household_members::left_at.is_null())
            .select(household_members::member_id)
            .first::<Uuid>(&mut *self.conn)
            .await
            .optional()?
            .is_some();
        if !actor_is_member {
            return Err(ClosePeriodPersistenceError::HouseholdNotFound);
        }
        diesel::insert_into(accounting_periods::table)
            .values((
                accounting_periods::household_id.eq(self.household_id),
                accounting_periods::period_start.eq(self.period_start),
                accounting_periods::status.eq("OPEN"),
                accounting_periods::revision.eq(1_i64),
            ))
            .on_conflict((
                accounting_periods::household_id,
                accounting_periods::period_start,
            ))
            .do_nothing()
            .execute(&mut *self.conn)
            .await?;
        let current_period = accounting_periods::table
            .filter(accounting_periods::household_id.eq(self.household_id))
            .filter(accounting_periods::period_start.eq(self.period_start))
            .for_update()
            .select((accounting_periods::status, accounting_periods::revision))
            .first::<(String, i64)>(&mut *self.conn)
            .await
            .optional()?;
        let (status, revision) = match current_period {
            Some((status, revision)) if status == "CLOSED" || status == "OPEN" => {
                (status, revision)
            }
            Some(_) => return Err(ClosePeriodPersistenceError::PeriodNotFound),
            None => ("OPEN".to_owned(), 1),
        };
        let draft_purchases = purchases::table
            .filter(purchases::household_id.eq(self.household_id))
            .filter(purchases::status.eq("DRAFT"))
            .filter(purchases::occurred_at.ge(period_start_utc(self.period_start)?))
            .filter(purchases::occurred_at.lt(period_start_utc(self.period_end)?))
            .select(purchases::id)
            .load::<Uuid>(&mut *self.conn)
            .await?;
        let posted_purchases = purchases::table
            .filter(purchases::household_id.eq(self.household_id))
            .filter(purchases::status.eq("POSTED"))
            .filter(purchases::occurred_at.ge(period_start_utc(self.period_start)?))
            .filter(purchases::occurred_at.lt(period_start_utc(self.period_end)?))
            .order(purchases::id.asc())
            .select((purchases::id, purchases::revision))
            .load::<(Uuid, i64)>(&mut *self.conn)
            .await?
            .into_iter()
            .map(|(id, revision)| {
                Ok(PurchaseSnapshot {
                    id: bookkeeping_application::PurchaseId::new(id.to_string())
                        .map_err(|_| ClosePeriodPersistenceError::InvalidDecision)?,
                    revision: u64::try_from(revision)
                        .map_err(|_| ClosePeriodPersistenceError::InvalidDecision)?,
                })
            })
            .collect::<Result<Vec<_>, ClosePeriodPersistenceError>>()?;
        let previews = super::settlement_preview::calculate_preview_with_connection(
            &mut *self.conn,
            &self.household_id.to_string(),
            &self.period,
        )
        .await
        .map_err(|_| ClosePeriodPersistenceError::InvalidDecision)?;
        let settlements = previews
            .into_iter()
            .map(|line| {
                Ok(SettlementInput {
                    member_id: bookkeeping_application::MemberId::new(line.member_id)
                        .map_err(|_| ClosePeriodPersistenceError::InvalidDecision)?,
                    member_name: line.member_name,
                    scheduled_yen: line.scheduled_yen,
                    paid_yen: line.paid_yen,
                    previous_owed_yen: line.carry_yen,
                    monthly_adjustment_yen: line.monthly_adjustment_yen,
                    advance_credit_yen: line.advance_credit_yen,
                })
            })
            .collect::<Result<Vec<_>, ClosePeriodPersistenceError>>()?;
        Ok(ClosePeriodCommand {
            period: period_model(self.period_start, &status, revision)?,
            expected_revision: self.expected_revision,
            draft_purchases: draft_purchases
                .into_iter()
                .map(|id| {
                    bookkeeping_application::PurchaseId::new(id.to_string())
                        .map_err(|_| ClosePeriodPersistenceError::InvalidDecision)
                })
                .collect::<Result<Vec<_>, _>>()?,
            actor_id: bookkeeping_application::MemberId::new(self.actor_id.to_string())
                .map_err(|_| ClosePeriodPersistenceError::InvalidDecision)?,
            settlements,
            purchases: posted_purchases,
        })
    }

    async fn persist(
        &mut self,
        decision: ClosePeriodDecision,
    ) -> Result<Self::Result, Self::Error> {
        let revision = i64::try_from(decision.period.revision())
            .map_err(|_| ClosePeriodPersistenceError::NumericOutOfRange("period revision"))?;
        let expected_revision = revision
            .checked_sub(1)
            .ok_or(ClosePeriodPersistenceError::InvalidDecision)?;
        validate_decision(&decision, self.period_start, revision, self.actor_id)?;
        persist_close_period_in_transaction(
            self.conn,
            self.household_id,
            decision,
            self.period_start,
            self.period_end,
            revision,
            expected_revision,
            self.actor_id,
            Uuid::now_v7(),
        )
        .await
    }
}

async fn persist_close_period_in_transaction(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    decision: ClosePeriodDecision,
    period_start: NaiveDate,
    period_end: NaiveDate,
    revision: i64,
    expected_revision: i64,
    actor_id: Uuid,
    settlement_run_id: Uuid,
) -> Result<PersistedClosePeriod, ClosePeriodPersistenceError> {
    use public::accounting_periods::dsl as periods;
    use public::households::dsl as households;
    use public::purchases::dsl as purchases;
    use public::settlement_runs::dsl as runs;

    let household_exists = households::households
        .filter(households::id.eq(household_id))
        .select(households::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .is_some();
    if !household_exists {
        return Err(ClosePeriodPersistenceError::HouseholdNotFound);
    }
    diesel::insert_into(periods::accounting_periods)
        .values((
            periods::household_id.eq(household_id),
            periods::period_start.eq(period_start),
            periods::status.eq("OPEN"),
            periods::revision.eq(1_i64),
        ))
        .on_conflict((periods::household_id, periods::period_start))
        .do_nothing()
        .execute(&mut *conn)
        .await?;

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

    let schedules = public::funding_schedules::table
        .filter(public::funding_schedules::household_id.eq(household_id))
        .filter(public::funding_schedules::starts_on.lt(period_end))
        .filter(
            public::funding_schedules::ends_on
                .is_null()
                .or(public::funding_schedules::ends_on.ge(period_start)),
        )
        .filter(
            public::funding_schedules::active
                .eq(true)
                .or(public::funding_schedules::ends_on.is_not_null()),
        )
        .select((
            public::funding_schedules::id,
            public::funding_schedules::member_id,
            public::funding_schedules::pool_id,
            public::funding_schedules::amount_yen,
            public::funding_schedules::cadence,
            public::funding_schedules::starts_on,
            public::funding_schedules::ends_on,
            public::funding_schedules::active,
            public::funding_schedules::created_at,
        ))
        .load::<(
            Uuid,
            Uuid,
            Uuid,
            i64,
            String,
            NaiveDate,
            Option<NaiveDate>,
            bool,
            DateTime<Utc>,
        )>(&mut *conn)
        .await?;
    let schedule_inputs = schedules
        .into_iter()
        .map(
            |(
                id,
                member_id,
                pool_id,
                amount_yen,
                cadence,
                starts_on,
                ends_on,
                active,
                created_at,
            )| {
                let cadence = match cadence.as_str() {
                    "MONTHLY" => FundingCadence::Monthly,
                    "WEEKLY" => FundingCadence::Weekly,
                    "CUSTOM" => FundingCadence::Custom,
                    _ => return Err(ClosePeriodPersistenceError::InvalidDecision),
                };
                Ok(FundingScheduleInput {
                    id: id.to_string(),
                    member_id: bookkeeping_application::MemberId::new(member_id.to_string())
                        .map_err(|_| ClosePeriodPersistenceError::InvalidDecision)?,
                    pool_id: pool_id.to_string(),
                    amount_yen,
                    cadence,
                    starts_on_day: starts_on.num_days_from_ce(),
                    ends_on_day: ends_on.map(|day| day.num_days_from_ce()),
                    active,
                    created_at_micros: created_at.timestamp_micros(),
                })
            },
        )
        .collect::<Result<Vec<_>, ClosePeriodPersistenceError>>()?;
    let obligation_decisions = decide_funding_obligations(
        period_start.num_days_from_ce(),
        period_end.num_days_from_ce(),
        schedule_inputs,
    )
    .map_err(|_| ClosePeriodPersistenceError::InvalidDecision)?;
    let existing_obligations = public::funding_obligations::table
        .filter(public::funding_obligations::household_id.eq(household_id))
        .filter(public::funding_obligations::period_start.eq(period_start))
        .for_update()
        .order_by((
            public::funding_obligations::member_id.asc(),
            public::funding_obligations::pool_id.asc(),
        ))
        .select((
            public::funding_obligations::member_id,
            public::funding_obligations::pool_id,
            public::funding_obligations::amount_yen,
        ))
        .load::<(Uuid, Uuid, i64)>(&mut *conn)
        .await?;
    let existing_amount_by_member_pool = existing_obligations
        .into_iter()
        .map(|(member_id, pool_id, amount_yen)| ((member_id, pool_id), amount_yen))
        .collect::<BTreeMap<_, _>>();
    for obligation in obligation_decisions {
        let id = Uuid::now_v7();
        let schedule_id = parse_uuid(&obligation.schedule_id, "schedule_id")?;
        let member_id = parse_uuid(obligation.member_id.as_str(), "member_id")?;
        let pool_id = parse_uuid(&obligation.pool_id, "pool_id")?;
        let amount_yen = obligation.amount_yen.max(
            existing_amount_by_member_pool
                .get(&(member_id, pool_id))
                .copied()
                .unwrap_or_default(),
        );
        diesel::insert_into(public::funding_obligations::table)
            .values((
                public::funding_obligations::id.eq(id),
                public::funding_obligations::household_id.eq(household_id),
                public::funding_obligations::schedule_id.eq(Some(schedule_id)),
                public::funding_obligations::member_id.eq(member_id),
                public::funding_obligations::pool_id.eq(pool_id),
                public::funding_obligations::period_start.eq(period_start),
                public::funding_obligations::amount_yen.eq(amount_yen),
            ))
            .on_conflict((
                public::funding_obligations::household_id,
                public::funding_obligations::member_id,
                public::funding_obligations::pool_id,
                public::funding_obligations::period_start,
            ))
            .do_update()
            .set((
                public::funding_obligations::schedule_id.eq(Some(schedule_id)),
                public::funding_obligations::amount_yen.eq(amount_yen),
            ))
            .execute(&mut *conn)
            .await?;
    }

    let obligations = public::funding_obligations::table
        .filter(public::funding_obligations::household_id.eq(household_id))
        .filter(public::funding_obligations::period_start.eq(period_start))
        .select((
            public::funding_obligations::member_id,
            public::funding_obligations::amount_yen,
            public::funding_obligations::paid_yen,
        ))
        .load::<(Uuid, i64, i64)>(&mut *conn)
        .await?;
    let mut funding_totals = BTreeMap::<Uuid, (i64, i64)>::new();
    for (member_id, scheduled_yen, paid_yen) in obligations {
        let totals = funding_totals.entry(member_id).or_default();
        totals.0 = totals
            .0
            .checked_add(scheduled_yen)
            .ok_or(ClosePeriodPersistenceError::InvalidDecision)?;
        totals.1 = totals
            .1
            .checked_add(paid_yen)
            .ok_or(ClosePeriodPersistenceError::InvalidDecision)?;
    }
    for line in &decision.settlements {
        let member_id = parse_uuid(line.member_id.as_str(), "member_id")?;
        let totals = funding_totals.remove(&member_id).unwrap_or_default();
        if (line.scheduled_yen, line.paid_yen) != totals {
            return Err(ClosePeriodPersistenceError::InvalidDecision);
        }
    }
    if !funding_totals.is_empty() {
        return Err(ClosePeriodPersistenceError::InvalidDecision);
    }

    let active_members = public::household_members::table
        .inner_join(
            public::members::table.on(public::members::id.eq(public::household_members::member_id)),
        )
        .filter(public::household_members::household_id.eq(household_id))
        .filter(
            public::household_members::left_at
                .is_null()
                .or(public::household_members::left_at.gt(period_start_utc(period_start)?)),
        )
        .select((
            public::household_members::member_id,
            public::members::display_name,
        ))
        .load::<(Uuid, String)>(&mut *conn)
        .await?;
    let active_members = active_members.into_iter().collect::<BTreeMap<_, _>>();
    if active_members.len() != decision.settlements.len()
        || decision.settlements.iter().any(|line| {
            parse_uuid(line.member_id.as_str(), "member_id")
                .ok()
                .and_then(|member_id| active_members.get(&member_id))
                != Some(&line.member_name)
        })
    {
        return Err(ClosePeriodPersistenceError::InvalidDecision);
    }

    let posted_purchases = purchases::purchases
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::status.eq("POSTED"))
        .filter(purchases::occurred_at.ge(period_start_utc(period_start)?))
        .filter(purchases::occurred_at.lt(period_start_utc(period_end)?))
        .for_update()
        .order(purchases::id.asc())
        .select((purchases::id, purchases::revision))
        .load::<(Uuid, i64)>(&mut *conn)
        .await?;
    validate_purchase_revisions(&decision, posted_purchases)?;

    let pools = public::pools::table
        .filter(public::pools::household_id.eq(household_id))
        .select((
            public::pools::id,
            public::pools::kind,
            public::pools::current_funding_version,
        ))
        .load::<(Uuid, String, Option<i32>)>(&mut *conn)
        .await?;
    let current_versions = pools
        .iter()
        .filter_map(|(pool_id, _, version)| version.map(|version| (*pool_id, version)))
        .collect::<BTreeMap<_, _>>();
    let rules = public::pool_funding_rules::table
        .filter(public::pool_funding_rules::household_id.eq(household_id))
        .select((
            public::pool_funding_rules::pool_id,
            public::pool_funding_rules::version,
            public::pool_funding_rules::owner_member_id,
            public::pool_funding_rules::settlement_policy,
        ))
        .load::<(Uuid, i32, Option<Uuid>, Option<String>)>(&mut *conn)
        .await?;
    let rules = rules
        .into_iter()
        .map(|(pool_id, version, owner_member_id, settlement_policy)| {
            ((pool_id, version), (owner_member_id, settlement_policy))
        })
        .collect::<BTreeMap<_, _>>();
    let shares = public::pool_funding_shares::table
        .filter(public::pool_funding_shares::household_id.eq(household_id))
        .order_by((
            public::pool_funding_shares::pool_id.asc(),
            public::pool_funding_shares::version.asc(),
            public::pool_funding_shares::member_id.asc(),
        ))
        .select((
            public::pool_funding_shares::pool_id,
            public::pool_funding_shares::version,
            public::pool_funding_shares::member_id,
            public::pool_funding_shares::weight,
        ))
        .load::<(Uuid, i32, Uuid, i64)>(&mut *conn)
        .await?;
    let mut shares_by_pool = BTreeMap::<(Uuid, i32), Vec<serde_json::Value>>::new();
    for (pool_id, version, member_id, weight) in shares {
        if current_versions.get(&pool_id) != Some(&version) {
            continue;
        }
        shares_by_pool
            .entry((pool_id, version))
            .or_default()
            .push(serde_json::json!({
                "MemberID": member_id.to_string(),
                "Weight": weight,
            }));
    }
    let mut funding_rules = BTreeMap::<String, serde_json::Value>::new();
    for (pool_id, kind, current_version) in pools {
        let version = current_version.unwrap_or_default();
        let (owner_member_id, settlement_policy) =
            rules.get(&(pool_id, version)).cloned().unwrap_or_default();
        let funding_shares = shares_by_pool.remove(&(pool_id, version));
        funding_rules.insert(
            pool_id.to_string(),
            serde_json::json!({
                "PoolID": pool_id.to_string(),
                "Kind": kind,
                "Version": version,
                "OwnerMemberID": owner_member_id.map(|id| id.to_string()).unwrap_or_default(),
                "SettlementPolicy": settlement_policy.unwrap_or_default(),
                "FundingShares": funding_shares,
            }),
        );
    }
    let funding_snapshot = serde_json::to_value(funding_rules)
        .map_err(|_| ClosePeriodPersistenceError::InvalidDecision)?;

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
                    public::settlement_run_adjustments::amount_yen.eq(line.monthly_adjustment_yen),
                ))
                .execute(&mut *conn)
                .await?;
        }
        if line.advance_credit_yen != 0 {
            let credit = i64::try_from(line.advance_credit_yen)
                .map_err(|_| ClosePeriodPersistenceError::NumericOutOfRange("advance credit"))?;
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

    for snapshot in &decision.purchases {
        let purchase_id = parse_uuid(snapshot.id.as_str(), "purchase_id")?;
        let purchase_revision = i64::try_from(snapshot.revision)
            .map_err(|_| ClosePeriodPersistenceError::NumericOutOfRange("purchase revision"))?;
        diesel::insert_into(public::settlement_run_purchase_snapshots::table)
            .values((
                public::settlement_run_purchase_snapshots::household_id.eq(household_id),
                public::settlement_run_purchase_snapshots::settlement_run_id.eq(settlement_run_id),
                public::settlement_run_purchase_snapshots::purchase_id.eq(purchase_id),
                public::settlement_run_purchase_snapshots::purchase_revision.eq(purchase_revision),
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
        period: decision.period.as_string(),
        revision: decision.period.revision(),
    })
}

fn validate_decision(
    decision: &ClosePeriodDecision,
    period_start: NaiveDate,
    revision: i64,
    actor_id: Uuid,
) -> Result<(), ClosePeriodPersistenceError> {
    let previous_revision = revision
        .checked_sub(1)
        .ok_or(ClosePeriodPersistenceError::InvalidDecision)?;
    if decision.period.year_month() != year_month(period_start)?
        || decision.period.status() != AccountingPeriodStatus::Closed
        || decision.event.period != decision.period.year_month()
        || decision.event.revision != decision.period.revision()
        || parse_uuid(decision.event.actor_id.as_str(), "actor_id")? != actor_id
        || decision.event.changed_fields != ["status"]
        || revision <= 1
        || i64::try_from(decision.period.revision()).ok() != Some(revision)
        || decision
            .settlements
            .iter()
            .any(|line| !settlement_line_is_valid(line))
        || decision
            .settlements
            .iter()
            .map(|line| parse_uuid(line.member_id.as_str(), "member_id"))
            .collect::<Result<std::collections::BTreeSet<_>, _>>()?
            .len()
            != decision.settlements.len()
        || decision
            .purchases
            .iter()
            .any(|purchase| purchase.revision == 0)
        || decision.period.revision().checked_sub(1) != u64::try_from(previous_revision).ok()
    {
        return Err(ClosePeriodPersistenceError::InvalidDecision);
    }
    Ok(())
}

fn settlement_line_is_valid(line: &bookkeeping_application::close_period::SettlementLine) -> bool {
    if line.member_name.trim().is_empty()
        || !(0..=1_000_000_000_000).contains(&line.scheduled_yen)
        || !(0..=1_000_000_000_000).contains(&line.paid_yen)
        || !(-1_000_000_000_000..=1_000_000_000_000).contains(&line.carry_yen)
        || !(-1_000_000_000_000..=1_000_000_000_000).contains(&line.monthly_adjustment_yen)
        || !(0..=1_000_000_000_000).contains(&line.advance_credit_yen)
    {
        return false;
    }
    line.scheduled_yen
        .checked_add(line.carry_yen)
        .and_then(|amount| amount.checked_add(line.monthly_adjustment_yen))
        .and_then(|amount| amount.checked_sub(line.paid_yen))
        .and_then(|amount| amount.checked_sub(line.advance_credit_yen))
        .filter(|amount| (-3_000_000_000_000..=3_000_000_000_000).contains(amount))
        == Some(line.amount_due_yen)
}

fn validate_purchase_revisions(
    decision: &ClosePeriodDecision,
    persisted: Vec<(Uuid, i64)>,
) -> Result<(), ClosePeriodPersistenceError> {
    let expected = decision
        .purchases
        .iter()
        .map(|snapshot| {
            let id = parse_uuid(snapshot.id.as_str(), "purchase_id")?;
            let revision = i64::try_from(snapshot.revision)
                .map_err(|_| ClosePeriodPersistenceError::NumericOutOfRange("purchase revision"))?;
            Ok((id, revision))
        })
        .collect::<Result<BTreeMap<_, _>, ClosePeriodPersistenceError>>()?;
    let actual = persisted.into_iter().collect::<BTreeMap<_, _>>();
    if actual != expected || actual.len() != decision.purchases.len() {
        return Err(ClosePeriodPersistenceError::StalePurchase);
    }
    Ok(())
}

fn period_bounds(period: YearMonth) -> Result<(NaiveDate, NaiveDate), ClosePeriodPersistenceError> {
    let year = i32::from(period.year());
    let month = u32::from(period.month());
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

fn year_month(date: NaiveDate) -> Result<YearMonth, ClosePeriodPersistenceError> {
    YearMonth::new(
        u16::try_from(date.year()).map_err(|_| ClosePeriodPersistenceError::InvalidPeriod)?,
        u8::try_from(date.month()).map_err(|_| ClosePeriodPersistenceError::InvalidPeriod)?,
    )
    .map_err(|_| ClosePeriodPersistenceError::InvalidPeriod)
}

fn period_model(
    period_start: NaiveDate,
    status: &str,
    revision: i64,
) -> Result<AccountingPeriod, ClosePeriodPersistenceError> {
    let year_month = year_month(period_start)?;
    let revision =
        u64::try_from(revision).map_err(|_| ClosePeriodPersistenceError::InvalidDecision)?;
    match status {
        "OPEN" => AccountingPeriod::open(year_month, revision),
        "CLOSED" => AccountingPeriod::closed(year_month, revision),
        _ => return Err(ClosePeriodPersistenceError::PeriodNotFound),
    }
    .map_err(|_| ClosePeriodPersistenceError::InvalidDecision)
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
