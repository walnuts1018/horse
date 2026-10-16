use async_trait::async_trait;
use bookkeeping_application::{
    settlement_query::{GetSettlementQuery, SettlementQueryResult},
    workflow_failure::WorkflowFailure,
};
use bookkeeping_domain::close_period::ClosePeriodError;
use cross_context_application::settlement_workflow::{
    CloseSettlementCommand, LoanBalanceProjection, SettlementQueryFailure, SettlementWorkflow,
};
use horse_postgres_common::PgPool;
use uuid::Uuid;

pub struct PostgresSettlementWorkflow {
    pool: PgPool,
}

impl PostgresSettlementWorkflow {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl SettlementWorkflow for PostgresSettlementWorkflow {
    async fn close(
        &self,
        command: CloseSettlementCommand,
    ) -> Result<SettlementQueryResult, WorkflowFailure<ClosePeriodError, String>> {
        crate::policy_settlement::close_period(
            &self.pool,
            &command.household_id.to_string(),
            &command.actor_id.to_string(),
            &command.period,
            command.expected_revision,
        )
        .await
        .map_err(map_close_failure)?;
        get_many(&self.pool, &[query(command.household_id, command.period)])
            .await
            .map_err(WorkflowFailure::Infrastructure)?
            .into_iter()
            .next()
            .ok_or_else(|| {
                WorkflowFailure::Infrastructure("closed settlement not found".to_owned())
            })
    }

    async fn get(
        &self,
        actor_id: Uuid,
        query: GetSettlementQuery,
    ) -> Result<SettlementQueryResult, SettlementQueryFailure> {
        let household_id = Uuid::parse_str(&query.household_id)
            .map_err(|error| SettlementQueryFailure::Infrastructure(error.to_string()))?;
        let member = household_postgres::household_access::is_active_member(
            &self.pool,
            household_id,
            actor_id,
        )
        .await
        .map_err(|error| SettlementQueryFailure::Infrastructure(format!("{error:?}")))?;
        if !member {
            return Err(SettlementQueryFailure::NotFound);
        }
        get_many(&self.pool, &[query])
            .await
            .map_err(SettlementQueryFailure::Infrastructure)?
            .into_iter()
            .next()
            .ok_or(SettlementQueryFailure::NotFound)
    }

    async fn get_many(
        &self,
        queries: Vec<GetSettlementQuery>,
    ) -> Result<Vec<SettlementQueryResult>, String> {
        get_many(&self.pool, &queries).await
    }

    async fn loan_balances(
        &self,
        actor_id: Uuid,
        household_id: Uuid,
    ) -> Result<Vec<LoanBalanceProjection>, SettlementQueryFailure> {
        let member = household_postgres::household_access::is_active_member(
            &self.pool,
            household_id,
            actor_id,
        )
        .await
        .map_err(|error| SettlementQueryFailure::Infrastructure(format!("{error:?}")))?;
        if !member {
            return Err(SettlementQueryFailure::NotFound);
        }
        bookkeeping_postgres::loan_balance_query::list(&self.pool, household_id)
            .await
            .map(|rows| {
                rows.into_iter()
                    .map(|row| LoanBalanceProjection {
                        id: row.id,
                        lender_member_id: row.lender_member_id,
                        borrower_member_id: row.borrower_member_id,
                        principal_yen: row.principal_yen,
                        repaid_yen: row.repaid_yen,
                        balance_yen: row.balance_yen,
                        created_at: row.created_at,
                    })
                    .collect()
            })
            .map_err(|error| SettlementQueryFailure::Infrastructure(format!("{error:?}")))
    }
}

async fn get_many(
    pool: &PgPool,
    queries: &[GetSettlementQuery],
) -> Result<Vec<SettlementQueryResult>, String> {
    crate::policy_settlement::get_many(pool, queries)
        .await
        .map_err(|error| format!("{error:?}"))
}

fn query(household_id: Uuid, period: String) -> GetSettlementQuery {
    GetSettlementQuery {
        household_id: household_id.to_string(),
        period,
    }
}

fn map_close_failure(
    failure: WorkflowFailure<
        ClosePeriodError,
        bookkeeping_postgres::close_period::ClosePeriodPersistenceError,
    >,
) -> WorkflowFailure<ClosePeriodError, String> {
    match failure {
        WorkflowFailure::Decision(error) => WorkflowFailure::Decision(error),
        WorkflowFailure::NotFound => WorkflowFailure::NotFound,
        WorkflowFailure::Conflict => WorkflowFailure::Conflict,
        WorkflowFailure::Infrastructure(error) => {
            WorkflowFailure::Infrastructure(format!("{error:?}"))
        }
    }
}
