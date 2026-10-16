use crate::{PgPool, evidence_operations::EvidencePostgresOperations};
use bookkeeping_application::workflow_failure::{WorkflowFailure, WorkflowRejection};
use bookkeeping_postgres::{
    account_income, account_movement, correct_purchase, record_repayment, record_transfer,
    statement_transfer,
};

pub async fn persist_record_account_income(
    pool: &PgPool,
    input: account_income::PersistRecordAccountIncome,
    decision: bookkeeping_domain::account_income::RecordAccountIncomeDecision,
) -> Result<
    account_income::PersistedAccountIncome,
    WorkflowFailure<WorkflowRejection, account_income::AccountIncomePersistenceError>,
> {
    account_income::persist_record_account_income(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
    )
    .await
    .map_err(|error| match error {
        account_income::AccountIncomePersistenceError::IdempotencyConflict
        | account_income::AccountIncomePersistenceError::AccountingPeriodClosed => {
            WorkflowFailure::Conflict
        }
        account_income::AccountIncomePersistenceError::HouseholdNotFound
        | account_income::AccountIncomePersistenceError::MemberNotFound
        | account_income::AccountIncomePersistenceError::AccountNotFound
        | account_income::AccountIncomePersistenceError::SourceDocumentNotFound => {
            WorkflowFailure::NotFound
        }
        account_income::AccountIncomePersistenceError::InvalidIdempotencyKey
        | account_income::AccountIncomePersistenceError::InvalidDecision
        | account_income::AccountIncomePersistenceError::StatementExtractionUnavailable
        | account_income::AccountIncomePersistenceError::StatementReferenceNotFound
        | account_income::AccountIncomePersistenceError::StatementAlreadyConfirmed
        | account_income::AccountIncomePersistenceError::StatementIsNotExternalIncome
        | account_income::AccountIncomePersistenceError::StatementIncomeMismatch => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
        }
        error => WorkflowFailure::Infrastructure(error),
    })
}

pub async fn persist_link_statement_income(
    pool: &PgPool,
    input: account_income::PersistLinkStatementIncome,
    decision: bookkeeping_domain::account_income::LinkStatementIncomeDecision,
) -> Result<
    account_income::PersistedAccountIncome,
    WorkflowFailure<WorkflowRejection, account_income::AccountIncomePersistenceError>,
> {
    account_income::persist_link_statement_income(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
    )
    .await
    .map_err(|error| match error {
        account_income::AccountIncomePersistenceError::AccountingPeriodClosed
        | account_income::AccountIncomePersistenceError::IdempotencyConflict
        | account_income::AccountIncomePersistenceError::StatementAlreadyConfirmed => {
            WorkflowFailure::Conflict
        }
        account_income::AccountIncomePersistenceError::HouseholdNotFound
        | account_income::AccountIncomePersistenceError::MemberNotFound
        | account_income::AccountIncomePersistenceError::AccountNotFound
        | account_income::AccountIncomePersistenceError::SourceDocumentNotFound => {
            WorkflowFailure::NotFound
        }
        account_income::AccountIncomePersistenceError::InvalidUuid(_)
        | account_income::AccountIncomePersistenceError::InvalidDecision
        | account_income::AccountIncomePersistenceError::StatementExtractionUnavailable
        | account_income::AccountIncomePersistenceError::StatementReferenceNotFound
        | account_income::AccountIncomePersistenceError::StatementIsNotExternalIncome
        | account_income::AccountIncomePersistenceError::StatementIncomeMismatch
        | account_income::AccountIncomePersistenceError::InvalidIdempotencyKey => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
        }
        error => WorkflowFailure::Infrastructure(error),
    })
}

pub async fn persist_record_transfer(
    pool: &PgPool,
    input: record_transfer::PersistRecordTransfer,
    decision: bookkeeping_domain::record_transfer::RecordTransferDecision,
) -> Result<record_transfer::PersistedTransfer, record_transfer::RecordTransferPersistenceError> {
    record_transfer::persist_record_transfer(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
        &mut crate::policy_settlement::PolicySettlementReferences,
    )
    .await
}

pub async fn find_record_transfer_replay(
    pool: &PgPool,
    input: &record_transfer::FindRecordTransferReplay,
) -> Result<Option<String>, record_transfer::RecordTransferPersistenceError> {
    record_transfer::find_record_transfer_replay(pool, input, &EvidencePostgresOperations).await
}

