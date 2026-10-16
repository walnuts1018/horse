use bookkeeping_application::workflow_failure::{WorkflowFailure, WorkflowRejection};
use cross_context_application::account_financial_mutations::{
    AccountFinancialMutationsHandler, AccountIncomeMutationResult, AccountMovementMutationResult,
    AccountMutationAccount, AccountMutationError,
};
use uuid::Uuid;

use crate::{PgPool, evidence_workflows};

pub struct PostgresAccountFinancialMutations {
    pool: PgPool,
}

impl PostgresAccountFinancialMutations {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait::async_trait]
impl AccountFinancialMutationsHandler for PostgresAccountFinancialMutations {
    async fn record_account_movement(
        &self,
        command: bookkeeping_application::account_movement::RecordAccountMovementWorkflowCommand,
    ) -> Result<AccountMovementMutationResult, AccountMutationError> {
        let household_id = parse_id(&command.household_id)?;
        let value = evidence_workflows::record_account_movement_workflow(&self.pool, command)
            .await
            .map_err(map_workflow_failure)?;
        let movement_id = parse_id(&value.id)?;
        let result =
            bookkeeping_postgres::account_command_result::load_account_movement_command_result(
                &self.pool,
                household_id,
                movement_id,
            )
            .await
            .map_err(|error| AccountMutationError::Infrastructure(format!("{error:?}")))?;
        Ok(AccountMovementMutationResult {
            id: result.id,
            household_id: result.household_id,
            kind: result.kind,
            occurred_at: result.occurred_at,
            from_account_id: result.from_account_id,
            from_account: map_account(result.from_account),
            to_account_id: result.to_account_id,
            to_account: map_account(result.to_account),
            amount_yen: result.amount_yen,
            description: result.description,
            created_at: result.created_at,
        })
    }

    async fn record_account_income(
        &self,
        command: bookkeeping_application::account_income::RecordAccountIncomeWorkflowCommand,
    ) -> Result<AccountIncomeMutationResult, AccountMutationError> {
        let household_id = parse_id(&command.household_id)?;
        let value = evidence_workflows::record_account_income_workflow(&self.pool, command)
            .await
            .map_err(map_workflow_failure)?;
        let income_id = parse_id(&value.id)?;
        let result =
            bookkeeping_postgres::account_command_result::load_account_income_command_result(
                &self.pool,
                household_id,
                income_id,
            )
            .await
            .map_err(|error| AccountMutationError::Infrastructure(format!("{error:?}")))?;
        Ok(AccountIncomeMutationResult {
            id: result.id,
            household_id: result.household_id,
            account_id: result.account_id,
            account: map_account(result.account),
            occurred_at: result.occurred_at,
            amount_yen: result.amount_yen,
            description: result.description,
            created_at: result.created_at,
        })
    }
}

fn map_account(
    value: bookkeeping_postgres::account_command_result::AccountCommandAccount,
) -> AccountMutationAccount {
    AccountMutationAccount {
        id: value.id,
        name: value.name,
        kind: value.kind,
        card_last4: value.card_last4,
        pool_id: value.pool_id,
    }
}

fn parse_id(value: &str) -> Result<Uuid, AccountMutationError> {
    Uuid::parse_str(value)
        .map_err(|error| AccountMutationError::Infrastructure(format!("{error:?}")))
}

fn map_workflow_failure<E: std::fmt::Debug>(
    error: WorkflowFailure<WorkflowRejection, E>,
) -> AccountMutationError {
    match error {
        WorkflowFailure::NotFound => AccountMutationError::NotFound,
        WorkflowFailure::Conflict => AccountMutationError::Conflict,
        WorkflowFailure::Decision(WorkflowRejection::InvalidInput) => {
            AccountMutationError::InvalidInput
        }
        WorkflowFailure::Infrastructure(error) => {
            AccountMutationError::Infrastructure(format!("{error:?}"))
        }
    }
}
