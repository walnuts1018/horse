use chrono::{DateTime, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, AsyncPgConnection, RunQueryDsl as AsyncRunQueryDsl};
use serde_json::Value;
use uuid::Uuid;

use evidence_application::ingestion_job::{
    ClaimExtractionJob, ClaimExtractionJobOperations, ClaimExtractionJobWorkflowError,
    ExtractionFailureKind, ExtractionJob, ExtractionJobStatus, FinishExtractionJobError,
    FinishedJob, QueuedJob, RunningJob, ValidLease,
    claim_extraction_job_by_id as claim_extraction_job_by_id_workflow,
    claim_next_extraction_job as claim_next_extraction_job_workflow,
    claim_next_local_extraction_job as claim_next_local_extraction_job_workflow,
    finish_extraction_failure, finish_extraction_success, recover_expired_extraction_job,
};

use crate::{PgPool, schema::public};

const LEASE_DURATION_MS: i64 = 35 * 60 * 1000;

#[derive(Debug, Clone, PartialEq)]
pub struct ClaimedExtractionJob {
    pub id: String,
    pub household_id: String,
    pub source_document_id: String,
    pub source_hash: String,
    pub object_key: String,
    pub mime_type: String,
    pub analysis_object_key: Option<String>,
    pub analysis_mime_type: Option<String>,
    pub analysis_transform: Option<[f64; 9]>,
    pub source_kind: String,
    pub source_created_by: String,
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
    InvalidAnalysisTransform,
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
    claim_extraction_job(pool, None, false, now).await
}

pub async fn claim_next_local_extraction_job(
    pool: &PgPool,
    now: DateTime<Utc>,
) -> Result<Option<ClaimedExtractionJob>, IngestionJobError> {
    claim_extraction_job(pool, None, true, now).await
}

pub async fn claim_extraction_job_by_id(
    pool: &PgPool,
    job_id: &str,
    local_csv_only: bool,
    now: DateTime<Utc>,
) -> Result<Option<ClaimedExtractionJob>, IngestionJobError> {
    parse_uuid(job_id, "job_id")?;
    claim_extraction_job(pool, Some(job_id), local_csv_only, now).await
}

async fn claim_extraction_job(
    pool: &PgPool,
    job_id: Option<&str>,
    local_csv_only: bool,
    now: DateTime<Utc>,
) -> Result<Option<ClaimedExtractionJob>, IngestionJobError> {
    let now_ms = now.timestamp_millis();
    let lease_token = Uuid::now_v7().to_string();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| IngestionJobError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<Option<ClaimedExtractionJob>, IngestionJobError, _>(async move |conn| {
            let mut operations = PostgresClaimExtractionJobOperations { conn };
            let result = if let Some(job_id) = job_id {
                claim_extraction_job_by_id_workflow(
                    &mut operations,
                    now_ms,
                    job_id,
                    local_csv_only,
                    lease_token,
                    LEASE_DURATION_MS,
                )
                .await
            } else if local_csv_only {
                claim_next_local_extraction_job_workflow(
                    &mut operations,
                    now_ms,
                    lease_token,
                    LEASE_DURATION_MS,
                )
                .await
            } else {
                claim_next_extraction_job_workflow(
                    &mut operations,
                    now_ms,
                    lease_token,
                    LEASE_DURATION_MS,
                )
                .await
            };
            result.map_err(|error| match error {
                ClaimExtractionJobWorkflowError::Operations(error) => error,
                ClaimExtractionJobWorkflowError::Decision(_) => {
                    IngestionJobError::InvalidTransition
                }
            })
        })
        .await
}

struct ClaimCandidate {
    row: JobRow,
    document: SourceDocumentRow,
    queued: QueuedJob,
}

struct PostgresClaimExtractionJobOperations<'a> {
    conn: &'a mut AsyncPgConnection,
}

