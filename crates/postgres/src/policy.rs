use chrono::{NaiveDate, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use policy_workflows::{budget, recurring_expense};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum PolicyPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidWorkflow,
    HouseholdNotFound,
    MemberNotFound,
    BudgetNotFound,
    RecurringExpenseNotFound,
    Conflict,
    CategoryNotFound,
    AccountNotFound,
}

impl From<DieselError> for PolicyPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_create_budget(
    pool: &PgPool,
    command: budget::CreateBudgetCommand,
) -> Result<budget::Budget, PolicyPersistenceError> {
    let decision =
        budget::decide_create(command).map_err(|_| PolicyPersistenceError::InvalidWorkflow)?;
    let actor = parse_uuid(&decision.actor_id, "actor_id")?;
    let household = parse_uuid(
        decision
            .household_id
            .as_deref()
            .ok_or(PolicyPersistenceError::InvalidWorkflow)?,
        "household_id",
    )?;
    let id = parse_uuid(&decision.id, "budget_id")?;
    let category = decision
        .category_id
        .as_deref()
        .map(|value| parse_uuid(value, "category_id"))
        .transpose()?;
    let name = decision
        .name
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
    let amount = decision
        .amount_yen
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
    let starts = decision
        .starts_on
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
    let ends = decision
        .ends_on
        .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PolicyPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<budget::Budget, PolicyPersistenceError, _>(async |conn| {
            use public::{
                budgets::dsl as budgets, categories::dsl as categories,
                household_events::dsl as events, household_members::dsl as members,
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
                .execute(&mut *conn)
                .await?;
            record_event(
                conn,
                household,
                id,
                "budget.updated",
                1,
                vec!["name", "amountYen", "categoryId", "startsOn", "endsOn"],
                actor,
            )
            .await?;
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
    command: budget::UpdateBudgetCommand,
) -> Result<budget::Budget, PolicyPersistenceError> {
    let decision =
        budget::decide_update(command).map_err(|_| PolicyPersistenceError::InvalidWorkflow)?;
    let actor = parse_uuid(&decision.actor_id, "actor_id")?;
    let household = parse_uuid(
        decision
            .household_id
            .as_deref()
            .ok_or(PolicyPersistenceError::InvalidWorkflow)?,
        "household_id",
    )?;
    let id = parse_uuid(&decision.id, "budget_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PolicyPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<budget::Budget, PolicyPersistenceError, _>(async |conn| {
            use public::{
                budgets::dsl as budgets, categories::dsl as categories,
                household_events::dsl as events, household_members::dsl as members,
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
            let expected = decision
                .expected_revision
                .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
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
            let name = decision
                .name
                .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
            let amount = decision
                .amount_yen
                .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
            let starts = decision
                .starts_on
                .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
            let ends = decision
                .ends_on
                .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
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
            .execute(&mut *conn)
            .await?;
            record_event(
                conn,
                row.0,
                id,
                "budget.updated",
                revision,
                vec!["name", "amountYen", "categoryId", "startsOn", "endsOn"],
                actor,
            )
            .await?;
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
    command: budget::ArchiveBudgetCommand,
) -> Result<budget::Budget, PolicyPersistenceError> {
    let decision =
        budget::decide_archive(command).map_err(|_| PolicyPersistenceError::InvalidWorkflow)?;
    let actor = parse_uuid(&decision.actor_id, "actor_id")?;
    let household = parse_uuid(
        decision
            .household_id
            .as_deref()
            .ok_or(PolicyPersistenceError::InvalidWorkflow)?,
        "household_id",
    )?;
    let id = parse_uuid(&decision.id, "budget_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PolicyPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<budget::Budget, PolicyPersistenceError, _>(async |conn| {
            use public::{
                budgets::dsl as budgets, household_events::dsl as events,
                household_members::dsl as members,
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
            let expected = decision
                .expected_revision
                .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
            if row.7 != expected {
                return Err(PolicyPersistenceError::Conflict);
            }
            let archived = decision
                .archived
                .ok_or(PolicyPersistenceError::InvalidWorkflow)?;
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
            .execute(&mut *conn)
            .await?;
            record_event(
                conn,
                row.0,
                id,
                "budget.archived",
                revision,
                vec!["archived"],
                actor,
            )
            .await?;
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
    command: recurring_expense::CreateRecurringExpenseCommand,
) -> Result<recurring_expense::RecurringExpense, PolicyPersistenceError> {
    let decision = recurring_expense::decide_create(command)
        .map_err(|_| PolicyPersistenceError::InvalidWorkflow)?;
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
    (&mut *pooled)
        .transaction::<recurring_expense::RecurringExpense, PolicyPersistenceError, _>(
            async |conn| {
                use public::{
                    accounts::dsl as accounts, categories::dsl as categories,
                    household_events::dsl as events, household_members::dsl as members,
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
                    .execute(&mut *conn)
                    .await?;
                record_event(
                    conn,
                    household,
                    id,
                    "recurring_expense.updated",
                    1,
                    vec![
                        "name",
                        "merchant",
                        "amountYen",
                        "categoryId",
                        "paymentAccountId",
                        "cadence",
                        "startsOn",
                        "nextDueOn",
                        "endsOn",
                    ],
                    actor,
                )
                .await?;
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
    command: recurring_expense::UpdateRecurringExpenseCommand,
) -> Result<recurring_expense::RecurringExpense, PolicyPersistenceError> {
    let decision = recurring_expense::decide_update(command)
        .map_err(|_| PolicyPersistenceError::InvalidWorkflow)?;
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
    (&mut *pooled)
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
                .execute(&mut *conn)
                .await?;
                record_event(
                    conn,
                    row.0,
                    id,
                    "recurring_expense.updated",
                    revision,
                    vec![
                        "name",
                        "merchant",
                        "amountYen",
                        "categoryId",
                        "paymentAccountId",
                        "cadence",
                        "startsOn",
                        "nextDueOn",
                        "endsOn",
                    ],
                    actor,
                )
                .await?;
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
    command: recurring_expense::ArchiveRecurringExpenseCommand,
) -> Result<recurring_expense::RecurringExpense, PolicyPersistenceError> {
    let decision = recurring_expense::decide_archive(command)
        .map_err(|_| PolicyPersistenceError::InvalidWorkflow)?;
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
    (&mut *pooled)
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
                .execute(&mut *conn)
                .await?;
                record_event(
                    conn,
                    row.0,
                    id,
                    "recurring_expense.archived",
                    revision,
                    vec!["archived"],
                    actor,
                )
                .await?;
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

async fn require_member(
    conn: &mut diesel_async::AsyncPgConnection,
    household: Uuid,
    actor: Uuid,
) -> Result<(), PolicyPersistenceError> {
    use public::household_members::dsl as members;
    let found = diesel::select(diesel::dsl::exists(
        members::household_members
            .filter(members::household_id.eq(household))
            .filter(members::member_id.eq(actor))
            .filter(members::left_at.is_null()),
    ))
    .first::<bool>(conn)
    .await?;
    if found {
        Ok(())
    } else {
        Err(PolicyPersistenceError::MemberNotFound)
    }
}

async fn record_event(
    conn: &mut diesel_async::AsyncPgConnection,
    household: Uuid,
    entity: Uuid,
    event_type: &'static str,
    revision: i64,
    fields: Vec<&str>,
    actor: Uuid,
) -> Result<(), PolicyPersistenceError> {
    use public::household_events::dsl as events;
    let fields = fields
        .into_iter()
        .map(|field| Some(field.to_owned()))
        .collect::<Vec<_>>();
    diesel::insert_into(events::household_events)
        .values((
            events::household_id.eq(household),
            events::event_type.eq(event_type),
            events::entity_kind.eq(if event_type.starts_with("budget") {
                "budget"
            } else {
                "recurring_expense"
            }),
            events::entity_id.eq(entity),
            events::revision.eq(revision),
            events::changed_fields.eq(fields),
            events::actor_id.eq(actor),
            events::created_at.eq(Utc::now()),
        ))
        .execute(conn)
        .await?;
    Ok(())
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, PolicyPersistenceError> {
    Uuid::parse_str(value).map_err(|_| PolicyPersistenceError::InvalidUuid(field))
}
