use std::collections::BTreeSet;
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
struct ItemId(Uuid);

impl ItemId {
    fn new(value: &str) -> Result<Self, BulkDeleteItemsError> {
        Uuid::parse_str(value)
            .map(Self)
            .map_err(|_| BulkDeleteItemsError::InvalidItemId)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct PurchaseId(Uuid);

impl PurchaseId {
    fn new(value: &str) -> Result<Self, BulkDeleteItemsError> {
        Uuid::parse_str(value)
            .map(Self)
            .map_err(|_| BulkDeleteItemsError::InvalidPurchaseId)
    }
}

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
    InvalidPurchaseId,
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
    let target_ids = command
        .targets
        .iter()
        .map(|target| ItemId::new(&target.item_id))
        .collect::<Result<Vec<_>, _>>()?;
    let mut seen = BTreeSet::new();
    for (target, item_id) in command.targets.iter().zip(&target_ids) {
        if target.base_revision == 0 {
            return Err(BulkDeleteItemsError::InvalidRevision);
        }
        if !seen.insert(item_id) {
            return Err(BulkDeleteItemsError::DuplicateTarget);
        }
    }

    let purchase_ids = purchases
        .iter()
        .map(|purchase| PurchaseId::new(&purchase.purchase_id))
        .collect::<Result<Vec<_>, _>>()?;
    for purchase in &purchases {
        for item in &purchase.items {
            ItemId::new(&item.item_id)?;
        }
    }

    let mut decisions = Vec::with_capacity(purchases.len());
    let mut consumed = BTreeSet::new();
    for (purchase, purchase_id) in purchases.into_iter().zip(purchase_ids) {
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
            purchase_id: purchase_id.0.to_string(),
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
