use chrono::{DateTime, NaiveDate, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use horse_postgres_common::insert_ui_change;
use uuid::Uuid;

use policy_application::set_funding_schedule::{
    ActiveFundingSchedule, SetFundingScheduleCommand, SetFundingScheduleDecision,
    SetFundingScheduleError, decide_set_funding_schedule,
};

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedFundingSchedule {
    pub id: String,
    pub household_id: String,
    pub member_id: String,
    pub pool_id: String,
    pub amount_yen: i64,
    pub cadence: String,
    pub starts_on: String,
    pub ends_on: Option<String>,
    pub active: bool,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug)]
pub enum SetFundingSchedulePersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidDate(&'static str),
    InvalidDecision,
    InvalidWorkflow(SetFundingScheduleError),
    HouseholdNotFound,
    ActorNotFound,
    MemberNotFound,
    PoolNotFound,
    NumericOutOfRange(&'static str),
}

impl From<DieselError> for SetFundingSchedulePersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_set_funding_schedule(
    pool: &PgPool,
    command: SetFundingScheduleCommand,
) -> Result<PersistedFundingSchedule, SetFundingSchedulePersistenceError> {
    let actor_id = parse_uuid(&command.actor_id, "actor_id")?;
    let household_id = parse_uuid(&command.household_id, "household_id")?;
    let member_id = parse_uuid(&command.member_id, "member_id")?;
    let pool_id = parse_uuid(&command.pool_id, "pool_id")?;
    let schedule_id = Uuid::now_v7();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SetFundingSchedulePersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedFundingSchedule, SetFundingSchedulePersistenceError, _>(
            async move |conn| {
                use public::funding_schedules::dsl as schedules;
                use public::household_members::dsl as members;
                use public::households::dsl as households;
                use public::pools::dsl as pools;

                households::households
                    .filter(households::id.eq(household_id))
                    .for_update()
                    .select(households::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .ok_or(SetFundingSchedulePersistenceError::HouseholdNotFound)?;

                let actor_role = members::household_members
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq(actor_id))
                    .filter(members::left_at.is_null())
                    .select(members::role)
                    .first::<String>(conn)
                    .await
                    .optional()?;
                if authorize(actor_role.as_deref(), Operation::FinancialData).is_err() {
                    return Err(SetFundingSchedulePersistenceError::ActorNotFound);
                }

                let member_is_active = members::household_members
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq(member_id))
                    .filter(members::left_at.is_null())
                    .select(members::member_id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .is_some();
                if !member_is_active {
                    return Err(SetFundingSchedulePersistenceError::MemberNotFound);
                }

                let pool_exists = pools::pools
                    .filter(pools::household_id.eq(household_id))
                    .filter(pools::id.eq(pool_id))
                    .select(pools::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .is_some();
                if !pool_exists {
                    return Err(SetFundingSchedulePersistenceError::PoolNotFound);
                }

                let active_rows = schedules::funding_schedules
                    .filter(schedules::household_id.eq(household_id))
                    .filter(schedules::member_id.eq(member_id))
                    .filter(schedules::pool_id.eq(pool_id))
                    .filter(schedules::active.eq(true))
                    .for_update()
                    .select((schedules::id, schedules::starts_on, schedules::ends_on))
                    .load::<(Uuid, NaiveDate, Option<NaiveDate>)>(conn)
                    .await?;
                let active_schedules = active_rows
                    .iter()
                    .map(|(id, starts_on, ends_on)| ActiveFundingSchedule {
                        id: id.to_string(),
                        starts_on: starts_on.format("%Y-%m-%d").to_string(),
                        ends_on: ends_on.map(|date| date.format("%Y-%m-%d").to_string()),
                    })
                    .collect();
                let decision = decide_set_funding_schedule(
                    SetFundingScheduleCommand {
                        schedule_id: schedule_id.to_string(),
                        ..command
                    },
                    active_schedules,
                )
                .map_err(SetFundingSchedulePersistenceError::InvalidWorkflow)?;
                validate_decision(
                    &decision,
                    actor_id,
                    household_id,
                    member_id,
                    pool_id,
                    schedule_id,
                )?;

                for update in &decision.prior_updates {
                    let id = parse_uuid(&update.id, "prior_schedule_id")?;
                    let end = update
                        .ends_on
                        .as_deref()
                        .map(|date| parse_date(date, "ends_on"))
                        .transpose()?;
                    diesel::update(schedules::funding_schedules.filter(schedules::id.eq(id)))
                        .set((schedules::ends_on.eq(end), schedules::active.eq(false)))
                        .execute(conn)
                        .await?;
                }

                let starts_on = parse_date(&decision.starts_on, "starts_on")?;
                let ends_on = decision
                    .ends_on
                    .as_deref()
                    .map(|date| parse_date(date, "ends_on"))
                    .transpose()?;
                let (id, created_at) = diesel::insert_into(schedules::funding_schedules)
                    .values((
                        schedules::id.eq(schedule_id),
                        schedules::household_id.eq(household_id),
                        schedules::member_id.eq(member_id),
                        schedules::pool_id.eq(pool_id),
                        schedules::amount_yen.eq(decision.amount_yen),
                        schedules::cadence.eq(&decision.cadence),
                        schedules::starts_on.eq(starts_on),
                        schedules::ends_on.eq(ends_on),
                        schedules::active.eq(true),
                    ))
                    .returning((schedules::id, schedules::created_at))
                    .get_result::<(Uuid, chrono::DateTime<chrono::Utc>)>(conn)
                    .await?;

                insert_ui_change(
                    conn,
                    household_id,
                    id,
                    1_i64,
                    actor_id,
                    created_at,
                    decision.ui_change(),
                )
                .await?;

                Ok(PersistedFundingSchedule {
                    id: id.to_string(),
                    household_id: household_id.to_string(),
                    member_id: member_id.to_string(),
                    pool_id: pool_id.to_string(),
                    amount_yen: decision.amount_yen,
                    cadence: decision.cadence,
                    starts_on: starts_on.format("%Y-%m-%d").to_string(),
                    ends_on: ends_on.map(|date| date.format("%Y-%m-%d").to_string()),
                    active: true,
                    created_at,
                })
            },
        )
        .await
}

fn validate_decision(
    decision: &SetFundingScheduleDecision,
    actor_id: Uuid,
    household_id: Uuid,
    member_id: Uuid,
    pool_id: Uuid,
    schedule_id: Uuid,
) -> Result<(), SetFundingSchedulePersistenceError> {
    if parse_uuid(&decision.actor_id, "actor_id")? != actor_id
        || parse_uuid(&decision.household_id, "household_id")? != household_id
        || parse_uuid(&decision.member_id, "member_id")? != member_id
        || parse_uuid(&decision.pool_id, "pool_id")? != pool_id
        || parse_uuid(&decision.schedule_id, "schedule_id")? != schedule_id
        || decision.changed_fields != ["amountYen", "startsOn", "endsOn"]
        || decision.amount_yen < 0
        || decision.amount_yen > 1_000_000_000_000
        || decision.cadence != "MONTHLY"
    {
        return Err(SetFundingSchedulePersistenceError::InvalidDecision);
    }
    Ok(())
}

fn parse_uuid(
    value: &str,
    field: &'static str,
) -> Result<Uuid, SetFundingSchedulePersistenceError> {
    Uuid::parse_str(value).map_err(|_| SetFundingSchedulePersistenceError::InvalidUuid(field))
}

fn parse_date(
    value: &str,
    field: &'static str,
) -> Result<NaiveDate, SetFundingSchedulePersistenceError> {
    NaiveDate::parse_from_str(value, "%Y-%m-%d")
        .map_err(|_| SetFundingSchedulePersistenceError::InvalidDate(field))
}
