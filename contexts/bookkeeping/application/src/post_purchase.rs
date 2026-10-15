use crate::{
    PostPurchaseCommand, PostPurchaseDecision, PostPurchaseError,
    create_and_post_purchase::{PostPurchasePersistence, ResolvedPostPurchasePayment},
    decide_post_purchase,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PostPurchaseWorkflowCommand {
    pub household_id: String,
    pub actor_id: String,
    pub purchase_id: String,
    pub expected_revision: u64,
    pub ledger_description: String,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LoadedPostPurchase {
    pub command: PostPurchaseCommand,
    pub resolved_payments: Vec<ResolvedPostPurchasePayment>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PostPurchaseWorkflowResult {
    pub purchase_id: String,
    pub journal_id: String,
    pub ledger_entry_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PostPurchaseWorkflowError<E> {
    Decision(PostPurchaseError),
    Operations(E),
}

pub trait PostPurchaseOperations {
    type Error;

    async fn load(
        &mut self,
        household_id: &str,
        purchase_id: &str,
        expected_revision: u64,
    ) -> Result<LoadedPostPurchase, Self::Error>;

    async fn persist(
        &mut self,
        persistence: PostPurchasePersistence,
        decision: PostPurchaseDecision,
    ) -> Result<PostPurchaseWorkflowResult, Self::Error>;
}

pub async fn post_purchase<O: PostPurchaseOperations>(
    operations: &mut O,
    command: PostPurchaseWorkflowCommand,
) -> Result<PostPurchaseWorkflowResult, PostPurchaseWorkflowError<O::Error>> {
    let loaded = operations
        .load(
            &command.household_id,
            &command.purchase_id,
            command.expected_revision,
        )
        .await
        .map_err(PostPurchaseWorkflowError::Operations)?;
    let decision =
        decide_post_purchase(loaded.command).map_err(PostPurchaseWorkflowError::Decision)?;
    operations
        .persist(
            PostPurchasePersistence {
                household_id: command.household_id,
                actor_id: command.actor_id,
                expected_revision: command.expected_revision,
                ledger_description: command.ledger_description,
                idempotency_key: command.idempotency_key,
                request_fingerprint: command.request_fingerprint,
                resolved_payments: loaded.resolved_payments,
            },
            decision,
        )
        .await
        .map_err(PostPurchaseWorkflowError::Operations)
}
