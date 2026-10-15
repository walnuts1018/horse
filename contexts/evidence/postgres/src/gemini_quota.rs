use chrono::{DateTime, Duration, NaiveDate, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, RunQueryDsl, insert_into, upsert::excluded};
use diesel_async::RunQueryDsl as AsyncRunQueryDsl;
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

pub async fn reserve(
    pool: &PgPool,
    route: &str,
    model: &str,
    input_tokens: i64,
    limit: QuotaLimit,
    now: DateTime<Utc>,
) -> Result<Result<Uuid, Duration>, QuotaError> {
    if input_tokens < 1 || limit.requests_per_minute < 1 || limit.input_tokens_minute < 1 || limit.requests_per_day < 1 {
        return Err(QuotaError::Database);
    }
    if input_tokens > limit.input_tokens_minute {
        return Err(QuotaError::InputTooLarge);
    }
    let day_start = DateTime::<Utc>::from_naive_utc_and_offset(
        NaiveDate::from_ymd_opt(now.year(), now.month(), now.day())
            .ok_or(QuotaError::Database)?
            .and_hms_opt(0, 0, 0)
            .ok_or(QuotaError::Database)?,
        Utc,
    );
    let reservation_id = Uuid::now_v7();
    let mut connection = pool.get().await.map_err(|_| QuotaError::Pool)?;
    connection
        .transaction::<Result<Uuid, Duration>, QuotaError, _>(async move |conn| {
            let lock = Lock { project_route: route, model, updated_at: now };
            insert_into(gemini_quota_reservation_locks::table)
                .values(&lock)
                .on_conflict((gemini_quota_reservation_locks::project_route, gemini_quota_reservation_locks::model))
                .do_update()
                .set(gemini_quota_reservation_locks::updated_at.eq(excluded(gemini_quota_reservation_locks::updated_at)))
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
            let (minute_requests, minute_tokens) = minute.clone().fold((0_i64, 0_i64), |(requests, tokens), row| {
                (requests + 1, tokens + row.actual_input_tokens.unwrap_or(row.input_tokens))
            });
            if minute_requests + 1 > limit.requests_per_minute
                || minute_tokens + input_tokens > limit.input_tokens_minute
                || rows.len() as i64 + 1 > limit.requests_per_day
            {
                let retry = if minute_requests + 1 > limit.requests_per_minute || minute_tokens + input_tokens > limit.input_tokens_minute {
                    rows.iter().filter(|row| row.reserved_at >= minute_start).map(|row| row.reserved_at + Duration::minutes(1) - now).min().unwrap_or(Duration::minutes(1))
                } else {
                    day_start + Duration::days(1) - now
                };
                return Ok(Err(retry.max(Duration::seconds(1))));
            }
            let reservation = NewReservation { id: reservation_id, project_route: route, model, reserved_at: now, input_tokens };
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

pub async fn settle(pool: &PgPool, reservation_id: Uuid, actual_input_tokens: i64, now: DateTime<Utc>) -> Result<(), QuotaError> {
    let mut connection = pool.get().await.map_err(|_| QuotaError::Pool)?;
    connection
        .transaction::<(), QuotaError, _>(async move |conn| {
            let row = gemini_quota_reservations::table
                .filter(gemini_quota_reservations::id.eq(reservation_id))
                .select(Reservation::as_select())
                .first::<Reservation>(conn)
                .await
                .map_err(|_| QuotaError::Database)?;
            let lock = Lock { project_route: &row.project_route, model: &row.model, updated_at: now };
            insert_into(gemini_quota_reservation_locks::table)
                .values(&lock)
                .on_conflict((gemini_quota_reservation_locks::project_route, gemini_quota_reservation_locks::model))
                .do_update()
                .set(gemini_quota_reservation_locks::updated_at.eq(excluded(gemini_quota_reservation_locks::updated_at)))
                .execute(conn)
                .await
                .map_err(|_| QuotaError::Database)?;
            diesel::update(gemini_quota_reservations::table.filter(gemini_quota_reservations::id.eq(reservation_id)))
                .set((gemini_quota_reservations::actual_input_tokens.eq(Some(actual_input_tokens)), gemini_quota_reservations::settled_at.eq(Some(now))))
                .execute(conn)
                .await
                .map_err(|_| QuotaError::Database)?;
            Ok(())
        })
        .await
        .map_err(|_| QuotaError::Database)
}
