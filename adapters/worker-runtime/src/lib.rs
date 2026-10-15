mod email;
mod mailbox;
mod object_store;
mod processing;
mod statement_csv;

pub use email::EmailAttachment;
pub use mailbox::MailboxConfig;

pub fn parse_email_attachments(data: &[u8]) -> Result<Vec<EmailAttachment>, &'static str> {
    email::parse_email(data).map(|email| email.attachments)
}

use std::{
    env,
    error::Error,
    sync::{Arc, Mutex},
    time::Duration,
};

use chrono::Utc;
use evidence_application::ingestion_job::{ExtractionExecutionOutcome, execute_extraction_job};
use evidence_postgres::ingestion_job::{
    ClaimedExtractionJob, RecordExtractionFailureInput, claim_extraction_job_by_id,
    claim_next_extraction_job, claim_next_local_extraction_job, record_extraction_failure,
    record_extraction_success, recover_expired_extraction_jobs,
};
use horse_postgres_common::connect;
use object_store::ObjectStore;
use processing::{ProcessConfig, process};
use serde_json::Value;
use tracing::{Instrument, field::Empty};

pub struct Worker {
    pool: horse_postgres_common::PgPool,
    store: ObjectStore,
    config: ProcessConfig,
    mailbox: Option<MailboxConfig>,
}

impl Worker {
    pub async fn from_env() -> Result<Self, Box<dyn Error + Send + Sync>> {
        let database_url = required_env("DATABASE_URL")?;
        let store = ObjectStore::from_env().await?;
        let pool = connect(database_url).await?;
        let mailbox = MailboxConfig::from_env()?;
        Ok(Self {
            pool,
            store,
            config: ProcessConfig::from_env().map_err(std::io::Error::other)?,
            mailbox,
        })
    }

    pub async fn run(
        &self,
        shutdown: tokio::sync::watch::Receiver<bool>,
    ) -> Result<(), Box<dyn Error + Send + Sync>> {
        if let Some(config) = &self.mailbox {
            let (worker_result, ()) = tokio::join!(
                self.run_extractions(shutdown.clone()),
                mailbox::run(&self.pool, &self.store, config, shutdown),
            );
            worker_result?;
            return Ok(());
        }
        self.run_extractions(shutdown).await
    }

    async fn run_extractions(
        &self,
        mut shutdown: tokio::sync::watch::Receiver<bool>,
    ) -> Result<(), Box<dyn Error + Send + Sync>> {
        loop {
            if *shutdown.borrow() {
                break;
            }
            let now = Utc::now();
            if let Err(error) = recover_expired_extraction_jobs(&self.pool, now).await {
                tracing::error!(error = ?error, "recover expired extraction jobs failed");
            }
            let claimed = if self.config.gemini_enabled() {
                claim_next_extraction_job(&self.pool, now).await
            } else {
                claim_next_local_extraction_job(&self.pool, now).await
            };
            match claimed {
                Ok(Some(job)) => {
                    if let Err(error) = self.process_one(job).await {
                        tracing::error!(error = %error, "save extraction result failed");
                    }
                }
                Ok(None) => {
                    tokio::select! {
                        _ = tokio::time::sleep(Duration::from_secs(1)) => {},
                        changed = shutdown.changed() => {
                            if changed.is_err() || *shutdown.borrow() {
                                break;
                            }
                        }
                    }
                }
                Err(error) => {
                    tracing::error!(error = ?error, "claim extraction job failed");
                    tokio::time::sleep(Duration::from_secs(1)).await;
                }
            }
        }
        Ok(())
    }

    pub async fn is_ready(&self) -> bool {
        self.pool.get().await.is_ok()
    }

    pub async fn run_notified_job(
        &self,
        job_id: &str,
    ) -> Result<bool, Box<dyn Error + Send + Sync>> {
        let Some(job) = claim_extraction_job_by_id(
            &self.pool,
            job_id,
            !self.config.gemini_enabled(),
            Utc::now(),
        )
        .await
        .map_err(|error| {
            std::io::Error::other(format!("claim notified extraction job: {error:?}"))
        })?
        else {
            return Ok(false);
        };
        self.process_one(job).await.map_err(std::io::Error::other)?;
        Ok(true)
    }

