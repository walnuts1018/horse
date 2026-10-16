use bookkeeping_application::{
    MemberId,
    workflow_failure::{WorkflowFailure, WorkflowRejection},
};
use bookkeeping_domain::PositiveYen;
use cross_context_application::financial_mutations::{
    FinancialMutationError, FinancialMutationResult, FinancialMutationsHandler,
    RecordRepaymentCommand, RecordTransferCommand, TransferKind,
};
use uuid::Uuid;

use crate::{PgPool, evidence_workflows, policy_data};

pub struct PostgresFinancialMutations {
    pool: PgPool,
}

impl PostgresFinancialMutations {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait::async_trait]
impl FinancialMutationsHandler for PostgresFinancialMutations {
    async fn record_transfer(
        &self,
        command: RecordTransferCommand,
    ) -> Result<FinancialMutationResult, FinancialMutationError> {
        let replay_input = bookkeeping_postgres::record_transfer::FindRecordTransferReplay {
            household_id: command.household_id.to_string(),
            actor_id: command.actor_id.to_string(),
            from_member_id: command.from_member_id.to_string(),
            to_member_id: command.to_member_id.to_string(),
            idempotency_key: command.idempotency_key.clone(),
            request_fingerprint: command.request_fingerprint,
            source_document_id: command.source_document_id.map(|id| id.to_string()),
            source_reference: command.source_reference.clone(),
            source_account_id: command.source_account_id.map(|id| id.to_string()),
        };
        let replay = evidence_workflows::find_record_transfer_replay(&self.pool, &replay_input)
            .await
            .map_err(map_transfer_failure)?;
        if let Some(ledger_entry_id) = replay {
            return parse_result_id(ledger_entry_id);
        }
        let context = evidence_workflows::load_transfer_command_context(
            &self.pool,
            command.household_id,
            command.from_member_id,
            command.payment_account_id,
            command.to_member_id,
            command.destination_account_id,
            command.occurred_at,
        )
        .await
        .map_err(map_context_failure)?;
        let amount = PositiveYen::new(command.amount_yen)
            .map_err(|_| FinancialMutationError::InvalidInput)?;
        let base = bookkeeping_application::record_transfer::TransferBase {
            actor_id: member_id(command.actor_id)?,
            from_member_id: member_id(command.from_member_id)?,
            to_member_id: member_id(command.to_member_id)?,
            amount,
            description: command.description,
            accounting_period: context.accounting_period,
            payment_account: context.payment_account,
            destination_account: context.destination_account,
        };
        let decision = match command.kind {
            TransferKind::Advance => {
                bookkeeping_application::record_transfer::RecordTransferCommand::Advance(base)
            }
            TransferKind::Loan => {
                bookkeeping_application::record_transfer::RecordTransferCommand::Loan(base)
            }
            TransferKind::Gift => {
                bookkeeping_application::record_transfer::RecordTransferCommand::Gift(base)
            }
        };
        let decision = bookkeeping_application::record_transfer::decide_record_transfer(decision)
            .map_err(|error| match error {
                bookkeeping_application::record_transfer::RecordTransferError::AccountingPeriodClosed => FinancialMutationError::Conflict,
                _ => FinancialMutationError::InvalidInput,
            })?;
        let result = evidence_workflows::persist_record_transfer(
            &self.pool,
            bookkeeping_postgres::record_transfer::PersistRecordTransfer {
                household_id: command.household_id.to_string(),
                actor_id: command.actor_id.to_string(),
                occurred_at: command.occurred_at,
                idempotency_key: command.idempotency_key,
                request_fingerprint: command.request_fingerprint,
                source_document_id: command.source_document_id.map(|id| id.to_string()),
                source_reference: command.source_reference,
                source_account_id: command.source_account_id.map(|id| id.to_string()),
            },
            decision,
        )
        .await
        .map_err(map_transfer_failure)?;
        parse_result_id(result.ledger_entry_id)
    }

