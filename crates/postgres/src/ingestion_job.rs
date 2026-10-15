use chrono::{DateTime, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl as AsyncRunQueryDsl};
use serde_json::Value;
use uuid::Uuid;

use bookkeeping_workflows::ingestion_job::{
    ExtractionFailureKind, ExtractionJobState, ExtractionJobStatus, FinishExtractionJobError,
    claim_extraction_job, finish_extraction_failure, finish_extraction_success,
    recover_expired_extraction_job,
};

use crate::{PgPool, schema::public};

const LEASE_DURATION_MS: i64 = 35 * 60 * 1000;

#[derive(Debug, Clone, PartialEq)]
pub struct ClaimedExtractionJob {
    pub id: String,
    pub household_id: String,
    pub source_document_id: String,
    pub version: i32,
    pub attempt_no: i32,
    pub lease_token: String,
    pub request: Value,
    pub previous_result: Option<Value>,
    pub provider: String,
    pub model: String,
}

#[derive(Debug)]
pub enum IngestionJobError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidTimestamp,
    InvalidState,
    InvalidTransition,
}

impl From<DieselError> for IngestionJobError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn claim_next_extraction_job(
    pool: &PgPool,
    now: DateTime<Utc>,
) -> Result<Option<ClaimedExtractionJob>, IngestionJobError> {
    let now_ms = now.timestamp_millis();
    let now = db_time(now_ms)?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| IngestionJobError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<Option<ClaimedExtractionJob>, IngestionJobError, _>(async move |conn| {
            use public::extraction_job_attempts::dsl as attempts;
            use public::extraction_jobs::dsl as jobs;

            let candidate = jobs::extraction_jobs
                .filter(jobs::failure_count.lt(jobs::max_attempts))
                .filter(jobs::status.eq("queued"))
                .filter(jobs::available_at.le(now))
                .order_by(jobs::available_at.asc())
                .then_order_by(jobs::created_at.asc())
                .for_update()
                .skip_locked()
                .select((
                    jobs::id,
                    jobs::household_id,
                    jobs::source_document_id,
                    jobs::version,
                    jobs::status,
                    jobs::provider,
                    jobs::model,
                    jobs::request,
                    jobs::result,
                    jobs::attempt_count,
                    jobs::max_attempts,
                    jobs::failure_count,
                    jobs::available_at,
                    jobs::lease_token,
                    jobs::lease_expires_at,
                ))
                .first::<JobRow>(&mut *conn)
                .await
                .optional()?;
            let Some(row) = candidate else {
                return Ok(None);
            };
            let state = row.state();
            let token = Uuid::now_v7();
            let transition =
                claim_extraction_job(&state, now_ms, token.to_string(), LEASE_DURATION_MS)
                    .map_err(|_| IngestionJobError::InvalidTransition)?;
            let changed = diesel::update(
                jobs::extraction_jobs
                    .filter(jobs::id.eq(row.id))
                    .filter(jobs::status.eq(&row.status))
                    .filter(jobs::attempt_count.eq(row.attempt_count))
                    .filter(jobs::lease_token.eq(row.lease_token)),
            )
            .set((
                jobs::status.eq("running"),
                jobs::attempt_count.eq(transition.attempt_no),
                jobs::started_at.eq(Some(now)),
                jobs::finished_at.eq(None::<DateTime<Utc>>),
                jobs::lease_token.eq(Some(token)),
                jobs::lease_expires_at.eq(Some(db_time(transition.lease_expires_at_ms)?)),
                jobs::error_code.eq(None::<String>),
                jobs::error_message.eq(None::<String>),
                jobs::updated_at.eq(now),
            ))
            .execute(&mut *conn)
            .await?;
            if changed != 1 {
                return Ok(None);
            }
            diesel::insert_into(attempts::extraction_job_attempts)
                .values((
                    attempts::household_id.eq(row.household_id),
                    attempts::source_document_id.eq(row.source_document_id),
                    attempts::job_id.eq(row.id),
                    attempts::attempt_no.eq(transition.attempt_no),
                    attempts::provider.eq(&row.provider),
                    attempts::model.eq(&row.model),
                    attempts::status.eq("running"),
                    attempts::started_at.eq(now),
                    attempts::response_metadata.eq(serde_json::json!({})),
                ))
                .execute(&mut *conn)
                .await?;
            Ok(Some(ClaimedExtractionJob {
                id: row.id.to_string(),
                household_id: row.household_id.to_string(),
                source_document_id: row.source_document_id.to_string(),
                version: row.version,
                attempt_no: transition.attempt_no,
                lease_token: token.to_string(),
                request: row.request,
                previous_result: row.result,
                provider: row.provider,
                model: row.model,
            }))
        })
        .await
}

