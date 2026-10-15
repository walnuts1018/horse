use chrono::Utc;
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use bookkeeping_workflows::create_pool::{CreatePoolCommand, decide_create_pool};

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedCreatePool {
    pub pool_id: String,
}

#[derive(Debug)]
pub enum CreatePoolPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidDecision,
    HouseholdNotFound,
    NumericOutOfRange(&'static str),
}

impl From<DieselError> for CreatePoolPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_create_pool(
    pool: &PgPool,
    command: CreatePoolCommand,
) -> Result<PersistedCreatePool, CreatePoolPersistenceError> {
    let decision =
        decide_create_pool(command).map_err(|_| CreatePoolPersistenceError::InvalidDecision)?;
    let actor_id = parse_uuid(&decision.actor_id, "actor_id")?;
    let household_id = parse_uuid(&decision.household_id, "household_id")?;
    let pool_id = parse_uuid(&decision.pool_id, "pool_id")?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CreatePoolPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedCreatePool, CreatePoolPersistenceError, _>(async |conn| {
            use public::allocation_defaults::dsl as defaults;
            use public::households::dsl as households;

            let (current_revision, current_version) = households::households
                .filter(households::id.eq(household_id))
                .for_update()
                .select((households::revision, households::default_allocation_version))
                .first::<(i64, i64)>(conn)
                .await
                .optional()?
                .ok_or(CreatePoolPersistenceError::HouseholdNotFound)?;
            let revision = current_revision
                .checked_add(1)
                .ok_or(CreatePoolPersistenceError::NumericOutOfRange("revision"))?;
            let version = current_version.checked_add(1).ok_or(
                CreatePoolPersistenceError::NumericOutOfRange("default_allocation_version"),
            )?;

            diesel::insert_into(public::pools::table)
                .values((
                    public::pools::id.eq(pool_id),
                    public::pools::household_id.eq(household_id),
                    public::pools::name.eq(&decision.name),
                    public::pools::kind.eq(&decision.kind),
                ))
                .execute(&mut *conn)
                .await?;

            diesel::insert_into(public::pool_funding_rules::table)
                .values((
                    public::pool_funding_rules::household_id.eq(household_id),
                    public::pool_funding_rules::pool_id.eq(pool_id),
                    public::pool_funding_rules::version.eq(1_i32),
                    public::pool_funding_rules::created_by.eq(actor_id),
                ))
                .execute(&mut *conn)
                .await?;

            let previous_weights = defaults::allocation_defaults
                .filter(defaults::household_id.eq(household_id))
                .filter(defaults::version.eq(current_version))
                .select((defaults::pool_id, defaults::weight))
                .load::<(Uuid, i64)>(&mut *conn)
                .await?;
            let values = previous_weights
                .into_iter()
                .map(|(previous_pool_id, weight)| {
                    (
                        defaults::household_id.eq(household_id),
                        defaults::version.eq(version),
                        defaults::pool_id.eq(previous_pool_id),
                        defaults::weight.eq(weight),
                    )
                })
                .collect::<Vec<_>>();
            if !values.is_empty() {
                diesel::insert_into(defaults::allocation_defaults)
                    .values(values)
                    .execute(&mut *conn)
                    .await?;
            }
            diesel::insert_into(defaults::allocation_defaults)
                .values((
                    defaults::household_id.eq(household_id),
                    defaults::version.eq(version),
                    defaults::pool_id.eq(pool_id),
                    defaults::weight.eq(1_i64),
                ))
                .execute(&mut *conn)
                .await?;

            diesel::update(households::households.filter(households::id.eq(household_id)))
                .set((
                    households::default_allocation_version.eq(version),
                    households::revision.eq(revision),
                    households::updated_at.eq(now),
                ))
                .execute(&mut *conn)
                .await?;

            diesel::insert_into(public::household_events::table)
                .values((
                    public::household_events::household_id.eq(household_id),
                    public::household_events::event_type.eq("pool.created"),
                    public::household_events::entity_kind.eq("pool"),
                    public::household_events::entity_id.eq(pool_id),
                    public::household_events::revision.eq(version),
                    public::household_events::changed_fields
                        .eq(vec![Some("name".to_owned()), Some("kind".to_owned())]),
                    public::household_events::actor_id.eq(actor_id),
                    public::household_events::created_at.eq(Utc::now()),
                ))
                .execute(&mut *conn)
                .await?;

            Ok(PersistedCreatePool {
                pool_id: pool_id.to_string(),
            })
        })
        .await
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, CreatePoolPersistenceError> {
    Uuid::parse_str(value).map_err(|_| CreatePoolPersistenceError::InvalidUuid(field))
}
