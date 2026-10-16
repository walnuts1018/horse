use chrono::NaiveDate;
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
    let change = decision.ui_change();
    let actor = parse_uuid(&decision.actor_id, "actor_id")?;
    let household = parse_uuid(&decision.household_id, "household_id")?;
    let id = parse_uuid(&decision.id, "budget_id")?;
    let category = decision
        .category_id
        .as_deref()
        .map(|value| parse_uuid(value, "category_id"))
        .transpose()?;
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
                budgets::dsl as budgets, categories::dsl as categories,
                households::dsl as households,
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
            record_event(conn, household, id, change, 1, actor).await?;
            Ok(budget::Budget {
                id: id.to_string(),
                household_id: household.to_string(),
                category_id: category.map(|value| value.to_string()),
                name,
                amount_yen: amount,
                starts_on: starts,
                ends_on: ends,
                archived: false,
                revision: 1,
            })
        })
        .await
}

pub async fn persist_update_budget(
    pool: &PgPool,
    decision: budget::UpdateBudgetDecision,
) -> Result<budget::Budget, PolicyPersistenceError> {
    let change = decision.ui_change();
    let actor = parse_uuid(&decision.actor_id, "actor_id")?;
    let household = parse_uuid(&decision.household_id, "household_id")?;
    let id = parse_uuid(&decision.id, "budget_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PolicyPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<budget::Budget, PolicyPersistenceError, _>(async |conn| {
            use public::{budgets::dsl as budgets, categories::dsl as categories};
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
            if row.7 != expected {
                return Err(PolicyPersistenceError::Conflict);
            }
            let category = decision
                .category_id
                .as_deref()
                .map(|value| parse_uuid(value, "category_id"))
                .transpose()?;
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
            let revision = expected + 1;
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
            record_event(conn, row.0, id, change, revision, actor).await?;
            Ok(budget::Budget {
                id: id.to_string(),
                household_id: row.0.to_string(),
                category_id: category.map(|value| value.to_string()),
                name,
                amount_yen: amount,
                starts_on: starts,
                ends_on: ends,
                archived: row.6,
                revision,
            })
        })
        .await
}

pub async fn persist_archive_budget(
    pool: &PgPool,
    decision: budget::ArchiveBudgetDecision,
) -> Result<budget::Budget, PolicyPersistenceError> {
    let change = decision.ui_change();
    let actor = parse_uuid(&decision.actor_id, "actor_id")?;
    let household = parse_uuid(&decision.household_id, "household_id")?;
    let id = parse_uuid(&decision.id, "budget_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PolicyPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<budget::Budget, PolicyPersistenceError, _>(async |conn| {
            use public::budgets::dsl as budgets;
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
            if row.7 != expected {
                return Err(PolicyPersistenceError::Conflict);
            }
            let archived = decision.archived;
            let revision = expected + 1;
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
            record_event(conn, row.0, id, change, revision, actor).await?;
            Ok(budget::Budget {
                id: id.to_string(),
                household_id: row.0.to_string(),
                category_id: row.1.map(|value| value.to_string()),
                name: row.2,
                amount_yen: row.3,
                starts_on: row.4,
                ends_on: row.5,
                archived,
                revision,
            })
        })
        .await
}

pub async fn persist_create_recurring_expense(
    pool: &PgPool,
    decision: recurring_expense::RecurringExpenseDecision,
) -> Result<recurring_expense::RecurringExpense, PolicyPersistenceError> {
    let change = decision.ui_change();
    let actor = parse_uuid(&decision.actor_id, "actor_id")?;
    let household = parse_uuid(
        decision
            .household_id
            .as_deref()
            .ok_or(PolicyPersistenceError::InvalidWorkflow)?,
        "household_id",
    )?;
    let id = parse_uuid(&decision.id, "expense_id")?;
    let category = decision
        .category_id
        .as_deref()
        .map(|value| parse_uuid(value, "category_id"))
        .transpose()?;
    let account = decision
        .payment_account_id
        .as_deref()
        .map(|value| parse_uuid(value, "payment_account_id"))
        .transpose()?;
    let name = decision
        .name
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
    let merchant = decision
        .merchant
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
    let amount = decision
        .amount_yen
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
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
                    id: id.to_string(),
                    household_id: household.to_string(),
                    name,
                    merchant,
                    amount_yen: amount,
                    currency: recurring_expense::CURRENCY.to_owned(),
                    category_id: category.map(|value| value.to_string()),
                    payment_account_id: account.map(|value| value.to_string()),
                    cadence: decision.cadence.unwrap(),
                    starts_on: starts,
                    next_due_on: due,
                    ends_on: ends,
                    archived: false,
                    revision: 1,
                })
            },
        )
        .await
}

