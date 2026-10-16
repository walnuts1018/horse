use chrono::{Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use horse_household_change::UiChangeDescriptor;
use horse_postgres_common::insert_ui_change;
use policy_application::{budget, recurring_expense};
use uuid::Uuid;

use crate::{PgPool, schema::public};

pub struct PolicyOperations<'a> {
    pool: &'a PgPool,
}

impl<'a> PolicyOperations<'a> {
    pub fn new(pool: &'a PgPool) -> Self {
        Self { pool }
    }
}

#[derive(Debug)]
pub enum PolicyPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidWorkflow,
    InvalidBudget(budget::BudgetError),
    InvalidRecurringExpense(recurring_expense::RecurringExpenseError),
    HouseholdNotFound,
    MemberNotFound,
    BudgetNotFound,
    RecurringExpenseNotFound,
    PurchaseNotFound,
    Conflict,
    CategoryNotFound,
    AccountNotFound,
}

impl From<DieselError> for PolicyPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

impl From<budget::BudgetError> for PolicyPersistenceError {
    fn from(error: budget::BudgetError) -> Self {
        Self::InvalidBudget(error)
    }
}

impl From<recurring_expense::RecurringExpenseError> for PolicyPersistenceError {
    fn from(error: recurring_expense::RecurringExpenseError) -> Self {
        Self::InvalidRecurringExpense(error)
    }
}

