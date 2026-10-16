use diesel::{ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use household_application::household_query::{HouseholdQuery, HouseholdQueryError};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum HouseholdQueryPersistenceError {
    Pool(String),
    Database(diesel::result::Error),
}

impl From<diesel::result::Error> for HouseholdQueryPersistenceError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

pub async fn list_household_ids(
    pool: &PgPool,
    actor_id: Uuid,
) -> Result<Vec<Uuid>, HouseholdQueryPersistenceError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| HouseholdQueryPersistenceError::Pool(error.to_string()))?;
    use public::household_members;

    household_members::table
        .filter(household_members::member_id.eq(actor_id))
        .filter(household_members::left_at.is_null())
        .order_by(household_members::joined_at.asc())
        .select(household_members::household_id)
        .load::<Uuid>(&mut *conn)
        .await
        .map_err(Into::into)
}

#[derive(Debug, Clone)]
pub struct PostgresHouseholdQuery {
    pool: PgPool,
}

impl PostgresHouseholdQuery {
    pub const fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl HouseholdQuery for PostgresHouseholdQuery {
    fn list_household_ids<'a>(
        &'a self,
        actor_id: Uuid,
    ) -> std::pin::Pin<
        Box<dyn std::future::Future<Output = Result<Vec<Uuid>, HouseholdQueryError>> + Send + 'a>,
    > {
        Box::pin(async move {
            list_household_ids(&self.pool, actor_id)
                .await
                .map_err(|error| HouseholdQueryError::Infrastructure(format!("{error:?}")))
        })
    }

    fn load_household<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<
                        household_application::household_query::HouseholdReadModel,
                        HouseholdQueryError,
                    >,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            crate::create_household::load_household_workflow(&self.pool, actor_id, household_id)
                .await
                .map_err(|error| match error {
                    household_application::workflow_failure::WorkflowFailure::NotFound => {
                        HouseholdQueryError::HouseholdNotFound
                    }
                    household_application::workflow_failure::WorkflowFailure::Decision(_)
                    | household_application::workflow_failure::WorkflowFailure::Conflict
                    | household_application::workflow_failure::WorkflowFailure::Infrastructure(_) => {
                        HouseholdQueryError::Infrastructure(format!("{error:?}"))
                    }
                })
        })
    }
}
