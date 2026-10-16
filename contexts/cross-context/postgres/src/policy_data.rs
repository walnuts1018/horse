use async_trait::async_trait;
use bookkeeping_postgres::policy_data::PolicyData;
use policy_application::settlement_data::SettlementDataRead;
use uuid::Uuid;

use crate::{AsyncPgConnection, PgPool};

pub struct PolicyQueries {
    reader: policy_postgres::settlement_data::PostgresSettlementDataRead,
}

impl PolicyQueries {
    pub fn new(pool: PgPool) -> Self {
        Self {
            reader: policy_postgres::settlement_data::PostgresSettlementDataRead::new(pool),
        }
    }
}

#[async_trait]
impl PolicyData for PolicyQueries {
    async fn pool_funding_owner(
        &mut self,
        _conn: &mut AsyncPgConnection,
        household_id: Uuid,
        pool_id: Uuid,
        version: i32,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        self.reader
            .pool_funding_owner(household_id, pool_id, version)
            .await
            .map_err(policy_read_error)
    }

    async fn active_funding_schedule(
        &mut self,
        _conn: &mut AsyncPgConnection,
        household_id: Uuid,
        member_id: Uuid,
        pool_id: Uuid,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        self.reader
            .active_funding_schedule(household_id, member_id, pool_id)
            .await
            .map_err(policy_read_error)
    }
}

fn policy_read_error(error: String) -> diesel::result::Error {
    diesel::result::Error::SerializationError(Box::new(std::io::Error::other(error)))
}

pub async fn record_funding_workflow(
    pool: &PgPool,
    command: bookkeeping_application::funding_record::RecordFundingWorkflowCommand,
) -> Result<
    bookkeeping_application::funding_record::RecordedFunding,
    bookkeeping_application::workflow_failure::WorkflowFailure<
        bookkeeping_domain::funding_record::RecordFundingError,
        bookkeeping_postgres::funding_record::RecordFundingWorkflowAdapterError,
    >,
> {
    bookkeeping_postgres::funding_record::record_funding_workflow(
        pool,
        command,
        PolicyQueries::new(pool.clone()),
    )
    .await
}

pub use bookkeeping_postgres::financial_command_context::{
    FinancialCommandContextError, RepaymentCommandContext, RepaymentCommandContextInput,
    TransferCommandContext, TransferCommandContextInput,
};

pub async fn load_transfer_command_context(
    pool: &PgPool,
    household_id: Uuid,
    from_member_id: Uuid,
    payment_account_id: Option<Uuid>,
    to_member_id: Uuid,
    destination_account_id: Option<Uuid>,
    occurred_at: chrono::DateTime<chrono::Utc>,
) -> Result<TransferCommandContext, FinancialCommandContextError> {
    bookkeeping_postgres::financial_command_context::load_transfer_command_context(
        pool,
        TransferCommandContextInput {
            household_id,
            from_member_id,
            payment_account_id,
            to_member_id,
            destination_account_id,
            occurred_at,
        },
        &mut PolicyQueries::new(pool.clone()),
    )
    .await
}

pub async fn load_repayment_command_context(
    pool: &PgPool,
    input: RepaymentCommandContextInput,
) -> Result<RepaymentCommandContext, FinancialCommandContextError> {
    bookkeeping_postgres::financial_command_context::load_repayment_command_context(
        pool,
        input,
        &mut PolicyQueries::new(pool.clone()),
    )
    .await
}
