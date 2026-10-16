use async_trait::async_trait;
use bookkeeping_postgres::policy_data::PolicyData;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{AsyncPgConnection, PgPool, schema::public};

#[derive(Default)]
pub struct PolicyQueries;

#[async_trait]
impl PolicyData for PolicyQueries {
    async fn pool_funding_owner(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        pool_id: Uuid,
        version: i32,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        public::pool_funding_rules::table
            .filter(public::pool_funding_rules::household_id.eq(household_id))
            .filter(public::pool_funding_rules::pool_id.eq(pool_id))
            .filter(public::pool_funding_rules::version.eq(version))
            .select(public::pool_funding_rules::owner_member_id)
            .first::<Option<Uuid>>(conn)
            .await
            .optional()
            .map(Option::flatten)
    }

    async fn active_funding_schedule(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        member_id: Uuid,
        pool_id: Uuid,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        public::funding_schedules::table
            .filter(public::funding_schedules::household_id.eq(household_id))
            .filter(public::funding_schedules::member_id.eq(member_id))
            .filter(public::funding_schedules::pool_id.eq(pool_id))
            .filter(public::funding_schedules::active.eq(true))
            .order(public::funding_schedules::created_at.desc())
            .select(public::funding_schedules::id)
            .first::<Uuid>(conn)
            .await
            .optional()
    }
}

pub async fn record_funding_workflow(
    pool: &PgPool,
    command: bookkeeping_application::funding_record::RecordFundingWorkflowCommand,
) -> Result<
    bookkeeping_application::funding_record::RecordedFunding,
    bookkeeping_application::funding_record::RecordFundingWorkflowError<
        bookkeeping_postgres::funding_record::RecordFundingWorkflowAdapterError,
    >,
> {
    bookkeeping_postgres::funding_record::record_funding_workflow(pool, command, PolicyQueries)
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
        &mut PolicyQueries,
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
        &mut PolicyQueries,
    )
    .await
}
