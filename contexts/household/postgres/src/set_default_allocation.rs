use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use horse_postgres_common::insert_ui_change;
use household_application::default_allocation::{
    DefaultAllocationState, SetDefaultAllocationCommand, SetDefaultAllocationDecision,
    SetDefaultAllocationError, SetDefaultAllocationOperations, SetDefaultAllocationResult,
    ValidatedDefaultAllocation, decide_set_default_allocation, set_default_allocation,
};
use std::collections::BTreeSet;
use uuid::Uuid;

use crate::{PgPool, schema::public};

pub type PersistedDefaultAllocation = SetDefaultAllocationResult;

#[derive(Debug)]
pub enum SetDefaultAllocationPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    NumericOutOfRange(&'static str),
    HouseholdNotFound,
    MemberNotFound,
    PoolNotFound,
    InvalidDecision,
    InvalidWorkflow(SetDefaultAllocationError),
}

impl From<DieselError> for SetDefaultAllocationPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_set_default_allocation(
    pool: &PgPool,
    command: SetDefaultAllocationCommand,
) -> Result<PersistedDefaultAllocation, SetDefaultAllocationPersistenceError> {
    let mut operations = DefaultAllocationOperations { pool };
    set_default_allocation(&mut operations, command).await
}

pub struct PostgresDefaultAllocationHandler {
    pool: PgPool,
}

impl PostgresDefaultAllocationHandler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl household_application::default_allocation::DefaultAllocationHandler
    for PostgresDefaultAllocationHandler
{
    fn set<'a>(
        &'a self,
        command: SetDefaultAllocationCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<
                        SetDefaultAllocationResult,
                        household_application::default_allocation::DefaultAllocationFailure,
                    >,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            persist_set_default_allocation(&self.pool, command)
                .await
                .map_err(|error| match error {
                    SetDefaultAllocationPersistenceError::InvalidUuid(_)
                    | SetDefaultAllocationPersistenceError::InvalidWorkflow(_) => {
                        household_application::default_allocation::DefaultAllocationFailure::Decision
                    }
                    SetDefaultAllocationPersistenceError::HouseholdNotFound
                    | SetDefaultAllocationPersistenceError::MemberNotFound
                    | SetDefaultAllocationPersistenceError::PoolNotFound => {
                        household_application::default_allocation::DefaultAllocationFailure::NotFound
                    }
                    SetDefaultAllocationPersistenceError::Pool(message) => {
                        household_application::default_allocation::DefaultAllocationFailure::Infrastructure(message)
                    }
                    SetDefaultAllocationPersistenceError::Database(error) => {
                        household_application::default_allocation::DefaultAllocationFailure::Infrastructure(error.to_string())
                    }
                    SetDefaultAllocationPersistenceError::NumericOutOfRange(_) => {
                        household_application::default_allocation::DefaultAllocationFailure::Infrastructure("numeric value is out of range".to_owned())
                    }
                    SetDefaultAllocationPersistenceError::InvalidDecision => {
                        household_application::default_allocation::DefaultAllocationFailure::Infrastructure("invalid allocation decision".to_owned())
                    }
                })
        })
    }
}

struct DefaultAllocationOperations<'a> {
    pool: &'a PgPool,
}

impl SetDefaultAllocationOperations for DefaultAllocationOperations<'_> {
    type Error = SetDefaultAllocationPersistenceError;

    async fn persist(
        &mut self,
        command: ValidatedDefaultAllocation,
    ) -> Result<SetDefaultAllocationResult, Self::Error> {
        persist_validated(self.pool, command).await
    }
}

impl From<SetDefaultAllocationError> for SetDefaultAllocationPersistenceError {
    fn from(error: SetDefaultAllocationError) -> Self {
        Self::InvalidWorkflow(error)
    }
}