pub async fn persist_create_budget(
    pool: &PgPool,
    decision: budget::CreateBudgetDecision,
) -> Result<budget::Budget, PolicyPersistenceError> {
    let change = policy_application::ui_changes::budget_created();
    let actor = decision.actor_id.value();
    let household = decision.household_id.value();
    let id = decision.id.value();
    let category = decision.category_id.as_ref().map(|value| value.value());
    let name = decision.name.as_str().to_owned();
    let amount = decision.amount_yen.value();
    let starts = decision.starts_on;
    let ends = decision.ends_on;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PolicyPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<budget::Budget, PolicyPersistenceError, _>(async |conn| {
            use public::{
                budget_versions::dsl as versions, budgets::dsl as budgets,
                categories::dsl as categories, households::dsl as households,
            };
            let found = households::households
                .filter(households::id.eq(household))
                .for_update()
                .select(households::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .is_some();
            if !found {
                return Err(PolicyPersistenceError::HouseholdNotFound);
            }
            require_member(conn, household, actor).await?;
            if let Some(category_id) = category {
                let found = categories::categories
                    .filter(categories::id.eq(category_id))
                    .filter(categories::household_id.eq(household))
                    .select(categories::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .is_some();
                if !found {
                    return Err(PolicyPersistenceError::CategoryNotFound);
                }
            }
            diesel::insert_into(budgets::budgets)
                .values((
                    budgets::id.eq(id),
                    budgets::household_id.eq(household),
                    budgets::category_id.eq(category),
                    budgets::name.eq(&name),
                    budgets::amount_yen.eq(amount),
                    budgets::starts_on.eq(starts),
                    budgets::ends_on.eq(ends),
                ))
                .execute(conn)
                .await?;
            diesel::insert_into(versions::budget_versions)
                .values((
                    versions::budget_id.eq(id),
                    versions::household_id.eq(household),
                    versions::revision.eq(1),
                    versions::effective_from.eq(month_start(starts)),
                    versions::category_id.eq(category),
                    versions::name.eq(&name),
                    versions::amount_yen.eq(amount),
                    versions::starts_on.eq(starts),
                    versions::ends_on.eq(ends),
                    versions::archived.eq(false),
                ))
                .execute(conn)
                .await?;
            record_event(conn, household, id, change, 1, actor).await?;
            Ok(budget::Budget::reconstitute(
                budget::BudgetReconstitution {
                    id: budget::BudgetId::from_persisted(id),
                    household_id: budget::BudgetHouseholdId::from_persisted(household),
                    category_id: category.map(budget::BudgetCategoryId::from_persisted),
                    name,
                    amount_yen: amount,
                    starts_on: starts,
                    ends_on: ends,
                    archived: false,
                    revision: 1,
                },
            )?)
        })
        .await
}

pub async fn persist_update_budget(
    pool: &PgPool,
    decision: budget::UpdateBudgetDecision,
) -> Result<budget::Budget, PolicyPersistenceError> {
    let change = policy_application::ui_changes::budget_updated();
    let actor = decision.actor_id.value();
    let household = decision.household_id.value();
    let id = decision.id.value();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PolicyPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<budget::Budget, PolicyPersistenceError, _>(async |conn| {
            use public::{
                budget_versions::dsl as versions, budgets::dsl as budgets,
                categories::dsl as categories,
            };
            let row = budgets::budgets
                .filter(budgets::id.eq(id))
                .filter(budgets::household_id.eq(household))
                .for_update()
                .select((
                    budgets::household_id,
                    budgets::category_id,
                    budgets::name,
                    budgets::amount_yen,
                    budgets::starts_on,
                    budgets::ends_on,
                    budgets::archived,
                    budgets::revision,
                ))
                .first::<(
                    Uuid,
                    Option<Uuid>,
                    String,
                    i64,
                    NaiveDate,
                    Option<NaiveDate>,
                    bool,
                    i64,
                )>(conn)
                .await
                .optional()?
                .ok_or(PolicyPersistenceError::BudgetNotFound)?;
            require_member(conn, row.0, actor).await?;
            let expected = decision.expected_revision;
            if row.7 != expected.value() {
                return Err(PolicyPersistenceError::Conflict);
            }
            let category = decision.category_id.as_ref().map(|value| value.value());
            if let Some(category_id) = category {
                let found = categories::categories
                    .filter(categories::id.eq(category_id))
                    .filter(categories::household_id.eq(row.0))
                    .select(categories::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .is_some();
                if !found {
                    return Err(PolicyPersistenceError::CategoryNotFound);
                }
            }
            let name = decision.name.as_str().to_owned();
            let amount = decision.amount_yen.value();
            let starts = decision.starts_on;
            let ends = match decision.ends_on {
                budget::EndDateChange::Unchanged => row.5,
                budget::EndDateChange::Set(value) => value,
            };
            let revision = expected.next()?.value();
            diesel::update(
                budgets::budgets
                    .filter(budgets::id.eq(id))
                    .filter(budgets::household_id.eq(row.0)),
            )
            .set((
                budgets::category_id.eq(category),
                budgets::name.eq(&name),
                budgets::amount_yen.eq(amount),
                budgets::starts_on.eq(starts),
                budgets::ends_on.eq(ends),
                budgets::revision.eq(revision),
                budgets::updated_at.eq(now),
            ))
            .execute(conn)
            .await?;
            diesel::insert_into(versions::budget_versions)
                .values((
                    versions::budget_id.eq(id),
                    versions::household_id.eq(row.0),
                    versions::revision.eq(revision),
                    versions::effective_from.eq(decision.effective_from),
                    versions::category_id.eq(category),
                    versions::name.eq(&name),
                    versions::amount_yen.eq(amount),
                    versions::starts_on.eq(starts),
                    versions::ends_on.eq(ends),
                    versions::archived.eq(row.6),
                ))
                .execute(conn)
                .await?;
            record_event(conn, row.0, id, change, revision, actor).await?;
            Ok(budget::Budget::reconstitute(
                budget::BudgetReconstitution {
                    id: budget::BudgetId::from_persisted(id),
                    household_id: budget::BudgetHouseholdId::from_persisted(row.0),
                    category_id: category.map(budget::BudgetCategoryId::from_persisted),
                    name,
                    amount_yen: amount,
                    starts_on: starts,
                    ends_on: ends,
                    archived: row.6,
                    revision,
                },
            )?)
        })
        .await
}

pub async fn persist_archive_budget(
    pool: &PgPool,
    decision: budget::ArchiveBudgetDecision,
) -> Result<budget::Budget, PolicyPersistenceError> {
    let change = policy_application::ui_changes::budget_archived();
    let actor = decision.actor_id.value();
    let household = decision.household_id.value();
    let id = decision.id.value();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PolicyPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<budget::Budget, PolicyPersistenceError, _>(async |conn| {
            use public::{budget_versions::dsl as versions, budgets::dsl as budgets};
            let row = budgets::budgets
                .filter(budgets::id.eq(id))
                .filter(budgets::household_id.eq(household))
                .for_update()
                .select((
                    budgets::household_id,
                    budgets::category_id,
                    budgets::name,
                    budgets::amount_yen,
                    budgets::starts_on,
                    budgets::ends_on,
                    budgets::archived,
                    budgets::revision,
                ))
                .first::<(
                    Uuid,
                    Option<Uuid>,
                    String,
                    i64,
                    NaiveDate,
                    Option<NaiveDate>,
                    bool,
                    i64,
                )>(conn)
                .await
                .optional()?
                .ok_or(PolicyPersistenceError::BudgetNotFound)?;
            require_member(conn, row.0, actor).await?;
            let expected = decision.expected_revision;
            if row.7 != expected.value() {
                return Err(PolicyPersistenceError::Conflict);
            }
            let effective_from = current_budget_effective_month();
            let snapshots = versions::budget_versions
                .filter(versions::budget_id.eq(id))
                .filter(versions::household_id.eq(row.0))
                .order((versions::effective_from.asc(), versions::revision.desc()))
                .select((
                    versions::effective_from,
                    versions::revision,
                    versions::category_id,
                    versions::name,
                    versions::amount_yen,
                    versions::starts_on,
                    versions::ends_on,
                    versions::archived,
                ))
                .load::<BudgetSnapshotRow>(conn)
                .await?;
            let current_snapshot = snapshots
                .iter()
                .filter(|snapshot| snapshot.0 <= effective_from)
                .max_by_key(|snapshot| (snapshot.0, snapshot.1))
                .or_else(|| snapshots.as_slice().first())
                .cloned()
                .ok_or(PolicyPersistenceError::BudgetNotFound)?;
            let mut seen_future_months = std::collections::HashSet::new();
            let future_snapshots = snapshots
                .iter()
                .filter(|snapshot| snapshot.0 > effective_from)
                .filter(|snapshot| seen_future_months.insert(snapshot.0))
                .cloned()
                .collect::<Vec<_>>();
            let archived = decision.archived;
            let revision = expected.next()?.value();
            diesel::update(
                budgets::budgets
                    .filter(budgets::id.eq(id))
                    .filter(budgets::household_id.eq(row.0)),
            )
            .set((
                budgets::archived.eq(archived),
                budgets::revision.eq(revision),
                budgets::updated_at.eq(now),
            ))
            .execute(conn)
            .await?;
            insert_budget_snapshot(
                conn,
                id,
                row.0,
                revision,
                effective_from,
                &current_snapshot,
                archived,
            )
            .await?;
            for snapshot in future_snapshots {
                insert_budget_snapshot(conn, id, row.0, revision, snapshot.0, &snapshot, archived)
                    .await?;
            }
            record_event(conn, row.0, id, change, revision, actor).await?;
            Ok(budget::Budget::reconstitute(
                budget::BudgetReconstitution {
                    id: budget::BudgetId::from_persisted(id),
                    household_id: budget::BudgetHouseholdId::from_persisted(row.0),
                    category_id: row.1.map(budget::BudgetCategoryId::from_persisted),
                    name: row.2,
                    amount_yen: row.3,
                    starts_on: row.4,
                    ends_on: row.5,
                    archived,
                    revision,
                },
            )?)
        })
        .await
}

fn month_start(date: NaiveDate) -> NaiveDate {
    date.with_day(1).expect("first day of month is valid")
}

fn current_budget_effective_month() -> NaiveDate {
    let tokyo = FixedOffset::east_opt(9 * 60 * 60).expect("Tokyo offset is valid");
    month_start(Utc::now().with_timezone(&tokyo).date_naive())
}

type BudgetSnapshotRow = (
    NaiveDate,
    i64,
    Option<Uuid>,
    String,
    i64,
    NaiveDate,
    Option<NaiveDate>,
    bool,
);

async fn insert_budget_snapshot(
    conn: &mut crate::AsyncPgConnection,
    budget_id: Uuid,
    household_id: Uuid,
    revision: i64,
    effective_from: NaiveDate,
    snapshot: &BudgetSnapshotRow,
    archived: bool,
) -> Result<(), PolicyPersistenceError> {
    use public::budget_versions::dsl as versions;
    diesel::insert_into(versions::budget_versions)
        .values((
            versions::budget_id.eq(budget_id),
            versions::household_id.eq(household_id),
            versions::revision.eq(revision),
            versions::effective_from.eq(effective_from),
            versions::category_id.eq(snapshot.2),
            versions::name.eq(&snapshot.3),
            versions::amount_yen.eq(snapshot.4),
            versions::starts_on.eq(snapshot.5),
            versions::ends_on.eq(snapshot.6),
            versions::archived.eq(archived),
        ))
        .execute(conn)
        .await?;
    Ok(())
}

pub async fn persist_create_recurring_expense(
    pool: &PgPool,
    decision: recurring_expense::RecurringExpenseDecision,
) -> Result<recurring_expense::RecurringExpense, PolicyPersistenceError> {
    let change =
        policy_application::ui_changes::recurring_expense_changed(decision.archived.is_some());
    let actor = decision.actor_id.value();
    let id = decision.id.value();
    let household = decision
        .household_id
        .as_ref()
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?
        .value();
    let category = decision.category_id.as_ref().map(|value| value.value());
    let account = decision
        .payment_account_id
        .as_ref()
        .map(|value| value.value());
    let name = decision
        .name
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
    let merchant = decision
        .merchant
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
    let amount = decision
        .amount_yen
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?
        .value();
    let cadence = decision
        .cadence
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?
        .as_str();
    let starts = decision
        .starts_on
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
    let due = decision
        .next_due_on
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
    let ends = decision
        .ends_on
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PolicyPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<recurring_expense::RecurringExpense, PolicyPersistenceError, _>(
            async |conn| {
                use public::{
                    accounts::dsl as accounts, categories::dsl as categories,
                    households::dsl as households, recurring_expenses::dsl as expenses,
                };
                let found = households::households
                    .filter(households::id.eq(household))
                    .for_update()
                    .select(households::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .is_some();
                if !found {
                    return Err(PolicyPersistenceError::HouseholdNotFound);
                }
                require_member(conn, household, actor).await?;
                if let Some(value) = category {
                    let found = categories::categories
                        .filter(categories::id.eq(value))
                        .filter(categories::household_id.eq(household))
                        .select(categories::id)
                        .first::<Uuid>(conn)
                        .await
                        .optional()?
                        .is_some();
                    if !found {
                        return Err(PolicyPersistenceError::CategoryNotFound);
                    }
                }
                if let Some(value) = account {
                    let found = accounts::accounts
                        .filter(accounts::id.eq(value))
                        .filter(accounts::household_id.eq(household))
                        .select(accounts::id)
                        .first::<Uuid>(conn)
                        .await
                        .optional()?
                        .is_some();
                    if !found {
                        return Err(PolicyPersistenceError::AccountNotFound);
                    }
                }
                diesel::insert_into(expenses::recurring_expenses)
                    .values((
                        expenses::id.eq(id),
                        expenses::household_id.eq(household),
                        expenses::name.eq(&name),
                        expenses::merchant.eq(&merchant),
                        expenses::amount_yen.eq(amount),
                        expenses::category_id.eq(category),
                        expenses::payment_account_id.eq(account),
                        expenses::cadence.eq(cadence),
                        expenses::starts_on.eq(starts),
                        expenses::next_due_on.eq(due),
                        expenses::ends_on.eq(ends),
                    ))
                    .execute(conn)
                    .await?;
                record_event(conn, household, id, change, 1, actor).await?;
                Ok(recurring_expense::RecurringExpense {
                    id: recurring_expense::RecurringExpenseId::from_persisted(id),
                    household_id: recurring_expense::RecurringExpenseHouseholdId::from_persisted(
                        household,
                    ),
                    name,
                    merchant,
                    amount_yen: recurring_expense::RecurringExpenseAmountYen::try_from(amount)?,
                    currency: recurring_expense::CURRENCY.to_owned(),
                    category_id: category.map(|value| {
                        recurring_expense::RecurringExpenseCategoryId::from_persisted(value)
                    }),
                    payment_account_id: account.map(|value| {
                        recurring_expense::RecurringExpensePaymentAccountId::from_persisted(value)
                    }),
                    cadence: decision.cadence.unwrap(),
                    starts_on: starts,
                    next_due_on: due,
                    ends_on: ends,
                    archived: false,
                    revision: recurring_expense::RecurringExpenseRevision::try_from(1)?,
                })
            },
        )
        .await
}

pub async fn persist_update_recurring_expense(
    pool: &PgPool,
    decision: recurring_expense::RecurringExpenseDecision,
) -> Result<recurring_expense::RecurringExpense, PolicyPersistenceError> {
    let change =
        policy_application::ui_changes::recurring_expense_changed(decision.archived.is_some());
    let actor = decision.actor_id.value();
    let household = decision
        .household_id
        .as_ref()
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?
        .value();
    let id = decision.id.value();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PolicyPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<recurring_expense::RecurringExpense, PolicyPersistenceError, _>(
            async |conn| {
                use public::{
                    accounts::dsl as accounts, categories::dsl as categories,
                    recurring_expenses::dsl as expenses,
                };
                let row = expenses::recurring_expenses
                    .filter(expenses::id.eq(id))
                    .filter(expenses::household_id.eq(household))
                    .for_update()
                    .select((
                        expenses::household_id,
                        expenses::name,
                        expenses::merchant,
                        expenses::amount_yen,
                        expenses::category_id,
                        expenses::payment_account_id,
                        expenses::cadence,
                        expenses::starts_on,
                        expenses::next_due_on,
                        expenses::ends_on,
                        expenses::archived,
                        expenses::revision,
                    ))
                    .first::<(
                        Uuid,
                        String,
                        String,
                        i64,
                        Option<Uuid>,
                        Option<Uuid>,
                        String,
                        NaiveDate,
                        NaiveDate,
                        Option<NaiveDate>,
                        bool,
                        i64,
                    )>(conn)
                    .await
                    .optional()?
                    .ok_or(PolicyPersistenceError::RecurringExpenseNotFound)?;
                require_member(conn, row.0, actor).await?;
                let expected = decision
                    .expected_revision
                    .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
                if row.11 != expected.value() {
                    return Err(PolicyPersistenceError::Conflict);
                }
                let category = decision.category_id.as_ref().map(|value| value.value());
                if let Some(value) = category {
                    let found = categories::categories
                        .filter(categories::id.eq(value))
                        .filter(categories::household_id.eq(row.0))
                        .select(categories::id)
                        .first::<Uuid>(conn)
                        .await
                        .optional()?
                        .is_some();
                    if !found {
                        return Err(PolicyPersistenceError::CategoryNotFound);
                    }
                }
                let account = decision
                    .payment_account_id
                    .as_ref()
                    .map(|value| value.value());
                if let Some(value) = account {
                    let found = accounts::accounts
                        .filter(accounts::id.eq(value))
                        .filter(accounts::household_id.eq(row.0))
                        .select(accounts::id)
                        .first::<Uuid>(conn)
                        .await
                        .optional()?
                        .is_some();
                    if !found {
                        return Err(PolicyPersistenceError::AccountNotFound);
                    }
                }
                let name = decision
                    .name
                    .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
                let merchant = decision
                    .merchant
                    .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
                let amount = decision
                    .amount_yen
                    .ok_or(PolicyPersistenceError::InvalidWorkflow)?
                    .value();
                let cadence = decision
                    .cadence
                    .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
                let starts = decision
                    .starts_on
                    .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
                let due = decision
                    .next_due_on
                    .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
                let ends = decision
                    .ends_on
                    .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
                let revision = expected.next()?;
                let revision_value = revision.value();
                diesel::update(
                    expenses::recurring_expenses
                        .filter(expenses::id.eq(id))
                        .filter(expenses::household_id.eq(row.0)),
                )
                .set((
                    expenses::name.eq(&name),
                    expenses::merchant.eq(&merchant),
                    expenses::amount_yen.eq(amount),
                    expenses::category_id.eq(category),
                    expenses::payment_account_id.eq(account),
                    expenses::cadence.eq(cadence.as_str()),
                    expenses::starts_on.eq(starts),
                    expenses::next_due_on.eq(due),
                    expenses::ends_on.eq(ends),
                    expenses::revision.eq(revision_value),
                    expenses::updated_at.eq(now),
                ))
                .execute(conn)
                .await?;
                record_event(conn, row.0, id, change, revision_value, actor).await?;
                Ok(recurring_expense::RecurringExpense {
                    id: recurring_expense::RecurringExpenseId::from_persisted(id),
                    household_id: recurring_expense::RecurringExpenseHouseholdId::from_persisted(
                        row.0,
                    ),
                    name,
                    merchant,
                    amount_yen: recurring_expense::RecurringExpenseAmountYen::try_from(amount)?,
                    currency: recurring_expense::CURRENCY.to_owned(),
                    category_id: category.map(|value| {
                        recurring_expense::RecurringExpenseCategoryId::from_persisted(value)
                    }),
                    payment_account_id: account.map(|value| {
                        recurring_expense::RecurringExpensePaymentAccountId::from_persisted(value)
                    }),
                    cadence,
                    starts_on: starts,
                    next_due_on: due,
                    ends_on: ends,
                    archived: row.10,
                    revision,
                })
            },
        )
        .await
}

pub async fn persist_archive_recurring_expense(
    pool: &PgPool,
    decision: recurring_expense::RecurringExpenseDecision,
) -> Result<recurring_expense::RecurringExpense, PolicyPersistenceError> {
    let change =
        policy_application::ui_changes::recurring_expense_changed(decision.archived.is_some());
    let actor = decision.actor_id.value();
    let household = decision
        .household_id
        .as_ref()
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?
        .value();
    let id = decision.id.value();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PolicyPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<recurring_expense::RecurringExpense, PolicyPersistenceError, _>(
            async |conn| {
                use public::recurring_expenses::dsl as expenses;
                let row = expenses::recurring_expenses
                    .filter(expenses::id.eq(id))
                    .filter(expenses::household_id.eq(household))
                    .for_update()
                    .select((
                        expenses::household_id,
                        expenses::name,
                        expenses::merchant,
                        expenses::amount_yen,
                        expenses::category_id,
                        expenses::payment_account_id,
                        expenses::cadence,
                        expenses::starts_on,
                        expenses::next_due_on,
                        expenses::ends_on,
                        expenses::archived,
                        expenses::revision,
                    ))
                    .first::<(
                        Uuid,
                        String,
                        String,
                        i64,
                        Option<Uuid>,
                        Option<Uuid>,
                        String,
                        NaiveDate,
                        NaiveDate,
                        Option<NaiveDate>,
                        bool,
                        i64,
                    )>(conn)
                    .await
                    .optional()?
                    .ok_or(PolicyPersistenceError::RecurringExpenseNotFound)?;
                require_member(conn, row.0, actor).await?;
                let expected = decision
                    .expected_revision
                    .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
                if row.11 != expected.value() {
                    return Err(PolicyPersistenceError::Conflict);
                }
                let archived = decision
                    .archived
                    .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
                let revision = expected.next()?;
                let revision_value = revision.value();
                diesel::update(
                    expenses::recurring_expenses
                        .filter(expenses::id.eq(id))
                        .filter(expenses::household_id.eq(row.0)),
                )
                .set((
                    expenses::archived.eq(archived),
                    expenses::revision.eq(revision_value),
                    expenses::updated_at.eq(now),
                ))
                .execute(conn)
                .await?;
                record_event(conn, row.0, id, change, revision_value, actor).await?;
                let cadence = recurring_expense::Cadence::parse(&row.6)
                    .map_err(|_| PolicyPersistenceError::InvalidWorkflow)?;
                Ok(recurring_expense::RecurringExpense {
                    id: recurring_expense::RecurringExpenseId::from_persisted(id),
                    household_id: recurring_expense::RecurringExpenseHouseholdId::from_persisted(
                        row.0,
                    ),
                    name: row.1,
                    merchant: row.2,
                    amount_yen: recurring_expense::RecurringExpenseAmountYen::try_from(row.3)?,
                    currency: recurring_expense::CURRENCY.to_owned(),
                    category_id: row.4.map(|value| {
                        recurring_expense::RecurringExpenseCategoryId::from_persisted(value)
                    }),
                    payment_account_id: row.5.map(|value| {
                        recurring_expense::RecurringExpensePaymentAccountId::from_persisted(value)
                    }),
                    cadence,
                    starts_on: row.7,
                    next_due_on: row.8,
                    ends_on: row.9,
                    archived,
                    revision,
                })
            },
        )
        .await
}

pub async fn persist_match_recurring_expense(
    pool: &PgPool,
    decision: recurring_expense::MatchRecurringExpenseDecision,
) -> Result<recurring_expense::RecurringExpense, PolicyPersistenceError> {
    let change = policy_application::ui_changes::recurring_expense_matched();
    let actor = decision.actor_id.value();
    let household = decision.household_id.value();
    let id = decision.id.value();
    let purchase_id = decision.purchase_id.value();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PolicyPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<recurring_expense::RecurringExpense, PolicyPersistenceError, _>(
            async |conn| {
                use public::{
                    households::dsl as households, purchases::dsl as purchases,
                    recurring_expense_matches::dsl as matches, recurring_expenses::dsl as expenses,
                };
                let found = households::households
                    .filter(households::id.eq(household))
                    .for_update()
                    .select(households::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .is_some();
                if !found {
                    return Err(PolicyPersistenceError::HouseholdNotFound);
                }
                require_member(conn, household, actor).await?;
                let row = expenses::recurring_expenses
                    .filter(expenses::id.eq(id))
                    .filter(expenses::household_id.eq(household))
                    .for_update()
                    .select((
                        expenses::household_id,
                        expenses::name,
                        expenses::merchant,
                        expenses::amount_yen,
                        expenses::currency,
                        expenses::category_id,
                        expenses::payment_account_id,
                        expenses::cadence,
                        expenses::starts_on,
                        expenses::next_due_on,
                        expenses::ends_on,
                        expenses::archived,
                        expenses::revision,
                    ))
                    .first::<(
                        Uuid,
                        String,
                        String,
                        i64,
                        String,
                        Option<Uuid>,
                        Option<Uuid>,
                        String,
                        NaiveDate,
                        NaiveDate,
                        Option<NaiveDate>,
                        bool,
                        i64,
                    )>(conn)
                    .await
                    .optional()?
                    .ok_or(PolicyPersistenceError::RecurringExpenseNotFound)?;
                if row.11 || row.12 != decision.expected_revision.value() {
                    return Err(PolicyPersistenceError::Conflict);
                }
                let purchase_status = purchases::purchases
                    .filter(purchases::id.eq(purchase_id))
                    .filter(purchases::household_id.eq(household))
                    .for_update()
                    .select(purchases::status)
                    .first::<String>(conn)
                    .await
                    .optional()?
                    .ok_or(PolicyPersistenceError::PurchaseNotFound)?;
                if purchase_status != "POSTED" {
                    return Err(PolicyPersistenceError::Conflict);
                }
                let already_matched = diesel::select(diesel::dsl::exists(
                    matches::recurring_expense_matches
                        .filter(matches::household_id.eq(household))
                        .filter(matches::purchase_id.eq(purchase_id)),
                ))
                .first::<bool>(conn)
                .await?;
                if already_matched {
                    return Err(PolicyPersistenceError::Conflict);
                }
                diesel::insert_into(matches::recurring_expense_matches)
                    .values((
                        matches::household_id.eq(household),
                        matches::recurring_expense_id.eq(id),
                        matches::due_on.eq(row.9),
                        matches::purchase_id.eq(purchase_id),
                        matches::matched_by.eq(actor),
                    ))
                    .execute(conn)
                    .await?;
                let cadence = recurring_expense::Cadence::parse(&row.7)
                    .map_err(|_| PolicyPersistenceError::InvalidWorkflow)?;
                let next_due = recurring_expense::next_due_on(row.8, row.9, cadence)
                    .map_err(|_| PolicyPersistenceError::InvalidWorkflow)?;
                let revision =
                    recurring_expense::RecurringExpenseRevision::try_from(row.12)?.next()?;
                let revision_value = revision.value();
                diesel::update(
                    expenses::recurring_expenses
                        .filter(expenses::id.eq(id))
                        .filter(expenses::household_id.eq(household)),
                )
                .set((
                    expenses::next_due_on.eq(next_due),
                    expenses::revision.eq(revision_value),
                    expenses::updated_at.eq(now),
                ))
                .execute(conn)
                .await?;
                record_event(conn, household, id, change, revision_value, actor).await?;
                Ok(recurring_expense::RecurringExpense {
                    id: recurring_expense::RecurringExpenseId::from_persisted(id),
                    household_id: recurring_expense::RecurringExpenseHouseholdId::from_persisted(
                        household,
                    ),
                    name: row.1,
                    merchant: row.2,
                    amount_yen: recurring_expense::RecurringExpenseAmountYen::try_from(row.3)?,
                    currency: row.4,
                    category_id: row.5.map(|value| {
                        recurring_expense::RecurringExpenseCategoryId::from_persisted(value)
                    }),
                    payment_account_id: row.6.map(|value| {
                        recurring_expense::RecurringExpensePaymentAccountId::from_persisted(value)
                    }),
                    cadence,
                    starts_on: row.8,
                    next_due_on: next_due,
                    ends_on: row.10,
                    archived: row.11,
                    revision,
                })
            },
        )
        .await
}

async fn require_member(
    conn: &mut diesel_async::AsyncPgConnection,
    household: Uuid,
    actor: Uuid,
) -> Result<(), PolicyPersistenceError> {
    use public::household_members::dsl as members;
    let role = members::household_members
        .filter(members::household_id.eq(household))
        .filter(members::member_id.eq(actor))
        .filter(members::left_at.is_null())
        .select(members::role)
        .first::<String>(conn)
        .await
        .optional()?;
    if authorize(role.as_deref(), Operation::FinancialData).is_err() {
        return Err(PolicyPersistenceError::MemberNotFound);
    }
    Ok(())
}

async fn record_event(
    conn: &mut diesel_async::AsyncPgConnection,
    household: Uuid,
    entity: Uuid,
    change: UiChangeDescriptor,
    revision: i64,
    actor: Uuid,
) -> Result<(), PolicyPersistenceError> {
    insert_ui_change(
        conn,
        household,
        entity,
        revision,
        actor,
        chrono::Utc::now(),
        change,
    )
    .await?;
    Ok(())
}

impl budget::BudgetOperations for PolicyOperations<'_> {
    type Error = PolicyPersistenceError;

    fn classify_error(error: Self::Error) -> policy_application::WorkflowFailure {
        classify_policy_error(error)
    }

    async fn persist_create(
        &mut self,
        decision: budget::CreateBudgetDecision,
    ) -> Result<budget::Budget, Self::Error> {
        persist_create_budget(self.pool, decision).await
    }

    async fn persist_update(
        &mut self,
        decision: budget::UpdateBudgetDecision,
    ) -> Result<budget::Budget, Self::Error> {
        persist_update_budget(self.pool, decision).await
    }

    async fn persist_archive(
        &mut self,
        decision: budget::ArchiveBudgetDecision,
    ) -> Result<budget::Budget, Self::Error> {
        persist_archive_budget(self.pool, decision).await
    }
}

impl recurring_expense::RecurringExpenseOperations for PolicyOperations<'_> {
    type Error = PolicyPersistenceError;

    fn classify_error(error: Self::Error) -> policy_application::WorkflowFailure {
        classify_policy_error(error)
    }

    async fn persist_create(
        &mut self,
        decision: recurring_expense::RecurringExpenseDecision,
    ) -> Result<recurring_expense::RecurringExpense, Self::Error> {
        persist_create_recurring_expense(self.pool, decision).await
    }

    async fn persist_update(
        &mut self,
        decision: recurring_expense::RecurringExpenseDecision,
    ) -> Result<recurring_expense::RecurringExpense, Self::Error> {
        persist_update_recurring_expense(self.pool, decision).await
    }

    async fn persist_archive(
        &mut self,
        decision: recurring_expense::RecurringExpenseDecision,
    ) -> Result<recurring_expense::RecurringExpense, Self::Error> {
        persist_archive_recurring_expense(self.pool, decision).await
    }

    async fn persist_match(
        &mut self,
        decision: recurring_expense::MatchRecurringExpenseDecision,
    ) -> Result<recurring_expense::RecurringExpense, Self::Error> {
        persist_match_recurring_expense(self.pool, decision).await
    }
}

fn classify_policy_error(error: PolicyPersistenceError) -> policy_application::WorkflowFailure {
    use policy_application::WorkflowFailure;
    match error {
        PolicyPersistenceError::InvalidUuid(_)
        | PolicyPersistenceError::InvalidWorkflow
        | PolicyPersistenceError::InvalidBudget(_)
        | PolicyPersistenceError::InvalidRecurringExpense(_) => WorkflowFailure::Decision,
        PolicyPersistenceError::HouseholdNotFound
        | PolicyPersistenceError::MemberNotFound
        | PolicyPersistenceError::BudgetNotFound
        | PolicyPersistenceError::RecurringExpenseNotFound
        | PolicyPersistenceError::PurchaseNotFound
        | PolicyPersistenceError::CategoryNotFound
        | PolicyPersistenceError::AccountNotFound => WorkflowFailure::NotFound,
        PolicyPersistenceError::Conflict => WorkflowFailure::Conflict,
        PolicyPersistenceError::Pool(error) => WorkflowFailure::Infrastructure(error),
        PolicyPersistenceError::Database(error) => {
            WorkflowFailure::Infrastructure(error.to_string())
        }
    }
}
