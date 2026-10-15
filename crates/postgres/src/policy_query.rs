use std::collections::HashMap;

use chrono::{DateTime, FixedOffset, NaiveDate, Utc};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, OptionalExtension, QueryDsl, dsl::count, pg::Pg,
    result::Error as DieselError,
};
use diesel_async::RunQueryDsl as AsyncRunQueryDsl;
use policy_workflows::{budget, recurring_expense};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum PolicyQueryError {
    Pool(String),
    Database(DieselError),
    InvalidUuid,
    InvalidPeriod,
    InvalidPagination,
    NotFound,
    MemberNotFound,
    AmountOverflow,
}

impl From<DieselError> for PolicyQueryError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BudgetReadModel {
    pub budget: budget::Budget,
    pub actual_yen: i64,
    pub remaining_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Page<T> {
    pub rows: Vec<T>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

pub async fn budgets(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    period: &str,
    after_id: Option<Uuid>,
    before_id: Option<Uuid>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<Page<BudgetReadModel>, PolicyQueryError> {
    let period_start = parse_period(period)?;
    let (limit, backwards) = page_limit(first, last)?;
    let period_end = period_start
        .checked_add_months(chrono::Months::new(1))
        .ok_or(PolicyQueryError::InvalidPeriod)?;
    let (start_utc, end_utc) = (
        period_start
            .and_hms_opt(0, 0, 0)
            .unwrap()
            .and_local_timezone(tokyo())
            .single()
            .unwrap()
            .with_timezone(&Utc),
        period_end
            .and_hms_opt(0, 0, 0)
            .unwrap()
            .and_local_timezone(tokyo())
            .single()
            .unwrap()
            .with_timezone(&Utc),
    );
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PolicyQueryError::Pool(error.to_string()))?;
    require_member(&mut conn, household_id, actor_id).await?;
    use public::budgets::dsl as b;

    let total_count = b::budgets
        .filter(b::household_id.eq(household_id))
        .filter(b::archived.eq(false))
        .filter(b::starts_on.lt(period_end))
        .filter(b::ends_on.is_null().or(b::ends_on.ge(period_start)))
        .select(count(b::id))
        .first::<i64>(&mut conn)
        .await?;
    let mut query = b::budgets
        .filter(b::household_id.eq(household_id))
        .filter(b::archived.eq(false))
        .filter(b::starts_on.lt(period_end))
        .filter(b::ends_on.is_null().or(b::ends_on.ge(period_start)))
        .into_boxed::<Pg>();
    if let Some(cursor_id) = after_id {
        let cursor_name = b::budgets
            .filter(b::household_id.eq(household_id))
            .filter(b::archived.eq(false))
            .filter(b::starts_on.lt(period_end))
            .filter(b::ends_on.is_null().or(b::ends_on.ge(period_start)))
            .filter(b::id.eq(cursor_id))
            .select(b::name)
            .first::<String>(&mut conn)
            .await
            .optional()?
            .ok_or(PolicyQueryError::NotFound)?;
        query = query.filter(
            b::name
                .gt(cursor_name.clone())
                .or(b::name.eq(cursor_name).and(b::id.gt(cursor_id))),
        );
    }
    if let Some(cursor_id) = before_id {
        let cursor_name = b::budgets
            .filter(b::household_id.eq(household_id))
            .filter(b::archived.eq(false))
            .filter(b::starts_on.lt(period_end))
            .filter(b::ends_on.is_null().or(b::ends_on.ge(period_start)))
            .filter(b::id.eq(cursor_id))
            .select(b::name)
            .first::<String>(&mut conn)
            .await
            .optional()?
            .ok_or(PolicyQueryError::NotFound)?;
        query = query.filter(
            b::name
                .lt(cursor_name.clone())
                .or(b::name.eq(cursor_name).and(b::id.lt(cursor_id))),
        );
    }
    let query = if backwards {
        query.order((b::name.desc(), b::id.desc()))
    } else {
        query.order((b::name.asc(), b::id.asc()))
    };
    let mut rows = query
        .select((
            b::id,
            b::household_id,
            b::category_id,
            b::name,
            b::amount_yen,
            b::starts_on,
            b::ends_on,
            b::archived,
            b::revision,
        ))
        .limit(i64::from(limit + 1))
        .load::<BudgetRow>(&mut conn)
        .await?;
    let has_more = rows.len() > limit as usize;
    if has_more {
        rows.truncate(limit as usize);
    }
    if backwards {
        rows.reverse();
    }
    let projections = budget_projections(&mut conn, rows, start_utc, end_utc).await?;
    Ok(Page {
        rows: projections,
        total_count,
        has_next_page: if backwards {
            before_id.is_some()
        } else {
            has_more
        },
        has_previous_page: if backwards {
            has_more
        } else {
            after_id.is_some()
        },
    })
}

pub async fn budget(
    pool: &PgPool,
    actor_id: Uuid,
    budget_id: Uuid,
    period: &str,
) -> Result<BudgetReadModel, PolicyQueryError> {
    let period_start = parse_period(period)?;
    let period_end = period_start
        .checked_add_months(chrono::Months::new(1))
        .ok_or(PolicyQueryError::InvalidPeriod)?;
    let start_utc = period_start
        .and_hms_opt(0, 0, 0)
        .unwrap()
        .and_local_timezone(tokyo())
        .single()
        .unwrap()
        .with_timezone(&Utc);
    let end_utc = period_end
        .and_hms_opt(0, 0, 0)
        .unwrap()
        .and_local_timezone(tokyo())
        .single()
        .unwrap()
        .with_timezone(&Utc);
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PolicyQueryError::Pool(error.to_string()))?;
    use public::budgets::dsl as b;
    let row = b::budgets
        .filter(b::id.eq(budget_id))
        .select((
            b::id,
            b::household_id,
            b::category_id,
            b::name,
            b::amount_yen,
            b::starts_on,
            b::ends_on,
            b::archived,
            b::revision,
        ))
        .first::<BudgetRow>(&mut conn)
        .await
        .optional()?
        .ok_or(PolicyQueryError::NotFound)?;
    require_member(&mut conn, row.1, actor_id).await?;
    budget_projections(&mut conn, vec![row], start_utc, end_utc)
        .await?
        .into_iter()
        .next()
        .ok_or(PolicyQueryError::NotFound)
}

pub async fn budget_for_current_period(
    pool: &PgPool,
    actor_id: Uuid,
    budget_id: Uuid,
) -> Result<BudgetReadModel, PolicyQueryError> {
    let period = Utc::now()
        .with_timezone(&tokyo())
        .format("%Y-%m")
        .to_string();
    budget(pool, actor_id, budget_id, &period).await
}

pub async fn recurring_expenses(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    include_archived: bool,
    after_id: Option<Uuid>,
    before_id: Option<Uuid>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<Page<recurring_expense::RecurringExpense>, PolicyQueryError> {
    let (limit, backwards) = page_limit(first, last)?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PolicyQueryError::Pool(error.to_string()))?;
    require_member(&mut conn, household_id, actor_id).await?;
    use public::recurring_expenses::dsl as e;
    let total_count = if include_archived {
        e::recurring_expenses
            .filter(e::household_id.eq(household_id))
            .select(count(e::id))
            .first::<i64>(&mut conn)
            .await?
    } else {
        e::recurring_expenses
            .filter(e::household_id.eq(household_id))
            .filter(e::archived.eq(false))
            .select(count(e::id))
            .first::<i64>(&mut conn)
            .await?
    };
    let mut query = e::recurring_expenses
        .filter(e::household_id.eq(household_id))
        .into_boxed::<Pg>();
    if !include_archived {
        query = query.filter(e::archived.eq(false));
    }
    if let Some(cursor_id) = after_id {
        let mut cursor_query = e::recurring_expenses
            .filter(e::household_id.eq(household_id))
            .filter(e::id.eq(cursor_id))
            .into_boxed::<Pg>();
        if !include_archived {
            cursor_query = cursor_query.filter(e::archived.eq(false));
        }
        let cursor_due = cursor_query
            .select(e::next_due_on)
            .first::<NaiveDate>(&mut conn)
            .await
            .optional()?
            .ok_or(PolicyQueryError::NotFound)?;
        query = query.filter(
            e::next_due_on
                .gt(cursor_due)
                .or(e::next_due_on.eq(cursor_due).and(e::id.gt(cursor_id))),
        );
    }
    if let Some(cursor_id) = before_id {
        let mut cursor_query = e::recurring_expenses
            .filter(e::household_id.eq(household_id))
            .filter(e::id.eq(cursor_id))
            .into_boxed::<Pg>();
        if !include_archived {
            cursor_query = cursor_query.filter(e::archived.eq(false));
        }
        let cursor_due = cursor_query
            .select(e::next_due_on)
            .first::<NaiveDate>(&mut conn)
            .await
            .optional()?
            .ok_or(PolicyQueryError::NotFound)?;
        query = query.filter(
            e::next_due_on
                .lt(cursor_due)
                .or(e::next_due_on.eq(cursor_due).and(e::id.lt(cursor_id))),
        );
    }
    let query = if backwards {
        query.order((e::next_due_on.desc(), e::id.desc()))
    } else {
        query.order((e::next_due_on.asc(), e::id.asc()))
    };
    let mut rows = query
        .select((
            e::id,
            e::household_id,
            e::name,
            e::merchant,
            e::amount_yen,
            e::currency,
            e::category_id,
            e::payment_account_id,
            e::cadence,
            e::starts_on,
            e::next_due_on,
            e::ends_on,
            e::archived,
            e::revision,
        ))
        .limit(i64::from(limit + 1))
        .load::<RecurringExpenseRow>(&mut conn)
        .await?;
    let has_more = rows.len() > limit as usize;
    if has_more {
        rows.truncate(limit as usize);
    }
    if backwards {
        rows.reverse();
    }
    Ok(Page {
        rows: rows
            .into_iter()
            .map(recurring_expense_from_row)
            .collect::<Result<Vec<_>, _>>()?,
        total_count,
        has_next_page: if backwards {
            before_id.is_some()
        } else {
            has_more
        },
        has_previous_page: if backwards {
            has_more
        } else {
            after_id.is_some()
        },
    })
}

pub async fn budget_household_id(pool: &PgPool, budget_id: Uuid) -> Result<Uuid, PolicyQueryError> {
    use public::budgets::dsl as b;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PolicyQueryError::Pool(error.to_string()))?;
    b::budgets
        .filter(b::id.eq(budget_id))
        .select(b::household_id)
        .first::<Uuid>(&mut conn)
        .await
        .optional()?
        .ok_or(PolicyQueryError::NotFound)
}