pub async fn record_extraction_success(
    pool: &PgPool,
    job_id: &str,
    attempt_no: i32,
    lease_token: &str,
    result: Value,
    now: DateTime<Utc>,
) -> Result<bool, IngestionJobError> {
    finish_job(
        pool,
        job_id,
        attempt_no,
        lease_token,
        now,
        |state, token, now_ms| finish_extraction_success(state, token, now_ms),
        Some(result),
        None,
    )
    .await
}

pub async fn record_extraction_failure(
    pool: &PgPool,
    job_id: &str,
    attempt_no: i32,
    lease_token: &str,
    failure_kind: ExtractionFailureKind,
    error_code: &str,
    error_message: &str,
    retry_delay_ms: i64,
    now: DateTime<Utc>,
) -> Result<bool, IngestionJobError> {
    finish_job(
        pool,
        job_id,
        attempt_no,
        lease_token,
        now,
        |state, token, now_ms| {
            finish_extraction_failure(
                state,
                token,
                now_ms,
                retry_delay_ms,
                failure_kind,
                state.max_attempts,
            )
        },
        None,
        Some((error_code.to_owned(), error_message.to_owned())),
    )
    .await
}

async fn finish_job<F>(
    pool: &PgPool,
    job_id: &str,
    attempt_no: i32,
    lease_token: &str,
    now: DateTime<Utc>,
    decide: F,
    result: Option<Value>,
    failure: Option<(String, String)>,
) -> Result<bool, IngestionJobError>
where
    F: FnOnce(
        &ExtractionJobState,
        &str,
        i64,
    ) -> Result<
        bookkeeping_workflows::ingestion_job::FinishExtractionJob,
        FinishExtractionJobError,
    >,
{
    let job_id = parse_uuid(job_id, "job_id")?;
    let lease_token = parse_uuid(lease_token, "lease_token")?;
    let now_ms = now.timestamp_millis();
    let now = db_time(now_ms)?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| IngestionJobError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<bool, IngestionJobError, _>(async move |conn| {
            use public::extraction_job_attempts::dsl as attempts;
            use public::extraction_jobs::dsl as jobs;

            let row = jobs::extraction_jobs
                .filter(jobs::id.eq(job_id))
                .filter(jobs::status.eq("running"))
                .filter(jobs::lease_token.eq(Some(lease_token)))
                .filter(jobs::lease_expires_at.gt(Some(now)))
                .for_update()
                .select((
                    jobs::id,
                    jobs::status,
                    jobs::attempt_count,
                    jobs::max_attempts,
                    jobs::failure_count,
                    jobs::available_at,
                    jobs::lease_token,
                    jobs::lease_expires_at,
                ))
                .first::<StateRow>(&mut *conn)
                .await
                .optional()?;
            let Some(row) = row else {
                return Ok(false);
            };
                let state = row.state();
            let transition = decide(&state, &lease_token.to_string(), now_ms)
                .map_err(|_| IngestionJobError::InvalidTransition)?;
            if state.attempt_count != attempt_no {
                return Ok(false);
            }
            let error_code = failure.as_ref().map(|value| value.0.as_str());
            let error_message = failure.as_ref().map(|value| value.1.as_str());
            let changed = diesel::update(
                jobs::extraction_jobs
                    .filter(jobs::id.eq(job_id))
                    .filter(jobs::status.eq("running"))
                    .filter(jobs::attempt_count.eq(attempt_no))
                    .filter(jobs::lease_token.eq(Some(lease_token)))
                    .filter(jobs::lease_expires_at.gt(Some(now))),
            )
            .set((
                jobs::status.eq(status_text(transition.status)),
                jobs::failure_count.eq(transition.failure_count),
                jobs::available_at.eq(db_time(transition.available_at_ms)?),
                jobs::finished_at.eq(transition.finished_at_ms.map(db_time).transpose()?),
                jobs::lease_token.eq(None::<Uuid>),
                jobs::lease_expires_at.eq(None::<DateTime<Utc>>),
                jobs::result.eq(result),
                jobs::error_code.eq(error_code),
                jobs::error_message.eq(error_message),
                jobs::updated_at.eq(now),
            ))
            .execute(&mut *conn)
            .await?;
            if changed != 1 {
                return Ok(false);
            }
            let attempt_status = if transition.status == ExtractionJobStatus::Succeeded {
                "succeeded"
            } else {
                "failed"
            };
            diesel::update(
                attempts::extraction_job_attempts
                    .filter(attempts::job_id.eq(job_id))
                    .filter(attempts::attempt_no.eq(attempt_no))
                    .filter(attempts::status.eq("running")),
            )
            .set((
                attempts::status.eq(attempt_status),
                attempts::finished_at.eq(Some(now)),
                attempts::error_code.eq(error_code),
                attempts::error_message.eq(error_message),
                attempts::response_metadata.eq(failure.as_ref().map_or_else(
                    || serde_json::json!({}),
                    |(code, message)| serde_json::json!({"errorCode": code, "errorMessage": message}),
                )),
            ))
            .execute(&mut *conn)
            .await?;
            Ok(true)
        })
        .await
}

