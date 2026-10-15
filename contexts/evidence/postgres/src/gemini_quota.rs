use chrono::{DateTime, Datelike, Duration, NaiveDate, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, insert_into, upsert::excluded};
use diesel_async::{AsyncConnection, RunQueryDsl as AsyncRunQueryDsl};
use uuid::Uuid;

use crate::PgPool;

diesel::table! {
    gemini_quota_reservation_locks (project_route, model) {
        project_route -> Text,
        model -> Text,
        updated_at -> Timestamptz,
    }
}

diesel::table! {
    gemini_quota_reservations (id) {
        id -> Uuid,
        project_route -> Text,
        model -> Text,
        reserved_at -> Timestamptz,
        input_tokens -> Int8,
        actual_input_tokens -> Nullable<Int8>,
        settled_at -> Nullable<Timestamptz>,
    }
}

diesel::table! {
    gemini_quota_cooldowns (project_route, model) {
        project_route -> Text,
        model -> Text,
        blocked_until -> Timestamptz,
        updated_at -> Timestamptz,
    }
}

diesel::allow_tables_to_appear_in_same_query!(
    gemini_quota_reservation_locks,
    gemini_quota_reservations,
    gemini_quota_cooldowns,
);

#[derive(Clone, Copy)]
pub struct QuotaLimit {
    pub requests_per_minute: i64,
    pub input_tokens_minute: i64,
    pub requests_per_day: i64,
}

#[derive(Debug)]
pub enum QuotaError {
    Pool,
    Database,
    InputTooLarge,
}

#[derive(Queryable, Selectable)]
#[diesel(table_name = gemini_quota_reservations)]
struct Reservation {
    id: Uuid,
    project_route: String,
    model: String,
    reserved_at: DateTime<Utc>,
    input_tokens: i64,
    actual_input_tokens: Option<i64>,
    settled_at: Option<DateTime<Utc>>,
}

#[derive(Insertable)]
#[diesel(table_name = gemini_quota_reservation_locks)]
struct Lock<'a> {
    project_route: &'a str,
    model: &'a str,
    updated_at: DateTime<Utc>,
}

#[derive(Insertable)]
#[diesel(table_name = gemini_quota_reservations)]
struct NewReservation<'a> {
    id: Uuid,
    project_route: &'a str,
    model: &'a str,
    reserved_at: DateTime<Utc>,
    input_tokens: i64,
}

#[derive(Insertable)]
#[diesel(table_name = gemini_quota_cooldowns)]
struct Cooldown<'a> {
    project_route: &'a str,
    model: &'a str,
    blocked_until: DateTime<Utc>,
    updated_at: DateTime<Utc>,
}

pub async fn record_cooldown(
    pool: &PgPool,
    route: &str,
    model: &str,
    delay: Duration,
    now: DateTime<Utc>,
) -> Result<(), QuotaError> {
    if delay <= Duration::zero() {
        return Ok(());
    }
    let blocked_until = now + delay;
    let mut connection = pool.get().await.map_err(|_| QuotaError::Pool)?;
    connection
        .transaction::<(), QuotaError, _>(async move |conn| {
            let lock = Lock {
                project_route: route,
                model,
                updated_at: now,
            };
            insert_into(gemini_quota_reservation_locks::table)
                .values(&lock)
                .on_conflict((
                    gemini_quota_reservation_locks::project_route,
                    gemini_quota_reservation_locks::model,
                ))
                .do_update()
                .set(
                    gemini_quota_reservation_locks::updated_at
                        .eq(excluded(gemini_quota_reservation_locks::updated_at)),
                )
                .execute(conn)
                .await
                .map_err(|_| QuotaError::Database)?;
            let current = gemini_quota_cooldowns::table
                .filter(gemini_quota_cooldowns::project_route.eq(route))
                .filter(gemini_quota_cooldowns::model.eq(model))
                .select(gemini_quota_cooldowns::blocked_until)
                .first::<DateTime<Utc>>(conn)
                .await
                .optional()
                .map_err(|_| QuotaError::Database)?;
            let cooldown = Cooldown {
                project_route: route,
                model,
                blocked_until: current.map_or(blocked_until, |current| current.max(blocked_until)),
                updated_at: now,
            };
            insert_into(gemini_quota_cooldowns::table)
                .values(&cooldown)
                .on_conflict((
                    gemini_quota_cooldowns::project_route,
                    gemini_quota_cooldowns::model,
                ))
                .do_update()
                .set((
                    gemini_quota_cooldowns::blocked_until
                        .eq(excluded(gemini_quota_cooldowns::blocked_until)),
                    gemini_quota_cooldowns::updated_at
                        .eq(excluded(gemini_quota_cooldowns::updated_at)),
                ))
                .execute(conn)
                .await
                .map_err(|_| QuotaError::Database)?;
            Ok(())
        })
        .await
        .map_err(|_| QuotaError::Database)?;
    Ok(())
}

