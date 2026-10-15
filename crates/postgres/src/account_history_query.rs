use std::collections::{HashMap, HashSet};

use chrono::{DateTime, Utc};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, OptionalExtension, QueryDsl,
    result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountProjection {
    pub id: Uuid,
    pub name: String,
    pub kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<Uuid>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountMovementProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub kind: String,
    pub occurred_at: DateTime<Utc>,
    pub from_account_id: Uuid,
    pub from_account: AccountProjection,
    pub to_account_id: Uuid,
    pub to_account: AccountProjection,
    pub amount_yen: i64,
    pub description: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountIncomeProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub account_id: Uuid,
    pub account: AccountProjection,
    pub amount_yen: i64,
    pub occurred_at: DateTime<Utc>,
    pub description: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountMovementPage {
    pub rows: Vec<AccountMovementProjection>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountIncomePage {
    pub rows: Vec<AccountIncomeProjection>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug)]
pub enum AccountHistoryQueryError {
    Pool(String),
    Database(DieselError),
    HouseholdNotFound,
    InvalidPagination,
    InvalidCursor,
    InvalidPersistedData,
}

impl From<DieselError> for AccountHistoryQueryError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn account_movements(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    after_id: Option<Uuid>,
    before_id: Option<Uuid>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<AccountMovementPage, AccountHistoryQueryError> {
    let (page_size, backward) = page_size(first, last)?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| AccountHistoryQueryError::Pool(error.to_string()))?;
    use public::account_movements as movements;

    require_member(&mut conn, actor_id, household_id).await?;
    let total_count = movements::table
        .filter(movements::household_id.eq(household_id))
        .count()
        .get_result::<i64>(&mut *conn)
        .await?;
    let after = movement_cursor(&mut conn, household_id, after_id).await?;
    let before = movement_cursor(&mut conn, household_id, before_id).await?;
    let mut query = movements::table
        .filter(movements::household_id.eq(household_id))
        .into_boxed();
    if let Some((occurred_at, id)) = after {
        query = query.filter(
            movements::occurred_at
                .lt(occurred_at)
                .or(movements::occurred_at
                    .eq(occurred_at)
                    .and(movements::id.lt(id))),
        );
    }
    if let Some((occurred_at, id)) = before {
        query = query.filter(
            movements::occurred_at
                .gt(occurred_at)
                .or(movements::occurred_at
                    .eq(occurred_at)
                    .and(movements::id.gt(id))),
        );
    }
    let mut rows = if backward {
        query
            .order_by(movements::occurred_at.asc())
            .then_order_by(movements::id.asc())
            .limit(i64::try_from(page_size + 1).expect("page size is bounded"))
            .select((
                movements::id,
                movements::household_id,
                movements::kind,
                movements::occurred_at,
                movements::from_account_id,
                movements::to_account_id,
                movements::amount_yen,
                movements::description,
                movements::created_at,
            ))
            .load::<(
                Uuid,
                Uuid,
                String,
                DateTime<Utc>,
                Uuid,
                Uuid,
                i64,
                String,
                DateTime<Utc>,
            )>(&mut *conn)
            .await?
    } else {
        query
            .order_by(movements::occurred_at.desc())
            .then_order_by(movements::id.desc())
            .limit(i64::try_from(page_size + 1).expect("page size is bounded"))
            .select((
                movements::id,
                movements::household_id,
                movements::kind,
                movements::occurred_at,
                movements::from_account_id,
                movements::to_account_id,
                movements::amount_yen,
                movements::description,
                movements::created_at,
            ))
            .load::<(
                Uuid,
                Uuid,
                String,
                DateTime<Utc>,
                Uuid,
                Uuid,
                i64,
                String,
                DateTime<Utc>,
            )>(&mut *conn)
            .await?
    };
    let has_extra = rows.len() > page_size;
    rows.truncate(page_size);
    if backward {
        rows.reverse();
    }
    let account_ids = rows
        .iter()
        .flat_map(|row| [row.4, row.5])
        .collect::<HashSet<_>>();
    let accounts = load_accounts(&mut conn, household_id, account_ids).await?;
    let rows = rows
        .into_iter()
        .map(
            |(
                id,
                household_id,
                kind,
                occurred_at,
                from_id,
                to_id,
                amount_yen,
                description,
                created_at,
            )| {
                let from_account = accounts
                    .get(&from_id)
                    .cloned()
                    .ok_or(AccountHistoryQueryError::InvalidPersistedData)?;
                let to_account = accounts
                    .get(&to_id)
                    .cloned()
                    .ok_or(AccountHistoryQueryError::InvalidPersistedData)?;
                if !matches!(kind.as_str(), "TRANSFER" | "CARD_BILL_PAYMENT") {
                    return Err(AccountHistoryQueryError::InvalidPersistedData);
                }
                Ok(AccountMovementProjection {
                    id,
                    household_id,
                    kind,
                    occurred_at,
                    from_account_id: from_id,
                    from_account,
                    to_account_id: to_id,
                    to_account,
                    amount_yen,
                    description,
                    created_at,
                })
            },
        )
        .collect::<Result<Vec<_>, _>>()?;
    Ok(AccountMovementPage {
        rows,
        total_count,
        has_next_page: if backward {
            before_id.is_some()
        } else {
            has_extra
        },
        has_previous_page: if backward {
            has_extra
        } else {
            after_id.is_some()
        },
    })
}

pub async fn account_incomes(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    after_id: Option<Uuid>,
    before_id: Option<Uuid>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<AccountIncomePage, AccountHistoryQueryError> {
    let (page_size, backward) = page_size(first, last)?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| AccountHistoryQueryError::Pool(error.to_string()))?;
    use public::account_incomes as incomes;

    require_member(&mut conn, actor_id, household_id).await?;
    let total_count = incomes::table
        .filter(incomes::household_id.eq(household_id))
        .count()
        .get_result::<i64>(&mut *conn)
        .await?;
    let after = income_cursor(&mut conn, household_id, after_id).await?;
    let before = income_cursor(&mut conn, household_id, before_id).await?;
    let mut query = incomes::table
        .filter(incomes::household_id.eq(household_id))
        .into_boxed();
    if let Some((occurred_at, id)) = after {
        query = query.filter(
            incomes::occurred_at
                .lt(occurred_at)
                .or(incomes::occurred_at.eq(occurred_at).and(incomes::id.lt(id))),
        );
    }
    if let Some((occurred_at, id)) = before {
        query = query.filter(
            incomes::occurred_at
                .gt(occurred_at)
                .or(incomes::occurred_at.eq(occurred_at).and(incomes::id.gt(id))),
        );
    }
    let mut rows = if backward {
        query
            .order_by(incomes::occurred_at.asc())
            .then_order_by(incomes::id.asc())
            .limit(i64::try_from(page_size + 1).expect("page size is bounded"))
            .select((
                incomes::id,
                incomes::household_id,
                incomes::account_id,
                incomes::amount_yen,
                incomes::occurred_at,
                incomes::description,
                incomes::created_at,
            ))
            .load::<(Uuid, Uuid, Uuid, i64, DateTime<Utc>, String, DateTime<Utc>)>(&mut *conn)
            .await?
    } else {
        query
            .order_by(incomes::occurred_at.desc())
            .then_order_by(incomes::id.desc())
            .limit(i64::try_from(page_size + 1).expect("page size is bounded"))
            .select((
                incomes::id,
                incomes::household_id,
                incomes::account_id,
                incomes::amount_yen,
                incomes::occurred_at,
                incomes::description,
                incomes::created_at,
            ))
            .load::<(Uuid, Uuid, Uuid, i64, DateTime<Utc>, String, DateTime<Utc>)>(&mut *conn)
            .await?
    };
    let has_extra = rows.len() > page_size;
    rows.truncate(page_size);
    if backward {
        rows.reverse();
    }
    let accounts = load_accounts(
        &mut conn,
        household_id,
        rows.iter().map(|row| row.2).collect(),
    )
    .await?;
    let rows = rows
        .into_iter()
        .map(
            |(id, household_id, account_id, amount_yen, occurred_at, description, created_at)| {
                let account = accounts
                    .get(&account_id)
                    .cloned()
                    .ok_or(AccountHistoryQueryError::InvalidPersistedData)?;
                Ok::<AccountIncomeProjection, AccountHistoryQueryError>(AccountIncomeProjection {
                    id,
                    household_id,
                    account_id,
                    account,
                    amount_yen,
                    occurred_at,
                    description,
                    created_at,
                })
            },
        )
        .collect::<Result<Vec<_>, _>>()?;
    Ok(AccountIncomePage {
        rows,
        total_count,
        has_next_page: if backward {
            before_id.is_some()
        } else {
            has_extra
        },
        has_previous_page: if backward {
            has_extra
        } else {
            after_id.is_some()
        },
    })
}

async fn require_member(
    conn: &mut diesel_async::AsyncPgConnection,
    actor_id: Uuid,
    household_id: Uuid,
) -> Result<(), AccountHistoryQueryError> {
    use public::household_members;

    let is_member = diesel::select(diesel::dsl::exists(
        household_members::table
            .filter(household_members::household_id.eq(household_id))
            .filter(household_members::member_id.eq(actor_id))
            .filter(household_members::left_at.is_null()),
    ))
    .get_result::<bool>(conn)
    .await?;
    if is_member {
        Ok(())
    } else {
        Err(AccountHistoryQueryError::HouseholdNotFound)
    }
}

async fn movement_cursor(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    id: Option<Uuid>,
) -> Result<Option<(DateTime<Utc>, Uuid)>, AccountHistoryQueryError> {
    use public::account_movements as movements;
    match id {
        Some(id) => movements::table
            .filter(movements::household_id.eq(household_id))
            .filter(movements::id.eq(id))
            .select((movements::occurred_at, movements::id))
            .first::<(DateTime<Utc>, Uuid)>(conn)
            .await
            .optional()
            .map_err(AccountHistoryQueryError::from)?
            .ok_or(AccountHistoryQueryError::InvalidCursor)
            .map(Some),
        None => Ok(None),
    }
}

async fn income_cursor(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    id: Option<Uuid>,
) -> Result<Option<(DateTime<Utc>, Uuid)>, AccountHistoryQueryError> {
    use public::account_incomes as incomes;
    match id {
        Some(id) => incomes::table
            .filter(incomes::household_id.eq(household_id))
            .filter(incomes::id.eq(id))
            .select((incomes::occurred_at, incomes::id))
            .first::<(DateTime<Utc>, Uuid)>(conn)
            .await
            .optional()
            .map_err(AccountHistoryQueryError::from)?
            .ok_or(AccountHistoryQueryError::InvalidCursor)
            .map(Some),
        None => Ok(None),
    }
}

async fn load_accounts(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    account_ids: HashSet<Uuid>,
) -> Result<HashMap<Uuid, AccountProjection>, AccountHistoryQueryError> {
    use public::accounts;
    if account_ids.is_empty() {
        return Ok(HashMap::new());
    }
    let rows = accounts::table
        .filter(accounts::household_id.eq(household_id))
        .filter(accounts::id.eq_any(account_ids))
        .select((
            accounts::id,
            accounts::name,
            accounts::kind,
            accounts::card_last4,
            accounts::pool_id,
        ))
        .load::<(Uuid, String, String, Option<String>, Option<Uuid>)>(conn)
        .await?;
    rows.into_iter()
        .map(|(id, name, kind, card_last4, pool_id)| {
            if !matches!(kind.as_str(), "BANK" | "CASH" | "CARD" | "OTHER") {
                return Err(AccountHistoryQueryError::InvalidPersistedData);
            }
            Ok((
                id,
                AccountProjection {
                    id,
                    name,
                    kind,
                    card_last4,
                    pool_id,
                },
            ))
        })
        .collect()
}

fn page_size(
    first: Option<i32>,
    last: Option<i32>,
) -> Result<(usize, bool), AccountHistoryQueryError> {
    if first.is_some() && last.is_some() {
        return Err(AccountHistoryQueryError::InvalidPagination);
    }
    let backward = last.is_some();
    let size = usize::try_from(last.or(first).unwrap_or(50))
        .ok()
        .filter(|size| (1..=100).contains(size))
        .ok_or(AccountHistoryQueryError::InvalidPagination)?;
    Ok((size, backward))
}