    async fn record_repayment(
        &self,
        command: RecordRepaymentCommand,
    ) -> Result<FinancialMutationResult, FinancialMutationError> {
        let replay_input = bookkeeping_postgres::record_repayment::FindRecordRepaymentReplay {
            household_id: command.household_id.to_string(),
            actor_id: command.actor_id.to_string(),
            from_member_id: command.from_member_id.to_string(),
            to_member_id: command.to_member_id.to_string(),
            idempotency_key: command.idempotency_key.clone(),
            request_fingerprint: command.request_fingerprint,
            source_document_id: command.source_document_id.map(|id| id.to_string()),
            source_reference: command.source_reference.clone(),
            source_account_id: command.source_account_id.map(|id| id.to_string()),
        };
        let replay = evidence_workflows::find_record_repayment_replay(&self.pool, &replay_input)
            .await
            .map_err(map_repayment_failure)?;
        if let Some(ledger_entry_id) = replay {
            return parse_result_id(ledger_entry_id);
        }
        let context = evidence_workflows::load_repayment_command_context(
            &self.pool,
            policy_data::RepaymentCommandContextInput {
                household_id: command.household_id,
                loan_id: command.loan_id,
                payment_account_id: command.payment_account_id,
                destination_account_id: command.destination_account_id,
                from_member_id: command.from_member_id,
                to_member_id: command.to_member_id,
                occurred_at: command.occurred_at,
            },
        )
        .await
        .map_err(map_context_failure)?;
        let amount = PositiveYen::new(command.amount_yen)
            .map_err(|_| FinancialMutationError::InvalidInput)?;
        let decision = bookkeeping_application::record_repayment::decide_record_repayment(
            bookkeeping_application::record_repayment::RecordRepaymentCommand {
                loan: context.loan,
                repaid_so_far: context.repaid_so_far,
                from_member: member_id(command.from_member_id)?,
                to_member: member_id(command.to_member_id)?,
                amount,
                accounting_period: context.accounting_period,
                source: context.source,
                destination_account: context.destination_account,
                description: command.description,
            },
        )
        .map_err(|error| match error {
            bookkeeping_application::record_repayment::RepaymentError::AccountingPeriodClosed => {
                FinancialMutationError::Conflict
            }
            _ => FinancialMutationError::InvalidInput,
        })?;
        let result = evidence_workflows::persist_record_repayment(
            &self.pool,
            bookkeeping_postgres::record_repayment::PersistRecordRepayment {
                household_id: command.household_id.to_string(),
                actor_id: command.actor_id.to_string(),
                occurred_at: command.occurred_at,
                idempotency_key: command.idempotency_key,
                request_fingerprint: command.request_fingerprint,
                source_document_id: command.source_document_id.map(|id| id.to_string()),
                source_reference: command.source_reference,
                source_account_id: command.source_account_id.map(|id| id.to_string()),
            },
            decision,
        )
        .await
        .map_err(map_repayment_failure)?;
        parse_result_id(result.ledger_entry_id)
    }
}

fn member_id(id: Uuid) -> Result<MemberId, FinancialMutationError> {
    MemberId::new(id.to_string())
        .map_err(|error| FinancialMutationError::Infrastructure(format!("{error:?}")))
}

fn parse_result_id(id: String) -> Result<FinancialMutationResult, FinancialMutationError> {
    let ledger_entry_id = Uuid::parse_str(&id)
        .map_err(|error| FinancialMutationError::Infrastructure(format!("{error:?}")))?;
    Ok(FinancialMutationResult { ledger_entry_id })
}

fn map_context_failure<E: std::fmt::Debug>(
    error: WorkflowFailure<WorkflowRejection, E>,
) -> FinancialMutationError {
    match error {
        WorkflowFailure::NotFound => FinancialMutationError::NotFound,
        WorkflowFailure::Conflict => FinancialMutationError::Conflict,
        WorkflowFailure::Decision(WorkflowRejection::InvalidInput) => {
            FinancialMutationError::InvalidInput
        }
        WorkflowFailure::Infrastructure(error) => {
            FinancialMutationError::Infrastructure(format!("{error:?}"))
        }
    }
}

fn map_transfer_failure(
    error: WorkflowFailure<
        WorkflowRejection,
        bookkeeping_postgres::record_transfer::RecordTransferPersistenceError,
    >,
) -> FinancialMutationError {
    match error {
        WorkflowFailure::NotFound => FinancialMutationError::NotFound,
        WorkflowFailure::Conflict => FinancialMutationError::Conflict,
        WorkflowFailure::Decision(WorkflowRejection::InvalidInput) => {
            FinancialMutationError::InvalidInput
        }
        WorkflowFailure::Infrastructure(error) => {
            FinancialMutationError::Infrastructure(format!("{error:?}"))
        }
    }
}

fn map_repayment_failure(
    error: WorkflowFailure<
        WorkflowRejection,
        bookkeeping_postgres::record_repayment::RecordRepaymentPersistenceError,
    >,
) -> FinancialMutationError {
    match error {
        WorkflowFailure::NotFound => FinancialMutationError::NotFound,
        WorkflowFailure::Conflict => FinancialMutationError::Conflict,
        WorkflowFailure::Decision(WorkflowRejection::InvalidInput) => {
            FinancialMutationError::InvalidInput
        }
        WorkflowFailure::Infrastructure(error) => {
            FinancialMutationError::Infrastructure(format!("{error:?}"))
        }
    }
}