impl ClaimExtractionJobOperations for PostgresClaimExtractionJobOperations<'_> {
    type Error = IngestionJobError;
    type Candidate = ClaimCandidate;
    type Claimed = ClaimedExtractionJob;

    fn queued_job<'a>(&self, candidate: &'a Self::Candidate) -> &'a QueuedJob {
        &candidate.queued
    }

    async fn load_candidate(
        &mut self,
        now_ms: i64,
        job_id: Option<&str>,
        local_csv_only: bool,
    ) -> Result<Option<Self::Candidate>, Self::Error> {
        use public::extraction_jobs::dsl as jobs;

        let now = db_time(now_ms)?;
        use public::source_documents::dsl as documents;
        let local_document_ids = documents::source_documents
            .filter(documents::mime_type.eq("text/csv"))
            .filter(documents::source_kind.eq_any(["card", "bank"]))
            .select(documents::id);
        macro_rules! candidate_query {
            ($($filter:expr),*) => {
                jobs::extraction_jobs
                    .filter(jobs::failure_count.lt(jobs::max_attempts))
                    .filter(jobs::status.eq("queued"))
                    .filter(jobs::available_at.le(now))
                    $(.filter($filter))*
                    .order_by(jobs::available_at.asc())
                    .then_order_by(jobs::id.asc())
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
                        jobs::finished_at,
                    ))
                    .first::<JobRow>(self.conn)
                    .await
                    .optional()
            };
        }
        let row = match (job_id, local_csv_only) {
            (Some(job_id), true) => {
                let id = parse_uuid(job_id, "job_id")?;
                candidate_query!(
                    jobs::id.eq(id),
                    jobs::source_document_id.eq_any(local_document_ids)
                )?
            }
            (Some(job_id), false) => {
                let id = parse_uuid(job_id, "job_id")?;
                candidate_query!(jobs::id.eq(id))?
            }
            (None, true) => candidate_query!(jobs::source_document_id.eq_any(local_document_ids))?,
            (None, false) => candidate_query!()?,
        };
        let Some(row) = row else {
            return Ok(None);
        };
        let ExtractionJob::Queued(queued) = row.job()? else {
            return Ok(None);
        };
        let document = documents::source_documents
            .filter(documents::id.eq(row.source_document_id))
            .filter(documents::household_id.eq(row.household_id))
            .select((
                documents::sha256,
                documents::object_key,
                documents::mime_type,
                documents::source_kind,
                documents::analysis_object_key,
                documents::analysis_mime_type,
                documents::analysis_transform,
                documents::created_by,
            ))
            .first::<SourceDocumentRow>(self.conn)
            .await?;
        Ok(Some(ClaimCandidate {
            row,
            document,
            queued,
        }))
    }

    async fn persist_claim(
        &mut self,
        candidate: Self::Candidate,
        claim: ClaimExtractionJob,
        now_ms: i64,
    ) -> Result<Option<Self::Claimed>, Self::Error> {
        use public::extraction_job_attempts::dsl as attempts;
        use public::extraction_jobs::dsl as jobs;

        let now = db_time(now_ms)?;
        let row = candidate.row;
        let token = parse_uuid(&claim.lease_token, "lease_token")?;
        let lease_expires_at = db_time(claim.lease_expires_at_ms)?;
        let changed = diesel::update(
            jobs::extraction_jobs
                .filter(jobs::id.eq(row.id))
                .filter(jobs::status.eq(&row.status))
                .filter(jobs::attempt_count.eq(row.attempt_count))
                .filter(jobs::lease_token.eq(row.lease_token)),
        )
        .set((
            jobs::status.eq("running"),
            jobs::attempt_count.eq(claim.attempt_no),
            jobs::started_at.eq(Some(now)),
            jobs::finished_at.eq(None::<DateTime<Utc>>),
            jobs::lease_token.eq(Some(token)),
            jobs::lease_expires_at.eq(Some(lease_expires_at)),
            jobs::error_code.eq(None::<String>),
            jobs::error_message.eq(None::<String>),
            jobs::interaction_reply_preview.eq(""),
            jobs::updated_at.eq(now),
        ))
        .execute(self.conn)
        .await?;
        if changed != 1 {
            return Ok(None);
        }
        diesel::insert_into(attempts::extraction_job_attempts)
            .values((
                attempts::household_id.eq(row.household_id),
                attempts::source_document_id.eq(row.source_document_id),
                attempts::job_id.eq(row.id),
                attempts::attempt_no.eq(claim.attempt_no),
                attempts::provider.eq(&row.provider),
                attempts::model.eq(&row.model),
                attempts::status.eq("running"),
                attempts::started_at.eq(now),
                attempts::response_metadata.eq(serde_json::json!({})),
            ))
            .execute(self.conn)
            .await?;
        let document = candidate.document;
        Ok(Some(ClaimedExtractionJob {
            id: row.id.to_string(),
            household_id: row.household_id.to_string(),
            source_document_id: row.source_document_id.to_string(),
            source_hash: document.sha256,
            object_key: document.object_key,
            mime_type: document.mime_type,
            analysis_object_key: document.analysis_object_key,
            analysis_mime_type: document.analysis_mime_type,
            analysis_transform: document
                .analysis_transform
                .map(serde_json::from_value)
                .transpose()
                .map_err(|_| IngestionJobError::InvalidAnalysisTransform)?,
            source_kind: document.source_kind,
            source_created_by: document.created_by.to_string(),
            version: row.version,
            attempt_no: claim.attempt_no,
            lease_token: claim.lease_token,
            request: row.request,
            previous_result: row.result,
            provider: row.provider,
            model: row.model,
        }))
    }
}

