use crate::{
    Actor,
    purchase_mutations::{self, PurchaseCorrectionConnection},
};
use async_graphql::{Context, Error, ID, Result};
use cross_context_postgres::purchase_command_result;
use cross_context_postgres::purchase_command_result::PurchaseCommandResultError;
use horse_postgres_common::PgPool;
use household_postgres::household_access;

pub async fn graphql_purchase_corrections(
    ctx: &Context<'_>,
    purchase_id: ID,
    first: Option<i32>,
    after: Option<String>,
) -> Result<PurchaseCorrectionConnection> {
    let actor = ctx
        .data::<Actor>()
        .copied()
        .map_err(|_| Error::new("authentication required"))?;
    let purchase_id = purchase_mutations::decode_purchase_id(&purchase_id)?;
    let pool = ctx.data::<PgPool>()?;
    let household_id = purchase_command_result::load_purchase_household_id(pool, purchase_id)
        .await
        .map_err(map_purchase_error)?
        .ok_or_else(not_found)?;
    if !household_access::is_active_member(pool, household_id, actor.0)
        .await
        .map_err(crate::internal_error)?
    {
        return Err(not_found());
    }
    purchase_mutations::purchase_corrections_connection(
        pool,
        household_id,
        purchase_id,
        first,
        after.as_deref(),
    )
    .await
}

fn map_purchase_error(error: PurchaseCommandResultError) -> Error {
    match error {
        PurchaseCommandResultError::PurchaseNotFound => not_found(),
        PurchaseCommandResultError::Pool(_) | PurchaseCommandResultError::Database(_) => {
            crate::internal_error(error)
        }
        PurchaseCommandResultError::NumericOutOfRange
        | PurchaseCommandResultError::InvalidPurchaseStatus => crate::internal_error(error),
    }
}

fn not_found() -> Error {
    Error::new("purchase not found")
}
