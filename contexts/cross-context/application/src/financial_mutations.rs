use async_trait::async_trait;
use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TransferKind {
    Advance,
    Loan,
    Gift,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RecordTransferCommand {
    pub household_id: Uuid,
    pub actor_id: Uuid,
    pub from_member_id: Uuid,
    pub to_member_id: Uuid,
    pub kind: TransferKind,
    pub payment_account_id: Option<Uuid>,
    pub destination_account_id: Option<Uuid>,
    pub amount_yen: i64,
    pub occurred_at: DateTime<Utc>,
    pub description: String,
    pub idempotency_key: String,
    pub source_document_id: Option<Uuid>,
    pub source_reference: Option<String>,
    pub source_account_id: Option<Uuid>,
    pub request_fingerprint: [u8; 32],
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RecordRepaymentCommand {
    pub household_id: Uuid,
    pub actor_id: Uuid,
    pub from_member_id: Uuid,
    pub to_member_id: Uuid,
    pub loan_id: Uuid,
    pub payment_account_id: Option<Uuid>,
    pub destination_account_id: Option<Uuid>,
    pub amount_yen: i64,
    pub occurred_at: DateTime<Utc>,
    pub description: String,
    pub idempotency_key: String,
    pub source_document_id: Option<Uuid>,
    pub source_reference: Option<String>,
    pub source_account_id: Option<Uuid>,
    pub request_fingerprint: [u8; 32],
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct FinancialMutationResult {
    pub ledger_entry_id: Uuid,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum FinancialMutationError {
    NotFound,
    Conflict,
    InvalidInput,
    Infrastructure(String),
}

impl std::fmt::Display for FinancialMutationError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::NotFound => f.write_str("financial mutation target not found"),
            Self::Conflict => f.write_str("financial mutation conflict"),
            Self::InvalidInput => f.write_str("invalid financial mutation input"),
            Self::Infrastructure(error) => f.write_str(error),
        }
    }
}

#[async_trait]
pub trait FinancialMutationsHandler: Send + Sync {
    async fn record_transfer(
        &self,
        command: RecordTransferCommand,
    ) -> Result<FinancialMutationResult, FinancialMutationError>;

    async fn record_repayment(
        &self,
        command: RecordRepaymentCommand,
    ) -> Result<FinancialMutationResult, FinancialMutationError>;
}
