use bookkeeping_domain::purchase_commands::{
    AllocatedPurchase, AllocatedPurchaseItem, AllocationRuleId, AllocationRuleVersion, CategoryId,
    CreatePurchaseAllocation, CreatePurchaseCommand, CreatePurchaseDecision, CreatePurchaseError,
    PurchaseAllocationWeight, decide_create_purchase,
};
use bookkeeping_domain::{PoolId, PositiveYen};
use chrono::DateTime;
use uuid::Uuid;

use crate::{
    AccountId, PostPurchaseCommand, PostPurchaseDecision, PostPurchaseError, PurchaseLifecycle,
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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatedPurchaseState {
    pub revision: u64,
    pub lifecycle: PurchaseLifecycle,
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

    async fn lock_accounting_period(
        &mut self,
        household_id: &str,
        occurred_at: DateTime<chrono::Utc>,
    ) -> Result<(), Self::Error>;

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
    let occurred_at = command.purchase.occurred_at.value();
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
            category_id: item.category_id.map(|id| id.value().to_string()),
            product_variant_id: item.product_variant_id.map(|id| id.value().to_string()),
            allocations: item
                .requested_allocations
                .iter()
                .map(|allocation| RequestedPoolAllocation {
                    pool_id: allocation.pool_id.as_str().to_owned(),
                    weight: allocation.weight.value(),
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

    operations
        .lock_accounting_period(&command.household_id, occurred_at)
        .await
        .map_err(CreateAndPostPurchaseError::Operations)?;

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
    if state.lifecycle != PurchaseLifecycle::Draft {
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

pub(crate) fn apply_allocation_context(
    command: CreatePurchaseCommand,
    context: PurchaseAllocationContext,
) -> Result<AllocatedPurchase, ()> {
    if command.items.len() != context.items.len() {
        return Err(());
    }
    let allocation_rule_version =
        AllocationRuleVersion::new(context.allocation_rule_version).ok_or(())?;
    let items = command
        .items
        .iter()
        .zip(context.items)
        .map(|(item, resolved)| {
            let category_id = if item.category_id.is_none() {
                resolved
                    .category_id
                    .as_deref()
                    .map(Uuid::parse_str)
                    .transpose()
                    .map_err(|_| ())?
                    .map(CategoryId::new)
            } else {
                item.category_id
            };
            let allocations = resolved
                .allocations
                .into_iter()
                .map(|allocation| {
                    Ok(CreatePurchaseAllocation {
                        pool_id: PoolId::new(allocation.pool_id).map_err(|_| ())?,
                        weight: PurchaseAllocationWeight::new(allocation.weight).ok_or(())?,
                        rule_version: AllocationRuleVersion::new(allocation.rule_version)
                            .ok_or(())?,
                        rule_id: allocation
                            .rule_id
                            .as_deref()
                            .map(Uuid::parse_str)
                            .transpose()
                            .map_err(|_| ())?
                            .map(AllocationRuleId::new),
                    })
                })
                .collect::<Result<Vec<_>, ()>>()?;
            Ok(AllocatedPurchaseItem {
                category_id,
                allocations,
                allocation_override: resolved.allocation_override,
            })
        })
        .collect::<Result<Vec<_>, ()>>()?;
    AllocatedPurchase::resolve(command, allocation_rule_version, items).map_err(|_| ())
}

pub(crate) fn normalize_merchant(value: &str) -> String {
    value
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .to_lowercase()
}
