use async_trait::async_trait;
use bookkeeping_application::{account_income, account_movement};
use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AccountMutationAccount {
    pub id: Uuid,
    pub name: String,
    pub kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<Uuid>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AccountMovementMutationResult {
    pub id: Uuid,
    pub household_id: Uuid,
    pub kind: String,
    pub occurred_at: DateTime<Utc>,
    pub from_account_id: Uuid,
    pub from_account: AccountMutationAccount,
    pub to_account_id: Uuid,
    pub to_account: AccountMutationAccount,
    pub amount_yen: i64,
    pub description: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AccountIncomeMutationResult {
    pub id: Uuid,
    pub household_id: Uuid,
    pub account_id: Uuid,
    pub account: AccountMutationAccount,
    pub occurred_at: DateTime<Utc>,
    pub amount_yen: i64,
    pub description: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum AccountMutationError {
    NotFound,
    Conflict,
    InvalidInput,
    Infrastructure(String),
}

#[async_trait]
pub trait AccountFinancialMutationsHandler: Send + Sync {
    async fn record_account_movement(
        &self,
        command: account_movement::RecordAccountMovementWorkflowCommand,
    ) -> Result<AccountMovementMutationResult, AccountMutationError>;

    async fn record_account_income(
        &self,
        command: account_income::RecordAccountIncomeWorkflowCommand,
    ) -> Result<AccountIncomeMutationResult, AccountMutationError>;
}
