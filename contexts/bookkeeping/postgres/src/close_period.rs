use std::collections::BTreeMap;

use bookkeeping_application::close_period::{
    ClosePeriodCommand, ClosePeriodDecision, ClosePeriodWorkflowError, FundingCadence,
    FundingScheduleInput, PurchaseSnapshot, SettlementInput, close_period as close_period_workflow,
};
use bookkeeping_domain::{AccountingPeriod, AccountingPeriodStatus, YearMonth};
use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, TimeZone, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use horse_authorization::{Operation, authorize};
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
    CorruptPersistedData(&'static str),
    SettlementPreview,
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
    policy: &mut impl super::settlement_preview::PolicySettlementData,
) -> Result<PersistedClosePeriod, ClosePeriodWorkflowError<ClosePeriodPersistenceError>> {
    let household_id =
        parse_uuid(household_id, "household_id").map_err(ClosePeriodWorkflowError::Operations)?;
    let actor_id =
        parse_uuid(actor_id, "actor_id").map_err(ClosePeriodWorkflowError::Operations)?;
    let year_month = YearMonth::parse(period).map_err(|_| {
        ClosePeriodWorkflowError::Operations(ClosePeriodPersistenceError::InvalidPeriod)
    })?;
    let (period_start, period_end) =
        period_bounds(year_month).map_err(ClosePeriodWorkflowError::Operations)?;
    let mut pooled = pool.get().await.map_err(|error| {
        ClosePeriodWorkflowError::Operations(ClosePeriodPersistenceError::Pool(error.to_string()))
    })?;
    pooled
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
                policy,
            };
            Ok::<_, DieselError>(close_period_workflow(&mut operations).await)
        })
        .await
        .map_err(|error| {
            ClosePeriodWorkflowError::Operations(ClosePeriodPersistenceError::Database(error))
        })?
}

struct PgClosePeriodOperations<'a, P> {
    conn: &'a mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    period_start: NaiveDate,
    period_end: NaiveDate,
    period: String,
    expected_revision: u64,
    actor_id: Uuid,
    policy: &'a mut P,
}