async fn persist_validated(
    pool: &PgPool,
    validated: ValidatedDefaultAllocation,
) -> Result<PersistedDefaultAllocation, SetDefaultAllocationPersistenceError> {
    let household_id = validated.household_id();
    let actor_id = validated.actor_id();
    let pool_ids = validated
        .weights()
        .iter()
        .map(|weight| weight.pool_id.value())
        .collect::<Vec<_>>();
    let expected_pool_ids = pool_ids.iter().copied().collect::<BTreeSet<_>>();
    if expected_pool_ids.len() != pool_ids.len() {
        return Err(SetDefaultAllocationPersistenceError::InvalidDecision);
    }

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SetDefaultAllocationPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedDefaultAllocation, SetDefaultAllocationPersistenceError, _>(
            async move |conn| {
                use public::allocation_defaults::dsl as defaults;
                use public::household_members::dsl as members;
                use public::households::dsl as households;
                use public::pools::dsl as pools;

                let state = households::households
                    .filter(households::id.eq(household_id))
                    .for_update()
                    .select((households::revision, households::default_allocation_version))
                    .first::<(i64, i64)>(conn)
                    .await
                    .optional()?
                    .ok_or(SetDefaultAllocationPersistenceError::HouseholdNotFound)?;

                let actor_role = members::household_members
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq(actor_id))
                    .filter(members::left_at.is_null())
                    .select(members::role)
                    .first::<String>(conn)
                    .await
                    .optional()?;
                if authorize(actor_role.as_deref(), Operation::ManageHousehold).is_err() {
                    return Err(SetDefaultAllocationPersistenceError::MemberNotFound);
                }

                let scoped_pool_ids = pools::pools
                    .filter(pools::household_id.eq(household_id))
                    .filter(pools::id.eq_any(&pool_ids))
                    .select(pools::id)
                    .load::<Uuid>(conn)
                    .await?
                    .into_iter()
                    .collect::<BTreeSet<_>>();
                if scoped_pool_ids != expected_pool_ids {
                    return Err(SetDefaultAllocationPersistenceError::PoolNotFound);
                }

                let current = DefaultAllocationState {
                    revision: u64::try_from(state.0)
                        .map_err(|_| SetDefaultAllocationPersistenceError::InvalidDecision)?
                        .try_into()
                        .map_err(|_| SetDefaultAllocationPersistenceError::InvalidDecision)?,
                    version: u64::try_from(state.1)
                        .map_err(|_| SetDefaultAllocationPersistenceError::InvalidDecision)?
                        .try_into()
                        .map_err(|_| SetDefaultAllocationPersistenceError::InvalidDecision)?,
                };
                let decision = decide_set_default_allocation(validated, current)
                    .map_err(SetDefaultAllocationPersistenceError::InvalidWorkflow)?;
                validate_decision(&decision, household_id, actor_id)?;
                let revision = i64::try_from(decision.revision.value()).map_err(|_| {
                    SetDefaultAllocationPersistenceError::NumericOutOfRange("revision")
                })?;
                let version = i64::try_from(decision.version.value()).map_err(|_| {
                    SetDefaultAllocationPersistenceError::NumericOutOfRange("version")
                })?;

                for weight in &decision.weights {
                    let pool_id = weight.pool_id.value();
                    diesel::insert_into(defaults::allocation_defaults)
                        .values((
                            defaults::household_id.eq(household_id),
                            defaults::version.eq(version),
                            defaults::pool_id.eq(pool_id),
                            defaults::weight.eq(weight.weight.value()),
                        ))
                        .execute(conn)
                        .await?;
                }

                diesel::update(households::households.filter(households::id.eq(household_id)))
                    .set((
                        households::default_allocation_version.eq(version),
                        households::revision.eq(revision),
                        households::updated_at.eq(now),
                    ))
                    .execute(conn)
                    .await?;

                insert_ui_change(
                    conn,
                    household_id,
                    household_id,
                    version,
                    actor_id,
                    chrono::Utc::now(),
                    household_application::ui_changes::default_allocation_updated(
                        &decision.changed_fields,
                    ),
                )
                .await?;

                Ok(PersistedDefaultAllocation {
                    revision: decision.revision,
                    version: decision.version,
                })
            },
        )
        .await
}

fn validate_decision(
    decision: &SetDefaultAllocationDecision,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<(), SetDefaultAllocationPersistenceError> {
    if decision.household_id.value() != household_id
        || decision.actor_id.value() != actor_id
        || decision.weights.is_empty()
        || decision.changed_fields != ["defaultAllocation"]
    {
        return Err(SetDefaultAllocationPersistenceError::InvalidDecision);
    }
    Ok(())
}