pub async fn persist_update_recurring_expense(
    pool: &PgPool,
    decision: recurring_expense::RecurringExpenseDecision,
) -> Result<recurring_expense::RecurringExpense, PolicyPersistenceError> {
    let change = decision.ui_change();
    let actor = parse_uuid(&decision.actor_id, "actor_id")?;
    let household = parse_uuid(
        decision
            .household_id
            .as_deref()
            .ok_or(PolicyPersistenceError::InvalidWorkflow)?,
        "household_id",
    )?;
    let id = parse_uuid(&decision.id, "expense_id")?;
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
                if row.11 != expected {
                    return Err(PolicyPersistenceError::Conflict);
                }
                let category = decision
                    .category_id
                    .as_deref()
                    .map(|value| parse_uuid(value, "category_id"))
                    .transpose()?;
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
                    .as_deref()
                    .map(|value| parse_uuid(value, "payment_account_id"))
                    .transpose()?;
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
                    .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
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
                let revision = expected + 1;
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
                    expenses::revision.eq(revision),
                    expenses::updated_at.eq(now),
                ))
                .execute(conn)
                .await?;
                record_event(conn, row.0, id, change, revision, actor).await?;
                Ok(recurring_expense::RecurringExpense {
                    id: id.to_string(),
                    household_id: row.0.to_string(),
                    name,
                    merchant,
                    amount_yen: amount,
                    currency: recurring_expense::CURRENCY.to_owned(),
                    category_id: category.map(|value| value.to_string()),
                    payment_account_id: account.map(|value| value.to_string()),
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
    let change = decision.ui_change();
    let actor = parse_uuid(&decision.actor_id, "actor_id")?;
    let household = parse_uuid(
        decision
            .household_id
            .as_deref()
            .ok_or(PolicyPersistenceError::InvalidWorkflow)?,
        "household_id",
    )?;
    let id = parse_uuid(&decision.id, "expense_id")?;
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
                if row.11 != expected {
                    return Err(PolicyPersistenceError::Conflict);
                }
                let archived = decision
                    .archived
                    .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
                let revision = expected + 1;
                diesel::update(
                    expenses::recurring_expenses
                        .filter(expenses::id.eq(id))
                        .filter(expenses::household_id.eq(row.0)),
                )
                .set((
                    expenses::archived.eq(archived),
                    expenses::revision.eq(revision),
                    expenses::updated_at.eq(now),
                ))
                .execute(conn)
                .await?;
                record_event(conn, row.0, id, change, revision, actor).await?;
                let cadence = recurring_expense::Cadence::parse(&row.6)
                    .map_err(|_| PolicyPersistenceError::InvalidWorkflow)?;
                Ok(recurring_expense::RecurringExpense {
                    id: id.to_string(),
                    household_id: row.0.to_string(),
                    name: row.1,
                    merchant: row.2,
                    amount_yen: row.3,
                    currency: recurring_expense::CURRENCY.to_owned(),
                    category_id: row.4.map(|value| value.to_string()),
                    payment_account_id: row.5.map(|value| value.to_string()),
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
    let change = decision.ui_change();
    let actor = parse_uuid(&decision.actor_id, "actor_id")?;
    let household = parse_uuid(&decision.household_id, "household_id")?;
    let id = parse_uuid(&decision.id, "expense_id")?;
    let purchase_id = parse_uuid(&decision.purchase_id, "purchase_id")?;
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
                if row.11 || row.12 != decision.expected_revision {
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
                let revision = row.12 + 1;
                diesel::update(
                    expenses::recurring_expenses
                        .filter(expenses::id.eq(id))
                        .filter(expenses::household_id.eq(household)),
                )
                .set((
                    expenses::next_due_on.eq(next_due),
                    expenses::revision.eq(revision),
                    expenses::updated_at.eq(now),
                ))
                .execute(conn)
                .await?;
                record_event(conn, household, id, change, revision, actor).await?;
                Ok(recurring_expense::RecurringExpense {
                    id: id.to_string(),
                    household_id: household.to_string(),
                    name: row.1,
                    merchant: row.2,
                    amount_yen: row.3,
                    currency: row.4,
                    category_id: row.5.map(|value| value.to_string()),
                    payment_account_id: row.6.map(|value| value.to_string()),
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

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, PolicyPersistenceError> {
    Uuid::parse_str(value).map_err(|_| PolicyPersistenceError::InvalidUuid(field))
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
