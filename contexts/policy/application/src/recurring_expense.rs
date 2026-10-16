pub use policy_domain::recurring_expense::next_due_on;
pub use policy_domain::*;

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
