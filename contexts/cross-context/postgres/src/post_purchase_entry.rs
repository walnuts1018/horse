use bookkeeping_application::workflow_failure::{WorkflowFailure, WorkflowRejection};
use bookkeeping_application::{
    post_purchase::{PostPurchaseWorkflowError, PostPurchaseWorkflowResult},
    post_purchase_entry::{
        PostPurchaseEntryCommand, PostPurchaseEntryOperations, PostPurchaseEntryResult,
    },
};
use bookkeeping_postgres::post_purchase as postgres_post;
use uuid::Uuid;

use crate::{PgPool, purchase_command_result};

#[derive(Debug)]
pub enum PostPurchaseEntryAdapterError {
    HouseholdId(purchase_command_result::PurchaseCommandResultError),
    Access(household_postgres::household_access::HouseholdAccessError),
    Idempotency(postgres_post::PostPurchasePersistenceError),
    Projection(purchase_command_result::PurchaseCommandResultError),
    Post(postgres_post::PostPurchaseWorkflowAdapterError),
}

pub struct PostPurchaseEntryOutput {
    pub household_id: Option<Uuid>,
    pub result: PostPurchaseEntryResult,
}

pub async fn post_purchase_entry(
    pool: &PgPool,
    command: PostPurchaseEntryCommand,
) -> Result<
    PostPurchaseEntryOutput,
    WorkflowFailure<WorkflowRejection, PostPurchaseEntryAdapterError>,
> {
    let mut operations = PostPurchaseEntryAdapter {
        pool,
        household_id: None,
    };
    let result =
        bookkeeping_application::post_purchase_entry::post_purchase_entry(&mut operations, command)
            .await
            .map_err(|error| match error {
                PostPurchaseWorkflowError::Decision(_) => WorkflowFailure::Conflict,
                PostPurchaseWorkflowError::Operations(error) => classify_entry_error(error),
            })?;
    Ok(PostPurchaseEntryOutput {
        household_id: operations.household_id,
        result,
    })
}

fn classify_entry_error(
    error: PostPurchaseEntryAdapterError,
) -> WorkflowFailure<WorkflowRejection, PostPurchaseEntryAdapterError> {
    match error {
        error @ (PostPurchaseEntryAdapterError::HouseholdId(_)
        | PostPurchaseEntryAdapterError::Projection(_)) => WorkflowFailure::Infrastructure(error),
        PostPurchaseEntryAdapterError::Idempotency(error) => classify_post_error(error)
            .map_infrastructure(PostPurchaseEntryAdapterError::Idempotency),
        PostPurchaseEntryAdapterError::Post(error) => match error {
            postgres_post::PostPurchaseWorkflowAdapterError::Load(error) => match error {
                bookkeeping_postgres::post_purchase_loader::PostPurchaseLoadError::MissingPurchase
                | bookkeeping_postgres::post_purchase_loader::PostPurchaseLoadError::HouseholdMismatch(_) => WorkflowFailure::NotFound,
                bookkeeping_postgres::post_purchase_loader::PostPurchaseLoadError::AccountingPeriodClosed => WorkflowFailure::Conflict,
                database @ bookkeeping_postgres::post_purchase_loader::PostPurchaseLoadError::Database(_) => WorkflowFailure::Infrastructure(PostPurchaseEntryAdapterError::Post(postgres_post::PostPurchaseWorkflowAdapterError::Load(database))),
                _ => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
            },
            postgres_post::PostPurchaseWorkflowAdapterError::Persistence(error) => classify_post_error(error)
                .map_infrastructure(|error| PostPurchaseEntryAdapterError::Post(postgres_post::PostPurchaseWorkflowAdapterError::Persistence(error))),
            error @ (postgres_post::PostPurchaseWorkflowAdapterError::Pool(_)
            | postgres_post::PostPurchaseWorkflowAdapterError::Database(_)) => WorkflowFailure::Infrastructure(PostPurchaseEntryAdapterError::Post(error)),
        },
        error @ PostPurchaseEntryAdapterError::Access(_) => WorkflowFailure::Infrastructure(error),
    }
}

fn classify_post_error(
    error: postgres_post::PostPurchasePersistenceError,
) -> WorkflowFailure<WorkflowRejection, postgres_post::PostPurchasePersistenceError> {
    use postgres_post::PostPurchasePersistenceError as E;
    match error {
        E::AccountingPeriodClosed | E::StalePurchase | E::IdempotencyConflict => {
            WorkflowFailure::Conflict
        }
        E::HouseholdMismatch => WorkflowFailure::NotFound,
        E::Pool(_) | E::Database(_) => WorkflowFailure::Infrastructure(error),
        _ => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
    }
}

trait MapInfrastructure<T, U> {
    fn map_infrastructure(self, map: impl FnOnce(T) -> U) -> WorkflowFailure<WorkflowRejection, U>;
}

