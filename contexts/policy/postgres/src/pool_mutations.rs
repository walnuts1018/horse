use chrono::Utc;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use household_postgres::default_allocation_snapshot::{
    AddPoolToDefaultAllocationError, add_pool_to_default_allocation,
};
use policy_application::create_pool::{CreatePoolCommand, decide_create_pool};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum CreatePoolMutationError {
    Pool(String),
    Database(DieselError),
    InvalidWorkflow(policy_application::create_pool::CreatePoolError),
    HouseholdNotFound,
    MemberNotFound,
    NumericOutOfRange(&'static str),
}

impl From<DieselError> for CreatePoolMutationError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_create_pool(
    pool: &PgPool,
    command: CreatePoolCommand,
) -> Result<Uuid, CreatePoolMutationError> {
    let decision = decide_create_pool(command).map_err(CreatePoolMutationError::InvalidWorkflow)?;
    let actor_id = decision.actor_id.value();
    let household_id = decision.household_id.value();
    let pool_id = decision.pool_id.value();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CreatePoolMutationError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<Uuid, CreatePoolMutationError, _>(async move |conn| {
            public::households::table
                .filter(public::households::id.eq(household_id))
                .select(public::households::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .ok_or(CreatePoolMutationError::HouseholdNotFound)?;
            let role = public::household_members::table
                .filter(public::household_members::household_id.eq(household_id))
                .filter(public::household_members::member_id.eq(actor_id))
                .filter(public::household_members::left_at.is_null())
                .select(public::household_members::role)
                .first::<String>(&mut *conn)
                .await
                .optional()?;
            if authorize(role.as_deref(), Operation::ManageHousehold).is_err() {
                return Err(CreatePoolMutationError::MemberNotFound);
            }
            diesel::insert_into(public::pools::table)
                .values((
                    public::pools::id.eq(pool_id),
                    public::pools::household_id.eq(household_id),
                    public::pools::name.eq(&decision.name),
                    public::pools::kind.eq(match decision.kind {
                        policy_application::create_pool::PoolKind::Personal => "PERSONAL",
                        policy_application::create_pool::PoolKind::Shared => "SHARED",
                    }),
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
            let (_, version) = add_pool_to_default_allocation(&mut *conn, household_id, pool_id)
                .await
                .map_err(|error| match error {
                    AddPoolToDefaultAllocationError::Database(error) => {
                        CreatePoolMutationError::Database(error)
                    }
                    AddPoolToDefaultAllocationError::NumericOutOfRange => {
                        CreatePoolMutationError::NumericOutOfRange("household defaults")
                    }
                    AddPoolToDefaultAllocationError::HouseholdNotFound => {
                        CreatePoolMutationError::HouseholdNotFound
                    }
                })?;
            horse_postgres_common::insert_ui_change(
                &mut *conn,
                household_id,
                pool_id,
                version,
                actor_id,
                Utc::now(),
                policy_application::ui_changes::pool_created(),
            )
            .await?;
            Ok(pool_id)
        })
        .await
}

pub struct PostgresCreatePoolHandler {
    pool: PgPool,
}

impl PostgresCreatePoolHandler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl policy_application::create_pool::CreatePoolHandler for PostgresCreatePoolHandler {
    fn create<'a>(
        &'a self,
        command: CreatePoolCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<Uuid, policy_application::WorkflowFailure>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            persist_create_pool(&self.pool, command)
                .await
                .map_err(|error| match error {
                    CreatePoolMutationError::InvalidWorkflow(_) => {
                        policy_application::WorkflowFailure::Decision
                    }
                    CreatePoolMutationError::HouseholdNotFound
                    | CreatePoolMutationError::MemberNotFound => {
                        policy_application::WorkflowFailure::NotFound
                    }
                    CreatePoolMutationError::Pool(message) => {
                        policy_application::WorkflowFailure::Infrastructure(message)
                    }
                    CreatePoolMutationError::Database(error) => {
                        policy_application::WorkflowFailure::Infrastructure(error.to_string())
                    }
                    CreatePoolMutationError::NumericOutOfRange(_) => {
                        policy_application::WorkflowFailure::Infrastructure(
                            "numeric value is out of range".to_owned(),
                        )
                    }
                })
        })
    }
}