pub async fn reserve(
    pool: &PgPool,
    route: &str,
    model: &str,
    input_tokens: i64,
    limit: QuotaLimit,
    now: DateTime<Utc>,
) -> Result<Result<Uuid, Duration>, QuotaError> {
    if input_tokens < 1
        || limit.requests_per_minute < 1
        || limit.input_tokens_minute < 1
        || limit.requests_per_day < 1
    {
        return Err(QuotaError::Database);
    }
    if input_tokens > limit.input_tokens_minute {
        return Err(QuotaError::InputTooLarge);
    }
    let (day_start, day_reset) = los_angeles_day_bounds(now)?;
    let reservation_id = Uuid::now_v7();
    let mut connection = pool.get().await.map_err(|_| QuotaError::Pool)?;
    connection
        .transaction::<Result<Uuid, Duration>, QuotaError, _>(async move |conn| {
            let lock = Lock {
                project_route: route,
                model,
                updated_at: now,
            };
            insert_into(gemini_quota_reservation_locks::table)
                .values(&lock)
                .on_conflict((
                    gemini_quota_reservation_locks::project_route,
                    gemini_quota_reservation_locks::model,
                ))
                .do_update()
                .set(
                    gemini_quota_reservation_locks::updated_at
                        .eq(excluded(gemini_quota_reservation_locks::updated_at)),
                )
                .execute(conn)
                .await
                .map_err(|_| QuotaError::Database)?;

            let cooldown = gemini_quota_cooldowns::table
                .filter(gemini_quota_cooldowns::project_route.eq(route))
                .filter(gemini_quota_cooldowns::model.eq(model))
                .select(gemini_quota_cooldowns::blocked_until)
                .first::<DateTime<Utc>>(conn)
                .await
                .optional()
                .map_err(|_| QuotaError::Database)?;
            if let Some(blocked_until) = cooldown.filter(|until| *until > now) {
                return Ok(Err(blocked_until - now));
            }

            diesel::delete(
                gemini_quota_reservations::table
                    .filter(gemini_quota_reservations::project_route.eq(route))
                    .filter(gemini_quota_reservations::model.eq(model))
                    .filter(gemini_quota_reservations::reserved_at.lt(day_start)),
            )
            .execute(conn)
            .await
            .map_err(|_| QuotaError::Database)?;

            let rows = gemini_quota_reservations::table
                .filter(gemini_quota_reservations::project_route.eq(route))
                .filter(gemini_quota_reservations::model.eq(model))
                .filter(gemini_quota_reservations::reserved_at.ge(day_start))
                .select(Reservation::as_select())
                .load::<Reservation>(conn)
                .await
                .map_err(|_| QuotaError::Database)?;
            let minute_start = now - Duration::minutes(1);
            let minute = rows.iter().filter(|row| row.reserved_at >= minute_start);
            let (minute_requests, minute_tokens) =
                minute
                    .clone()
                    .fold((0_i64, 0_i64), |(requests, tokens), row| {
                        (
                            requests + 1,
                            tokens + row.actual_input_tokens.unwrap_or(row.input_tokens),
                        )
                    });
            if minute_requests + 1 > limit.requests_per_minute
                || minute_tokens + input_tokens > limit.input_tokens_minute
                || rows.len() as i64 + 1 > limit.requests_per_day
            {
                let retry = if minute_requests + 1 > limit.requests_per_minute
                    || minute_tokens + input_tokens > limit.input_tokens_minute
                {
                    rows.iter()
                        .filter(|row| row.reserved_at >= minute_start)
                        .map(|row| row.reserved_at + Duration::minutes(1) - now)
                        .min()
                        .unwrap_or(Duration::minutes(1))
                } else {
                    day_reset - now
                };
                return Ok(Err(retry.max(Duration::seconds(1))));
            }
            let reservation = NewReservation {
                id: reservation_id,
                project_route: route,
                model,
                reserved_at: now,
                input_tokens,
            };
            insert_into(gemini_quota_reservations::table)
                .values(&reservation)
                .execute(conn)
                .await
                .map_err(|_| QuotaError::Database)?;
            Ok(Ok(reservation_id))
        })
        .await
        .map_err(|_| QuotaError::Database)
}

