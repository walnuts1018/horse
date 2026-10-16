use chrono::{DateTime, NaiveDate, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use horse_household_change::UiChangeDescriptor;
use horse_postgres_common::insert_ui_change;
use policy_application::set_funding_schedule::{
    ActiveFundingSchedule, ActorId, FundingScheduleDate, FundingScheduleId, HouseholdId, MemberId,
    PoolId, SetFundingScheduleCommand, SetFundingScheduleDecision, SetFundingScheduleError,
    SetFundingScheduleOperations, SetFundingScheduleWorkflowError,
};
use uuid::Uuid;

use crate::{PgPool, schema::public};

pub use policy_application::set_funding_schedule::PersistedFundingSchedule;

pub struct PostgresSetFundingScheduleHandler {
    pool: PgPool,
}

impl PostgresSetFundingScheduleHandler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl policy_application::set_funding_schedule::SetFundingScheduleHandler
    for PostgresSetFundingScheduleHandler
{
    fn handle<'a>(
        &'a self,
        command: SetFundingScheduleCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<PersistedFundingSchedule, policy_application::WorkflowFailure>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move { persist_set_funding_schedule(&self.pool, command).await })
    }
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
) -> Result<PersistedFundingSchedule, policy_application::WorkflowFailure> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| policy_application::WorkflowFailure::Infrastructure(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedFundingSchedule, SetFundingSchedulePersistenceError, _>(
            async move |conn| {
                let mut operations = PostgresSetFundingScheduleOperations { conn };
                policy_application::set_funding_schedule::set_funding_schedule(
                    &mut operations,
                    command,
                )
                .await
                .map_err(|error| match error {
                    SetFundingScheduleWorkflowError::Decision(error) => {
                        SetFundingSchedulePersistenceError::InvalidWorkflow(error)
                    }
                    SetFundingScheduleWorkflowError::Operations(error) => error,
                })
            },
        )
        .await
        .map_err(classify_set_funding_schedule_error)
}

fn classify_set_funding_schedule_error(
    error: SetFundingSchedulePersistenceError,
) -> policy_application::WorkflowFailure {
    use policy_application::WorkflowFailure;
    match error {
        SetFundingSchedulePersistenceError::HouseholdNotFound
        | SetFundingSchedulePersistenceError::ActorNotFound
        | SetFundingSchedulePersistenceError::MemberNotFound
        | SetFundingSchedulePersistenceError::PoolNotFound => WorkflowFailure::NotFound,
        SetFundingSchedulePersistenceError::InvalidUuid(_)
        | SetFundingSchedulePersistenceError::InvalidDate(_)
        | SetFundingSchedulePersistenceError::InvalidDecision => WorkflowFailure::Decision,
        SetFundingSchedulePersistenceError::InvalidWorkflow(error) => match error {
            SetFundingScheduleError::DuplicateActiveSchedule => WorkflowFailure::Conflict,
            SetFundingScheduleError::InvalidAmount | SetFundingScheduleError::InvalidEndDate => {
                WorkflowFailure::Decision
            }
            SetFundingScheduleError::InvalidExistingSchedule => {
                WorkflowFailure::Infrastructure("stored funding schedule is invalid".to_owned())
            }
        },
        SetFundingSchedulePersistenceError::Pool(message) => {
            WorkflowFailure::Infrastructure(message)
        }
        SetFundingSchedulePersistenceError::Database(error) => {
            WorkflowFailure::Infrastructure(error.to_string())
        }
        SetFundingSchedulePersistenceError::NumericOutOfRange(field) => {
            WorkflowFailure::Infrastructure(format!("{field} is out of range"))
        }
    }
}

struct PostgresSetFundingScheduleOperations<'a> {
    conn: &'a mut crate::AsyncPgConnection,
}

