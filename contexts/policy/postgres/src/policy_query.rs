use std::collections::HashMap;

use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, JoinOnDsl, OptionalExtension, QueryDsl, dsl::count,
    pg::Pg, result::Error as DieselError,
};
use diesel_async::RunQueryDsl as AsyncRunQueryDsl;
use policy_application::{budget, recurring_expense};
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

#[derive(Clone, Copy, Debug)]
pub struct BudgetsQuery<'a> {
    pub actor_id: Uuid,
    pub household_id: Uuid,
    pub period: &'a str,
    pub after_id: Option<Uuid>,
    pub before_id: Option<Uuid>,
    pub first: Option<i32>,
    pub last: Option<i32>,
}

#[derive(Clone, Copy, Debug)]
pub struct BudgetPeriodsQuery<'a> {
    pub actor_id: Uuid,
    pub household_id: Uuid,
    pub from: &'a str,
    pub to: &'a str,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BudgetPeriodReadModel {
    pub period: String,
    pub budgets: Vec<BudgetReadModel>,
}

#[derive(Clone, Copy, Debug)]
pub struct RecurringExpensesQuery {
    pub actor_id: Uuid,
    pub household_id: Uuid,
    pub include_archived: bool,
    pub after_id: Option<Uuid>,
    pub before_id: Option<Uuid>,
    pub first: Option<i32>,
    pub last: Option<i32>,
}