pub async fn recurring_expense_household_id(
    pool: &PgPool,
    expense_id: Uuid,
) -> Result<Uuid, PolicyQueryError> {
    use public::recurring_expenses::dsl as e;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PolicyQueryError::Pool(error.to_string()))?;
    e::recurring_expenses
        .filter(e::id.eq(expense_id))
        .select(e::household_id)
        .first::<Uuid>(&mut conn)
        .await
        .optional()?
        .ok_or(PolicyQueryError::NotFound)
}

type BudgetRow = (
    Uuid,
    Uuid,
    Option<Uuid>,
    String,
    i64,
    NaiveDate,
    Option<NaiveDate>,
    bool,
    i64,
);
type RecurringExpenseRow = (
    Uuid,
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
);

async fn budget_projections(
    conn: &mut crate::AsyncPgConnection,
    rows: Vec<BudgetRow>,
    start: DateTime<Utc>,
    end: DateTime<Utc>,
) -> Result<Vec<BudgetReadModel>, PolicyQueryError> {
    if rows.is_empty() {
        return Ok(Vec::new());
    }
    use public::{
        purchase_corrections::dsl as corrections, purchase_items::dsl as items,
        purchases::dsl as purchases,
    };
    let household_ids = rows.iter().map(|row| row.1).collect::<Vec<_>>();
    let purchases = purchases::purchases
        .filter(purchases::household_id.eq_any(&household_ids))
        .filter(purchases::occurred_at.ge(start))
        .filter(purchases::occurred_at.lt(end))
        .filter(purchases::status.eq_any(["POSTED", "CLOSED"]))
        .select((purchases::id, purchases::household_id))
        .load::<(Uuid, Uuid)>(conn)
        .await?;
    let purchase_ids = purchases.iter().map(|row| row.0).collect::<Vec<_>>();
    let household_by_purchase = purchases.into_iter().collect::<HashMap<_, _>>();
    let mut actuals: HashMap<(Uuid, Option<Uuid>), i64> = HashMap::new();
    let mut item_categories = HashMap::new();
    if !purchase_ids.is_empty() {
        let purchase_items = items::purchase_items
            .filter(items::purchase_id.eq_any(&purchase_ids))
            .select((
                items::id,
                items::purchase_id,
                items::category_id,
                items::amount_yen,
            ))
            .load::<(Uuid, Uuid, Option<Uuid>, i64)>(conn)
            .await?;
        for (item_id, purchase_id, category_id, amount) in purchase_items {
            let household = household_by_purchase[&purchase_id];
            add_actual(&mut actuals, (household, None), amount)?;
            if let Some(category) = category_id {
                add_actual(&mut actuals, (household, Some(category)), amount)?;
            }
            item_categories.insert(item_id, category_id);
        }
        let purchase_corrections = corrections::purchase_corrections
            .filter(corrections::purchase_id.eq_any(&purchase_ids))
            .select((
                corrections::purchase_id,
                corrections::item_id,
                corrections::amount_delta_yen,
            ))
            .load::<(Uuid, Option<Uuid>, i64)>(conn)
            .await?;
        for (purchase_id, item_id, amount) in purchase_corrections {
            let household = household_by_purchase[&purchase_id];
            add_actual(&mut actuals, (household, None), amount)?;
            if let Some(category) =
                item_id.and_then(|id| item_categories.get(&id).copied().flatten())
            {
                add_actual(&mut actuals, (household, Some(category)), amount)?;
            }
        }
    }
    let period_start = start.with_timezone(&tokyo()).date_naive();
    let period_end = end.with_timezone(&tokyo()).date_naive();
    rows.into_iter()
        .map(|row| {
            let applies_to_period =
                row.5 < period_end && row.6.is_none_or(|date| date >= period_start);
            let actual_yen = if applies_to_period {
                *actuals.get(&(row.1, row.2)).unwrap_or(&0)
            } else {
                0
            };
            let remaining_yen = row
                .4
                .checked_sub(actual_yen)
                .ok_or(PolicyQueryError::AmountOverflow)?;
            Ok(BudgetReadModel {
                budget: budget::Budget {
                    id: row.0.to_string(),
                    household_id: row.1.to_string(),
                    category_id: row.2.map(|id| id.to_string()),
                    name: row.3,
                    amount_yen: row.4,
                    starts_on: row.5,
                    ends_on: row.6,
                    archived: row.7,
                    revision: row.8,
                },
                actual_yen,
                remaining_yen,
            })
        })
        .collect()
}

