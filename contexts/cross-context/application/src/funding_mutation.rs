use async_trait::async_trait;
use bookkeeping_application::{
    funding_record::RecordFundingWorkflowCommand, workflow_failure::WorkflowFailure,
};
use bookkeeping_domain::funding_record::RecordFundingError;
use uuid::Uuid;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct FundingMutationResult {
    pub ledger_entry_id: Uuid,
    pub projection: crate::ledger_query::FundingLedgerEntryProjection,
}

#[async_trait]
pub trait FundingMutationHandler: Send + Sync {
    async fn record_funding(
        &self,
        command: RecordFundingWorkflowCommand,
    ) -> Result<FundingMutationResult, WorkflowFailure<RecordFundingError, String>>;
}
