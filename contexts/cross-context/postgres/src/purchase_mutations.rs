use async_trait::async_trait;
use bookkeeping_application::{
    create_and_post_purchase::CreateAndPostPurchaseCommand,
    create_purchase_workflow::CreatePurchaseWorkflowCommand,
    post_purchase_entry::PostPurchaseEntryCommand,
    purchase_mutations::{
        CorrectPurchaseCommand, CreatePurchaseRejection, PostPurchaseEntryOutput,
        PurchaseMutationHandler, SharedPurchaseDraft as ApplicationSharedPurchaseDraft,
        SharedPurchaseDraftError, SharedPurchaseDraftUpdate,
    },
    workflow_failure::{WorkflowFailure, WorkflowRejection},
};

use crate::PgPool;

pub struct PostgresPurchaseMutationHandler {
    pool: PgPool,
}

impl PostgresPurchaseMutationHandler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl PurchaseMutationHandler for PostgresPurchaseMutationHandler {
    async fn create_purchase(
        &self,
        command: CreatePurchaseWorkflowCommand,
    ) -> Result<
        bookkeeping_application::create_and_post_purchase::CreatedPurchase,
        WorkflowFailure<CreatePurchaseRejection, String>,
    > {
        crate::create_purchase_workflow::create_purchase_workflow(&self.pool, command)
            .await
            .map_err(map_create_purchase_failure)
    }

    async fn create_and_post_purchase(
        &self,
        command: CreateAndPostPurchaseCommand,
    ) -> Result<
        bookkeeping_application::create_and_post_purchase::CreateAndPostPurchaseResult,
        WorkflowFailure<WorkflowRejection, String>,
    > {
        crate::purchase_create_and_post::create_and_post_purchase_workflow(&self.pool, command)
            .await
            .map_err(map_failure)
    }

    async fn post_purchase(
        &self,
        command: PostPurchaseEntryCommand,
    ) -> Result<PostPurchaseEntryOutput, WorkflowFailure<WorkflowRejection, String>> {
        crate::post_purchase_entry::post_purchase_entry(&self.pool, command)
            .await
            .map(|output| PostPurchaseEntryOutput {
                household_id: output.household_id,
                result: output.result,
            })
            .map_err(map_failure)
    }

    async fn correct_purchase(
        &self,
        command: CorrectPurchaseCommand,
    ) -> Result<(), WorkflowFailure<WorkflowRejection, String>> {
        let request = bookkeeping_postgres::correct_purchase::CorrectPurchaseRequest {
            household_id: command.household_id.to_string(),
            actor_id: command.actor_id.to_string(),
            purchase_id: command.purchase_id.to_string(),
            item_id: command.item_id.map(|id| id.to_string()),
            amount_delta_yen: command.amount_delta_yen,
            reason: command.reason,
            allocations: command
                .allocations
                .into_iter()
                .map(|(id, weight)| (id.to_string(), weight))
                .collect(),
            payments: command
                .payments
                .into_iter()
                .map(|(id, amount)| (id.to_string(), amount))
                .collect(),
            idempotency_key: command.idempotency_key,
            request_fingerprint: command.request_fingerprint,
            source_document_id: command.source_document_id.map(|id| id.to_string()),
            source_reference: command.source_reference,
        };
        crate::evidence_workflows::correct_purchase(&self.pool, request)
            .await
            .map(|_| ())
            .map_err(map_failure)
    }

    async fn get_shared_draft(
        &self,
        actor_id: uuid::Uuid,
        draft_id: uuid::Uuid,
    ) -> Result<ApplicationSharedPurchaseDraft, SharedPurchaseDraftError> {
        crate::reference_data::get_shared_purchase_draft(
            &self.pool,
            &actor_id.to_string(),
            &draft_id.to_string(),
        )
        .await
        .map(map_shared_draft)
        .map_err(map_shared_draft_error)
    }

    async fn create_shared_draft(
        &self,
        actor_id: uuid::Uuid,
        household_id: uuid::Uuid,
        payload: String,
    ) -> Result<ApplicationSharedPurchaseDraft, SharedPurchaseDraftError> {
        crate::reference_data::create_shared_purchase_draft(
            &self.pool,
            &actor_id.to_string(),
            &household_id.to_string(),
            &payload,
        )
        .await
        .map(map_shared_draft)
        .map_err(map_shared_draft_error)
    }

    async fn update_shared_draft(
        &self,
        actor_id: uuid::Uuid,
        draft_id: uuid::Uuid,
        expected_revision: i64,
        payload: String,
    ) -> Result<SharedPurchaseDraftUpdate, SharedPurchaseDraftError> {
        crate::reference_data::update_shared_purchase_draft(
            &self.pool,
            &actor_id.to_string(),
            &draft_id.to_string(),
            expected_revision,
            &payload,
        )
        .await
        .map(|result| SharedPurchaseDraftUpdate {
            draft: map_shared_draft(result.draft),
            conflict: result.conflict,
        })
        .map_err(map_shared_draft_error)
    }
}

fn map_shared_draft(
    draft: bookkeeping_postgres::shared_draft::SharedPurchaseDraft,
) -> ApplicationSharedPurchaseDraft {
    ApplicationSharedPurchaseDraft {
        id: uuid::Uuid::parse_str(&draft.id).expect("persisted draft id is a UUID"),
        household_id: uuid::Uuid::parse_str(&draft.household_id)
            .expect("persisted household id is a UUID"),
        revision: draft.revision,
        payload: draft.payload,
        created_at: draft.created_at,
        updated_at: draft.updated_at,
    }
}

fn map_shared_draft_error(
    error: bookkeeping_postgres::shared_draft::SharedPurchaseDraftPersistenceError,
) -> SharedPurchaseDraftError {
    use bookkeeping_postgres::shared_draft::SharedPurchaseDraftPersistenceError as E;
    match error {
        E::InvalidUuid(_) | E::InvalidWorkflow(_) | E::InvalidPayload(_) => {
            SharedPurchaseDraftError::BadInput
        }
        E::HouseholdNotFound | E::DraftNotFound => SharedPurchaseDraftError::NotFound,
        E::Pool(message) => SharedPurchaseDraftError::Infrastructure(message),
        E::Database(error) => SharedPurchaseDraftError::Infrastructure(error.to_string()),
        E::RevisionOutOfRange => {
            SharedPurchaseDraftError::Infrastructure("revision out of range".to_owned())
        }
    }
}

fn map_failure<E: std::fmt::Debug>(
    failure: WorkflowFailure<WorkflowRejection, E>,
) -> WorkflowFailure<WorkflowRejection, String> {
    match failure {
        WorkflowFailure::Decision(rejection) => WorkflowFailure::Decision(rejection),
        WorkflowFailure::NotFound => WorkflowFailure::NotFound,
        WorkflowFailure::Conflict => WorkflowFailure::Conflict,
        WorkflowFailure::Infrastructure(error) => {
            WorkflowFailure::Infrastructure(format!("{error:?}"))
        }
    }
}

fn map_create_purchase_failure<E: std::fmt::Debug>(
    failure: WorkflowFailure<CreatePurchaseRejection, E>,
) -> WorkflowFailure<CreatePurchaseRejection, String> {
    match failure {
        WorkflowFailure::Decision(rejection) => WorkflowFailure::Decision(rejection),
        WorkflowFailure::NotFound => WorkflowFailure::NotFound,
        WorkflowFailure::Conflict => WorkflowFailure::Conflict,
        WorkflowFailure::Infrastructure(error) => {
            WorkflowFailure::Infrastructure(format!("{error:?}"))
        }
    }
}
