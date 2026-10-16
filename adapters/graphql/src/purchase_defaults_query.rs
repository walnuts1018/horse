use async_graphql::{Context, Error, ID, Result, SimpleObject};
use bookkeeping_application::purchase_defaults_query::{
    PurchaseDefaultsQuery, PurchaseDefaultsQueryError,
};

use crate::{Actor, DateTime};

#[derive(Clone, Debug, SimpleObject)]
pub struct PurchaseDefault {
    pub merchant: String,
    pub payment_account_id: ID,
    pub purchase_count: i32,
    pub last_used_at: DateTime,
}

pub async fn graphql_purchase_defaults(
    ctx: &Context<'_>,
    household_id: ID,
) -> Result<Vec<PurchaseDefault>> {
    let actor = ctx
        .data::<Actor>()
        .copied()
        .map_err(|_| Error::new("authentication required"))?;
    let household_id = crate::decode_household_id(&household_id)?;
    let rows = ctx
        .data::<std::sync::Arc<dyn PurchaseDefaultsQuery>>()?
        .query(actor.0, household_id)
        .await
        .map_err(map_query_error)?;
    rows.into_iter()
        .map(|row| {
            Ok(PurchaseDefault {
                merchant: row.merchant,
                payment_account_id: crate::catalog_mutations::encode_node_id(
                    "Account",
                    &row.payment_account_id.to_string(),
                )
                .map_err(|_| crate::internal_error("invalid account node ID"))?,
                purchase_count: row.purchase_count,
                last_used_at: row.last_used_at.into(),
            })
        })
        .collect()
}

fn map_query_error(error: PurchaseDefaultsQueryError) -> Error {
    use PurchaseDefaultsQueryError as E;
    match error {
        E::HouseholdNotFound => Error::new("household access denied"),
        E::CountOutOfRange => Error::new("purchase count exceeds GraphQL Int"),
        E::Infrastructure(_) => crate::internal_error(error),
    }
}
