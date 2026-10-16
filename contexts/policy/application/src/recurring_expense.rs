pub use policy_domain::recurring_expense::next_due_on;
pub use policy_domain::*;

pub trait RecurringExpenseOperations {
    type Error: From<RecurringExpenseError>;
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
) -> Result<RecurringExpense, O::Error> {
    operations
        .persist_create(policy_domain::recurring_expense::decide_create(command)?)
        .await
}
pub async fn update_recurring_expense<O: RecurringExpenseOperations>(
    operations: &mut O,
    command: UpdateRecurringExpenseCommand,
) -> Result<RecurringExpense, O::Error> {
    operations
        .persist_update(policy_domain::recurring_expense::decide_update(command)?)
        .await
}
pub async fn archive_recurring_expense<O: RecurringExpenseOperations>(
    operations: &mut O,
    command: ArchiveRecurringExpenseCommand,
) -> Result<RecurringExpense, O::Error> {
    operations
        .persist_archive(policy_domain::recurring_expense::decide_archive(command)?)
        .await
}
pub async fn match_recurring_expense<O: RecurringExpenseOperations>(
    operations: &mut O,
    command: MatchRecurringExpenseCommand,
) -> Result<RecurringExpense, O::Error> {
    operations
        .persist_match(policy_domain::recurring_expense::decide_match(command)?)
        .await
}
