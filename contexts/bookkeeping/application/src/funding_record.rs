use chrono::{DateTime, Utc};

pub use bookkeeping_domain::funding_record::{
    FundingAccount, FundingAccountKind, FundingCommandContext, FundingJournalLine, FundingPoolKind,
    FundingRecordedEvent, PostingSide, RecordFundingCommand, RecordFundingDecision,
    RecordFundingError, decide_record_funding,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordFundingWorkflowCommand {
    pub record: RecordFundingCommand,
    pub household_id: String,
    pub occurred_at: DateTime<Utc>,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
    pub source_account_id: Option<String>,
    pub destination_account_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecordedFunding {
    pub journal_id: String,
    pub ledger_entry_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RecordFundingWorkflowError<E> {
    Decision(RecordFundingError),
    Operations(E),
}

pub trait RecordFundingOperations {
    type Error;

    async fn replay(
        &mut self,
        command: &RecordFundingWorkflowCommand,
    ) -> Result<Option<RecordedFunding>, Self::Error>;

    async fn load(
        &mut self,
        command: &RecordFundingWorkflowCommand,
    ) -> Result<FundingCommandContext, Self::Error>;

    async fn persist(
        &mut self,
        command: &RecordFundingWorkflowCommand,
        decision: RecordFundingDecision,
    ) -> Result<RecordedFunding, Self::Error>;
}

pub async fn record_funding<O: RecordFundingOperations>(
    operations: &mut O,
    command: RecordFundingWorkflowCommand,
) -> Result<RecordedFunding, RecordFundingWorkflowError<O::Error>> {
    if let Some(result) = operations
        .replay(&command)
        .await
        .map_err(RecordFundingWorkflowError::Operations)?
    {
        return Ok(result);
    }
    let context = operations
        .load(&command)
        .await
        .map_err(RecordFundingWorkflowError::Operations)?;
    let mut record = command.record.clone();
    record.accounting_period = context.accounting_period;
    record.source_account = context.source_account;
    record.destination_account = context.destination_account;
    let decision = decide_record_funding(record).map_err(RecordFundingWorkflowError::Decision)?;
    operations
        .persist(&command, decision)
        .await
        .map_err(RecordFundingWorkflowError::Operations)
}
