use std::future::Future;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct QueuedJob {
    pub id: String,
    pub attempt_count: i32,
    pub max_attempts: i32,
    pub failure_count: i32,
    pub available_at_ms: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ValidLease {
    pub token: String,
    pub expires_at_ms: i64,
}

// 実行中のジョブは必ず有効なリースを持つ。リースなしのRunning状態は型で表現できない。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RunningJob {
    pub id: String,
    pub attempt_count: i32,
    pub max_attempts: i32,
    pub failure_count: i32,
    pub available_at_ms: i64,
    pub lease: ValidLease,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FinishedJob {
    pub id: String,
    pub finished_at_ms: Option<i64>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ExtractionJob {
    Queued(QueuedJob),
    Running(RunningJob),
    Succeeded(FinishedJob),
    Failed(FinishedJob),
    Cancelled(FinishedJob),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ExtractionJobStatus {
    Queued,
    Running,
    Succeeded,
    Failed,
    Cancelled,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ClaimExtractionJob {
    pub lease_token: String,
    pub attempt_no: i32,
    pub lease_expires_at_ms: i64,
}

pub trait ClaimExtractionJobOperations {
    type Error;
    type Candidate;
    type Claimed;

    fn queued_job(&self, candidate: &Self::Candidate) -> &QueuedJob;

    async fn load_next_candidate(
        &mut self,
        now_ms: i64,
    ) -> Result<Option<Self::Candidate>, Self::Error>;

    async fn persist_claim(
        &mut self,
        candidate: Self::Candidate,
        claim: ClaimExtractionJob,
        now_ms: i64,
    ) -> Result<Option<Self::Claimed>, Self::Error>;
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ClaimExtractionJobWorkflowError<E> {
    Operations(E),
    Decision(ClaimExtractionJobError),
}

pub async fn claim_next_extraction_job<O>(
    operations: &mut O,
    now_ms: i64,
    lease_token: impl Into<String>,
    lease_duration_ms: i64,
) -> Result<Option<O::Claimed>, ClaimExtractionJobWorkflowError<O::Error>>
where
    O: ClaimExtractionJobOperations,
{
    let Some(candidate) = operations
        .load_next_candidate(now_ms)
        .await
        .map_err(ClaimExtractionJobWorkflowError::Operations)?
    else {
        return Ok(None);
    };
    let claim = claim_extraction_job(
        operations.queued_job(&candidate),
        now_ms,
        lease_token,
        lease_duration_ms,
    )
    .map_err(ClaimExtractionJobWorkflowError::Decision)?;
    operations
        .persist_claim(candidate, claim, now_ms)
        .await
        .map_err(ClaimExtractionJobWorkflowError::Operations)
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ClaimExtractionJobError {
    InvalidState,
    InvalidLease,
    InvalidCounter,
}

pub fn claim_extraction_job(
    job: &QueuedJob,
    now_ms: i64,
    lease_token: impl Into<String>,
    lease_duration_ms: i64,
) -> Result<ClaimExtractionJob, ClaimExtractionJobError> {
    if lease_duration_ms <= 0 {
        return Err(ClaimExtractionJobError::InvalidLease);
    }
    if job.attempt_count < 0 || job.max_attempts <= 0 || job.failure_count < 0 {
        return Err(ClaimExtractionJobError::InvalidCounter);
    }
    if job.available_at_ms > now_ms {
        return Err(ClaimExtractionJobError::InvalidState);
    }
    if job.failure_count >= job.max_attempts {
        return Err(ClaimExtractionJobError::InvalidState);
    }
    let attempt_no = job
        .attempt_count
        .checked_add(1)
        .ok_or(ClaimExtractionJobError::InvalidCounter)?;
    let lease_expires_at_ms = now_ms
        .checked_add(lease_duration_ms)
        .ok_or(ClaimExtractionJobError::InvalidLease)?;
    let lease_token = lease_token.into();
    if lease_token.is_empty() {
        return Err(ClaimExtractionJobError::InvalidLease);
    }
    Ok(ClaimExtractionJob {
        lease_token,
        attempt_no,
        lease_expires_at_ms,
    })
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ExtractionFailureKind {
    Retryable,
    SpendCap,
    Permanent,
}

#[derive(Debug, PartialEq, Eq)]
pub enum ExtractionExecutionOutcome<F, E> {
    Succeeded(Result<bool, E>),
    Failed {
        failure: F,
        persistence: Result<bool, E>,
    },
}

pub async fn execute_extraction_job<
    O,
    F,
    E,
    ProcessFuture,
    PersistSuccess,
    SuccessFuture,
    PersistFailure,
    FailureFuture,
>(
    process: ProcessFuture,
    persist_success: PersistSuccess,
    persist_failure: PersistFailure,
) -> ExtractionExecutionOutcome<F, E>
where
    ProcessFuture: Future<Output = Result<O, F>>,
    PersistSuccess: FnOnce(O) -> SuccessFuture,
    SuccessFuture: Future<Output = Result<bool, E>>,
    PersistFailure: FnOnce(&F) -> FailureFuture,
    FailureFuture: Future<Output = Result<bool, E>>,
{
    match process.await {
        Ok(result) => ExtractionExecutionOutcome::Succeeded(persist_success(result).await),
        Err(failure) => ExtractionExecutionOutcome::Failed {
            persistence: persist_failure(&failure).await,
            failure,
        },
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FinishExtractionJob {
    pub status: ExtractionJobStatus,
    pub failure_count: i32,
    pub available_at_ms: i64,
    pub finished_at_ms: Option<i64>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FinishExtractionJobError {
    StaleLease,
    InvalidCounter,
    InvalidRetryDelay,
}

pub fn finish_extraction_failure(
    job: &RunningJob,
    now_ms: i64,
    retry_delay_ms: i64,
    failure_kind: ExtractionFailureKind,
    max_failures: i32,
) -> Result<FinishExtractionJob, FinishExtractionJobError> {
    if job.lease.expires_at_ms <= now_ms {
        return Err(FinishExtractionJobError::StaleLease);
    }
    if job.failure_count < 0 || max_failures <= 0 {
        return Err(FinishExtractionJobError::InvalidCounter);
    }
    if retry_delay_ms < 0 {
        return Err(FinishExtractionJobError::InvalidRetryDelay);
    }
    let failure_count = match failure_kind {
        ExtractionFailureKind::SpendCap => job.failure_count,
        ExtractionFailureKind::Retryable | ExtractionFailureKind::Permanent => job
            .failure_count
            .checked_add(1)
            .ok_or(FinishExtractionJobError::InvalidCounter)?,
    };
    let retry = failure_kind == ExtractionFailureKind::SpendCap
        || failure_kind == ExtractionFailureKind::Retryable && failure_count < max_failures;
    let available_at_ms = if retry {
        now_ms
            .checked_add(retry_delay_ms)
            .ok_or(FinishExtractionJobError::InvalidRetryDelay)?
    } else {
        job.available_at_ms
    };
    Ok(FinishExtractionJob {
        status: if retry {
            ExtractionJobStatus::Queued
        } else {
            ExtractionJobStatus::Failed
        },
        failure_count,
        available_at_ms,
        finished_at_ms: (!retry).then_some(now_ms),
    })
}

pub fn finish_extraction_success(
    job: &RunningJob,
    now_ms: i64,
) -> Result<FinishExtractionJob, FinishExtractionJobError> {
    // リース所有者の一致は呼び出し側の条件付き更新で保証し、ここでは有効期限のみ検証する。
    if job.lease.expires_at_ms <= now_ms {
        return Err(FinishExtractionJobError::StaleLease);
    }
    Ok(FinishExtractionJob {
        status: ExtractionJobStatus::Succeeded,
        failure_count: job.failure_count,
        available_at_ms: job.available_at_ms,
        finished_at_ms: Some(now_ms),
    })
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecoverExpiredExtractionJob {
    pub status: ExtractionJobStatus,
    pub failure_count: i32,
    pub available_at_ms: i64,
    pub finished_at_ms: Option<i64>,
}

pub fn recover_expired_extraction_job(
    job: &RunningJob,
    now_ms: i64,
    max_failures: i32,
) -> Result<RecoverExpiredExtractionJob, FinishExtractionJobError> {
    if job.lease.expires_at_ms > now_ms {
        return Err(FinishExtractionJobError::StaleLease);
    }
    if job.failure_count < 0 || max_failures <= 0 {
        return Err(FinishExtractionJobError::InvalidCounter);
    }
    let failure_count = job
        .failure_count
        .checked_add(1)
        .ok_or(FinishExtractionJobError::InvalidCounter)?;
    let retry = failure_count < max_failures;
    Ok(RecoverExpiredExtractionJob {
        status: if retry {
            ExtractionJobStatus::Queued
        } else {
            ExtractionJobStatus::Failed
        },
        failure_count,
        available_at_ms: if retry { now_ms } else { job.available_at_ms },
        finished_at_ms: (!retry).then_some(now_ms),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn running_job() -> RunningJob {
        RunningJob {
            id: "job".to_owned(),
            attempt_count: 2,
            max_attempts: 5,
            failure_count: 1,
            available_at_ms: 0,
            lease: ValidLease {
                token: "active-token".to_owned(),
                expires_at_ms: 10_000,
            },
        }
    }

    #[test]
    fn spend_cap_requeues_without_consuming_failure_budget() {
        let job = running_job();
        let transition =
            finish_extraction_failure(&job, 9_000, 30_000, ExtractionFailureKind::SpendCap, 5)
                .expect("current lease can be retried");

        assert_eq!(transition.status, ExtractionJobStatus::Queued);
        assert_eq!(transition.failure_count, 1);
        assert_eq!(transition.available_at_ms, 39_000);
        assert_eq!(transition.finished_at_ms, None);
    }

    #[test]
    fn expired_lease_cannot_finish_after_reclaim_window() {
        let job = running_job();
        let result = finish_extraction_success(&job, 10_000);

        assert_eq!(result, Err(FinishExtractionJobError::StaleLease));
    }

    #[test]
    fn expired_attempts_consume_failure_budget_and_eventually_fail() {
        let mut job = running_job();
        job.failure_count = 4;
        let transition = recover_expired_extraction_job(&job, 10_000, 5)
            .expect("expired lease can be recovered");

        assert_eq!(transition.status, ExtractionJobStatus::Failed);
        assert_eq!(transition.failure_count, 5);
        assert_eq!(transition.finished_at_ms, Some(10_000));
    }
}
