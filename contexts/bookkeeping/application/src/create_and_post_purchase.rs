use bookkeeping_domain::PositiveYen;

use crate::{
    AccountId, PostPurchaseCommand, PostPurchaseDecision, PostPurchaseError, PurchaseStatus,
    create_purchase::{
        AllocationRuleVersion, CreatePurchaseAllocation, CreatePurchaseCommand,
        CreatePurchaseDecision, CreatePurchaseError, CreatePurchaseItem, decide_create_purchase,
    },
    decide_post_purchase,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateAndPostPurchaseCommand {
    pub actor_id: String,
    pub household_id: String,
    pub idempotency_key: String,
    pub create_request_fingerprint: [u8; 32],
    pub post_request_fingerprint: [u8; 32],
    pub purchase: CreatePurchaseCommand,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseItemAllocationRequest {
    pub category_id: Option<String>,
    pub product_variant_id: Option<String>,
    pub allocations: Vec<RequestedPoolAllocation>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RequestedPoolAllocation {
    pub pool_id: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseAllocationContext {
    pub allocation_rule_version: u64,
    pub items: Vec<PurchaseItemAllocationContext>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseItemAllocationContext {
    pub category_id: Option<String>,
    pub allocations: Vec<ResolvedPurchaseAllocation>,
    pub allocation_override: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ResolvedPurchaseAllocation {
    pub pool_id: String,
    pub weight: i64,
    pub rule_version: u64,
    pub rule_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePurchasePersistence {
    pub household_id: String,
    pub actor_id: String,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatedPurchase {
    pub purchase_id: String,
    pub created: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct CreatedPurchaseState {
    pub revision: u64,
    pub status: PurchaseStatus,
    pub merchant: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ResolvedPostPurchasePayment {
    pub line_no: u32,
    pub account_id: AccountId,
    pub amount: PositiveYen,
    pub is_fallback: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LoadedPostPurchase {
    pub command: PostPurchaseCommand,
    pub resolved_payments: Vec<ResolvedPostPurchasePayment>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PostPurchasePersistence {
    pub household_id: String,
    pub actor_id: String,
    pub expected_revision: u64,
    pub ledger_description: String,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
    pub resolved_payments: Vec<ResolvedPostPurchasePayment>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PostedPurchase {
    pub purchase_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateAndPostPurchaseResult {
    pub purchase_id: String,
    pub created: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CreateAndPostPurchaseError<E> {
    CreateDecision(CreatePurchaseError),
    PostDecision(PostPurchaseError),
    AllocationContextMismatch,
    CreatedPurchaseMissing,
    Operations(E),
}

pub trait CreateAndPostPurchaseOperations {
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

    async fn load_purchase_state(
        &mut self,
        household_id: &str,
        purchase_id: &str,
    ) -> Result<Option<CreatedPurchaseState>, Self::Error>;

    async fn load_post_purchase(
        &mut self,
        household_id: &str,
        purchase_id: &str,
        expected_revision: u64,
    ) -> Result<LoadedPostPurchase, Self::Error>;

    async fn persist_post(
        &mut self,
        persistence: PostPurchasePersistence,
        decision: PostPurchaseDecision,
    ) -> Result<PostedPurchase, Self::Error>;
}

pub async fn create_and_post_purchase<O: CreateAndPostPurchaseOperations>(
    operations: &mut O,
    command: CreateAndPostPurchaseCommand,
) -> Result<CreateAndPostPurchaseResult, CreateAndPostPurchaseError<O::Error>> {
    operations
        .require_active_member(&command.household_id, &command.actor_id)
        .await
        .map_err(CreateAndPostPurchaseError::Operations)?;

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
                .map(|allocation| RequestedPoolAllocation {
                    pool_id: allocation.pool_id.clone(),
                    weight: allocation.weight,
                })
                .collect(),
        })
        .collect::<Vec<_>>();
    let allocation_context = operations
        .load_allocation_context(&command.household_id, &merchant_key, &allocation_inputs)
        .await
        .map_err(CreateAndPostPurchaseError::Operations)?;
    let purchase = apply_allocation_context(command.purchase, allocation_context)
        .map_err(|_| CreateAndPostPurchaseError::AllocationContextMismatch)?;
    let decision =
        decide_create_purchase(purchase).map_err(CreateAndPostPurchaseError::CreateDecision)?;

    let created = operations
        .persist_create(
            CreatePurchasePersistence {
                household_id: command.household_id.clone(),
                actor_id: command.actor_id.clone(),
                idempotency_key: command.idempotency_key.clone(),
                request_fingerprint: command.create_request_fingerprint,
            },
            decision,
        )
        .await
        .map_err(CreateAndPostPurchaseError::Operations)?;

    let state = operations
        .load_purchase_state(&command.household_id, &created.purchase_id)
        .await
        .map_err(CreateAndPostPurchaseError::Operations)?
        .ok_or(CreateAndPostPurchaseError::CreatedPurchaseMissing)?;
    if state.status != PurchaseStatus::Draft {
        return Ok(CreateAndPostPurchaseResult {
            purchase_id: created.purchase_id,
            created: created.created,
        });
    }

    let loaded = operations
        .load_post_purchase(&command.household_id, &created.purchase_id, state.revision)
        .await
        .map_err(CreateAndPostPurchaseError::Operations)?;
    let post_decision =
        decide_post_purchase(loaded.command).map_err(CreateAndPostPurchaseError::PostDecision)?;
    operations
        .persist_post(
            PostPurchasePersistence {
                household_id: command.household_id,
                actor_id: command.actor_id,
                expected_revision: state.revision,
                ledger_description: state.merchant,
                idempotency_key: command.idempotency_key,
                request_fingerprint: command.post_request_fingerprint,
                resolved_payments: loaded.resolved_payments,
            },
            post_decision,
        )
        .await
        .map_err(CreateAndPostPurchaseError::Operations)?;

    Ok(CreateAndPostPurchaseResult {
        purchase_id: created.purchase_id,
        created: created.created,
    })
}

fn apply_allocation_context(
    mut command: CreatePurchaseCommand,
    context: PurchaseAllocationContext,
) -> Result<CreatePurchaseCommand, ()> {
    if command.items.len() != context.items.len() {
        return Err(());
    }
    command.allocation_rule_version =
        AllocationRuleVersion::new(context.allocation_rule_version).ok_or(())?;
    for (item, resolved) in command.items.iter_mut().zip(context.items) {
        if item.category_id.is_none() {
            item.category_id = resolved.category_id;
        }
        item.allocation_override = resolved.allocation_override;
        item.allocations = resolved
            .allocations
            .into_iter()
            .map(|allocation| CreatePurchaseAllocation {
                pool_id: allocation.pool_id,
                weight: allocation.weight,
                rule_version: allocation.rule_version,
                rule_id: allocation.rule_id,
            })
            .collect();
    }
    Ok(command)
}

fn normalize_merchant(value: &str) -> String {
    value
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .to_lowercase()
}
