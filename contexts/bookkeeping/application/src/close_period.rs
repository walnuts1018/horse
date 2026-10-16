use crate::workflow_failure::WorkflowFailure;
use bookkeeping_domain::close_period::{ClosePeriodCommand, ClosePeriodDecision, ClosePeriodError};

pub trait ClosePeriodOperations {
    type Error;
    type Result;

    async fn load(&mut self) -> Result<ClosePeriodCommand, Self::Error>;

    async fn persist(&mut self, decision: ClosePeriodDecision)
    -> Result<Self::Result, Self::Error>;
}

pub async fn close_period<O: ClosePeriodOperations>(
    operations: &mut O,
) -> Result<O::Result, WorkflowFailure<ClosePeriodError, O::Error>> {
    let command = operations
        .load()
        .await
        .map_err(WorkflowFailure::Infrastructure)?;
    let decision =
        bookkeeping_domain::decide_close_period(command).map_err(WorkflowFailure::Decision)?;
    operations
        .persist(decision)
        .await
        .map_err(WorkflowFailure::Infrastructure)
}
