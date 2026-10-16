use async_trait::async_trait;
use bookkeeping_application::{
    account_history_query::{AccountIncomeProjection, AccountMovementProjection},
    purchase_search_query::PurchaseProjection,
    workflow_failure::{WorkflowFailure, WorkflowRejection},
};
use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct StatementLinkCommand {
    pub household_id: Uuid,
    pub actor_id: Uuid,
    pub target_id: Uuid,
    pub source_document_id: Uuid,
    pub source_reference: String,
    pub source_account_id: Option<Uuid>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct StatementTransferLinkCommand {
    pub household_id: Uuid,
    pub actor_id: Uuid,
    pub ledger_entry_id: Uuid,
    pub source_document_id: Uuid,
    pub source_reference: String,
    pub source_account_id: Uuid,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct StatementRefundCorrectionLinkCommand {
    pub household_id: Uuid,
    pub actor_id: Uuid,
    pub correction_id: Uuid,
    pub source_document_id: Uuid,
    pub source_reference: String,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TransferLedgerEntryProjection {
    pub id: Uuid,
    pub kind: String,
    pub occurred_at: DateTime<Utc>,
    pub description: String,
    pub amount_yen: i64,
    pub currency: String,
    pub created_at: DateTime<Utc>,
    pub from_member: TransferMemberProjection,
    pub to_member: TransferMemberProjection,
    pub loan: Option<LoanBalanceProjection>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TransferMemberProjection {
    pub id: Uuid,
    pub display_name: String,
    pub role: String,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct LoanBalanceProjection {
    pub id: Uuid,
    pub lender_member_id: Uuid,
    pub borrower_member_id: Uuid,
    pub principal_yen: i64,
    pub repaid_yen: i64,
    pub balance_yen: i64,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum StatementLinkMutationResult {
    Movement(AccountMovementProjection),
    Income(AccountIncomeProjection),
    Transfer(TransferLedgerEntryProjection),
    RefundCorrection(PurchaseProjection),
}

pub type StatementLinkMutationFailure = WorkflowFailure<WorkflowRejection, String>;

#[async_trait]
pub trait StatementLinkMutationsHandler: Send + Sync {
    async fn link_movement(
        &self,
        command: StatementLinkCommand,
    ) -> Result<AccountMovementProjection, StatementLinkMutationFailure>;

    async fn link_income(
        &self,
        command: StatementLinkCommand,
    ) -> Result<AccountIncomeProjection, StatementLinkMutationFailure>;

    async fn link_transfer(
        &self,
        command: StatementTransferLinkCommand,
    ) -> Result<TransferLedgerEntryProjection, StatementLinkMutationFailure>;

    async fn link_refund_correction(
        &self,
        command: StatementRefundCorrectionLinkCommand,
    ) -> Result<PurchaseProjection, StatementLinkMutationFailure>;
}