pub async fn record_extraction_success(
    pool: &PgPool,
    job: &ClaimedExtractionJob,
    result: Value,
    now: DateTime<Utc>,
) -> Result<bool, IngestionJobError> {
    let success = SuccessMetadata {
        household_id: job.household_id.clone(),
        source_document_id: job.source_document_id.clone(),
        source_hash: job.source_hash.clone(),
        source_kind: job.source_kind.clone(),
        created_by: job.source_created_by.clone(),
        version: i64::from(job.version),
    };
    finish_job(
        pool,
        FinishJobInput {
            job_id: job.id.clone(),
            attempt_no: job.attempt_no,
            lease_token: job.lease_token.clone(),
            now,
            result: Some(result),
            failure: None,
            success: Some(success),
        },
        finish_extraction_success,
    )
    .await
}

pub struct RecordExtractionFailureInput {
    pub job_id: String,
    pub attempt_no: i32,
    pub lease_token: String,
    pub failure_kind: ExtractionFailureKind,
    pub error_code: String,
    pub error_message: String,
    pub partial_result: Option<Value>,
    pub retry_delay_ms: i64,
    pub now: DateTime<Utc>,
}

pub async fn record_extraction_failure(
    pool: &PgPool,
    input: RecordExtractionFailureInput,
) -> Result<bool, IngestionJobError> {
    let RecordExtractionFailureInput {
        job_id,
        attempt_no,
        lease_token,
        failure_kind,
        error_code,
        error_message,
        partial_result,
        retry_delay_ms,
        now,
    } = input;
    finish_job(
        pool,
        FinishJobInput {
            job_id,
            attempt_no,
            lease_token,
            now,
            result: partial_result,
            failure: Some((error_code, error_message)),
            success: None,
        },
        move |job, now_ms| {
            finish_extraction_failure(job, now_ms, retry_delay_ms, failure_kind, job.max_attempts)
        },
    )
    .await
}

pub async fn save_interaction_reply_preview(
    pool: &PgPool,
    job_id: &str,
    attempt_no: i32,
    lease_token: &str,
    preview: &str,
    now: DateTime<Utc>,
) -> Result<bool, IngestionJobError> {
    let job_id = parse_uuid(job_id, "job_id")?;
    let lease_token = parse_uuid(lease_token, "lease_token")?;
    let now = db_time(now.timestamp_millis())?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| IngestionJobError::Pool(error.to_string()))?;
    use public::extraction_jobs::dsl as jobs;
    let updated = diesel::update(
        jobs::extraction_jobs
            .filter(jobs::id.eq(job_id))
            .filter(jobs::status.eq("running"))
            .filter(jobs::attempt_count.eq(attempt_no))
            .filter(jobs::lease_token.eq(Some(lease_token)))
            .filter(jobs::lease_expires_at.gt(Some(now))),
    )
    .set((
        jobs::interaction_reply_preview.eq(preview),
        jobs::updated_at.eq(now),
    ))
    .execute(&mut *pooled)
    .await?;
    Ok(updated == 1)
}

