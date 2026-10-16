#[derive(Debug)]
pub enum WorkflowFailure<DecisionError, InfrastructureError> {
    Decision(DecisionError),
    InvalidInput,
    NotFound,
    Conflict,
    Infrastructure(InfrastructureError),
}
