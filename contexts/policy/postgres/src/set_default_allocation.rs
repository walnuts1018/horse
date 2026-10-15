use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use policy_application::set_default_allocation::{
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
    let household_id = parse_uuid(&validated.household_id, "household_id")?;
    let actor_id = parse_uuid(&validated.actor_id, "actor_id")?;
    let pool_ids = validated
        .weights
        .iter()
        .map(|weight| parse_uuid(&weight.pool_id, "pool_id"))
        .collect::<Result<Vec<_>, _>>()?;
    let expected_pool_ids = pool_ids.iter().copied().collect::<BTreeSet<_>>();
    if expected_pool_ids.len() != pool_ids.len() {
        return Err(SetDefaultAllocationPersistenceError::InvalidDecision);
    }

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SetDefaultAllocationPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedDefaultAllocation, SetDefaultAllocationPersistenceError, _>(
            async move |conn| {
                use public::allocation_defaults::dsl as defaults;
                use public::household_events::dsl as events;
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

                let member_exists = members::household_members
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq(actor_id))
                    .filter(members::left_at.is_null())
                    .select(members::member_id)
                    .first::<Uuid>(&mut *conn)
                    .await
                    .optional()?
                    .is_some();
                if !member_exists {
                    return Err(SetDefaultAllocationPersistenceError::MemberNotFound);
                }

                let scoped_pool_ids = pools::pools
                    .filter(pools::household_id.eq(household_id))
                    .filter(pools::id.eq_any(&pool_ids))
                    .select(pools::id)
                    .load::<Uuid>(&mut *conn)
                    .await?
                    .into_iter()
                    .collect::<BTreeSet<_>>();
                if scoped_pool_ids != expected_pool_ids {
                    return Err(SetDefaultAllocationPersistenceError::PoolNotFound);
                }

                let current = DefaultAllocationState {
                    revision: u64::try_from(state.0)
                        .map_err(|_| SetDefaultAllocationPersistenceError::InvalidDecision)?,
                    version: u64::try_from(state.1)
                        .map_err(|_| SetDefaultAllocationPersistenceError::InvalidDecision)?,
                };
                let decision = decide_set_default_allocation(validated, current)
                    .map_err(SetDefaultAllocationPersistenceError::InvalidWorkflow)?;
                validate_decision(&decision, household_id, actor_id)?;
                let revision = i64::try_from(decision.revision).map_err(|_| {
                    SetDefaultAllocationPersistenceError::NumericOutOfRange("revision")
                })?;
                let version = i64::try_from(decision.version).map_err(|_| {
                    SetDefaultAllocationPersistenceError::NumericOutOfRange("version")
                })?;

                for weight in &decision.weights {
                    let pool_id = parse_uuid(&weight.pool_id, "pool_id")?;
                    diesel::insert_into(defaults::allocation_defaults)
                        .values((
                            defaults::household_id.eq(household_id),
                            defaults::version.eq(version),
                            defaults::pool_id.eq(pool_id),
                            defaults::weight.eq(weight.weight),
                        ))
                        .execute(&mut *conn)
                        .await?;
                }

                diesel::update(households::households.filter(households::id.eq(household_id)))
                    .set((
                        households::default_allocation_version.eq(version),
                        households::revision.eq(revision),
                        households::updated_at.eq(now),
                    ))
                    .execute(&mut *conn)
                    .await?;

                let changed_fields = decision
                    .changed_fields
                    .iter()
                    .cloned()
                    .map(Some)
                    .collect::<Vec<_>>();
                diesel::insert_into(events::household_events)
                    .values((
                        events::household_id.eq(household_id),
                        events::event_type.eq("allocation.default.updated"),
                        events::entity_kind.eq("household"),
                        events::entity_id.eq(household_id),
                        events::revision.eq(version),
                        events::changed_fields.eq(changed_fields),
                        events::actor_id.eq(actor_id),
                    ))
                    .execute(&mut *conn)
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
    if parse_uuid(&decision.household_id, "household_id")? != household_id
        || parse_uuid(&decision.actor_id, "actor_id")? != actor_id
        || decision.weights.is_empty()
        || decision.changed_fields != ["defaultAllocation"]
        || decision.weights.iter().any(|weight| weight.weight <= 0)
    {
        return Err(SetDefaultAllocationPersistenceError::InvalidDecision);
    }
    Ok(())
}

fn parse_uuid(
    value: &str,
    field: &'static str,
) -> Result<Uuid, SetDefaultAllocationPersistenceError> {
    Uuid::parse_str(value).map_err(|_| SetDefaultAllocationPersistenceError::InvalidUuid(field))
}
