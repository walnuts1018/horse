use std::collections::{BTreeMap, BTreeSet};

use crate::{AllocationShares, AllocationWeight, PoolId, Yen, allocate};
use uuid::Uuid;

const FIELD_CATEGORY_ID: &str = "categoryId";
const FIELD_PRODUCT_VARIANT_ID: &str = "productVariantId";
const FIELD_ALLOCATIONS: &str = "allocations";
const FIELD_TAGS: &str = "tags";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkUpdateItemTarget {
    pub item_id: String,
    pub base_revision: u64,
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct BulkUpdateItemPatch {
    pub category_id_set: bool,
    pub category_id: Option<String>,
    pub product_variant_id_set: bool,
    pub product_variant_id: Option<String>,
    pub allocations_set: bool,
    pub allocations: Vec<ItemAllocationWeight>,
    pub tag_ids_set: bool,
    pub tag_ids: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ItemAllocationWeight {
    pub pool_id: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkUpdateItemsCommand {
    pub targets: Vec<BulkUpdateItemTarget>,
    pub patch: BulkUpdateItemPatch,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkUpdatePurchaseState {
    pub purchase_id: String,
    pub revision: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkUpdateItemState {
    pub item_id: String,
    pub purchase_id: String,
    pub revision: u64,
    pub amount_yen: i64,
    pub allocation_override: bool,
    pub field_revisions: BTreeMap<String, u64>,
    pub effective_allocations: Vec<ItemAllocationWeight>,
    pub allocation_rule_id: Option<String>,
    pub allocation_rule_version: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkUpdateItemDecision {
    pub item_id: String,
    pub purchase_id: String,
    pub revision: u64,
    pub field_revisions: BTreeMap<String, u64>,
    pub category_id: Option<Option<String>>,
    pub product_variant_id: Option<Option<String>>,
    pub allocation_override: bool,
    pub allocations: Option<Vec<ItemAllocation>>,
    pub allocation_rule_id: Option<String>,
    pub allocation_rule_version: u64,
    pub changed_fields: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ItemAllocation {
    pub pool_id: String,
    pub weight: i64,
    pub amount_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkUpdatePurchaseDecision {
    pub purchase_id: String,
    pub revision: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkUpdateItemsDecision {
    pub items: Vec<BulkUpdateItemDecision>,
    pub purchases: Vec<BulkUpdatePurchaseDecision>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BulkUpdateItemsError {
    EmptyTargets,
    TooManyTargets,
    EmptyPatch,
    InvalidItemId,
    InvalidTagId,
    InvalidPurchaseId,
    DuplicateTarget,
    DuplicateTag,
    MissingItem,
    InvalidRevision,
    Conflict,
    InvalidAmount,
    InvalidAllocation,
    DuplicateAllocationPool,
    InvalidAllocationVersion,
    RevisionOverflow,
}

pub fn decide_bulk_update_items(
    command: BulkUpdateItemsCommand,
    purchases: Vec<BulkUpdatePurchaseState>,
    items: Vec<BulkUpdateItemState>,
) -> Result<BulkUpdateItemsDecision, BulkUpdateItemsError> {
    if command.targets.is_empty() {
        return Err(BulkUpdateItemsError::EmptyTargets);
    }
    if command.targets.len() > 100 {
        return Err(BulkUpdateItemsError::TooManyTargets);
    }
    if !command.patch.category_id_set
        && !command.patch.product_variant_id_set
        && !command.patch.allocations_set
        && !command.patch.tag_ids_set
    {
        return Err(BulkUpdateItemsError::EmptyPatch);
    }
    let mut target_ids = BTreeSet::new();
    for target in &command.targets {
        Uuid::parse_str(&target.item_id).map_err(|_| BulkUpdateItemsError::InvalidItemId)?;
        if target.base_revision == 0 {
            return Err(BulkUpdateItemsError::InvalidRevision);
        }
        if !target_ids.insert(target.item_id.as_str()) {
            return Err(BulkUpdateItemsError::DuplicateTarget);
        }
    }
    let mut purchases_by_id = BTreeMap::new();
    for purchase in purchases {
        Uuid::parse_str(&purchase.purchase_id)
            .map_err(|_| BulkUpdateItemsError::InvalidPurchaseId)?;
        purchases_by_id.insert(purchase.purchase_id.clone(), purchase);
    }
    let mut items_by_id = BTreeMap::new();
    for item in items {
        Uuid::parse_str(&item.item_id).map_err(|_| BulkUpdateItemsError::InvalidItemId)?;
        Uuid::parse_str(&item.purchase_id).map_err(|_| BulkUpdateItemsError::InvalidPurchaseId)?;
        items_by_id.insert(item.item_id.clone(), item);
    }
    if items_by_id.len() != target_ids.len() {
        return Err(BulkUpdateItemsError::MissingItem);
    }

    let mut tag_ids = BTreeSet::new();
    if command.patch.tag_ids_set {
        for tag_id in &command.patch.tag_ids {
            Uuid::parse_str(tag_id).map_err(|_| BulkUpdateItemsError::InvalidTagId)?;
            if !tag_ids.insert(tag_id.as_str()) {
                return Err(BulkUpdateItemsError::DuplicateTag);
            }
        }
    }
    if command.patch.allocations_set {
        validate_weights(&command.patch.allocations)?;
    }

    let mut decisions = Vec::with_capacity(command.targets.len());
    let mut updated_purchase_ids = BTreeSet::new();
    for target in &command.targets {
        let item = items_by_id
            .get(&target.item_id)
            .ok_or(BulkUpdateItemsError::MissingItem)?;
        if item.revision != target.base_revision {
            return Err(BulkUpdateItemsError::Conflict);
        }
        if !purchases_by_id.contains_key(&item.purchase_id) {
            return Err(BulkUpdateItemsError::MissingItem);
        }
        let revision = item
            .revision
            .checked_add(1)
            .ok_or(BulkUpdateItemsError::RevisionOverflow)?;
        let mut changed_fields = Vec::with_capacity(4);
        if command.patch.category_id_set {
            changed_fields.push(FIELD_CATEGORY_ID.to_owned());
        }
        if command.patch.product_variant_id_set {
            changed_fields.push(FIELD_PRODUCT_VARIANT_ID.to_owned());
        }
        if command.patch.allocations_set {
            changed_fields.push(FIELD_ALLOCATIONS.to_owned());
        }
        if command.patch.tag_ids_set {
            changed_fields.push(FIELD_TAGS.to_owned());
        }
        let mut field_revisions = item.field_revisions.clone();
        for field in &changed_fields {
            field_revisions.insert(field.clone(), revision);
        }
        let allocations_changed = command.patch.allocations_set
            || (command.patch.category_id_set || command.patch.product_variant_id_set)
                && !item.allocation_override;
        let allocations = if allocations_changed {
            if item.allocation_rule_version == 0 {
                return Err(BulkUpdateItemsError::InvalidAllocationVersion);
            }
            let shares = allocation_shares(&item.effective_allocations)?;
            let amount =
                Yen::new(item.amount_yen).map_err(|_| BulkUpdateItemsError::InvalidAmount)?;
            Some(
                allocate(amount, &shares)
                    .into_iter()
                    .map(|allocation| {
                        Ok(ItemAllocation {
                            pool_id: allocation.pool_id().as_str().to_owned(),
                            weight: i64::try_from(allocation.weight())
                                .map_err(|_| BulkUpdateItemsError::InvalidAllocation)?,
                            amount_yen: allocation.amount().value(),
                        })
                    })
                    .collect::<Result<Vec<_>, BulkUpdateItemsError>>()?,
            )
        } else {
            None
        };
        updated_purchase_ids.insert(item.purchase_id.as_str());
        decisions.push(BulkUpdateItemDecision {
            item_id: item.item_id.clone(),
            purchase_id: item.purchase_id.clone(),
            revision,
            field_revisions,
            category_id: command
                .patch
                .category_id_set
                .then(|| command.patch.category_id.clone()),
            product_variant_id: command
                .patch
                .product_variant_id_set
                .then(|| command.patch.product_variant_id.clone()),
            allocation_override: item.allocation_override || command.patch.allocations_set,
            allocations,
            allocation_rule_id: if command.patch.allocations_set {
                None
            } else {
                item.allocation_rule_id.clone()
            },
            allocation_rule_version: item.allocation_rule_version,
            changed_fields,
        });
    }
    if target_ids.len() != decisions.len() {
        return Err(BulkUpdateItemsError::MissingItem);
    }
    let purchase_decisions = updated_purchase_ids
        .into_iter()
        .map(|purchase_id| {
            let purchase = purchases_by_id
                .get(purchase_id)
                .ok_or(BulkUpdateItemsError::MissingItem)?;
            Ok(BulkUpdatePurchaseDecision {
                purchase_id: purchase_id.to_owned(),
                revision: purchase
                    .revision
                    .checked_add(1)
                    .ok_or(BulkUpdateItemsError::RevisionOverflow)?,
            })
        })
        .collect::<Result<Vec<_>, BulkUpdateItemsError>>()?;
    Ok(BulkUpdateItemsDecision {
        items: decisions,
        purchases: purchase_decisions,
    })
}

fn validate_weights(weights: &[ItemAllocationWeight]) -> Result<(), BulkUpdateItemsError> {
    allocation_shares(weights).map(|_| ())
}

fn allocation_shares(
    weights: &[ItemAllocationWeight],
) -> Result<AllocationShares, BulkUpdateItemsError> {
    let mut seen = BTreeSet::new();
    let weights = weights
        .iter()
        .map(|weight| {
            if !seen.insert(weight.pool_id.as_str()) {
                return Err(BulkUpdateItemsError::DuplicateAllocationPool);
            }
            AllocationWeight::new(
                PoolId::new(weight.pool_id.clone())
                    .map_err(|_| BulkUpdateItemsError::InvalidAllocation)?,
                u64::try_from(weight.weight)
                    .map_err(|_| BulkUpdateItemsError::InvalidAllocation)?,
            )
            .map_err(|_| BulkUpdateItemsError::InvalidAllocation)
        })
        .collect::<Result<Vec<_>, _>>()?;
    AllocationShares::new(weights).map_err(|_| BulkUpdateItemsError::InvalidAllocation)
}