struct FinishJobInput {
    job_id: String,
    attempt_no: i32,
    lease_token: String,
    now: DateTime<Utc>,
    result: Option<Value>,
    failure: Option<(String, String)>,
    success: Option<SuccessMetadata>,
}

async fn finish_job<F>(
    pool: &PgPool,
    input: FinishJobInput,
    decide: F,
) -> Result<bool, IngestionJobError>
where
    F: FnOnce(
            &RunningJob,
            i64,
        ) -> Result<
            evidence_application::ingestion_job::ExtractionJobTransition,
            FinishExtractionJobError,
        > + Send,
{
    let FinishJobInput {
        job_id,
        attempt_no,
        lease_token,
        now,
        result,
        failure,
        success,
    } = input;
    let job_id = parse_uuid(&job_id, "job_id")?;
    let lease_token = parse_uuid(&lease_token, "lease_token")?;
    let now_ms = now.timestamp_millis();
    let now = db_time(now_ms)?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| IngestionJobError::Pool(error.to_string()))?;
    (*pooled)
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
                    jobs::result,
                    jobs::finished_at,
                ))
                .first::<StateRow>(&mut *conn)
                .await
                .optional()?;
            let Some(row) = row else {
                return Ok(false);
            };
            let job = row.job()?;
            let ExtractionJob::Running(job) = job else {
                return Ok(false);
            };
            let transition = decide(&job, now_ms)
                .map_err(|_| IngestionJobError::InvalidTransition)?;
            if job.attempt_count != attempt_no {
                return Ok(false);
            }
            let error_code = failure.as_ref().map(|value| value.0.as_str());
            let error_message = failure.as_ref().map(|value| value.1.as_str());
            let result = if failure.is_some() {
                Some(merge_failed_result(row.result, result))
            } else {
                result.map(Some)
            };
            let result_for_effects = result.clone();
            let changed = diesel::update(
                jobs::extraction_jobs
                    .filter(jobs::id.eq(job_id))
                    .filter(jobs::status.eq("running"))
                    .filter(jobs::attempt_count.eq(attempt_no))
                    .filter(jobs::lease_token.eq(Some(lease_token)))
                    .filter(jobs::lease_expires_at.gt(Some(now))),
            )
            .set(JobFinishChangeset {
                status: status_text(transition.status()),
                failure_count: transition.failure_count(),
                available_at: db_time(transition.available_at_ms())?,
                finished_at: Some(transition.finished_at_ms().map(db_time).transpose()?),
                lease_token: Some(None),
                lease_expires_at: Some(None),
                result,
                error_code: Some(error_code),
                error_message: Some(error_message),
                interaction_reply_preview: "",
                updated_at: now,
            })
            .execute(&mut *conn)
            .await?;
            if changed != 1 {
                return Ok(false);
            }
            let attempt_status = if transition.status() == ExtractionJobStatus::Succeeded {
                "succeeded"
            } else {
                "failed"
            };
            let attempt_updated = diesel::update(
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
            if attempt_updated != 1 {
                return Err(IngestionJobError::InvalidTransition);
            }
            if failure.is_none()
                && let (Some(Some(result)), Some(success)) =
                    (result_for_effects.as_ref(), success.as_ref())
            {
                persist_success_effects(&mut *conn, job_id, result, success, now).await?;
            }
            Ok(true)
        })
        .await
}

#[derive(diesel::Queryable)]
struct SourceDocumentRow {
    sha256: String,
    object_key: String,
    mime_type: String,
    source_kind: String,
    analysis_object_key: Option<String>,
    analysis_mime_type: Option<String>,
    analysis_transform: Option<Value>,
    created_by: Uuid,
}

struct SuccessMetadata {
    household_id: String,
    source_document_id: String,
    source_hash: String,
    source_kind: String,
    created_by: String,
    version: i64,
}