    async fn process_one(&self, job: ClaimedExtractionJob) -> Result<(), String> {
        let span = tracing::info_span!(
            "extract.document",
            extraction.attempt = job.attempt_no,
            document.mime_type = %job.mime_type,
            document.source_kind = %job.source_kind,
            extraction.balanced = Empty,
            extraction.issue_count = Empty,
            otel.status_code = Empty,
            otel.status_description = Empty,
        );
        let success_job = job.clone();
        let failure_job = job.clone();
        let result_attributes = Arc::new(Mutex::new(None));
        let success_attributes = Arc::clone(&result_attributes);
        let pool = &self.pool;
        let previous_result = job.previous_result.clone();
        let outcome = execute_extraction_job(
            process(&self.pool, &self.store, &self.config, &job),
            |mut result| async move {
                let balanced = result.get("balanced").and_then(Value::as_bool);
                let issue_count = result.get("issues").and_then(Value::as_array).map(Vec::len);
                if let (Some(balanced), Some(issue_count)) = (balanced, issue_count)
                    && let Ok(mut attributes) = success_attributes.lock()
                {
                    *attributes = Some((balanced, issue_count));
                }
                append_previous_runs(&mut result, previous_result.as_ref());
                record_extraction_success(pool, &success_job, result, Utc::now()).await
            },
            |failure| {
                let job_id = failure_job.id.clone();
                let attempt_no = failure_job.attempt_no;
                let lease_token = failure_job.lease_token.clone();
                let retry_delay_ms = failure.retry_delay_ms(attempt_no, Utc::now());
                let kind = failure.kind;
                let code = failure.code.clone();
                let safe_message = failure.safe_message;
                let partial_result = Some(failure.partial_result.clone());
                async move {
                    record_extraction_failure(
                        pool,
                        RecordExtractionFailureInput {
                            job_id,
                            attempt_no,
                            lease_token,
                            failure_kind: kind,
                            error_code: code,
                            error_message: safe_message.to_owned(),
                            partial_result,
                            retry_delay_ms,
                            now: Utc::now(),
                        },
                    )
                    .await
                }
            },
        )
        .instrument(span.clone())
        .await;
        let _entered = span.enter();
        if matches!(&outcome, ExtractionExecutionOutcome::Failed { .. }) {
            span.record("otel.status_code", "ERROR");
            span.record("otel.status_description", "extraction_failed");
        }
        match outcome {
            ExtractionExecutionOutcome::Succeeded(Ok(true)) => {
                if let Ok(attributes) = result_attributes.lock()
                    && let Some((balanced, issue_count)) = *attributes
                {
                    span.record("extraction.balanced", balanced);
                    span.record("extraction.issue_count", issue_count);
                }
                tracing::info!(job_id = %job.id, attempt = job.attempt_no, "extraction completed")
            }
            ExtractionExecutionOutcome::Succeeded(Ok(false)) => {
                tracing::warn!(job_id = %job.id, attempt = job.attempt_no, "extraction completion rejected by lease fence")
            }
            ExtractionExecutionOutcome::Succeeded(Err(error)) => {
                tracing::error!(job_id = %job.id, error = ?error, "save extraction result failed");
                return Err(format!("save extraction result failed: {error:?}"));
            }
            ExtractionExecutionOutcome::Failed {
                failure,
                persistence: Ok(true),
            } => {
                tracing::warn!(job_id = %job.id, attempt = job.attempt_no, error_code = %failure.code, "extraction attempt failed")
            }
            ExtractionExecutionOutcome::Failed {
                persistence: Ok(false),
                ..
            } => {
                tracing::warn!(job_id = %job.id, attempt = job.attempt_no, "extraction failure rejected by lease fence")
            }
            ExtractionExecutionOutcome::Failed {
                persistence: Err(error),
                ..
            } => {
                tracing::error!(job_id = %job.id, error = ?error, "save extraction failure failed");
                return Err(format!("save extraction failure failed: {error:?}"));
            }
        }
        Ok(())
    }
}

fn append_previous_runs(result: &mut Value, previous: Option<&Value>) {
    let Some(previous_runs) = previous
        .and_then(|value| value.get("runs"))
        .and_then(Value::as_array)
    else {
        return;
    };
    let Some(result) = result.as_object_mut() else {
        return;
    };
    let current = result
        .remove("runs")
        .and_then(|value| value.as_array().cloned())
        .unwrap_or_default();
    let mut runs = previous_runs.clone();
    runs.extend(current);
    result.insert("runs".to_owned(), Value::Array(runs));
}

fn required_env(name: &str) -> Result<String, Box<dyn Error + Send + Sync>> {
    let value = env::var(name)?;
    if value.trim().is_empty() {
        return Err(format!("{name} must be configured").into());
    }
    Ok(value)
}