fn add_actual(
    totals: &mut HashMap<(Uuid, Option<Uuid>), i64>,
    scope: (Uuid, Option<Uuid>),
    amount: i64,
) -> Result<(), PolicyQueryError> {
    let total = totals.entry(scope).or_default();
    *total = total
        .checked_add(amount)
        .ok_or(PolicyQueryError::AmountOverflow)?;
    Ok(())
}

fn page_limit(first: Option<i32>, last: Option<i32>) -> Result<(i32, bool), PolicyQueryError> {
    if first.is_some() && last.is_some() {
        return Err(PolicyQueryError::InvalidPagination);
    }
    let backwards = last.is_some();
    let limit = first.or(last).unwrap_or(50);
    if !(1..=100).contains(&limit) {
        return Err(PolicyQueryError::InvalidPagination);
    }
    Ok((limit, backwards))
}

fn parse_period(period: &str) -> Result<NaiveDate, PolicyQueryError> {
    let date = NaiveDate::parse_from_str(&format!("{period}-01"), "%Y-%m-%d")
        .map_err(|_| PolicyQueryError::InvalidPeriod)?;
    if date.format("%Y-%m").to_string() != period {
        return Err(PolicyQueryError::InvalidPeriod);
    }
    Ok(date)
}

fn tokyo() -> FixedOffset {
    FixedOffset::east_opt(9 * 60 * 60).expect("Tokyo offset is valid")
}

async fn require_member(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<(), PolicyQueryError> {
    use public::household_members::dsl as members;
    let found = members::household_members
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .filter(members::left_at.is_null())
        .select(members::member_id)
        .first::<Uuid>(conn)
        .await
        .optional()?
        .is_some();
    if found {
        Ok(())
    } else {
        Err(PolicyQueryError::MemberNotFound)
    }
}

fn recurring_expense_from_row(
    row: RecurringExpenseRow,
) -> Result<recurring_expense::RecurringExpense, PolicyQueryError> {
    Ok(recurring_expense::RecurringExpense {
        id: row.0.to_string(),
        household_id: row.1.to_string(),
        name: row.2,
        merchant: row.3,
        amount_yen: row.4,
        currency: row.5,
        category_id: row.6.map(|id| id.to_string()),
        payment_account_id: row.7.map(|id| id.to_string()),
        cadence: recurring_expense::Cadence::parse(&row.8)
            .map_err(|_| PolicyQueryError::Database(DieselError::NotFound))?,
        starts_on: row.9,
        next_due_on: row.10,
        ends_on: row.11,
        archived: row.12,
        revision: row.13,
    })
}
