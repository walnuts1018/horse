use async_graphql::{Context, ID, Result};
use horse_postgres_common::PgPool;
use household_postgres::{
    create_household as postgres_household, household_query as postgres_query,
};

use crate::{Actor, household_lifecycle};

pub async fn graphql_households(ctx: &Context<'_>) -> Result<Vec<household_lifecycle::Household>> {
    let actor = ctx
        .data::<Actor>()
        .copied()
        .map_err(|_| async_graphql::Error::new("authentication required"))?;
    load_households_for_actor(ctx.data::<PgPool>()?, actor.0).await
}

pub(crate) async fn load_households_for_actor(
    pool: &PgPool,
    actor_id: uuid::Uuid,
) -> Result<Vec<household_lifecycle::Household>> {
    let household_ids = postgres_query::list_household_ids(pool, actor_id)
        .await
        .map_err(crate::internal_error)?;
    let mut households = Vec::with_capacity(household_ids.len());
    for household_id in household_ids {
        let projection = postgres_household::load_household(pool, actor_id, household_id)
            .await
            .map_err(crate::internal_error)?;
        households.push(household_lifecycle::map_household(projection)?);
    }
    Ok(households)
}

pub async fn graphql_household(
    ctx: &Context<'_>,
    id: ID,
) -> Result<Option<household_lifecycle::Household>> {
    let actor = ctx
        .data::<Actor>()
        .copied()
        .map_err(|_| async_graphql::Error::new("authentication required"))?;
    let household_id = crate::decode_household_id(&id)?;
    match postgres_household::load_household(ctx.data::<PgPool>()?, actor.0, household_id).await {
        Ok(projection) => household_lifecycle::map_household(projection).map(Some),
        Err(
            postgres_household::CreateHouseholdPersistenceError::HouseholdNotFound
            | postgres_household::CreateHouseholdPersistenceError::MemberNotFound,
        ) => Ok(None),
        Err(error) => Err(crate::internal_error(error)),
    }
}
