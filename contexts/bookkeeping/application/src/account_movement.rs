pub use bookkeeping_domain::account_movement::*;

use bookkeeping_domain::{AccountId, MemberId, OpenAccountingPeriod};
use chrono::{DateTime, Utc};

#[derive(Debug, Clone)]
pub struct RecordAccountMovementWorkflowCommand {
    pub actor_id: MemberId,
    pub from_account_id: AccountId,
    pub to_account_id: AccountId,
    pub kind: AccountMovementKind,
    pub occurred_at_unix_micros: i64,
    pub amount: bookkeeping_domain::PositiveYen,
    pub description: String,
    pub statement_source: Option<StatementMovementSource>,
    pub household_id: String,
    pub occurred_at: DateTime<Utc>,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
    pub account_ids: Vec<String>,
}

pub trait RecordAccountMovementOperations {
    type Error;
    async fn load(
        &mut self,
        command: &RecordAccountMovementWorkflowCommand,
    ) -> Result<
        (
            OpenAccountingPeriod,
            std::collections::BTreeMap<String, AccountKind>,
            bool,
        ),
        Self::Error,
    >;
    async fn persist(
        &mut self,
        command: &RecordAccountMovementWorkflowCommand,
        decision: RecordAccountMovementDecision,
    ) -> Result<MovementWorkflowResult, Self::Error>;
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MovementWorkflowResult {
    pub id: String,
    pub amount_yen: i64,
    pub description: String,
}

#[derive(Debug)]
pub enum RecordAccountMovementWorkflowError<E> {
    Decision(AccountMovementError),
    Operations(E),
    NotFound,
    Conflict,
}

pub async fn record_account_movement<O: RecordAccountMovementOperations>(
    operations: &mut O,
    workflow: RecordAccountMovementWorkflowCommand,
) -> Result<MovementWorkflowResult, RecordAccountMovementWorkflowError<O::Error>> {
    let (period, kinds, missing) = operations
        .load(&workflow)
        .await
        .map_err(RecordAccountMovementWorkflowError::Operations)?;
    if missing {
        return Err(RecordAccountMovementWorkflowError::NotFound);
    }
    let from_account_kind = *kinds
        .get(workflow.from_account_id.as_str())
        .ok_or(RecordAccountMovementWorkflowError::NotFound)?;
    let to_account_kind = *kinds
        .get(workflow.to_account_id.as_str())
        .ok_or(RecordAccountMovementWorkflowError::NotFound)?;
    let decision = decide_record_account_movement(RecordAccountMovementCommand {
        actor_id: workflow.actor_id.clone(),
        from_account_id: workflow.from_account_id.clone(),
        from_account_kind,
        to_account_id: workflow.to_account_id.clone(),
        to_account_kind,
        kind: workflow.kind,
        occurred_at_unix_micros: workflow.occurred_at_unix_micros,
        amount: workflow.amount,
        description: workflow.description.clone(),
        accounting_period: period,
        statement_source: workflow.statement_source.clone(),
    })
    .map_err(RecordAccountMovementWorkflowError::Decision)?;
    operations
        .persist(&workflow, decision)
        .await
        .map_err(RecordAccountMovementWorkflowError::Operations)
}
