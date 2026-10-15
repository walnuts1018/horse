use chrono::{DateTime, Utc};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, OptionalExtension, QueryDsl, dsl::count_star,
    result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LedgerCursor {
    pub id: Uuid,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LedgerRow {
    pub id: Uuid,
    pub source_kind: String,
    pub source_id: Uuid,
    pub source_purchase_id: Option<Uuid>,
    pub occurred_at: DateTime<Utc>,
    pub description: String,
    pub amount_yen: i64,
    pub from_member_id: Option<Uuid>,
    pub pool_id: Option<Uuid>,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug)]
pub struct LedgerPage {
    pub rows: Vec<LedgerRow>,
    pub total_count: i64,
    pub has_extra: bool,
    pub backward: bool,
    pub after_has_previous_page: bool,
    pub before_has_next_page: bool,
}

#[derive(Debug)]
pub enum LedgerQueryError {
    Pool(String),
    Database(DieselError),
    HouseholdNotFound,
    InvalidPagination,
    MissingCursor,
}

impl From<DieselError> for LedgerQueryError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn ledger(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    after: Option<LedgerCursor>,
    before: Option<LedgerCursor>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<LedgerPage, LedgerQueryError> {
    if !crate::household_access::is_active_member(pool, household_id, actor_id)
        .await
        .map_err(|error| match error {
            crate::household_access::HouseholdAccessError::Pool(message) => {
                LedgerQueryError::Pool(message)
            }
            crate::household_access::HouseholdAccessError::Database(error) => {
                LedgerQueryError::Database(error)
            }
        })?
    {
        return Err(LedgerQueryError::HouseholdNotFound);
    }
    let (page_size, backward) = page_size(first, last)?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| LedgerQueryError::Pool(error.to_string()))?;
    use public::ledger_entries as ledger;
    let after_time = match &after {
        Some(cursor) => Some(cursor_time(&mut conn, household_id, cursor.id).await?),
        None => None,
    };
    let before_time = match &before {
        Some(cursor) => Some(cursor_time(&mut conn, household_id, cursor.id).await?),
        None => None,
    };
    let total_count = ledger::table
        .filter(ledger::household_id.eq(household_id))
        .select(count_star())
        .first::<i64>(&mut *conn)
        .await?;
    let mut after_has_previous_page = false;
    if let Some(cursor_time) = after_time {
        after_has_previous_page = ledger::table
            .filter(ledger::household_id.eq(household_id))
            .filter(
                ledger::occurred_at.gt(cursor_time).or(ledger::occurred_at
                    .eq(cursor_time)
                    .and(ledger::id.gt(after.as_ref().expect("after cursor exists").id))),
            )
            .select(ledger::id)
            .first::<Uuid>(&mut *conn)
            .await
            .optional()?
            .is_some();
    }
    let mut before_has_next_page = false;
    if let Some(cursor_time) = before_time {
        before_has_next_page = ledger::table
            .filter(ledger::household_id.eq(household_id))
            .filter(
                ledger::occurred_at.lt(cursor_time).or(ledger::occurred_at
                    .eq(cursor_time)
                    .and(ledger::id.lt(before.as_ref().expect("before cursor exists").id))),
            )
            .select(ledger::id)
            .first::<Uuid>(&mut *conn)
            .await
            .optional()?
            .is_some();
    }
    let mut query = ledger::table
        .left_join(
            public::purchase_corrections::table.on(public::purchase_corrections::id
                .eq(ledger::source_id)
                .and(public::purchase_corrections::household_id.eq(ledger::household_id))),
        )
        .filter(ledger::household_id.eq(household_id))
        .into_boxed();
    if let (Some(cursor), Some(cursor_time)) = (after, after_time) {
        query = query.filter(
            ledger::occurred_at.lt(cursor_time).or(ledger::occurred_at
                .eq(cursor_time)
                .and(ledger::id.lt(cursor.id))),
        );
    }
    if let (Some(cursor), Some(cursor_time)) = (before, before_time) {
        query = query.filter(
            ledger::occurred_at.gt(cursor_time).or(ledger::occurred_at
                .eq(cursor_time)
                .and(ledger::id.gt(cursor.id))),
        );
    }
    let selected = query.select((
        ledger::id,
        ledger::source_kind,
        ledger::source_id,
        public::purchase_corrections::purchase_id.nullable(),
        ledger::occurred_at,
        ledger::description,
        ledger::amount_yen,
        ledger::from_member_id,
        ledger::pool_id,
        ledger::created_at,
    ));
    let values = if backward {
        selected
            .order((ledger::occurred_at.asc(), ledger::id.asc()))
            .limit(i64::from(page_size) + 1)
            .load::<(
                Uuid,
                String,
                Uuid,
                Option<Uuid>,
                DateTime<Utc>,
                String,
                i64,
                Option<Uuid>,
                Option<Uuid>,
                DateTime<Utc>,
            )>(&mut *conn)
            .await?
    } else {
        selected
            .order((ledger::occurred_at.desc(), ledger::id.desc()))
            .limit(i64::from(page_size) + 1)
            .load::<(
                Uuid,
                String,
                Uuid,
                Option<Uuid>,
                DateTime<Utc>,
                String,
                i64,
                Option<Uuid>,
                Option<Uuid>,
                DateTime<Utc>,
            )>(&mut *conn)
            .await?
    };
    let has_extra = values.len() > page_size as usize;
    let mut values = values
        .into_iter()
        .take(page_size as usize)
        .map(
            |(
                id,
                source_kind,
                source_id,
                source_purchase_id,
                occurred_at,
                description,
                amount_yen,
                from_member_id,
                pool_id,
                created_at,
            )| LedgerRow {
                id,
                source_kind,
                source_id,
                source_purchase_id,
                occurred_at,
                description,
                amount_yen,
                from_member_id,
                pool_id,
                created_at,
            },
        )
        .collect::<Vec<_>>();
    if backward {
        values.reverse();
    }
    Ok(LedgerPage {
        rows: values,
        total_count,
        has_extra,
        backward,
        after_has_previous_page,
        before_has_next_page,
    })
}

async fn cursor_time(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    cursor_id: Uuid,
) -> Result<DateTime<Utc>, LedgerQueryError> {
    use public::ledger_entries as ledger;
    ledger::table
        .filter(ledger::household_id.eq(household_id))
        .filter(ledger::id.eq(cursor_id))
        .select(ledger::occurred_at)
        .first::<DateTime<Utc>>(conn)
        .await
        .optional()?
        .ok_or(LedgerQueryError::MissingCursor)
}

fn page_size(first: Option<i32>, last: Option<i32>) -> Result<(i64, bool), LedgerQueryError> {
    if first.is_some() && last.is_some() {
        return Err(LedgerQueryError::InvalidPagination);
    }
    let backward = last.is_some();
    let size = last.or(first).unwrap_or(50);
    if !(1..=100).contains(&size) {
        return Err(LedgerQueryError::InvalidPagination);
    }
    Ok((i64::from(size), backward))
}