impl<T, U> MapInfrastructure<T, U> for WorkflowFailure<WorkflowRejection, T> {
    fn map_infrastructure(self, map: impl FnOnce(T) -> U) -> WorkflowFailure<WorkflowRejection, U> {
        match self {
            WorkflowFailure::Decision(error) => WorkflowFailure::Decision(error),
            WorkflowFailure::NotFound => WorkflowFailure::NotFound,
            WorkflowFailure::Conflict => WorkflowFailure::Conflict,
            WorkflowFailure::Infrastructure(error) => WorkflowFailure::Infrastructure(map(error)),
        }
    }
}

struct PostPurchaseEntryAdapter<'a> {
    pool: &'a PgPool,
    household_id: Option<Uuid>,
}

impl PostPurchaseEntryOperations for PostPurchaseEntryAdapter<'_> {
    type Error = PostPurchaseEntryAdapterError;

    async fn household_id(&mut self, purchase_id: &str) -> Result<Option<String>, Self::Error> {
        let purchase_id = Uuid::parse_str(purchase_id).map_err(|_| {
            PostPurchaseEntryAdapterError::HouseholdId(
                purchase_command_result::PurchaseCommandResultError::PurchaseNotFound,
            )
        })?;
        self.household_id =
            purchase_command_result::load_purchase_household_id(self.pool, purchase_id)
                .await
                .map_err(PostPurchaseEntryAdapterError::HouseholdId)?;
        Ok(self.household_id.map(|id| id.to_string()))
    }

    async fn is_active_member(
        &mut self,
        household_id: &str,
        actor_id: &str,
    ) -> Result<bool, Self::Error> {
        let household_id = Uuid::parse_str(household_id).map_err(|_| {
            PostPurchaseEntryAdapterError::Access(
                household_postgres::household_access::HouseholdAccessError::Pool(
                    "invalid household id".to_owned(),
                ),
            )
        })?;
        let actor_id = Uuid::parse_str(actor_id).map_err(|_| {
            PostPurchaseEntryAdapterError::Access(
                household_postgres::household_access::HouseholdAccessError::Pool(
                    "invalid actor id".to_owned(),
                ),
            )
        })?;
        household_postgres::household_access::is_active_member(self.pool, household_id, actor_id)
            .await
            .map_err(PostPurchaseEntryAdapterError::Access)
    }

    async fn find_idempotent(
        &mut self,
        household_id: &str,
        idempotency_key: &str,
        request_fingerprint: [u8; 32],
    ) -> Result<Option<PostPurchaseWorkflowResult>, Self::Error> {
        let household_id = Uuid::parse_str(household_id).map_err(|_| {
            PostPurchaseEntryAdapterError::Idempotency(
                postgres_post::PostPurchasePersistenceError::InvalidUuid("household_id"),
            )
        })?;
        postgres_post::find_idempotent_post_purchase(
            self.pool,
            household_id,
            idempotency_key,
            request_fingerprint,
        )
        .await
        .map(|value| {
            value.map(|value| PostPurchaseWorkflowResult {
                purchase_id: value.purchase_id.to_string(),
                journal_id: value.journal_id.to_string(),
                ledger_entry_id: value.ledger_entry_id.to_string(),
            })
        })
        .map_err(PostPurchaseEntryAdapterError::Idempotency)
    }

    async fn ledger_description(
        &mut self,
        household_id: &str,
        purchase_id: &str,
    ) -> Result<String, Self::Error> {
        let household_id = Uuid::parse_str(household_id).map_err(|_| {
            PostPurchaseEntryAdapterError::Projection(
                purchase_command_result::PurchaseCommandResultError::PurchaseNotFound,
            )
        })?;
        let purchase_id = Uuid::parse_str(purchase_id).map_err(|_| {
            PostPurchaseEntryAdapterError::Projection(
                purchase_command_result::PurchaseCommandResultError::PurchaseNotFound,
            )
        })?;
        purchase_command_result::load_purchase_command_result(self.pool, household_id, purchase_id)
            .await
            .map(|projection| projection.merchant)
            .map_err(PostPurchaseEntryAdapterError::Projection)
    }

    async fn post_purchase(
        &mut self,
        command: bookkeeping_application::post_purchase::PostPurchaseWorkflowCommand,
    ) -> Result<PostPurchaseWorkflowResult, PostPurchaseWorkflowError<Self::Error>> {
        crate::reference_data::post_purchase_workflow(self.pool, command)
            .await
            .map_err(|error| match error {
                PostPurchaseWorkflowError::Decision(error) => {
                    PostPurchaseWorkflowError::Decision(error)
                }
                PostPurchaseWorkflowError::Operations(error) => {
                    PostPurchaseWorkflowError::Operations(PostPurchaseEntryAdapterError::Post(
                        error,
                    ))
                }
            })
    }
}