pub async fn persist_record_repayment(
    pool: &PgPool,
    input: record_repayment::PersistRecordRepayment,
    decision: bookkeeping_domain::record_repayment::RecordRepaymentDecision,
) -> Result<record_repayment::PersistedRepayment, record_repayment::RecordRepaymentPersistenceError>
{
    record_repayment::persist_record_repayment(pool, input, decision, &EvidencePostgresOperations)
        .await
}

pub async fn find_record_repayment_replay(
    pool: &PgPool,
    input: &record_repayment::FindRecordRepaymentReplay,
) -> Result<Option<String>, record_repayment::RecordRepaymentPersistenceError> {
    record_repayment::find_record_repayment_replay(pool, input, &EvidencePostgresOperations).await
}

pub async fn persist_record_account_movement(
    pool: &PgPool,
    input: account_movement::PersistRecordAccountMovement,
    decision: bookkeeping_domain::account_movement::RecordAccountMovementDecision,
) -> Result<
    account_movement::PersistedAccountMovement,
    WorkflowFailure<WorkflowRejection, account_movement::AccountMovementPersistenceError>,
> {
    account_movement::persist_record_account_movement(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
    )
    .await
    .map_err(|error| match error {
        account_movement::AccountMovementPersistenceError::IdempotencyConflict
        | account_movement::AccountMovementPersistenceError::AccountingPeriodClosed => {
            WorkflowFailure::Conflict
        }
        account_movement::AccountMovementPersistenceError::HouseholdNotFound
        | account_movement::AccountMovementPersistenceError::MemberNotFound
        | account_movement::AccountMovementPersistenceError::AccountNotFound
        | account_movement::AccountMovementPersistenceError::SourceDocumentNotFound => {
            WorkflowFailure::NotFound
        }
        account_movement::AccountMovementPersistenceError::InvalidIdempotencyKey
        | account_movement::AccountMovementPersistenceError::InvalidDecision
        | account_movement::AccountMovementPersistenceError::StatementExtractionUnavailable
        | account_movement::AccountMovementPersistenceError::StatementReferenceNotFound
        | account_movement::AccountMovementPersistenceError::StatementAlreadyConfirmed
        | account_movement::AccountMovementPersistenceError::StatementIsNotBankMovement
        | account_movement::AccountMovementPersistenceError::StatementMovementMismatch => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
        }
        error => WorkflowFailure::Infrastructure(error),
    })
}

pub async fn persist_link_statement_movement(
    pool: &PgPool,
    input: account_movement::PersistLinkStatementMovement,
    decision: bookkeeping_domain::account_movement::LinkStatementMovementDecision,
) -> Result<
    account_movement::PersistedAccountMovement,
    WorkflowFailure<WorkflowRejection, account_movement::AccountMovementPersistenceError>,
> {
    account_movement::persist_link_statement_movement(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
    )
    .await
    .map_err(|error| match error {
        account_movement::AccountMovementPersistenceError::AccountingPeriodClosed
        | account_movement::AccountMovementPersistenceError::IdempotencyConflict
        | account_movement::AccountMovementPersistenceError::StatementAlreadyConfirmed => {
            WorkflowFailure::Conflict
        }
        account_movement::AccountMovementPersistenceError::HouseholdNotFound
        | account_movement::AccountMovementPersistenceError::MemberNotFound
        | account_movement::AccountMovementPersistenceError::AccountNotFound
        | account_movement::AccountMovementPersistenceError::SourceDocumentNotFound => {
            WorkflowFailure::NotFound
        }
        account_movement::AccountMovementPersistenceError::InvalidUuid(_)
        | account_movement::AccountMovementPersistenceError::InvalidDecision
        | account_movement::AccountMovementPersistenceError::StatementExtractionUnavailable
        | account_movement::AccountMovementPersistenceError::StatementReferenceNotFound
        | account_movement::AccountMovementPersistenceError::StatementIsNotBankMovement
        | account_movement::AccountMovementPersistenceError::StatementMovementMismatch
        | account_movement::AccountMovementPersistenceError::InvalidIdempotencyKey => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
        }
        error => WorkflowFailure::Infrastructure(error),
    })
}

