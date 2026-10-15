use std::collections::BTreeSet;

use crate::update_purchase::{
    UpdatePurchaseCommand, UpdatePurchaseDecision, UpdatePurchaseDraft, UpdatePurchaseError,
    decide_update_purchase,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkUpdatePurchaseTarget {
    pub command: UpdatePurchaseCommand,
    pub current: UpdatePurchaseDraft,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkUpdatePurchasesDecision {
    pub decisions: Vec<UpdatePurchaseDecision>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BulkUpdatePurchasesError {
    EmptyTargets,
    TooManyTargets,
    DuplicatePurchase,
    InvalidTarget(UpdatePurchaseError),
}

pub fn decide_bulk_update_purchases(
    targets: Vec<BulkUpdatePurchaseTarget>,
) -> Result<BulkUpdatePurchasesDecision, BulkUpdatePurchasesError> {
    if targets.is_empty() {
        return Err(BulkUpdatePurchasesError::EmptyTargets);
    }
    if targets.len() > 100 {
        return Err(BulkUpdatePurchasesError::TooManyTargets);
    }
    let mut purchase_ids = BTreeSet::<String>::new();
    let mut decisions = Vec::with_capacity(targets.len());
    for target in targets {
        if !purchase_ids.insert(target.command.purchase_id.as_str().to_owned()) {
            return Err(BulkUpdatePurchasesError::DuplicatePurchase);
        }
        decisions.push(
            decide_update_purchase(target.command, target.current)
                .map_err(BulkUpdatePurchasesError::InvalidTarget)?,
        );
    }
    Ok(BulkUpdatePurchasesDecision { decisions })
}