pub async fn recover_expired_extraction_jobs(
    pool: &PgPool,
    now: DateTime<Utc>,
) -> Result<usize, IngestionJobError> {
    let now_ms = now.timestamp_millis();
    let now = db_time(now_ms)?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| IngestionJobError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<usize, IngestionJobError, _>(async move |conn| {
            use public::extraction_job_attempts::dsl as attempts;
            use public::extraction_jobs::dsl as jobs;

            let rows = jobs::extraction_jobs
                .filter(jobs::status.eq("running"))
                .filter(jobs::lease_expires_at.le(Some(now)))
                .for_update()
                .skip_locked()
                .select((
                    jobs::id,
                    jobs::status,
                    jobs::attempt_count,
                    jobs::max_attempts,
                    jobs::failure_count,
                    jobs::available_at,
                    jobs::lease_token,
                    jobs::lease_expires_at,
                ))
                .load::<StateRow>(conn)
                .await?;
            let mut recovered = 0;
            for row in rows {
                let state = row.state();
                let transition = recover_expired_extraction_job(&state, now_ms, state.max_attempts)
                    .map_err(|_| IngestionJobError::InvalidTransition)?;
                let job_id = row.id;
                let attempt_no = state.attempt_count;
                let error_code = if transition.status == ExtractionJobStatus::Failed {
                    "retry_exhausted"
                } else {
                    "worker_interrupted"
                };
                let finished_at = transition.finished_at_ms.map(db_time).transpose()?;
                let updated = diesel::update(
                    jobs::extraction_jobs
                        .filter(jobs::id.eq(job_id))
                        .filter(jobs::status.eq("running"))
                        .filter(jobs::attempt_count.eq(attempt_no))
                        .filter(jobs::lease_token.eq(row.lease_token)),
                )
                .set((
                    jobs::status.eq(status_text(transition.status)),
                    jobs::failure_count.eq(transition.failure_count),
                    jobs::finished_at.eq(finished_at),
                    jobs::lease_token.eq(None::<Uuid>),
                    jobs::lease_expires_at.eq(None::<DateTime<Utc>>),
                    jobs::error_code.eq(Some(error_code)),
                    jobs::updated_at.eq(now),
                ))
                .execute(&mut *conn)
                .await?;
                if updated == 0 {
                    continue;
                }
                diesel::update(
                    attempts::extraction_job_attempts
                        .filter(attempts::job_id.eq(job_id))
                        .filter(attempts::attempt_no.eq(attempt_no))
                        .filter(attempts::status.eq("running")),
                )
                .set((
                    attempts::status.eq("failed"),
                    attempts::finished_at.eq(Some(now)),
                    attempts::error_code.eq(Some(error_code)),
                    attempts::error_message.eq(Some("Worker lease expired")),
                    attempts::response_metadata.eq(serde_json::json!({})),
                ))
                .execute(&mut *conn)
                .await?;
                recovered += 1;
            }
            Ok(recovered)
        })
        .await
}

