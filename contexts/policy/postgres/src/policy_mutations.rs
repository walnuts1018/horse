use policy_application::{budget, recurring_expense};

use crate::{PgPool, policy, policy_query};

pub struct PostgresBudgetMutationHandler {
    pool: PgPool,
}

impl PostgresBudgetMutationHandler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl budget::BudgetMutationHandler for PostgresBudgetMutationHandler {
    fn create<'a>(
        &'a self,
        command: budget::CreateBudgetCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<budget::Budget, policy_application::WorkflowFailure>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let mut operations = policy::PolicyOperations::new(&self.pool);
            let saved = budget::create_budget(&mut operations, command).await?;
            Ok(saved)
        })
    }

    fn update<'a>(
        &'a self,
        command: budget::UpdateBudgetCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<budget::Budget, policy_application::WorkflowFailure>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let mut operations = policy::PolicyOperations::new(&self.pool);
            let saved = budget::update_budget(&mut operations, command).await?;
            Ok(saved)
        })
    }

    fn archive<'a>(
        &'a self,
        actor_id: budget::BudgetActorId,
        id: budget::BudgetId,
        expected_revision: i64,
        archived: bool,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<budget::Budget, policy_application::WorkflowFailure>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let household_id = policy_query::budget_household_id(&self.pool, id.value())
                .await
                .map_err(classify_query_error)?;
            let command = budget::ArchiveBudgetCommand {
                actor_id,
                household_id: budget::BudgetHouseholdId::from_persisted(household_id),
                id,
                expected_revision,
                archived,
            };
            let mut operations = policy::PolicyOperations::new(&self.pool);
            let saved = budget::archive_budget(&mut operations, command).await?;
            Ok(saved)
        })
    }
}

impl budget::BudgetQuery for PostgresBudgetMutationHandler {
    fn query<'a>(
        &'a self,
        request: budget::BudgetQueryRequest,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<budget::BudgetPage, policy_application::WorkflowFailure>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let budget::BudgetQueryRequest {
                actor_id,
                household_id,
                period,
                after_id,
                before_id,
                first,
                last,
            } = request;
            let page = policy_query::budgets(
                &self.pool,
                policy_query::BudgetsQuery {
                    actor_id: actor_id.value(),
                    household_id: household_id.value(),
                    period: &period,
                    after_id,
                    before_id,
                    first,
                    last,
                },
            )
            .await
            .map_err(classify_query_error)?;
            Ok(budget::BudgetPage {
                rows: page
                    .rows
                    .into_iter()
                    .map(|value| budget::BudgetReadModel {
                        budget: value.budget,
                        actual_yen: value.actual_yen,
                        remaining_yen: value.remaining_yen,
                    })
                    .collect(),
                total_count: page.total_count,
                has_next_page: page.has_next_page,
                has_previous_page: page.has_previous_page,
            })
        })
    }

    fn periods<'a>(
        &'a self,
        request: budget::BudgetPeriodsQueryRequest,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<
                        Vec<budget::BudgetPeriodReadModel>,
                        policy_application::WorkflowFailure,
                    >,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let periods = policy_query::budget_periods(
                &self.pool,
                policy_query::BudgetPeriodsQuery {
                    actor_id: request.actor_id.value(),
                    household_id: request.household_id.value(),
                    from: &request.from,
                    to: &request.to,
                },
            )
            .await
            .map_err(classify_query_error)?;
            Ok(periods
                .into_iter()
                .map(|value| budget::BudgetPeriodReadModel {
                    period: value.period,
                    budgets: value
                        .budgets
                        .into_iter()
                        .map(|value| budget::BudgetReadModel {
                            budget: value.budget,
                            actual_yen: value.actual_yen,
                            remaining_yen: value.remaining_yen,
                        })
                        .collect(),
                })
                .collect())
        })
    }
}

pub struct PostgresRecurringExpenseMutationHandler {
    pool: PgPool,
}

impl PostgresRecurringExpenseMutationHandler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl recurring_expense::RecurringExpenseMutationHandler
    for PostgresRecurringExpenseMutationHandler
{
    fn create<'a>(
        &'a self,
        command: recurring_expense::CreateRecurringExpenseCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<
                        recurring_expense::RecurringExpense,
                        policy_application::WorkflowFailure,
                    >,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let mut operations = policy::PolicyOperations::new(&self.pool);
            recurring_expense::create_recurring_expense(&mut operations, command).await
        })
    }

    fn update<'a>(
        &'a self,
        command: recurring_expense::UpdateRecurringExpenseCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<
                        recurring_expense::RecurringExpense,
                        policy_application::WorkflowFailure,
                    >,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let mut operations = policy::PolicyOperations::new(&self.pool);
            recurring_expense::update_recurring_expense(&mut operations, command).await
        })
    }

    fn archive<'a>(
        &'a self,
        actor_id: recurring_expense::RecurringExpenseActorId,
        id: recurring_expense::RecurringExpenseId,
        expected_revision: i64,
        archived: bool,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<
                        recurring_expense::RecurringExpense,
                        policy_application::WorkflowFailure,
                    >,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let household_id = policy_query::recurring_expense_household_id(&self.pool, id.value())
                .await
                .map_err(classify_query_error)?;
            let command = recurring_expense::ArchiveRecurringExpenseCommand {
                actor_id,
                household_id: recurring_expense::RecurringExpenseHouseholdId::from_persisted(
                    household_id,
                ),
                id,
                expected_revision,
                archived,
            };
            let mut operations = policy::PolicyOperations::new(&self.pool);
            recurring_expense::archive_recurring_expense(&mut operations, command).await
        })
    }

    fn match_purchase<'a>(
        &'a self,
        command: recurring_expense::MatchRecurringExpenseCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<
                        recurring_expense::RecurringExpense,
                        policy_application::WorkflowFailure,
                    >,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let mut operations = policy::PolicyOperations::new(&self.pool);
            recurring_expense::match_recurring_expense(&mut operations, command).await
        })
    }
}

impl recurring_expense::RecurringExpenseQuery for PostgresRecurringExpenseMutationHandler {
    fn query<'a>(
        &'a self,
        request: recurring_expense::RecurringExpenseQueryRequest,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<
                        recurring_expense::RecurringExpensePage,
                        policy_application::WorkflowFailure,
                    >,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let recurring_expense::RecurringExpenseQueryRequest {
                actor_id,
                household_id,
                include_archived,
                after_id,
                before_id,
                first,
                last,
            } = request;
            let page = policy_query::recurring_expenses(
                &self.pool,
                policy_query::RecurringExpensesQuery {
                    actor_id: actor_id.value(),
                    household_id: household_id.value(),
                    include_archived,
                    after_id,
                    before_id,
                    first,
                    last,
                },
            )
            .await
            .map_err(classify_query_error)?;
            Ok(recurring_expense::RecurringExpensePage {
                rows: page.rows,
                total_count: page.total_count,
                has_next_page: page.has_next_page,
                has_previous_page: page.has_previous_page,
            })
        })
    }
}

fn classify_query_error(
    error: policy_query::PolicyQueryError,
) -> policy_application::WorkflowFailure {
    use policy_application::WorkflowFailure;
    match error {
        policy_query::PolicyQueryError::NotFound
        | policy_query::PolicyQueryError::MemberNotFound => WorkflowFailure::NotFound,
        policy_query::PolicyQueryError::InvalidUuid
        | policy_query::PolicyQueryError::InvalidPeriod
        | policy_query::PolicyQueryError::InvalidPagination => WorkflowFailure::Decision,
        error => WorkflowFailure::Infrastructure(format!("{error:?}")),
    }
}
