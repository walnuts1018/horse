use crate::post_purchase::{
    PostPurchaseWorkflowCommand, PostPurchaseWorkflowError, PostPurchaseWorkflowResult,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PostPurchaseEntryCommand {
    pub actor_id: String,
    pub purchase_id: String,
    pub expected_revision: u64,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PostPurchaseEntryResult {
    NotFound,
    AlreadyPosted(PostPurchaseWorkflowResult),
    Posted(PostPurchaseWorkflowResult),
}

pub trait PostPurchaseEntryOperations {
    type Error;

    async fn household_id(&mut self, purchase_id: &str) -> Result<Option<String>, Self::Error>;

    async fn is_active_member(
        &mut self,
        household_id: &str,
        actor_id: &str,
    ) -> Result<bool, Self::Error>;

    async fn find_idempotent(
        &mut self,
        household_id: &str,
        idempotency_key: &str,
        request_fingerprint: [u8; 32],
    ) -> Result<Option<PostPurchaseWorkflowResult>, Self::Error>;

    async fn ledger_description(
        &mut self,
        household_id: &str,
        purchase_id: &str,
    ) -> Result<String, Self::Error>;

    async fn post_purchase(
        &mut self,
        command: PostPurchaseWorkflowCommand,
    ) -> Result<PostPurchaseWorkflowResult, PostPurchaseWorkflowError<Self::Error>>;
}

pub async fn post_purchase_entry<O: PostPurchaseEntryOperations>(
    operations: &mut O,
    command: PostPurchaseEntryCommand,
) -> Result<PostPurchaseEntryResult, PostPurchaseWorkflowError<O::Error>> {
    let Some(household_id) = operations
        .household_id(&command.purchase_id)
        .await
        .map_err(PostPurchaseWorkflowError::Operations)?
    else {
        return Ok(PostPurchaseEntryResult::NotFound);
    };
    if !operations
        .is_active_member(&household_id, &command.actor_id)
        .await
        .map_err(PostPurchaseWorkflowError::Operations)?
    {
        return Ok(PostPurchaseEntryResult::NotFound);
    }
    if let Some(result) = operations
        .find_idempotent(
            &household_id,
            &command.idempotency_key,
            command.request_fingerprint,
        )
        .await
        .map_err(PostPurchaseWorkflowError::Operations)?
    {
        return Ok(PostPurchaseEntryResult::AlreadyPosted(result));
    }
    let ledger_description = operations
        .ledger_description(&household_id, &command.purchase_id)
        .await
        .map_err(PostPurchaseWorkflowError::Operations)?;
    operations
        .post_purchase(PostPurchaseWorkflowCommand {
            household_id,
            actor_id: command.actor_id,
            purchase_id: command.purchase_id,
            expected_revision: command.expected_revision,
            ledger_description,
            idempotency_key: command.idempotency_key,
            request_fingerprint: command.request_fingerprint,
        })
        .await
        .map(PostPurchaseEntryResult::Posted)
}