impl<P: super::settlement_preview::PolicySettlementData>
    bookkeeping_application::close_period::ClosePeriodOperations
    for PgClosePeriodOperations<'_, P>
{
    type Error = ClosePeriodPersistenceError;
    type Result = PersistedClosePeriod;

    async fn load(&mut self) -> Result<ClosePeriodCommand, Self::Error> {
        use public::{accounting_periods, purchases};

        let household_exists = household_postgres::household_access::household_exists(
            &mut *self.conn,
            self.household_id,
        )
        .await?;
        if !household_exists {
            return Err(ClosePeriodPersistenceError::HouseholdNotFound);
        }
        let actor_role = household_postgres::household_access::active_member_role(
            &mut *self.conn,
            self.household_id,
            self.actor_id,
        )
        .await?;
        if authorize(actor_role.as_deref(), Operation::FinancialData).is_err() {
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
            Some(_) => {
                return Err(ClosePeriodPersistenceError::CorruptPersistedData(
                    "accounting period status",
                ));
            }
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
                    id: bookkeeping_application::PurchaseId::new(id.to_string()).map_err(|_| {
                        ClosePeriodPersistenceError::CorruptPersistedData("purchase id")
                    })?,
                    revision: u64::try_from(revision).map_err(|_| {
                        ClosePeriodPersistenceError::CorruptPersistedData("purchase revision")
                    })?,
                })
            })
            .collect::<Result<Vec<_>, ClosePeriodPersistenceError>>()?;
        let previews = super::settlement_preview::calculate_preview_with_connection(
            &mut *self.conn,
            &self.household_id.to_string(),
            &self.period,
            self.policy,
        )
        .await
        .map_err(|_| ClosePeriodPersistenceError::SettlementPreview)?;
        let settlements = previews
            .into_iter()
            .map(|line| {
                Ok(SettlementInput {
                    member_id: bookkeeping_application::MemberId::new(line.member_id).map_err(
                        |_| {
                            ClosePeriodPersistenceError::CorruptPersistedData(
                                "settlement member id",
                            )
                        },
                    )?,
                    member_name: line.member_name,
                    scheduled_yen: line.scheduled_yen,
                    paid_yen: line.paid_yen,
                    previous_owed_yen: line.carry_yen,
                    monthly_adjustment_yen: line.monthly_adjustment_yen,
                    advance_credit_yen: line.advance_credit_yen,
                })
            })
            .collect::<Result<Vec<_>, ClosePeriodPersistenceError>>()?;
        let schedules = self
            .policy
            .schedules_for_period(
                &mut *self.conn,
                self.household_id,
                self.period_start,
                self.period_end,
            )
            .await?;
        let funding_schedules = schedules
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
                        _ => {
                            return Err(ClosePeriodPersistenceError::CorruptPersistedData(
                                "funding cadence",
                            ));
                        }
                    };
                    Ok(FundingScheduleInput {
                        id: id.to_string(),
                        member_id: bookkeeping_application::MemberId::new(member_id.to_string())
                            .map_err(|_| {
                                ClosePeriodPersistenceError::CorruptPersistedData(
                                    "funding schedule member id",
                                )
                            })?,
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
        Ok(ClosePeriodCommand {
            period: period_model(self.period_start, &status, revision)?,
            expected_revision: self.expected_revision,
            draft_purchases: draft_purchases
                .into_iter()
                .map(|id| {
                    bookkeeping_application::PurchaseId::new(id.to_string()).map_err(|_| {
                        ClosePeriodPersistenceError::CorruptPersistedData("purchase id")
                    })
                })
                .collect::<Result<Vec<_>, _>>()?,
            actor_id: bookkeeping_application::MemberId::new(self.actor_id.to_string())
                .map_err(|_| ClosePeriodPersistenceError::CorruptPersistedData("actor id"))?,
            settlements,
            purchases: posted_purchases,
            funding_schedules,
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
            self.policy,
            PersistClosePeriodInput {
                household_id: self.household_id,
                decision,
                period_start: self.period_start,
                period_end: self.period_end,
                revision,
                expected_revision,
                actor_id: self.actor_id,
                settlement_run_id: Uuid::now_v7(),
            },
        )
        .await
    }
}

struct PersistClosePeriodInput {
    household_id: Uuid,
    decision: ClosePeriodDecision,
    period_start: NaiveDate,
    period_end: NaiveDate,
    revision: i64,
    expected_revision: i64,
    actor_id: Uuid,
    settlement_run_id: Uuid,
}

async fn persist_close_period_in_transaction(
    conn: &mut diesel_async::AsyncPgConnection,
    policy: &mut impl super::settlement_preview::PolicySettlementData,
    input: PersistClosePeriodInput,
) -> Result<PersistedClosePeriod, ClosePeriodPersistenceError> {
    let PersistClosePeriodInput {
        household_id,
        decision,
        period_start,
        period_end,
        revision,
        expected_revision,
        actor_id,
        settlement_run_id,
    } = input;
    use public::accounting_periods::dsl as periods;
    use public::purchases::dsl as purchases;
    use public::settlement_runs::dsl as runs;

    let household_exists =
        household_postgres::household_access::household_exists(&mut *conn, household_id).await?;
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
    for obligation in &decision.funding_obligations {
        let id = Uuid::now_v7();
        let schedule_id = parse_decision_uuid(&obligation.schedule_id)?;
        let member_id = parse_decision_uuid(obligation.member_id.as_str())?;
        let pool_id = parse_decision_uuid(&obligation.pool_id)?;
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
        totals.0 = totals.0.checked_add(scheduled_yen).ok_or(
            ClosePeriodPersistenceError::CorruptPersistedData("funding obligation amount"),
        )?;
        totals.1 = totals.1.checked_add(paid_yen).ok_or(
            ClosePeriodPersistenceError::CorruptPersistedData("funding payment amount"),
        )?;
    }
    for line in &decision.settlements {
        let member_id = parse_decision_uuid(line.member_id.as_str())?;
        let totals = funding_totals.remove(&member_id).unwrap_or_default();
        if (line.scheduled_yen, line.paid_yen) != totals {
            return Err(ClosePeriodPersistenceError::InvalidDecision);
        }
    }
    if !funding_totals.is_empty() {
        return Err(ClosePeriodPersistenceError::InvalidDecision);
    }

    let active_members = policy
        .household_members_present_after(&mut *conn, household_id, period_start_utc(period_start)?)
        .await?;
    let active_members = active_members.into_iter().collect::<BTreeMap<_, _>>();
    if active_members.len() != decision.settlements.len()
        || decision.settlements.iter().any(|line| {
            parse_decision_uuid(line.member_id.as_str())
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
    let (rules, shares) = policy.rules_and_shares(&mut *conn, household_id).await?;
    let rules = rules
        .into_iter()
        .map(|(pool_id, version, owner_member_id, settlement_policy)| {
            ((pool_id, version), (owner_member_id, settlement_policy))
        })
        .collect::<BTreeMap<_, _>>();
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
        let member_id = parse_decision_uuid(line.member_id.as_str())?;
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
            let credit = line.advance_credit_yen;
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
        let purchase_id = parse_decision_uuid(snapshot.id.as_str())?;
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

    horse_postgres_common::insert_ui_change(
        &mut *conn,
        household_id,
        settlement_run_id,
        revision,
        actor_id,
        Utc::now(),
        bookkeeping_application::ui_changes::period_closed(
            decision.event.changed_fields.iter().copied(),
        ),
    )
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
        || parse_decision_uuid(decision.event.actor_id.as_str())? != actor_id
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
            .map(|line| parse_decision_uuid(line.member_id.as_str()))
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
            let id = parse_decision_uuid(snapshot.id.as_str())?;
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
    let revision = u64::try_from(revision)
        .map_err(|_| ClosePeriodPersistenceError::CorruptPersistedData("period revision"))?;
    match status {
        "OPEN" => AccountingPeriod::open(year_month, revision),
        "CLOSED" => AccountingPeriod::closed(year_month, revision),
        _ => {
            return Err(ClosePeriodPersistenceError::CorruptPersistedData(
                "accounting period status",
            ));
        }
    }
    .map_err(|_| ClosePeriodPersistenceError::CorruptPersistedData("accounting period"))
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

fn parse_decision_uuid(value: &str) -> Result<Uuid, ClosePeriodPersistenceError> {
    Uuid::parse_str(value).map_err(|_| ClosePeriodPersistenceError::InvalidDecision)
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, ClosePeriodPersistenceError> {
    Uuid::parse_str(value).map_err(|_| ClosePeriodPersistenceError::InvalidUuid(field))
}