#[derive(diesel::Queryable)]
struct JobRow {
    id: Uuid,
    household_id: Uuid,
    source_document_id: Uuid,
    version: i32,
    status: String,
    provider: String,
    model: String,
    request: Value,
    result: Option<Value>,
    attempt_count: i32,
    max_attempts: i32,
    failure_count: i32,
    available_at: DateTime<Utc>,
    lease_token: Option<Uuid>,
    lease_expires_at: Option<DateTime<Utc>>,
}

impl JobRow {
    fn state(&self) -> ExtractionJobState {
        ExtractionJobState {
            id: self.id.to_string(),
            status: parse_status(&self.status),
            attempt_count: self.attempt_count,
            max_attempts: self.max_attempts,
            failure_count: self.failure_count,
            available_at_ms: self.available_at.timestamp_millis(),
            lease_token: self.lease_token.map(|token| token.to_string()),
            lease_expires_at_ms: self
                .lease_expires_at
                .map(|expires_at| expires_at.timestamp_millis()),
        }
    }
}

#[derive(diesel::Queryable)]
struct StateRow {
    id: Uuid,
    status: String,
    attempt_count: i32,
    max_attempts: i32,
    failure_count: i32,
    available_at: DateTime<Utc>,
    lease_token: Option<Uuid>,
    lease_expires_at: Option<DateTime<Utc>>,
}

impl StateRow {
    fn state(&self) -> ExtractionJobState {
        ExtractionJobState {
            id: self.id.to_string(),
            status: parse_status(&self.status),
            attempt_count: self.attempt_count,
            max_attempts: self.max_attempts,
            failure_count: self.failure_count,
            available_at_ms: self.available_at.timestamp_millis(),
            lease_token: self.lease_token.map(|token| token.to_string()),
            lease_expires_at_ms: self
                .lease_expires_at
                .map(|expires_at| expires_at.timestamp_millis()),
        }
    }
}

fn parse_status(status: &str) -> ExtractionJobStatus {
    match status {
        "queued" => ExtractionJobStatus::Queued,
        "running" => ExtractionJobStatus::Running,
        "succeeded" => ExtractionJobStatus::Succeeded,
        "failed" => ExtractionJobStatus::Failed,
        _ => ExtractionJobStatus::Cancelled,
    }
}

fn status_text(status: ExtractionJobStatus) -> &'static str {
    match status {
        ExtractionJobStatus::Queued => "queued",
        ExtractionJobStatus::Running => "running",
        ExtractionJobStatus::Succeeded => "succeeded",
        ExtractionJobStatus::Failed => "failed",
        ExtractionJobStatus::Cancelled => "cancelled",
    }
}

fn db_time(timestamp_ms: i64) -> Result<DateTime<Utc>, IngestionJobError> {
    DateTime::from_timestamp_millis(timestamp_ms).ok_or(IngestionJobError::InvalidTimestamp)
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, IngestionJobError> {
    Uuid::parse_str(value).map_err(|_| IngestionJobError::InvalidUuid(field))
}