fn los_angeles_day_bounds(
    now: DateTime<Utc>,
) -> Result<(DateTime<Utc>, DateTime<Utc>), QuotaError> {
    let local_now = now.naive_utc() + los_angeles_offset(now);
    let local_start = local_now.date();
    let next_local_start = local_start.succ_opt().ok_or(QuotaError::Database)?;
    let start_guess = DateTime::<Utc>::from_naive_utc_and_offset(
        local_start
            .and_hms_opt(8, 0, 0)
            .ok_or(QuotaError::Database)?,
        Utc,
    );
    let reset_guess = DateTime::<Utc>::from_naive_utc_and_offset(
        next_local_start
            .and_hms_opt(8, 0, 0)
            .ok_or(QuotaError::Database)?,
        Utc,
    );
    let start = local_start
        .and_hms_opt(0, 0, 0)
        .ok_or(QuotaError::Database)?
        - los_angeles_offset(start_guess);
    let reset = next_local_start
        .and_hms_opt(0, 0, 0)
        .ok_or(QuotaError::Database)?
        - los_angeles_offset(reset_guess);
    Ok((
        DateTime::<Utc>::from_naive_utc_and_offset(start, Utc),
        DateTime::<Utc>::from_naive_utc_and_offset(reset, Utc),
    ))
}

fn los_angeles_offset(utc: DateTime<Utc>) -> Duration {
    let year = utc.year();
    let march_first = NaiveDate::from_ymd_opt(year, 3, 1).expect("valid calendar date");
    let november_first = NaiveDate::from_ymd_opt(year, 11, 1).expect("valid calendar date");
    let march_sunday = 1 + (7 - march_first.weekday().num_days_from_sunday()) % 7 + 7;
    let november_sunday = 1 + (7 - november_first.weekday().num_days_from_sunday()) % 7;
    let daylight_start = DateTime::<Utc>::from_naive_utc_and_offset(
        NaiveDate::from_ymd_opt(year, 3, march_sunday)
            .and_then(|date| date.and_hms_opt(10, 0, 0))
            .expect("valid daylight-saving transition"),
        Utc,
    );
    let daylight_end = DateTime::<Utc>::from_naive_utc_and_offset(
        NaiveDate::from_ymd_opt(year, 11, november_sunday)
            .and_then(|date| date.and_hms_opt(9, 0, 0))
            .expect("valid daylight-saving transition"),
        Utc,
    );
    if utc >= daylight_start && utc < daylight_end {
        Duration::hours(-7)
    } else {
        Duration::hours(-8)
    }
}

pub async fn settle(
    pool: &PgPool,
    reservation_id: Uuid,
    actual_input_tokens: i64,
    now: DateTime<Utc>,
) -> Result<(), QuotaError> {
    let mut connection = pool.get().await.map_err(|_| QuotaError::Pool)?;
    connection
        .transaction::<(), QuotaError, _>(async move |conn| {
            let row = gemini_quota_reservations::table
                .filter(gemini_quota_reservations::id.eq(reservation_id))
                .select(Reservation::as_select())
                .first::<Reservation>(conn)
                .await
                .map_err(|_| QuotaError::Database)?;
            let lock = Lock {
                project_route: &row.project_route,
                model: &row.model,
                updated_at: now,
            };
            insert_into(gemini_quota_reservation_locks::table)
                .values(&lock)
                .on_conflict((
                    gemini_quota_reservation_locks::project_route,
                    gemini_quota_reservation_locks::model,
                ))
                .do_update()
                .set(
                    gemini_quota_reservation_locks::updated_at
                        .eq(excluded(gemini_quota_reservation_locks::updated_at)),
                )
                .execute(conn)
                .await
                .map_err(|_| QuotaError::Database)?;
            diesel::update(
                gemini_quota_reservations::table
                    .filter(gemini_quota_reservations::id.eq(reservation_id)),
            )
            .set((
                gemini_quota_reservations::actual_input_tokens.eq(Some(actual_input_tokens)),
                gemini_quota_reservations::settled_at.eq(Some(now)),
            ))
            .execute(conn)
            .await
            .map_err(|_| QuotaError::Database)?;
            Ok(())
        })
        .await
        .map_err(|_| QuotaError::Database)
}
