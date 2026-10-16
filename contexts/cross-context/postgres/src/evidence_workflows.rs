use crate::{PgPool, evidence_operations::EvidencePostgresOperations};
use bookkeeping_postgres::{
    account_income, account_movement, correct_purchase, record_repayment, record_transfer,
    statement_transfer,
};

pub async fn persist_record_account_income(
    pool: &PgPool,
    input: account_income::PersistRecordAccountIncome,
    decision: bookkeeping_application::account_income::RecordAccountIncomeDecision,
) -> Result<account_income::PersistedAccountIncome, account_income::AccountIncomePersistenceError> {
    account_income::persist_record_account_income(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
    )
    .await
}

pub async fn persist_link_statement_income(
    pool: &PgPool,
    input: account_income::PersistLinkStatementIncome,
    decision: bookkeeping_application::account_income::LinkStatementIncomeDecision,
) -> Result<account_income::PersistedAccountIncome, account_income::AccountIncomePersistenceError> {
    account_income::persist_link_statement_income(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
    )
    .await
}

pub async fn persist_record_transfer(
    pool: &PgPool,
    input: record_transfer::PersistRecordTransfer,
    decision: bookkeeping_application::record_transfer::RecordTransferDecision,
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
    decision: bookkeeping_application::record_repayment::RecordRepaymentDecision,
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
    decision: bookkeeping_application::account_movement::RecordAccountMovementDecision,
) -> Result<
    account_movement::PersistedAccountMovement,
    account_movement::AccountMovementPersistenceError,
> {
    account_movement::persist_record_account_movement(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
    )
    .await
}

pub async fn persist_link_statement_movement(
    pool: &PgPool,
    input: account_movement::PersistLinkStatementMovement,
    decision: bookkeeping_application::account_movement::LinkStatementMovementDecision,
) -> Result<
    account_movement::PersistedAccountMovement,
    account_movement::AccountMovementPersistenceError,
> {
    account_movement::persist_link_statement_movement(
        pool,
        input,
        decision,
        &EvidencePostgresOperations,
    )
    .await
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
) -> Result<String, correct_purchase::CorrectPurchasePersistenceError> {
    correct_purchase::link_statement_refund_correction(pool, input, &EvidencePostgresOperations)
        .await
}

pub async fn persist_link_statement_transfer(
    pool: &PgPool,
    input: statement_transfer::PersistLinkStatementTransfer,
) -> Result<
    statement_transfer::PersistedStatementTransfer,
    statement_transfer::StatementTransferPersistenceError,
> {
    statement_transfer::persist_link_statement_transfer(pool, input, &EvidencePostgresOperations)
        .await
}