pub async fn budgets(
    pool: &PgPool,
    query: BudgetsQuery<'_>,
) -> Result<Page<BudgetReadModel>, PolicyQueryError> {
    let BudgetsQuery {
        actor_id,
        household_id,
        period,
        after_id,
        before_id,
        first,
        last,
    } = query;
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
    use public::{budget_versions::dsl as versions, budgets::dsl as current_budgets};
    let version_rows = versions::budget_versions
        .inner_join(
            current_budgets::budgets.on(versions::budget_id
                .eq(current_budgets::id)
                .and(versions::household_id.eq(current_budgets::household_id))),
        )
        .filter(versions::household_id.eq(household_id))
        .filter(versions::effective_from.le(period_start))
        .order((
            versions::budget_id.asc(),
            versions::effective_from.desc(),
            versions::revision.desc(),
        ))
        .select((
            versions::budget_id,
            versions::household_id,
            versions::category_id,
            versions::name,
            versions::amount_yen,
            versions::starts_on,
            versions::ends_on,
            versions::archived,
            versions::revision,
            versions::effective_from,
            current_budgets::revision,
        ))
        .load::<BudgetVersionRow>(&mut conn)
        .await?;
    let mut latest_by_budget = HashMap::new();
    for row in version_rows {
        latest_by_budget.entry(row.0).or_insert(row);
    }
    let mut rows = latest_by_budget
        .into_values()
        .filter(|row| !row.7 && row.5 < period_end && row.6.is_none_or(|date| date >= period_start))
        .map(|row| {
            (
                row.0, row.1, row.2, row.3, row.4, row.5, row.6, row.7, row.10,
            )
        })
        .collect::<Vec<BudgetRow>>();
    rows.sort_by(|left, right| left.3.cmp(&right.3).then_with(|| left.0.cmp(&right.0)));
    let total_count = i64::try_from(rows.len()).map_err(|_| PolicyQueryError::AmountOverflow)?;
    let after_index = after_id
        .map(|cursor_id| {
            rows.iter()
                .position(|row| row.0 == cursor_id)
                .ok_or(PolicyQueryError::NotFound)
        })
        .transpose()?;
    let before_index = before_id
        .map(|cursor_id| {
            rows.iter()
                .position(|row| row.0 == cursor_id)
                .ok_or(PolicyQueryError::NotFound)
        })
        .transpose()?;
    let start_index = after_index.map_or(0, |index| index + 1);
    let end_index = before_index.unwrap_or(rows.len());
    let mut rows = if start_index <= end_index {
        rows.drain(start_index..end_index).collect::<Vec<_>>()
    } else {
        Vec::new()
    };
    let has_more = rows.len() > limit as usize;
    if has_more {
        if backwards {
            rows.drain(..rows.len() - limit as usize);
        } else {
            rows.truncate(limit as usize);
        }
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

pub async fn budget_periods(
    pool: &PgPool,
    query: BudgetPeriodsQuery<'_>,
) -> Result<Vec<BudgetPeriodReadModel>, PolicyQueryError> {
    let from = parse_period(query.from)?;
    let to = parse_period(query.to)?;
    let month_count = (to.year() - from.year()) * 12 + to.month() as i32 - from.month() as i32 + 1;
    if month_count <= 0 || month_count > 24 {
        return Err(PolicyQueryError::InvalidPeriod);
    }
    let mut months = Vec::with_capacity(month_count as usize);
    let mut month = from;
    while month <= to {
        months.push(month);
        month = month
            .checked_add_months(chrono::Months::new(1))
            .ok_or(PolicyQueryError::InvalidPeriod)?;
    }
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PolicyQueryError::Pool(error.to_string()))?;
    require_member(&mut conn, query.household_id, query.actor_id).await?;
    use public::{budget_versions::dsl as versions, budgets::dsl as current_budgets};
    let version_rows = versions::budget_versions
        .inner_join(
            current_budgets::budgets.on(versions::budget_id
                .eq(current_budgets::id)
                .and(versions::household_id.eq(current_budgets::household_id))),
        )
        .filter(versions::household_id.eq(query.household_id))
        .filter(versions::effective_from.le(to))
        .order((
            versions::budget_id.asc(),
            versions::effective_from.desc(),
            versions::revision.desc(),
        ))
        .select((
            versions::budget_id,
            versions::household_id,
            versions::category_id,
            versions::name,
            versions::amount_yen,
            versions::starts_on,
            versions::ends_on,
            versions::archived,
            versions::revision,
            versions::effective_from,
            current_budgets::revision,
        ))
        .load::<BudgetVersionRow>(&mut conn)
        .await?;
    let mut versions_by_budget: HashMap<Uuid, Vec<BudgetVersionRow>> = HashMap::new();
    for row in version_rows {
        versions_by_budget.entry(row.0).or_default().push(row);
    }
    let month_rows = months
        .iter()
        .copied()
        .map(|period_start| {
            let period_end = period_start
                .checked_add_months(chrono::Months::new(1))
                .ok_or(PolicyQueryError::InvalidPeriod)?;
            let mut rows = versions_by_budget
                .values()
                .filter_map(|versions| {
                    versions
                        .iter()
                        .find(|row| row.9 <= period_start)
                        .filter(|row| {
                            !row.7
                                && row.5 < period_end
                                && row.6.is_none_or(|date| date >= period_start)
                        })
                        .map(|row| {
                            (
                                row.0,
                                row.1,
                                row.2,
                                row.3.clone(),
                                row.4,
                                row.5,
                                row.6,
                                row.7,
                                row.10,
                            )
                        })
                })
                .collect::<Vec<BudgetRow>>();
            rows.sort_by(|left, right| left.3.cmp(&right.3).then_with(|| left.0.cmp(&right.0)));
            Ok((period_start, rows))
        })
        .collect::<Result<Vec<_>, PolicyQueryError>>()?;
    let from_utc = months[0]
        .and_hms_opt(0, 0, 0)
        .unwrap()
        .and_local_timezone(tokyo())
        .single()
        .unwrap()
        .with_timezone(&Utc);
    let end_month = months[months.len() - 1]
        .checked_add_months(chrono::Months::new(1))
        .ok_or(PolicyQueryError::InvalidPeriod)?;
    let to_utc = end_month
        .and_hms_opt(0, 0, 0)
        .unwrap()
        .and_local_timezone(tokyo())
        .single()
        .unwrap()
        .with_timezone(&Utc);
    budget_period_projections(&mut conn, month_rows, from_utc, to_utc).await
}

pub async fn budget(
    pool: &PgPool,
    actor_id: Uuid,
    budget_id: Uuid,
    period: &str,
) -> Result<BudgetReadModel, PolicyQueryError> {
    budget_at_period(pool, actor_id, budget_id, period, false).await
}

pub async fn budget_including_archived(
    pool: &PgPool,
    actor_id: Uuid,
    budget_id: Uuid,
    period: &str,
) -> Result<BudgetReadModel, PolicyQueryError> {
    budget_at_period(pool, actor_id, budget_id, period, true).await
}

async fn budget_at_period(
    pool: &PgPool,
    actor_id: Uuid,
    budget_id: Uuid,
    period: &str,
    include_archived: bool,
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
    use public::{budget_versions::dsl as versions, budgets::dsl as current_budgets};
    let row = versions::budget_versions
        .inner_join(
            current_budgets::budgets.on(versions::budget_id
                .eq(current_budgets::id)
                .and(versions::household_id.eq(current_budgets::household_id))),
        )
        .filter(versions::budget_id.eq(budget_id))
        .filter(versions::effective_from.le(period_start))
        .order((versions::effective_from.desc(), versions::revision.desc()))
        .select((
            versions::budget_id,
            versions::household_id,
            versions::category_id,
            versions::name,
            versions::amount_yen,
            versions::starts_on,
            versions::ends_on,
            versions::archived,
            versions::revision,
            versions::effective_from,
            current_budgets::revision,
        ))
        .first::<BudgetVersionRow>(&mut conn)
        .await
        .optional()?
        .ok_or(PolicyQueryError::NotFound)?;
    require_member(&mut conn, row.1, actor_id).await?;
    if (!include_archived && row.7)
        || row.5 >= period_end
        || row.6.is_some_and(|date| date < period_start)
    {
        return Err(PolicyQueryError::NotFound);
    }
    let row = (
        row.0, row.1, row.2, row.3, row.4, row.5, row.6, row.7, row.10,
    );
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

pub async fn budget_for_current_period_including_archived(
    pool: &PgPool,
    actor_id: Uuid,
    budget_id: Uuid,
) -> Result<BudgetReadModel, PolicyQueryError> {
    let period = Utc::now()
        .with_timezone(&tokyo())
        .format("%Y-%m")
        .to_string();
    budget_including_archived(pool, actor_id, budget_id, &period).await
}

pub async fn recurring_expenses(
    pool: &PgPool,
    query: RecurringExpensesQuery,
) -> Result<Page<recurring_expense::RecurringExpense>, PolicyQueryError> {
    let RecurringExpensesQuery {
        actor_id,
        household_id,
        include_archived,
        after_id,
        before_id,
        first,
        last,
    } = query;
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
type BudgetVersionRow = (
    Uuid,
    Uuid,
    Option<Uuid>,
    String,
    i64,
    NaiveDate,
    Option<NaiveDate>,
    bool,
    i64,
    NaiveDate,
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
        .filter(purchases::status.eq("POSTED"))
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
            budget_read_model(row, actual_yen)
        })
        .collect()
}

async fn budget_period_projections(
    conn: &mut crate::AsyncPgConnection,
    month_rows: Vec<(NaiveDate, Vec<BudgetRow>)>,
    start: DateTime<Utc>,
    end: DateTime<Utc>,
) -> Result<Vec<BudgetPeriodReadModel>, PolicyQueryError> {
    let all_rows = month_rows
        .iter()
        .flat_map(|(_, rows)| rows.iter())
        .collect::<Vec<_>>();
    let mut actuals = HashMap::new();
    if !all_rows.is_empty() {
        use public::{
            purchase_corrections::dsl as corrections, purchase_items::dsl as items,
            purchases::dsl as purchases,
        };
        let household_ids = all_rows
            .iter()
            .map(|row| row.1)
            .collect::<std::collections::HashSet<_>>()
            .into_iter()
            .collect::<Vec<_>>();
        let purchases = purchases::purchases
            .filter(purchases::household_id.eq_any(&household_ids))
            .filter(purchases::occurred_at.ge(start))
            .filter(purchases::occurred_at.lt(end))
            .filter(purchases::status.eq("POSTED"))
            .select((
                purchases::id,
                purchases::household_id,
                purchases::occurred_at,
            ))
            .load::<(Uuid, Uuid, DateTime<Utc>)>(conn)
            .await?;
        let purchase_ids = purchases.iter().map(|row| row.0).collect::<Vec<_>>();
        let household_month_by_purchase = purchases
            .into_iter()
            .map(|(id, household, occurred_at)| {
                let occurred_on = occurred_at.with_timezone(&tokyo()).date_naive();
                (id, (household, month_start(occurred_on)))
            })
            .collect::<HashMap<_, _>>();
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
                let (household, month) = household_month_by_purchase[&purchase_id];
                add_period_actual(&mut actuals, (month, household, None), amount)?;
                if let Some(category_id) = category_id {
                    add_period_actual(&mut actuals, (month, household, Some(category_id)), amount)?;
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
                let (household, month) = household_month_by_purchase[&purchase_id];
                add_period_actual(&mut actuals, (month, household, None), amount)?;
                if let Some(category_id) =
                    item_id.and_then(|id| item_categories.get(&id).copied().flatten())
                {
                    add_period_actual(&mut actuals, (month, household, Some(category_id)), amount)?;
                }
            }
        }
    }
    month_rows
        .into_iter()
        .map(|(period_start, rows)| {
            let budgets = rows
                .into_iter()
                .map(|row| {
                    let actual = *actuals.get(&(period_start, row.1, row.2)).unwrap_or(&0);
                    budget_read_model(row, actual)
                })
                .collect::<Result<Vec<_>, _>>()?;
            Ok(BudgetPeriodReadModel {
                period: period_start.format("%Y-%m").to_string(),
                budgets,
            })
        })
        .collect()
}

