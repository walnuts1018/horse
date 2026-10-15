use crate::{
    create_and_post_purchase::{
        CreatePurchasePersistence, CreatedPurchase, PurchaseAllocationContext,
        PurchaseItemAllocationRequest, apply_allocation_context, normalize_merchant,
    },
    create_purchase::{
        CreatePurchaseCommand, CreatePurchaseDecision, CreatePurchaseError, decide_create_purchase,
    },
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePurchaseWorkflowCommand {
    pub household_id: String,
    pub actor_id: String,
    pub purchase: CreatePurchaseCommand,
    pub persistence: CreatePurchasePersistence,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CreatePurchaseWorkflowError<E> {
    Decision(CreatePurchaseError),
    AllocationContextMismatch,
    Operations(E),
}

pub trait CreatePurchaseOperations {
    type Error;

    async fn require_active_member(
        &mut self,
        household_id: &str,
        actor_id: &str,
    ) -> Result<(), Self::Error>;

    async fn load_allocation_context(
        &mut self,
        household_id: &str,
        merchant_key: &str,
        items: &[PurchaseItemAllocationRequest],
    ) -> Result<PurchaseAllocationContext, Self::Error>;

    async fn persist_create(
        &mut self,
        persistence: CreatePurchasePersistence,
        decision: CreatePurchaseDecision,
    ) -> Result<CreatedPurchase, Self::Error>;
}

pub async fn create_purchase<O: CreatePurchaseOperations>(
    operations: &mut O,
    command: CreatePurchaseWorkflowCommand,
) -> Result<CreatedPurchase, CreatePurchaseWorkflowError<O::Error>> {
    operations
        .require_active_member(&command.household_id, &command.actor_id)
        .await
        .map_err(CreatePurchaseWorkflowError::Operations)?;

    let merchant_key = normalize_merchant(&command.purchase.merchant);
    let allocation_inputs = command
        .purchase
        .items
        .iter()
        .map(|item| PurchaseItemAllocationRequest {
            category_id: item.category_id.clone(),
            product_variant_id: item.product_variant_id.clone(),
            allocations: item
                .allocations
                .iter()
                .map(|allocation| crate::create_and_post_purchase::RequestedPoolAllocation {
                    pool_id: allocation.pool_id.clone(),
                    weight: allocation.weight,
                })
                .collect(),
        })
        .collect::<Vec<_>>();
    let context = operations
        .load_allocation_context(&command.household_id, &merchant_key, &allocation_inputs)
        .await
        .map_err(CreatePurchaseWorkflowError::Operations)?;
    let purchase = apply_allocation_context(command.purchase, context)
        .map_err(|_| CreatePurchaseWorkflowError::AllocationContextMismatch)?;
    let decision = decide_create_purchase(purchase).map_err(CreatePurchaseWorkflowError::Decision)?;
    operations
        .persist_create(command.persistence, decision)
        .await
        .map_err(CreatePurchaseWorkflowError::Operations)
}
