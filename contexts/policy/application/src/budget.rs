pub use policy_domain::*;
use std::{future::Future, pin::Pin};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BudgetReadModel {
    pub budget: Budget,
    pub actual_yen: i64,
    pub remaining_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BudgetPage {
    pub rows: Vec<BudgetReadModel>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BudgetPeriodReadModel {
    pub period: String,
    pub budgets: Vec<BudgetReadModel>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BudgetQueryRequest {
    pub actor_id: BudgetActorId,
    pub household_id: BudgetHouseholdId,
    pub period: String,
    pub after_id: Option<uuid::Uuid>,
    pub before_id: Option<uuid::Uuid>,
    pub first: Option<i32>,
    pub last: Option<i32>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BudgetPeriodsQueryRequest {
    pub actor_id: BudgetActorId,
    pub household_id: BudgetHouseholdId,
    pub from: String,
    pub to: String,
}

pub trait BudgetQuery: Send + Sync {
    fn query<'a>(
        &'a self,
        request: BudgetQueryRequest,
    ) -> Pin<Box<dyn Future<Output = Result<BudgetPage, super::WorkflowFailure>> + Send + 'a>>;
    fn periods<'a>(
        &'a self,
        request: BudgetPeriodsQueryRequest,
    ) -> Pin<
        Box<
            dyn Future<Output = Result<Vec<BudgetPeriodReadModel>, super::WorkflowFailure>>
                + Send
                + 'a,
        >,
    >;
}

pub trait BudgetMutationHandler: Send + Sync {
    fn create<'a>(
        &'a self,
        command: CreateBudgetCommand,
    ) -> Pin<Box<dyn Future<Output = Result<Budget, super::WorkflowFailure>> + Send + 'a>>;
    fn update<'a>(
        &'a self,
        command: UpdateBudgetCommand,
    ) -> Pin<Box<dyn Future<Output = Result<Budget, super::WorkflowFailure>> + Send + 'a>>;
    fn archive<'a>(
        &'a self,
        actor_id: BudgetActorId,
        id: BudgetId,
        expected_revision: i64,
        archived: bool,
    ) -> Pin<Box<dyn Future<Output = Result<Budget, super::WorkflowFailure>> + Send + 'a>>;
}

pub trait BudgetOperations {
    type Error: From<BudgetError>;
    fn classify_error(error: Self::Error) -> super::WorkflowFailure;
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
) -> Result<Budget, super::WorkflowFailure> {
    operations
        .persist_create(
            policy_domain::budget::decide_create(command)
                .map_err(|_| super::WorkflowFailure::Decision)?,
        )
        .await
        .map_err(O::classify_error)
}
pub async fn update_budget<O: BudgetOperations>(
    operations: &mut O,
    command: UpdateBudgetCommand,
) -> Result<Budget, super::WorkflowFailure> {
    operations
        .persist_update(
            policy_domain::budget::decide_update(command)
                .map_err(|_| super::WorkflowFailure::Decision)?,
        )
        .await
        .map_err(O::classify_error)
}
pub async fn archive_budget<O: BudgetOperations>(
    operations: &mut O,
    command: ArchiveBudgetCommand,
) -> Result<Budget, super::WorkflowFailure> {
    operations
        .persist_archive(
            policy_domain::budget::decide_archive(command)
                .map_err(|_| super::WorkflowFailure::Decision)?,
        )
        .await
        .map_err(O::classify_error)
}
