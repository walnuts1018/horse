use crate::correct_purchase::{CorrectPurchaseDecision, CorrectPurchaseError, CorrectionCommand};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CorrectedPurchase {
    pub correction_id: String,
    pub journal_id: String,
    pub ledger_entry_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LoadedPurchaseCorrection {
    pub command: Option<CorrectionCommand>,
    pub replay: Option<CorrectedPurchase>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CorrectPurchaseWorkflowError<E> {
    Decision(CorrectPurchaseError),
    InvalidLoadedCorrection,
    Operations(E),
}

pub trait CorrectPurchaseOperations {
    type Error;

    async fn load(&mut self) -> Result<LoadedPurchaseCorrection, Self::Error>;

    async fn persist(
        &mut self,
        decision: CorrectPurchaseDecision,
    ) -> Result<CorrectedPurchase, Self::Error>;
}

pub async fn correct_purchase<O: CorrectPurchaseOperations>(
    operations: &mut O,
) -> Result<CorrectedPurchase, CorrectPurchaseWorkflowError<O::Error>> {
    let loaded = operations
        .load()
        .await
        .map_err(CorrectPurchaseWorkflowError::Operations)?;
    if let Some(replay) = loaded.replay {
        return Ok(replay);
    }
    let command = loaded
        .command
        .ok_or(CorrectPurchaseWorkflowError::InvalidLoadedCorrection)?;
    let decision = crate::correct_purchase::decide_correct_purchase(command)
        .map_err(CorrectPurchaseWorkflowError::Decision)?;
    operations
        .persist(decision)
        .await
        .map_err(CorrectPurchaseWorkflowError::Operations)
}