fn budget_read_model(row: BudgetRow, actual_yen: i64) -> Result<BudgetReadModel, PolicyQueryError> {
    let remaining_yen = row
        .4
        .checked_sub(actual_yen)
        .ok_or(PolicyQueryError::AmountOverflow)?;
    let budget = budget::Budget::reconstitute(
        budget::BudgetId::from_persisted(row.0),
        budget::BudgetHouseholdId::from_persisted(row.1),
        row.2.map(budget::BudgetCategoryId::from_persisted),
        row.3,
        row.4,
        row.5,
        row.6,
        row.7,
        row.8,
    )
    .map_err(|_| PolicyQueryError::Database(DieselError::NotFound))?;
    Ok(BudgetReadModel {
        budget,
        actual_yen,
        remaining_yen,
    })
}

fn add_period_actual(
    totals: &mut HashMap<(NaiveDate, Uuid, Option<Uuid>), i64>,
    scope: (NaiveDate, Uuid, Option<Uuid>),
    amount: i64,
) -> Result<(), PolicyQueryError> {
    let total = totals.entry(scope).or_default();
    *total = total
        .checked_add(amount)
        .ok_or(PolicyQueryError::AmountOverflow)?;
    Ok(())
}

fn month_start(date: NaiveDate) -> NaiveDate {
    date.with_day(1).expect("first day of month is valid")
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
        id: recurring_expense::RecurringExpenseId::from_persisted(row.0),
        household_id: recurring_expense::RecurringExpenseHouseholdId::from_persisted(row.1),
        name: row.2,
        merchant: row.3,
        amount_yen: recurring_expense::RecurringExpenseAmountYen::try_from(row.4)
            .map_err(|_| PolicyQueryError::Database(DieselError::NotFound))?,
        currency: row.5,
        category_id: row
            .6
            .map(recurring_expense::RecurringExpenseCategoryId::from_persisted),
        payment_account_id: row
            .7
            .map(recurring_expense::RecurringExpensePaymentAccountId::from_persisted),
        cadence: recurring_expense::Cadence::parse(&row.8)
            .map_err(|_| PolicyQueryError::Database(DieselError::NotFound))?,
        starts_on: row.9,
        next_due_on: row.10,
        ends_on: row.11,
        archived: row.12,
        revision: recurring_expense::RecurringExpenseRevision::try_from(row.13)
            .map_err(|_| PolicyQueryError::Database(DieselError::NotFound))?,
    })
}
