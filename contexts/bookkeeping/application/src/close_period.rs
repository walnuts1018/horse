use bookkeeping_domain::close_period::{ClosePeriodCommand, ClosePeriodDecision, ClosePeriodError};

#[derive(Debug)]
pub enum ClosePeriodWorkflowError<E> {
    Decision(ClosePeriodError),
    Operations(E),
}

pub trait ClosePeriodOperations {
    type Error;
    type Result;

    async fn load(&mut self) -> Result<ClosePeriodCommand, Self::Error>;

    async fn persist(&mut self, decision: ClosePeriodDecision)
    -> Result<Self::Result, Self::Error>;
}

pub async fn close_period<O: ClosePeriodOperations>(
    operations: &mut O,
) -> Result<O::Result, ClosePeriodWorkflowError<O::Error>> {
    let command = operations
        .load()
        .await
        .map_err(ClosePeriodWorkflowError::Operations)?;
    let decision = bookkeeping_domain::decide_close_period(command)
        .map_err(ClosePeriodWorkflowError::Decision)?;
    operations
        .persist(decision)
        .await
        .map_err(ClosePeriodWorkflowError::Operations)
}