impl SetFundingScheduleOperations for PostgresSetFundingScheduleOperations<'_> {
    type Error = SetFundingSchedulePersistenceError;
    type Output = PersistedFundingSchedule;

    async fn load_authorized_active_schedules(
        &mut self,
        actor_id: ActorId,
        household_id: HouseholdId,
        member_id: MemberId,
        pool_id: PoolId,
    ) -> Result<Vec<ActiveFundingSchedule>, Self::Error> {
        use public::funding_schedules::dsl as schedules;
        use public::household_members::dsl as members;
        use public::households::dsl as households;
        use public::pools::dsl as pools;

        let household_id = household_id.value();
        let member_id = member_id.value();
        let pool_id = pool_id.value();

        households::households
            .filter(households::id.eq(household_id))
            .for_update()
            .select(households::id)
            .first::<Uuid>(&mut *self.conn)
            .await
            .optional()?
            .ok_or(SetFundingSchedulePersistenceError::HouseholdNotFound)?;

        let actor_id = actor_id.value();
        let actor_role = members::household_members
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(actor_id))
            .filter(members::left_at.is_null())
            .select(members::role)
            .first::<String>(&mut *self.conn)
            .await
            .optional()?;
        if authorize(actor_role.as_deref(), Operation::ManageHousehold).is_err() {
            return Err(SetFundingSchedulePersistenceError::ActorNotFound);
        }

        let member_is_active = members::household_members
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(member_id))
            .filter(members::left_at.is_null())
            .select(members::member_id)
            .first::<Uuid>(&mut *self.conn)
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
            .first::<Uuid>(&mut *self.conn)
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
            .load::<(Uuid, NaiveDate, Option<NaiveDate>)>(&mut *self.conn)
            .await?;

        Ok(active_rows
            .into_iter()
            .map(|(id, starts_on, ends_on)| ActiveFundingSchedule {
                id: FundingScheduleId::from(id),
                starts_on: FundingScheduleDate::new(starts_on),
                ends_on: ends_on.map(FundingScheduleDate::new),
            })
            .collect())
    }

    async fn persist(
        &mut self,
        decision: SetFundingScheduleDecision,
        change: UiChangeDescriptor,
    ) -> Result<Self::Output, Self::Error> {
        use public::funding_schedules::dsl as schedules;
        let actor_id = decision.actor_id.value();
        let household_id = decision.household_id.value();
        let member_id = decision.member_id.value();
        let pool_id = decision.pool_id.value();
        let schedule_id = decision.schedule_id.value();

        for update in &decision.prior_updates {
            let end = update.ends_on.map(FundingScheduleDate::value);
            diesel::update(
                schedules::funding_schedules.filter(schedules::id.eq(update.id.value())),
            )
            .set((schedules::ends_on.eq(end), schedules::active.eq(false)))
            .execute(&mut *self.conn)
            .await?;
        }

        let starts_on = decision.starts_on.value();
        let ends_on = decision.ends_on.map(FundingScheduleDate::value);
        let (id, created_at) = diesel::insert_into(schedules::funding_schedules)
            .values((
                schedules::id.eq(schedule_id),
                schedules::household_id.eq(household_id),
                schedules::member_id.eq(member_id),
                schedules::pool_id.eq(pool_id),
                schedules::amount_yen.eq(decision.amount.value()),
                schedules::cadence.eq(decision.cadence.as_str()),
                schedules::starts_on.eq(starts_on),
                schedules::ends_on.eq(ends_on),
                schedules::active.eq(true),
            ))
            .returning((schedules::id, schedules::created_at))
            .get_result::<(Uuid, DateTime<Utc>)>(&mut *self.conn)
            .await?;

        insert_ui_change(
            self.conn,
            household_id,
            id,
            1_i64,
            actor_id,
            created_at,
            change,
        )
        .await?;

        Ok(PersistedFundingSchedule {
            id: id.to_string(),
            household_id: household_id.to_string(),
            member_id: member_id.to_string(),
            pool_id: pool_id.to_string(),
            amount_yen: decision.amount.value(),
            cadence: decision.cadence.as_str().to_owned(),
            starts_on: starts_on.format("%Y-%m-%d").to_string(),
            ends_on: ends_on.map(|date| date.format("%Y-%m-%d").to_string()),
            active: true,
            created_at,
        })
    }
}
