use async_graphql::{Context, ID, Result, SimpleObject};
use uuid::Uuid;

use crate::{Actor, household_lifecycle, household_query};

#[derive(Clone, Debug)]
pub struct ViewerIdentity {
    pub member_id: Uuid,
    pub subject: String,
    pub display_name: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct Viewer {
    id: ID,
    subject: String,
    display_name: String,
    households: Vec<household_lifecycle::Household>,
}

pub async fn graphql_viewer(ctx: &Context<'_>) -> Result<Viewer> {
    let identity = ctx
        .data::<ViewerIdentity>()
        .cloned()
        .map_err(|_| async_graphql::Error::new("authentication required"))?;
    let actor = ctx
        .data::<Actor>()
        .copied()
        .map_err(|_| async_graphql::Error::new("authentication required"))?;
    if identity.member_id != actor.0 {
        return Err(crate::internal_error(
            "authenticated viewer identity mismatch",
        ));
    }
    let households =
        household_query::load_households_for_actor(ctx.data::<horse_postgres::PgPool>()?, actor.0)
            .await?;
    Ok(Viewer {
        id: ID(identity.member_id.to_string()),
        subject: identity.subject,
        display_name: identity.display_name,
        households,
    })
}