pub async fn correct_purchase(
    pool: &PgPool,
    request: correct_purchase::CorrectPurchaseRequest,
) -> Result<
    correct_purchase::PersistedCorrectPurchase,
    correct_purchase::CorrectPurchasePersistenceError,
> {
    correct_purchase::correct_purchase(
        pool,
        request,
        &EvidencePostgresOperations,
        &mut crate::policy_settlement::PolicySettlementReferences,
    )
    .await
}

pub async fn link_statement_refund_correction(
    pool: &PgPool,
    input: correct_purchase::LinkStatementRefundCorrection,
) -> Result<
    String,
    WorkflowFailure<WorkflowRejection, correct_purchase::CorrectPurchasePersistenceError>,
> {
    correct_purchase::link_statement_refund_correction(pool, input, &EvidencePostgresOperations)
        .await
        .map_err(|error| match error {
            correct_purchase::CorrectPurchasePersistenceError::StatementAlreadyConfirmed
            | correct_purchase::CorrectPurchasePersistenceError::AccountingPeriodClosed
            | correct_purchase::CorrectPurchasePersistenceError::IdempotencyConflict => {
                WorkflowFailure::Conflict
            }
            correct_purchase::CorrectPurchasePersistenceError::HouseholdMismatch
            | correct_purchase::CorrectPurchasePersistenceError::MemberNotFound
            | correct_purchase::CorrectPurchasePersistenceError::CorrectionNotFound
            | correct_purchase::CorrectPurchasePersistenceError::PurchaseNotFound
            | correct_purchase::CorrectPurchasePersistenceError::SourceDocumentNotFound => {
                WorkflowFailure::NotFound
            }
            correct_purchase::CorrectPurchasePersistenceError::InvalidUuid(_)
            | correct_purchase::CorrectPurchasePersistenceError::InvalidDecision
            | correct_purchase::CorrectPurchasePersistenceError::StatementNotRefundable
            | correct_purchase::CorrectPurchasePersistenceError::StatementExtractionUnavailable
            | correct_purchase::CorrectPurchasePersistenceError::StatementReferenceNotFound
            | correct_purchase::CorrectPurchasePersistenceError::RefundDoesNotMatchCorrection => {
                WorkflowFailure::Decision(WorkflowRejection::InvalidInput)
            }
            error => WorkflowFailure::Infrastructure(error),
        })
}

pub async fn persist_link_statement_transfer(
    pool: &PgPool,
    input: statement_transfer::PersistLinkStatementTransfer,
) -> Result<
    statement_transfer::PersistedStatementTransfer,
    WorkflowFailure<WorkflowRejection, statement_transfer::StatementTransferPersistenceError>,
> {
    statement_transfer::persist_link_statement_transfer(pool, input, &EvidencePostgresOperations)
        .await
        .map_err(|error| match error {
            statement_transfer::StatementTransferPersistenceError::StatementAlreadyConfirmed => WorkflowFailure::Conflict,
            statement_transfer::StatementTransferPersistenceError::HouseholdNotFound
            | statement_transfer::StatementTransferPersistenceError::MemberNotFound
            | statement_transfer::StatementTransferPersistenceError::LedgerEntryNotFound
            | statement_transfer::StatementTransferPersistenceError::TransferNotFound
            | statement_transfer::StatementTransferPersistenceError::SourceDocumentNotFound
            | statement_transfer::StatementTransferPersistenceError::StatementReferenceNotFound
            | statement_transfer::StatementTransferPersistenceError::AccountNotFound => WorkflowFailure::NotFound,
            statement_transfer::StatementTransferPersistenceError::InvalidUuid(_)
            | statement_transfer::StatementTransferPersistenceError::InvalidSource
            | statement_transfer::StatementTransferPersistenceError::InvalidDecision
            | statement_transfer::StatementTransferPersistenceError::SourceIsNotBankStatement
            | statement_transfer::StatementTransferPersistenceError::StatementExtractionUnavailable
            | statement_transfer::StatementTransferPersistenceError::StatementDoesNotMatchTransfer
            | statement_transfer::StatementTransferPersistenceError::AccountIsNotBank
            | statement_transfer::StatementTransferPersistenceError::JournalEntryMismatch => WorkflowFailure::Decision(WorkflowRejection::InvalidInput),
            error => WorkflowFailure::Infrastructure(error),
        })
}
