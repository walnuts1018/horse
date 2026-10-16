pub use policy_domain::*;

pub trait BudgetOperations {
    type Error: From<BudgetError>;
    async fn persist_create(
        &mut self,
        decision: CreateBudgetDecision,
    ) -> Result<Budget, Self::Error>;
    async fn persist_update(
        &mut self,
        decision: UpdateBudgetDecision,
    ) -> Result<Budget, Self::Error>;
    async fn persist_archive(
        &mut self,
        decision: ArchiveBudgetDecision,
    ) -> Result<Budget, Self::Error>;
}

pub async fn create_budget<O: BudgetOperations>(
    operations: &mut O,
    command: CreateBudgetCommand,
) -> Result<Budget, O::Error> {
    operations
        .persist_create(policy_domain::budget::decide_create(command)?)
        .await
}
pub async fn update_budget<O: BudgetOperations>(
    operations: &mut O,
    command: UpdateBudgetCommand,
) -> Result<Budget, O::Error> {
    operations
        .persist_update(policy_domain::budget::decide_update(command)?)
        .await
}
pub async fn archive_budget<O: BudgetOperations>(
    operations: &mut O,
    command: ArchiveBudgetCommand,
) -> Result<Budget, O::Error> {
    operations
        .persist_archive(policy_domain::budget::decide_archive(command)?)
        .await
}
