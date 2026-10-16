use async_graphql::{Context, ID, Result};
use household_application::household_query::{HouseholdQuery, HouseholdQueryError};
use std::sync::Arc;

use crate::{Actor, household_lifecycle};

pub async fn graphql_households(ctx: &Context<'_>) -> Result<Vec<household_lifecycle::Household>> {
    let actor = ctx
        .data::<Actor>()
        .copied()
        .map_err(|_| async_graphql::Error::new("authentication required"))?;
    load_households_for_actor(ctx.data::<Arc<dyn HouseholdQuery>>()?.as_ref(), actor.0).await
}

pub(crate) async fn load_households_for_actor(
    query: &dyn HouseholdQuery,
    actor_id: uuid::Uuid,
) -> Result<Vec<household_lifecycle::Household>> {
    let household_ids = query
        .list_household_ids(actor_id)
        .await
        .map_err(crate::internal_error)?;
    let mut households = Vec::with_capacity(household_ids.len());
    for household_id in household_ids {
        let projection = query
            .load_household(actor_id, household_id)
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
    match ctx
        .data::<Arc<dyn HouseholdQuery>>()?
        .load_household(actor.0, household_id)
        .await
    {
        Ok(projection) => household_lifecycle::map_household(projection).map(Some),
        Err(HouseholdQueryError::HouseholdNotFound) => Ok(None),
        Err(HouseholdQueryError::Infrastructure(error)) => Err(crate::internal_error(error)),
    }
}
