use std::collections::BTreeSet;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkDeleteItemTarget {
    pub item_id: String,
    pub base_revision: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkDeleteItemCommand {
    pub targets: Vec<BulkDeleteItemTarget>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkDeleteItemState {
    pub item_id: String,
    pub revision: u64,
    pub amount_yen: i64,
    pub corrected: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkDeletePurchaseState {
    pub purchase_id: String,
    pub status: String,
    pub revision: u64,
    pub item_count: usize,
    pub items: Vec<BulkDeleteItemState>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkDeletePurchaseDecision {
    pub purchase_id: String,
    pub purchase_revision: u64,
    pub deleted_item_ids: Vec<String>,
    pub deleted_amount_yen: i64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BulkDeleteItemsError {
    EmptyTargets,
    TooManyTargets,
    InvalidItemId,
    DuplicateTarget,
    MissingItem,
    InvalidRevision,
    Conflict,
    ClosedPurchase,
    CorrectedItem,
    LastItem,
    InvalidAmount,
    RevisionOverflow,
}

pub fn decide_bulk_delete_items(
    command: BulkDeleteItemCommand,
    purchases: Vec<BulkDeletePurchaseState>,
) -> Result<Vec<BulkDeletePurchaseDecision>, BulkDeleteItemsError> {
    if command.targets.is_empty() {
        return Err(BulkDeleteItemsError::EmptyTargets);
    }
    if command.targets.len() > 100 {
        return Err(BulkDeleteItemsError::TooManyTargets);
    }
    let mut seen = BTreeSet::new();
    for target in &command.targets {
        if target.item_id.is_empty() {
            return Err(BulkDeleteItemsError::InvalidItemId);
        }
        if target.base_revision == 0 {
            return Err(BulkDeleteItemsError::InvalidRevision);
        }
        if !seen.insert(target.item_id.as_str()) {
            return Err(BulkDeleteItemsError::DuplicateTarget);
        }
    }

    let mut decisions = Vec::with_capacity(purchases.len());
    let mut consumed = BTreeSet::new();
    for purchase in purchases {
        if !matches!(purchase.status.as_str(), "DRAFT" | "POSTED") {
            return Err(BulkDeleteItemsError::ClosedPurchase);
        }
        let mut deleted_item_ids = Vec::new();
        let mut deleted_amount_yen = 0_i64;
        for item in &purchase.items {
            let Some(target) = command
                .targets
                .iter()
                .find(|target| target.item_id == item.item_id)
            else {
                continue;
            };
            consumed.insert(item.item_id.as_str());
            if target.base_revision != item.revision {
                return Err(BulkDeleteItemsError::Conflict);
            }
            if item.corrected {
                return Err(BulkDeleteItemsError::CorrectedItem);
            }
            deleted_amount_yen = deleted_amount_yen
                .checked_add(item.amount_yen)
                .filter(|amount| *amount <= bookkeeping_domain::MAX_YEN)
                .ok_or(BulkDeleteItemsError::InvalidAmount)?;
            deleted_item_ids.push(item.item_id.clone());
        }
        if deleted_item_ids.is_empty() {
            continue;
        }
        if purchase.item_count <= deleted_item_ids.len() {
            return Err(BulkDeleteItemsError::LastItem);
        }
        let purchase_revision = purchase
            .revision
            .checked_add(1)
            .ok_or(BulkDeleteItemsError::RevisionOverflow)?;
        decisions.push(BulkDeletePurchaseDecision {
            purchase_id: purchase.purchase_id,
            purchase_revision,
            deleted_item_ids,
            deleted_amount_yen,
        });
    }
    if consumed.len() != command.targets.len() {
        return Err(BulkDeleteItemsError::MissingItem);
    }
    Ok(decisions)
}
