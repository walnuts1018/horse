#[derive(Debug)]
pub enum WorkflowFailure<DecisionError, InfrastructureError> {
    Decision(DecisionError),
    NotFound,
    Conflict,
    Infrastructure(InfrastructureError),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WorkflowRejection {
    InvalidInput,
}
