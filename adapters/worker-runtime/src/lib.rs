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

use std::{env, error::Error, time::Duration};

use chrono::Utc;
use evidence_application::ingestion_job::{ExtractionExecutionOutcome, execute_extraction_job};
use evidence_postgres::ingestion_job::{
    ClaimedExtractionJob, claim_next_extraction_job, record_extraction_failure,
    record_extraction_success, recover_expired_extraction_jobs,
};
use horse_postgres_common::connect;
use object_store::ObjectStore;
use processing::{ProcessConfig, process};
use serde_json::Value;

pub struct Worker {
    pool: horse_postgres_common::PgPool,
    store: ObjectStore,
    config: ProcessConfig,
}

impl Worker {
    pub async fn from_env() -> Result<Self, Box<dyn Error + Send + Sync>> {
        let database_url = required_env("DATABASE_URL")?;
        let store = ObjectStore::from_env().await?;
        let pool = connect(database_url).await?;
        Ok(Self {
            pool,
            store,
            config: ProcessConfig::from_env(),
        })
    }

    pub async fn run(
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
            match claim_next_extraction_job(&self.pool, now).await {
                Ok(Some(job)) => self.process_one(job).await,
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

    async fn process_one(&self, job: ClaimedExtractionJob) {
        let success_job = job.clone();
        let failure_job = job.clone();
        let pool = &self.pool;
        let previous_result = job.previous_result.clone();
        let outcome = execute_extraction_job(
            process(&self.pool, &self.store, &self.config, &job),
            |mut result| async move {
                append_previous_runs(&mut result, previous_result.as_ref());
                record_extraction_success(pool, &success_job, result, Utc::now()).await
            },
            |failure| async move {
                let retry_delay_ms = failure.retry_delay_ms(failure_job.attempt_no, Utc::now());
                record_extraction_failure(
                    pool,
                    &failure_job.id,
                    failure_job.attempt_no,
                    &failure_job.lease_token,
                    failure.kind,
                    &failure.code,
                    failure.safe_message,
                    Some(failure.partial_result.clone()),
                    retry_delay_ms,
                    Utc::now(),
                )
                .await
            },
        )
        .await;
        match outcome {
            ExtractionExecutionOutcome::Succeeded(Ok(true)) => {
                tracing::info!(job_id = %job.id, attempt = job.attempt_no, "extraction completed")
            }
            ExtractionExecutionOutcome::Succeeded(Ok(false)) => {
                tracing::warn!(job_id = %job.id, attempt = job.attempt_no, "extraction completion rejected by lease fence")
            }
            ExtractionExecutionOutcome::Succeeded(Err(error)) => {
                tracing::error!(job_id = %job.id, error = ?error, "save extraction result failed")
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
                tracing::error!(job_id = %job.id, error = ?error, "save extraction failure failed")
            }
        }
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
