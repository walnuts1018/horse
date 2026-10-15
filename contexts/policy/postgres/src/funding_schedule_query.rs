use chrono::{DateTime, NaiveDate, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::set_funding_schedule::PersistedFundingSchedule;
use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum FundingSchedulesQueryError {
    Pool(String),
    Database(DieselError),
    HouseholdNotFound,
    MemberNotFound,
    CursorNotFound,
}

impl From<DieselError> for FundingSchedulesQueryError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub struct FundingSchedulesPage {
    pub rows: Vec<PersistedFundingSchedule>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Clone, Copy, Debug)]
pub struct FundingSchedulesQuery {
    pub actor_id: Uuid,
    pub household_id: Uuid,
    pub active: Option<bool>,
    pub after_id: Option<Uuid>,
    pub before_id: Option<Uuid>,
    pub page_size: usize,
    pub backward: bool,
}

#[derive(diesel::Queryable)]
struct FundingScheduleRow {
    id: Uuid,
    household_id: Uuid,
    member_id: Uuid,
    pool_id: Uuid,
    amount_yen: i64,
    cadence: String,
    starts_on: NaiveDate,
    ends_on: Option<NaiveDate>,
    active: bool,
    created_at: DateTime<Utc>,
}

pub async fn funding_schedules(
    pool: &PgPool,
    query: FundingSchedulesQuery,
) -> Result<FundingSchedulesPage, FundingSchedulesQueryError> {
    use public::{funding_schedules as schedules, household_members as members, households};

    let FundingSchedulesQuery {
        actor_id,
        household_id,
        active,
        after_id,
        before_id,
        page_size,
        backward,
    } = query;

    let mut conn = pool
        .get()
        .await
        .map_err(|error| FundingSchedulesQueryError::Pool(error.to_string()))?;
    let household_exists = households::table
        .filter(households::id.eq(household_id))
        .select(households::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .is_some();
    if !household_exists {
        return Err(FundingSchedulesQueryError::HouseholdNotFound);
    }
    let member_exists = members::table
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .filter(members::left_at.is_null())
        .select(members::member_id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .is_some();
    if !member_exists {
        return Err(FundingSchedulesQueryError::MemberNotFound);
    }

    let mut base_query = schedules::table
        .filter(schedules::household_id.eq(household_id))
        .into_boxed();
    if let Some(active) = active {
        base_query = base_query.filter(schedules::active.eq(active));
    }
    let ordered_rows = base_query
        .order_by((schedules::starts_on.asc(), schedules::id.asc()))
        .select((
            schedules::id,
            schedules::household_id,
            schedules::member_id,
            schedules::pool_id,
            schedules::amount_yen,
            schedules::cadence,
            schedules::starts_on,
            schedules::ends_on,
            schedules::active,
            schedules::created_at,
        ))
        .load::<FundingScheduleRow>(&mut *conn)
        .await?;
    let total_count = i64::try_from(ordered_rows.len()).unwrap_or(i64::MAX);
    let after_index = find_cursor_index(&ordered_rows, after_id)?;
    let before_index = find_cursor_index(&ordered_rows, before_id)?;
    let start = after_index.map_or(0, |index| index + 1);
    let end = before_index.unwrap_or(ordered_rows.len());
    let range_end = end.max(start).min(ordered_rows.len());
    let range_start = start.min(range_end);
    let selected = &ordered_rows[range_start..range_end];
    let has_more = selected.len() > page_size;
    let page_slice = if backward && has_more {
        &selected[selected.len() - page_size..]
    } else {
        &selected[..selected.len().min(page_size)]
    };
    let mut rows = page_slice
        .iter()
        .map(|row| PersistedFundingSchedule {
            id: row.id.to_string(),
            household_id: row.household_id.to_string(),
            member_id: row.member_id.to_string(),
            pool_id: row.pool_id.to_string(),
            amount_yen: row.amount_yen,
            cadence: row.cadence.clone(),
            starts_on: row.starts_on.format("%Y-%m-%d").to_string(),
            ends_on: row.ends_on.map(|date| date.format("%Y-%m-%d").to_string()),
            active: row.active,
            created_at: row.created_at,
        })
        .collect::<Vec<_>>();
    if backward {
        rows.reverse();
    }
    Ok(FundingSchedulesPage {
        rows,
        total_count,
        has_next_page: if backward {
            before_id.is_some()
        } else {
            has_more
        },
        has_previous_page: if backward {
            has_more
        } else {
            after_id.is_some()
        },
    })
}

fn find_cursor_index(
    rows: &[FundingScheduleRow],
    cursor_id: Option<Uuid>,
) -> Result<Option<usize>, FundingSchedulesQueryError> {
    cursor_id
        .map(|id| {
            rows.iter()
                .position(|row| row.id == id)
                .ok_or(FundingSchedulesQueryError::CursorNotFound)
        })
        .transpose()
}