async fn persist_success_effects(
    conn: &mut AsyncPgConnection,
    job_id: Uuid,
    result: &Value,
    metadata: &SuccessMetadata,
    now: DateTime<Utc>,
) -> Result<(), IngestionJobError> {
    let household_id = parse_uuid(&metadata.household_id, "household_id")?;
    let source_document_id = parse_uuid(&metadata.source_document_id, "source_document_id")?;
    let created_by = parse_uuid(&metadata.created_by, "source_created_by")?;

    if !result
        .get("conversation")
        .and_then(Value::as_array)
        .is_some_and(|conversation| !conversation.is_empty())
    {
        let observations = extraction_observations(result);
        for observation in observations {
            let occurred_at = observation
                .get("occurredAt")
                .and_then(Value::as_str)
                .and_then(|value| DateTime::parse_from_rfc3339(value).ok())
                .map(|value| value.with_timezone(&Utc));
            let row_id = Uuid::now_v7();
            let source_reference = observation
                .get("sourceReference")
                .and_then(Value::as_str)
                .unwrap_or_default();
            let transaction_kind = observation
                .get("transactionKind")
                .and_then(Value::as_str)
                .unwrap_or("RECEIPT_PURCHASE");
            let order_id = observation
                .get("orderId")
                .and_then(Value::as_str)
                .unwrap_or_default();
            let transaction_id = observation
                .get("transactionId")
                .and_then(Value::as_str)
                .unwrap_or_default();
            let merchant = observation
                .get("merchant")
                .and_then(Value::as_str)
                .unwrap_or_default();
            let amount = observation.get("amount").and_then(Value::as_i64);
            let card_last4 = observation
                .get("cardLast4")
                .and_then(Value::as_str)
                .unwrap_or_default();

            diesel::insert_into(public::source_observations::table)
                .values((
                    public::source_observations::id.eq(row_id),
                    public::source_observations::household_id.eq(household_id),
                    public::source_observations::source_document_id.eq(source_document_id),
                    public::source_observations::extraction_job_id.eq(job_id),
                    public::source_observations::source_reference.eq(source_reference),
                    public::source_observations::source_hash.eq(&metadata.source_hash),
                    public::source_observations::source_kind.eq(&metadata.source_kind),
                    public::source_observations::transaction_kind.eq(transaction_kind),
                    public::source_observations::external_order_id.eq(order_id),
                    public::source_observations::external_transaction_id.eq(transaction_id),
                    public::source_observations::merchant.eq(merchant),
                    public::source_observations::occurred_at.eq(occurred_at),
                    public::source_observations::amount_yen.eq(amount),
                    public::source_observations::card_last4.eq(card_last4),
                    public::source_observations::currency_code.eq("JPY"),
                    public::source_observations::raw_observation.eq(&observation),
                    public::source_observations::created_at.eq(now),
                ))
                .execute(conn)
                .await?;
        }
    }

    horse_postgres_common::insert_ui_change(
        conn,
        household_id,
        source_document_id,
        metadata.version,
        created_by,
        now,
        evidence_application::extraction_updated(),
    )
    .await?;

    Ok(())
}

