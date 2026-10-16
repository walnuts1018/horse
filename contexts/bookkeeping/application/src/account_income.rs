pub use bookkeeping_domain::account_income::*;

use bookkeeping_domain::{AccountId, MemberId, OpenAccountingPeriod};

#[derive(Debug, Clone)]
pub struct RecordAccountIncomeWorkflowCommand {
    pub actor_id: MemberId,
    pub account_id: AccountId,
    pub amount: bookkeeping_domain::PositiveYen,
    pub description: String,
    pub statement_source: Option<StatementIncomeSource>,
    pub household_id: String,
    pub occurred_at: chrono::DateTime<chrono::Utc>,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct IncomeWorkflowResult {
    pub id: String,
    pub occurred_at: chrono::DateTime<chrono::Utc>,
    pub amount_yen: i64,
    pub description: String,
}

pub trait RecordAccountIncomeOperations {
    type Error;
    async fn load(
        &mut self,
        command: &RecordAccountIncomeWorkflowCommand,
    ) -> Result<(OpenAccountingPeriod, Option<IncomeAccountKind>), Self::Error>;
    async fn persist(
        &mut self,
        command: &RecordAccountIncomeWorkflowCommand,
        decision: RecordAccountIncomeDecision,
    ) -> Result<IncomeWorkflowResult, Self::Error>;
}

#[derive(Debug)]
pub enum RecordAccountIncomeWorkflowError<E> {
    Decision(AccountIncomeError),
    Operations(E),
    NotFound,
    Conflict,
}

pub async fn record_account_income<O: RecordAccountIncomeOperations>(
    operations: &mut O,
    command: RecordAccountIncomeWorkflowCommand,
) -> Result<IncomeWorkflowResult, RecordAccountIncomeWorkflowError<O::Error>> {
    let (accounting_period, account_kind) = operations
        .load(&command)
        .await
        .map_err(RecordAccountIncomeWorkflowError::Operations)?;
    let account_kind = account_kind.ok_or(RecordAccountIncomeWorkflowError::NotFound)?;
    let decision = decide_record_account_income(RecordAccountIncomeCommand {
        actor_id: command.actor_id.clone(),
        account_id: command.account_id.clone(),
        account_kind,
        amount: command.amount,
        description: command.description.clone(),
        accounting_period,
        statement_source: command.statement_source.clone(),
    })
    .map_err(RecordAccountIncomeWorkflowError::Decision)?;
    operations
        .persist(&command, decision)
        .await
        .map_err(RecordAccountIncomeWorkflowError::Operations)
}
