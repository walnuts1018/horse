use async_trait::async_trait;
use bookkeeping_application::workflow_failure::WorkflowFailure;
use bookkeeping_postgres::{
    bulk_update_purchases::PersistBulkUpdatePurchases,
    save_purchase_item_edits::PersistSavePurchaseItemEdits,
};
use cross_context_application::purchase_bulk_mutations as application;
use uuid::Uuid;

use crate::{PgPool, purchase_command_result};

pub struct PostgresPurchaseBulkMutationHandler {
    pool: PgPool,
}

impl PostgresPurchaseBulkMutationHandler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl application::PurchaseBulkMutationHandler for PostgresPurchaseBulkMutationHandler {
    async fn authorize_purchase(
        &self,
        actor_id: Uuid,
        purchase_id: Uuid,
    ) -> Result<Uuid, application::PurchaseBulkMutationFailure> {
        let household_id =
            purchase_command_result::load_purchase_household_id(&self.pool, purchase_id)
                .await
                .map_err(|error| {
                    application::PurchaseBulkMutationFailure::Infrastructure(format!("{error:?}"))
                })?
                .ok_or(application::PurchaseBulkMutationFailure::NotFound)?;
        require_member(&self.pool, household_id, actor_id).await?;
        Ok(household_id)
    }

    async fn bulk_update_purchases(
        &self,
        request: application::BulkUpdatePurchasesRequest,
    ) -> Result<application::BulkUpdatePurchasesResult, application::PurchaseBulkMutationFailure>
    {
        let Some(first_id) = request
            .commands
            .first()
            .map(|command| parse_uuid(&command.purchase_id))
            .transpose()?
        else {
            return Err(application::PurchaseBulkMutationFailure::InvalidInput);
        };
        let household_id =
            purchase_command_result::load_purchase_household_id(&self.pool, first_id)
                .await
                .map_err(|error| {
                    application::PurchaseBulkMutationFailure::Infrastructure(format!("{error:?}"))
                })?
                .ok_or(application::PurchaseBulkMutationFailure::NotFound)?;
        require_member(&self.pool, household_id, request.actor_id).await?;
        let ids = crate::reference_data::persist_bulk_update_purchases(
            &self.pool,
            PersistBulkUpdatePurchases {
                household_id: household_id.to_string(),
                actor_id: request.actor_id.to_string(),
            },
            request.commands,
        )
        .await
        .map_err(map_workflow_failure)?;
        let purchase_ids = ids
            .iter()
            .map(|id| parse_uuid(id))
            .collect::<Result<Vec<_>, _>>()?;
        Ok(application::BulkUpdatePurchasesResult {
            household_id,
            purchase_ids,
        })
    }

    async fn save_purchase_item_edits(
        &self,
        request: application::SavePurchaseItemEditsRequest,
    ) -> Result<application::SavePurchaseItemEditsResult, application::PurchaseBulkMutationFailure>
    {
        require_member(&self.pool, request.household_id, request.actor_id).await?;
        let ids = crate::reference_data::persist_save_purchase_item_edits(
            &self.pool,
            PersistSavePurchaseItemEdits {
                household_id: request.household_id.to_string(),
                actor_id: request.actor_id.to_string(),
                purchase_id: request.purchase_id.to_string(),
            },
            request.items,
            request.purchase_patch,
        )
        .await
        .map_err(map_workflow_failure)?;
        let item_ids = ids
            .iter()
            .map(|id| parse_uuid(id))
            .collect::<Result<Vec<_>, _>>()?;
        Ok(application::SavePurchaseItemEditsResult {
            household_id: request.household_id,
            item_ids,
        })
    }
}

async fn require_member(
    pool: &PgPool,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<(), application::PurchaseBulkMutationFailure> {
    let is_member = crate::household_access::is_active_member(pool, household_id, actor_id)
        .await
        .map_err(|error| {
            application::PurchaseBulkMutationFailure::Infrastructure(format!("{error:?}"))
        })?;
    if is_member {
        Ok(())
    } else {
        Err(application::PurchaseBulkMutationFailure::NotFound)
    }
}

fn map_workflow_failure<E: std::fmt::Debug>(
    failure: WorkflowFailure<bookkeeping_application::workflow_failure::WorkflowRejection, E>,
) -> application::PurchaseBulkMutationFailure {
    match failure {
        WorkflowFailure::Decision(_) => application::PurchaseBulkMutationFailure::InvalidInput,
        WorkflowFailure::NotFound => application::PurchaseBulkMutationFailure::NotFound,
        WorkflowFailure::Conflict => application::PurchaseBulkMutationFailure::Conflict,
        WorkflowFailure::Infrastructure(error) => {
            application::PurchaseBulkMutationFailure::Infrastructure(format!("{error:?}"))
        }
    }
}

fn parse_uuid(value: &str) -> Result<Uuid, application::PurchaseBulkMutationFailure> {
    Uuid::parse_str(value).map_err(|_| application::PurchaseBulkMutationFailure::InvalidInput)
}
