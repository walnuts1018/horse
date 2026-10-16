pub use policy_domain::recurring_expense::next_due_on;
pub use policy_domain::*;
use std::{future::Future, pin::Pin};

pub trait RecurringExpenseMutationHandler: Send + Sync {
    fn create<'a>(
        &'a self,
        command: CreateRecurringExpenseCommand,
    ) -> Pin<Box<dyn Future<Output = Result<RecurringExpense, super::WorkflowFailure>> + Send + 'a>>;
    fn update<'a>(
        &'a self,
        command: UpdateRecurringExpenseCommand,
    ) -> Pin<Box<dyn Future<Output = Result<RecurringExpense, super::WorkflowFailure>> + Send + 'a>>;
    fn archive<'a>(
        &'a self,
        actor_id: RecurringExpenseActorId,
        id: RecurringExpenseId,
        expected_revision: i64,
        archived: bool,
    ) -> Pin<Box<dyn Future<Output = Result<RecurringExpense, super::WorkflowFailure>> + Send + 'a>>;
    fn match_purchase<'a>(
        &'a self,
        command: MatchRecurringExpenseCommand,
    ) -> Pin<Box<dyn Future<Output = Result<RecurringExpense, super::WorkflowFailure>> + Send + 'a>>;
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecurringExpensePage {
    pub rows: Vec<RecurringExpense>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecurringExpenseQueryRequest {
    pub actor_id: RecurringExpenseActorId,
    pub household_id: RecurringExpenseHouseholdId,
    pub include_archived: bool,
    pub after_id: Option<uuid::Uuid>,
    pub before_id: Option<uuid::Uuid>,
    pub first: Option<i32>,
    pub last: Option<i32>,
}

pub trait RecurringExpenseQuery: Send + Sync {
    fn query<'a>(
        &'a self,
        request: RecurringExpenseQueryRequest,
    ) -> Pin<
        Box<dyn Future<Output = Result<RecurringExpensePage, super::WorkflowFailure>> + Send + 'a>,
    >;
}

pub trait RecurringExpenseOperations {
    type Error: From<RecurringExpenseError>;
    fn classify_error(error: Self::Error) -> super::WorkflowFailure;
    async fn persist_create(
        &mut self,
        decision: RecurringExpenseDecision,
    ) -> Result<RecurringExpense, Self::Error>;
    async fn persist_update(
        &mut self,
        decision: RecurringExpenseDecision,
    ) -> Result<RecurringExpense, Self::Error>;
    async fn persist_archive(
        &mut self,
        decision: RecurringExpenseDecision,
    ) -> Result<RecurringExpense, Self::Error>;
    async fn persist_match(
        &mut self,
        decision: MatchRecurringExpenseDecision,
    ) -> Result<RecurringExpense, Self::Error>;
}

pub async fn create_recurring_expense<O: RecurringExpenseOperations>(
    operations: &mut O,
    command: CreateRecurringExpenseCommand,
) -> Result<RecurringExpense, super::WorkflowFailure> {
    operations
        .persist_create(
            policy_domain::recurring_expense::decide_create(command)
                .map_err(|_| super::WorkflowFailure::Decision)?,
        )
        .await
        .map_err(O::classify_error)
}
pub async fn update_recurring_expense<O: RecurringExpenseOperations>(
    operations: &mut O,
    command: UpdateRecurringExpenseCommand,
) -> Result<RecurringExpense, super::WorkflowFailure> {
    operations
        .persist_update(
            policy_domain::recurring_expense::decide_update(command)
                .map_err(|_| super::WorkflowFailure::Decision)?,
        )
        .await
        .map_err(O::classify_error)
}
pub async fn archive_recurring_expense<O: RecurringExpenseOperations>(
    operations: &mut O,
    command: ArchiveRecurringExpenseCommand,
) -> Result<RecurringExpense, super::WorkflowFailure> {
    operations
        .persist_archive(
            policy_domain::recurring_expense::decide_archive(command)
                .map_err(|_| super::WorkflowFailure::Decision)?,
        )
        .await
        .map_err(O::classify_error)
}
pub async fn match_recurring_expense<O: RecurringExpenseOperations>(
    operations: &mut O,
    command: MatchRecurringExpenseCommand,
) -> Result<RecurringExpense, super::WorkflowFailure> {
    operations
        .persist_match(
            policy_domain::recurring_expense::decide_match(command)
                .map_err(|_| super::WorkflowFailure::Decision)?,
        )
        .await
        .map_err(O::classify_error)
}
