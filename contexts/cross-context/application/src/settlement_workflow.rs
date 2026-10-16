use async_trait::async_trait;
use bookkeeping_application::settlement_query::{GetSettlementQuery, SettlementQueryResult};
use bookkeeping_domain::close_period::ClosePeriodError;
use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CloseSettlementCommand {
    pub household_id: Uuid,
    pub actor_id: Uuid,
    pub period: String,
    pub expected_revision: u64,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
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
pub enum SettlementQueryFailure {
    NotFound,
    Infrastructure(String),
}

#[async_trait]
pub trait SettlementWorkflow: Send + Sync {
    async fn close(
        &self,
        command: CloseSettlementCommand,
    ) -> Result<
        SettlementQueryResult,
        bookkeeping_application::workflow_failure::WorkflowFailure<ClosePeriodError, String>,
    >;

    async fn get(
        &self,
        actor_id: Uuid,
        query: GetSettlementQuery,
    ) -> Result<SettlementQueryResult, SettlementQueryFailure>;

    async fn get_many(
        &self,
        queries: Vec<GetSettlementQuery>,
    ) -> Result<Vec<SettlementQueryResult>, String>;

    async fn loan_balances(
        &self,
        actor_id: Uuid,
        household_id: Uuid,
    ) -> Result<Vec<LoanBalanceProjection>, SettlementQueryFailure>;
}
