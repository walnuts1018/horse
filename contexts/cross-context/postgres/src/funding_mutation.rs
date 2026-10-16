use async_trait::async_trait;
use bookkeeping_application::{
    funding_record::RecordFundingWorkflowCommand, workflow_failure::WorkflowFailure,
};
use bookkeeping_domain::funding_record::RecordFundingError;
use cross_context_application::funding_mutation::{FundingMutationHandler, FundingMutationResult};
use cross_context_application::ledger_query::{
    FundingLedgerEntryProjection, FundingMemberProjection, FundingPoolProjection,
    FundingPoolShareProjection,
};
use horse_postgres_common::PgPool;
use uuid::Uuid;

pub struct PostgresFundingMutationHandler {
    pool: PgPool,
}

impl PostgresFundingMutationHandler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl FundingMutationHandler for PostgresFundingMutationHandler {
    async fn record_funding(
        &self,
        command: RecordFundingWorkflowCommand,
    ) -> Result<FundingMutationResult, WorkflowFailure<RecordFundingError, String>> {
        let household_id = Uuid::parse_str(&command.household_id)
            .map_err(|error| WorkflowFailure::Infrastructure(error.to_string()))?;
        let persisted = crate::policy_data::record_funding_workflow(&self.pool, command)
            .await
            .map_err(map_funding_failure)?;
        let ledger_entry_id = Uuid::parse_str(&persisted.ledger_entry_id)
            .map_err(|error| WorkflowFailure::Infrastructure(error.to_string()))?;
        let result = crate::funding_command_result::load_funding_ledger_entry_result(
            &self.pool,
            household_id,
            ledger_entry_id,
        )
        .await
        .map_err(|error| WorkflowFailure::Infrastructure(format!("{error:?}")))?;
        let projection = FundingLedgerEntryProjection {
            id: result.id,
            household_id: result.household_id,
            occurred_at: result.occurred_at,
            description: result.description,
            amount_yen: result.amount_yen,
            currency: result.currency,
            created_at: result.created_at,
            member: FundingMemberProjection {
                id: result.member.id,
                display_name: result.member.display_name,
                role: result.member.role,
            },
            pool: FundingPoolProjection {
                id: result.pool.id,
                name: result.pool.name,
                kind: result.pool.kind,
                default_share_weight: result.pool.default_share_weight,
                funding_shares: result
                    .pool
                    .funding_shares
                    .into_iter()
                    .map(|share| FundingPoolShareProjection {
                        member_id: share.member_id,
                        member_name: share.member_name,
                        weight: share.weight,
                    })
                    .collect(),
                funding_rule_version: result.pool.funding_rule_version,
                owner_member_id: result.pool.owner_member_id,
                settlement_policy: result.pool.settlement_policy,
            },
        };
        Ok(FundingMutationResult {
            ledger_entry_id,
            projection,
        })
    }
}

fn map_funding_failure(
    failure: WorkflowFailure<
        RecordFundingError,
        bookkeeping_postgres::funding_record::RecordFundingWorkflowAdapterError,
    >,
) -> WorkflowFailure<RecordFundingError, String> {
    match failure {
        WorkflowFailure::Decision(error) => WorkflowFailure::Decision(error),
        WorkflowFailure::NotFound => WorkflowFailure::NotFound,
        WorkflowFailure::Conflict => WorkflowFailure::Conflict,
        WorkflowFailure::Infrastructure(error) => {
            WorkflowFailure::Infrastructure(format!("{error:?}"))
        }
    }
}
