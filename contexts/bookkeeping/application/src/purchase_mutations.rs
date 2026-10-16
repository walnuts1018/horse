use async_trait::async_trait;
use chrono::{DateTime, Utc};
use uuid::Uuid;

use crate::{
    create_and_post_purchase::{
        CreateAndPostPurchaseCommand, CreateAndPostPurchaseResult, CreatedPurchase,
    },
    create_purchase_workflow::CreatePurchaseWorkflowCommand,
    post_purchase_entry::{PostPurchaseEntryCommand, PostPurchaseEntryResult},
    workflow_failure::{WorkflowFailure, WorkflowRejection},
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PostPurchaseEntryOutput {
    pub household_id: Option<Uuid>,
    pub result: PostPurchaseEntryResult,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectPurchaseCommand {
    pub household_id: Uuid,
    pub actor_id: Uuid,
    pub purchase_id: Uuid,
    pub item_id: Option<Uuid>,
    pub amount_delta_yen: Option<i64>,
    pub reason: String,
    pub allocations: Vec<(Uuid, i64)>,
    pub payments: Vec<(Uuid, i64)>,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
    pub source_document_id: Option<Uuid>,
    pub source_reference: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SharedPurchaseDraft {
    pub id: Uuid,
    pub household_id: Uuid,
    pub revision: i64,
    pub payload: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SharedPurchaseDraftUpdate {
    pub draft: SharedPurchaseDraft,
    pub conflict: bool,
}

#[derive(Debug)]
pub enum SharedPurchaseDraftError {
    BadInput,
    NotFound,
    Infrastructure(String),
}

#[async_trait]
pub trait PurchaseMutationHandler: Send + Sync {
    async fn create_purchase(
        &self,
        command: CreatePurchaseWorkflowCommand,
    ) -> Result<CreatedPurchase, WorkflowFailure<WorkflowRejection, String>>;

    async fn create_and_post_purchase(
        &self,
        command: CreateAndPostPurchaseCommand,
    ) -> Result<CreateAndPostPurchaseResult, WorkflowFailure<WorkflowRejection, String>>;

    async fn post_purchase(
        &self,
        command: PostPurchaseEntryCommand,
    ) -> Result<PostPurchaseEntryOutput, WorkflowFailure<WorkflowRejection, String>>;

    async fn correct_purchase(
        &self,
        command: CorrectPurchaseCommand,
    ) -> Result<(), WorkflowFailure<WorkflowRejection, String>>;

    async fn get_shared_draft(
        &self,
        actor_id: Uuid,
        draft_id: Uuid,
    ) -> Result<SharedPurchaseDraft, SharedPurchaseDraftError>;

    async fn create_shared_draft(
        &self,
        actor_id: Uuid,
        household_id: Uuid,
        payload: String,
    ) -> Result<SharedPurchaseDraft, SharedPurchaseDraftError>;

    async fn update_shared_draft(
        &self,
        actor_id: Uuid,
        draft_id: Uuid,
        expected_revision: i64,
        payload: String,
    ) -> Result<SharedPurchaseDraftUpdate, SharedPurchaseDraftError>;
}
