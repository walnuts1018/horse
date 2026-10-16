use std::future::Future;

pub use evidence_domain::ingestion_job::*;

pub trait ClaimExtractionJobOperations {
    type Error;
    type Candidate;
    type Claimed;

    fn queued_job<'a>(&self, candidate: &'a Self::Candidate) -> &'a QueuedJob;

    async fn load_candidate(
        &mut self,
        now_ms: i64,
        job_id: Option<&str>,
        local_csv_only: bool,
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
    claim_eligible_job(
        operations,
        now_ms,
        None,
        false,
        lease_token,
        lease_duration_ms,
    )
    .await
}

pub async fn claim_next_local_extraction_job<O>(
    operations: &mut O,
    now_ms: i64,
    lease_token: impl Into<String>,
    lease_duration_ms: i64,
) -> Result<Option<O::Claimed>, ClaimExtractionJobWorkflowError<O::Error>>
where
    O: ClaimExtractionJobOperations,
{
    claim_eligible_job(
        operations,
        now_ms,
        None,
        true,
        lease_token,
        lease_duration_ms,
    )
    .await
}

pub async fn claim_extraction_job_by_id<O>(
    operations: &mut O,
    now_ms: i64,
    job_id: &str,
    local_csv_only: bool,
    lease_token: impl Into<String>,
    lease_duration_ms: i64,
) -> Result<Option<O::Claimed>, ClaimExtractionJobWorkflowError<O::Error>>
where
    O: ClaimExtractionJobOperations,
{
    claim_eligible_job(
        operations,
        now_ms,
        Some(job_id),
        local_csv_only,
        lease_token,
        lease_duration_ms,
    )
    .await
}

async fn claim_eligible_job<O>(
    operations: &mut O,
    now_ms: i64,
    job_id: Option<&str>,
    local_csv_only: bool,
    lease_token: impl Into<String>,
    lease_duration_ms: i64,
) -> Result<Option<O::Claimed>, ClaimExtractionJobWorkflowError<O::Error>>
where
    O: ClaimExtractionJobOperations,
{
    let Some(candidate) = operations
        .load_candidate(now_ms, job_id, local_csv_only)
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
