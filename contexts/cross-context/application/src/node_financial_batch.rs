use async_trait::async_trait;
use chrono::{DateTime, NaiveDate, Utc};
use std::collections::HashMap;
use uuid::Uuid;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct NodeFinancialBatchInput {
    pub household_ids: Vec<Uuid>,
    pub funding_schedule_ids: Vec<Uuid>,
    pub budget_ids: Vec<Uuid>,
    pub loan_balance_ids: Vec<Uuid>,
    pub ledger_entry_ids: Vec<Uuid>,
    pub transfer_ids: Vec<Uuid>,
    pub period_start: DateTime<Utc>,
    pub period_end: DateTime<Utc>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct FundingScheduleProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub member_id: Uuid,
    pub pool_id: Uuid,
    pub amount_yen: i64,
    pub cadence: String,
    pub starts_on: NaiveDate,
    pub ends_on: Option<NaiveDate>,
    pub active: bool,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct BudgetProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub category_id: Option<Uuid>,
    pub name: String,
    pub amount_yen: i64,
    pub actual_yen: i64,
    pub starts_on: NaiveDate,
    pub ends_on: Option<NaiveDate>,
    pub archived: bool,
    pub revision: i64,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct LoanBalanceProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub from_member_id: Uuid,
    pub to_member_id: Uuid,
    pub principal_yen: i64,
    pub repaid_yen: i64,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct NodeFinancialBatchProjection {
    pub funding_schedules: Vec<FundingScheduleProjection>,
    pub budgets: Vec<BudgetProjection>,
    pub loan_balances: Vec<LoanBalanceProjection>,
    pub funding_ledger_entries: HashMap<Uuid, super::ledger_query::FundingLedgerEntryProjection>,
    pub transfer_ledger_entries:
        HashMap<Uuid, super::statement_link_mutations::TransferLedgerEntryProjection>,
    pub transfers_by_id:
        HashMap<Uuid, super::statement_link_mutations::TransferLedgerEntryProjection>,
}

#[async_trait]
pub trait NodeFinancialBatchQuery: Send + Sync {
    async fn load(
        &self,
        input: NodeFinancialBatchInput,
    ) -> Result<NodeFinancialBatchProjection, String>;
}