fn extraction_observations(result: &Value) -> Vec<Value> {
    let mut observations = result
        .get("statements")
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .enumerate()
        .map(|(index, row)| {
            let mut observation = row.as_object().cloned().unwrap_or_default();
            if !observation.contains_key("sourceReference") {
                let row_number = row
                    .get("row")
                    .and_then(Value::as_i64)
                    .unwrap_or(index as i64 + 1);
                observation.insert(
                    "sourceReference".to_owned(),
                    Value::String(format!("row:{row_number}")),
                );
            }
            observation.insert(
                "transactionKind".to_owned(),
                row.get("kind")
                    .cloned()
                    .unwrap_or_else(|| Value::String("UNCLASSIFIED".to_owned())),
            );
            observation.insert(
                "amount".to_owned(),
                row.get("amount").cloned().unwrap_or(Value::Null),
            );
            Value::Object(observation)
        })
        .collect::<Vec<_>>();

    let receipt = result.get("receipt");
    if receipt
        .and_then(|value| value.get("items"))
        .and_then(Value::as_array)
        .is_some_and(|items| !items.is_empty())
    {
        let payments = receipt
            .and_then(|value| value.get("payments"))
            .and_then(Value::as_array);
        let card_numbers = payments
            .into_iter()
            .flatten()
            .filter_map(|payment| payment.get("cardLast4").and_then(Value::as_str))
            .filter(|last4| !last4.is_empty())
            .collect::<Vec<_>>();
        let card_last4 = card_numbers
            .as_slice()
            .first()
            .filter(|first| card_numbers.iter().all(|value| value == *first))
            .copied()
            .unwrap_or_default();
        observations.push(serde_json::json!({
            "sourceReference": "",
            "transactionKind": "RECEIPT_PURCHASE",
            "orderId": receipt.and_then(|value| value.get("orderId")).cloned().unwrap_or(Value::Null),
            "transactionId": receipt.and_then(|value| value.get("transactionId")).cloned().unwrap_or(Value::Null),
            "merchant": receipt.and_then(|value| value.get("merchant")).cloned().unwrap_or(Value::Null),
            "occurredAt": receipt.and_then(|value| value.get("purchasedAt")).cloned().unwrap_or(Value::Null),
            "amount": receipt.and_then(|value| value.get("total")).cloned().unwrap_or(Value::Null),
            "cardLast4": card_last4,
        }));
    }
    observations
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
    (*pooled)
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
                    jobs::result,
                    jobs::finished_at,
                ))
                .load::<StateRow>(conn)
                .await?;
            let mut recovered = 0;
            for row in rows {
                let job = row.job()?;
                let ExtractionJob::Running(job) = job else {
                    return Err(IngestionJobError::InvalidTransition);
                };
                let transition = recover_expired_extraction_job(&job, now_ms, job.max_attempts)
                    .map_err(|_| IngestionJobError::InvalidTransition)?;
                let job_id = row.id;
                let attempt_no = job.attempt_count;
                let error_code = if transition.status() == ExtractionJobStatus::Failed {
                    "retry_exhausted"
                } else {
                    "worker_interrupted"
                };
                let finished_at = transition.finished_at_ms().map(db_time).transpose()?;
                let updated = diesel::update(
                    jobs::extraction_jobs
                        .filter(jobs::id.eq(job_id))
                        .filter(jobs::status.eq("running"))
                        .filter(jobs::attempt_count.eq(attempt_no))
                        .filter(jobs::lease_token.eq(row.lease_token)),
                )
                .set((
                    jobs::status.eq(status_text(transition.status())),
                    jobs::failure_count.eq(transition.failure_count()),
                    jobs::available_at.eq(db_time(transition.available_at_ms())?),
                    jobs::finished_at.eq(finished_at),
                    jobs::lease_token.eq(None::<Uuid>),
                    jobs::lease_expires_at.eq(None::<DateTime<Utc>>),
                    jobs::error_code.eq(Some(error_code)),
                    jobs::interaction_reply_preview.eq(""),
                    jobs::updated_at.eq(now),
                ))
                .execute(&mut *conn)
                .await?;
                if updated != 1 {
                    return Err(IngestionJobError::InvalidTransition);
                }
                let attempt_updated = diesel::update(
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
                if attempt_updated != 1 {
                    return Err(IngestionJobError::InvalidTransition);
                }
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
    finished_at: Option<DateTime<Utc>>,
}

impl JobRow {
    fn job(&self) -> Result<ExtractionJob, IngestionJobError> {
        job_from_parts(
            &self.id.to_string(),
            &self.status,
            self.attempt_count,
            self.max_attempts,
            self.failure_count,
            self.available_at.timestamp_millis(),
            self.lease_token.map(|token| token.to_string()),
            self.lease_expires_at
                .map(|expires_at| expires_at.timestamp_millis()),
            self.finished_at
                .map(|finished_at| finished_at.timestamp_millis()),
        )
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
    result: Option<Value>,
    finished_at: Option<DateTime<Utc>>,
}

impl StateRow {
    fn job(&self) -> Result<ExtractionJob, IngestionJobError> {
        job_from_parts(
            &self.id.to_string(),
            &self.status,
            self.attempt_count,
            self.max_attempts,
            self.failure_count,
            self.available_at.timestamp_millis(),
            self.lease_token.map(|token| token.to_string()),
            self.lease_expires_at
                .map(|expires_at| expires_at.timestamp_millis()),
            self.finished_at
                .map(|finished_at| finished_at.timestamp_millis()),
        )
    }
}

// DB行を状態別のジョブ型に復元する。実行中なのにリースがない行はデータ不整合として扱う。
#[allow(clippy::too_many_arguments)]
fn job_from_parts(
    id: &str,
    status: &str,
    attempt_count: i32,
    max_attempts: i32,
    failure_count: i32,
    available_at_ms: i64,
    lease_token: Option<String>,
    lease_expires_at_ms: Option<i64>,
    finished_at_ms: Option<i64>,
) -> Result<ExtractionJob, IngestionJobError> {
    match parse_status(status) {
        ExtractionJobStatus::Queued => Ok(ExtractionJob::Queued(QueuedJob {
            id: id.to_owned(),
            attempt_count,
            max_attempts,
            failure_count,
            available_at_ms,
        })),
        ExtractionJobStatus::Running => {
            let (Some(token), Some(expires_at_ms)) = (lease_token, lease_expires_at_ms) else {
                return Err(IngestionJobError::InvalidState);
            };
            let lease = ValidLease::new(token, expires_at_ms)
                .map_err(|_| IngestionJobError::InvalidState)?;
            Ok(ExtractionJob::Running(RunningJob {
                id: id.to_owned(),
                attempt_count,
                max_attempts,
                failure_count,
                available_at_ms,
                lease,
            }))
        }
        ExtractionJobStatus::Succeeded => {
            let Some(finished_at_ms) = finished_at_ms else {
                return Err(IngestionJobError::InvalidState);
            };
            Ok(ExtractionJob::Succeeded(FinishedJob {
                id: id.to_owned(),
                finished_at_ms,
            }))
        }
        ExtractionJobStatus::Failed => {
            let Some(finished_at_ms) = finished_at_ms else {
                return Err(IngestionJobError::InvalidState);
            };
            Ok(ExtractionJob::Failed(FinishedJob {
                id: id.to_owned(),
                finished_at_ms,
            }))
        }
        ExtractionJobStatus::Cancelled => {
            let Some(finished_at_ms) = finished_at_ms else {
                return Err(IngestionJobError::InvalidState);
            };
            Ok(ExtractionJob::Cancelled(FinishedJob {
                id: id.to_owned(),
                finished_at_ms,
            }))
        }
    }
}

#[derive(diesel::AsChangeset)]
#[diesel(table_name = public::extraction_jobs)]
struct JobFinishChangeset<'a> {
    status: &'a str,
    failure_count: i32,
    available_at: DateTime<Utc>,
    finished_at: Option<Option<DateTime<Utc>>>,
    lease_token: Option<Option<Uuid>>,
    lease_expires_at: Option<Option<DateTime<Utc>>>,
    result: Option<Option<Value>>,
    error_code: Option<Option<&'a str>>,
    error_message: Option<Option<&'a str>>,
    interaction_reply_preview: &'a str,
    updated_at: DateTime<Utc>,
}

fn merge_failed_result(previous: Option<Value>, current: Option<Value>) -> Option<Value> {
    let Some(mut current) = current.filter(|value| {
        value
            .get("runs")
            .and_then(Value::as_array)
            .is_some_and(|runs| !runs.is_empty())
            || value
                .get("ocrLines")
                .and_then(Value::as_array)
                .is_some_and(|lines| !lines.is_empty())
            || value
                .get("issues")
                .and_then(Value::as_array)
                .is_some_and(|issues| !issues.is_empty())
    }) else {
        return previous;
    };
    let Some(current_object) = current.as_object_mut() else {
        return previous;
    };
    let mut previous_object = previous
        .and_then(|value| value.as_object().cloned())
        .unwrap_or_default();
    let mut runs = previous_object
        .remove("runs")
        .and_then(|value| value.as_array().cloned())
        .unwrap_or_default();
    if let Some(new_runs) = current_object
        .remove("runs")
        .and_then(|value| value.as_array().cloned())
    {
        runs.extend(new_runs);
    }
    if !runs.is_empty() {
        previous_object.insert("runs".to_owned(), Value::Array(runs));
    }
    for key in ["issues", "ocrLines", "transform", "transformPage"] {
        if let Some(value) = current_object.remove(key) {
            previous_object.insert(key.to_owned(), value);
        }
    }
    Some(Value::Object(previous_object))
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
