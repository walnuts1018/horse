use std::collections::{BTreeMap, BTreeSet};

use crate::{AllocationShares, AllocationWeight, PoolId, Yen, allocate};

use super::create_purchase::Measure;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdatePurchaseItemCommand {
    pub item_id: String,
    pub base_revision: u64,
    pub name: ValueChange<String>,
    pub amount_yen: ValueChange<i64>,
    pub quantity: OptionalValueChange<Measure>,
    pub printed_unit_price: OptionalValueChange<PrintedUnitPrice>,
    pub category_id: OptionalValueChange<String>,
    pub product_variant_id: OptionalValueChange<String>,
    pub product_learning: Option<ProductLearning>,
    pub allocations: ValueChange<Vec<ItemAllocationWeight>>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ValueChange<T> {
    Unchanged,
    Set(T),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum OptionalValueChange<T> {
    Unchanged,
    Set(T),
    Clear,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ProductLearning {
    ItemOnly,
    VariantName(String),
    MerchantAlias,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PrintedUnitPrice {
    pub amount_yen: i64,
    pub basis: Measure,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ItemAllocationWeight {
    pub pool_id: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ItemAllocation {
    pub pool_id: String,
    pub weight: i64,
    pub amount_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseItemState {
    pub revision: u64,
    pub name: String,
    pub amount_yen: i64,
    pub category_id: Option<String>,
    pub product_variant_id: Option<String>,
    pub quantity: Option<Measure>,
    pub printed_unit_price: Option<PrintedUnitPrice>,
    pub allocation_override: bool,
    pub allocation_rule_id: Option<String>,
    pub allocation_rule_version: u64,
    pub field_revisions: BTreeMap<String, u64>,
    pub allocations: Vec<ItemAllocationWeight>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocationBasis {
    pub weights: Vec<ItemAllocationWeight>,
    pub rule_id: Option<String>,
    pub rule_version: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdatePurchaseItemDecision {
    pub revision: u64,
    pub name: Option<String>,
    pub amount_yen: Option<i64>,
    pub quantity: Option<Option<Measure>>,
    pub printed_unit_price: Option<Option<PrintedUnitPrice>>,
    pub category_id: Option<Option<String>>,
    pub product_variant_id: Option<Option<String>>,
    pub product_learning: Option<ProductLearning>,
    pub allocation_override: bool,
    pub allocations: Option<Vec<ItemAllocation>>,
    pub allocation_rule_id: Option<String>,
    pub allocation_rule_version: u64,
    pub allocations_changed: bool,
    pub field_revisions: BTreeMap<String, u64>,
    pub changed_fields: Vec<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum UpdatePurchaseItemError {
    InvalidItemId,
    InvalidChangedFields,
    InvalidName,
    InvalidAmount,
    InvalidMeasure,
    InvalidAllocation,
    DuplicateAllocationPool,
    InvalidAllocationVersion,
    InvalidRevision,
    ProductLearningRequiresVariantChange,
    ProductLearningRequiresLinkedVariant,
    InvalidLearnedVariantName,
    Conflict,
}

pub fn decide_update_purchase_item(
    command: UpdatePurchaseItemCommand,
    current: PurchaseItemState,
    automatic_basis: Option<AllocationBasis>,
    default_basis: AllocationBasis,
) -> Result<UpdatePurchaseItemDecision, UpdatePurchaseItemError> {
    if command.item_id.is_empty() {
        return Err(UpdatePurchaseItemError::InvalidItemId);
    }
    if command.base_revision == 0 || command.base_revision > current.revision {
        return Err(UpdatePurchaseItemError::Conflict);
    }
    let mut changed_fields = Vec::new();
    if !matches!(&command.name, ValueChange::Unchanged) {
        changed_fields.push("name");
    }
    if !matches!(&command.amount_yen, ValueChange::Unchanged) {
        changed_fields.push("amountYen");
    }
    if !matches!(&command.quantity, OptionalValueChange::Unchanged) {
        changed_fields.push("quantity");
    }
    if !matches!(&command.printed_unit_price, OptionalValueChange::Unchanged) {
        changed_fields.push("printedUnitPrice");
    }
    if !matches!(&command.category_id, OptionalValueChange::Unchanged) {
        changed_fields.push("categoryId");
    }
    if !matches!(&command.product_variant_id, OptionalValueChange::Unchanged) {
        changed_fields.push("productVariantId");
    }
    if !matches!(&command.allocations, ValueChange::Unchanged) {
        changed_fields.push("allocations");
    }
    if changed_fields.is_empty() {
        return Err(UpdatePurchaseItemError::InvalidChangedFields);
    }
    for field in &changed_fields {
        if current
            .field_revisions
            .get(*field)
            .copied()
            .unwrap_or_default()
            > command.base_revision
        {
            return Err(UpdatePurchaseItemError::Conflict);
        }
    }

    let name = match &command.name {
        ValueChange::Unchanged => None,
        ValueChange::Set(value) => Some(value.trim().to_owned()),
    };
    if name
        .as_ref()
        .is_some_and(|value| value.is_empty() || value.chars().count() > 240)
    {
        return Err(UpdatePurchaseItemError::InvalidName);
    }
    let amount_yen_change = match &command.amount_yen {
        ValueChange::Unchanged => None,
        ValueChange::Set(amount) => Some(*amount),
    };
    if amount_yen_change.is_some_and(|amount| !(0..=crate::MAX_YEN).contains(&amount)) {
        return Err(UpdatePurchaseItemError::InvalidAmount);
    }
    if let OptionalValueChange::Set(price) = &command.printed_unit_price
        && !(0..=crate::MAX_YEN).contains(&price.amount_yen)
    {
        return Err(UpdatePurchaseItemError::InvalidMeasure);
    }
    if let OptionalValueChange::Set(value) = &command.category_id {
        validate_optional_id(Some(value))?;
    }
    if let OptionalValueChange::Set(value) = &command.product_variant_id {
        validate_optional_id(Some(value))?;
    }
    if let Some(learning) = &command.product_learning {
        if matches!(&command.product_variant_id, OptionalValueChange::Unchanged) {
            return Err(UpdatePurchaseItemError::ProductLearningRequiresVariantChange);
        }
        if !matches!(learning, &ProductLearning::ItemOnly)
            && !matches!(&command.product_variant_id, OptionalValueChange::Set(value) if !value.is_empty())
        {
            return Err(UpdatePurchaseItemError::ProductLearningRequiresLinkedVariant);
        }
        if let ProductLearning::VariantName(name) = learning {
            let trimmed = name.trim();
            if trimmed.is_empty() || name.len() > 240 {
                return Err(UpdatePurchaseItemError::InvalidLearnedVariantName);
            }
        }
    }

    let amount_yen = amount_yen_change.unwrap_or(current.amount_yen);
    let allocations_changed = !matches!(&command.allocations, ValueChange::Unchanged)
        || amount_yen != current.amount_yen
        || (!matches!(&command.category_id, OptionalValueChange::Unchanged)
            || !matches!(&command.product_variant_id, OptionalValueChange::Unchanged))
            && !current.allocation_override;
    let allocation_override =
        current.allocation_override || !matches!(&command.allocations, ValueChange::Unchanged);
    let (allocations, allocation_rule_id, allocation_rule_version) = if allocations_changed {
        let basis = if let ValueChange::Set(weights) = &command.allocations {
            AllocationBasis {
                weights: weights.clone(),
                rule_id: None,
                rule_version: default_basis.rule_version,
            }
        } else if (!matches!(&command.category_id, OptionalValueChange::Unchanged)
            || !matches!(&command.product_variant_id, OptionalValueChange::Unchanged))
            && !current.allocation_override
        {
            automatic_basis.unwrap_or_else(|| default_basis.clone())
        } else if current.allocations.is_empty() {
            default_basis.clone()
        } else {
            AllocationBasis {
                weights: current.allocations.clone(),
                rule_id: current.allocation_rule_id.clone(),
                rule_version: current.allocation_rule_version,
            }
        };
        if basis.rule_version == 0 {
            return Err(UpdatePurchaseItemError::InvalidAllocationVersion);
        }
        let shares = allocation_shares(&basis.weights)?;
        let yen = Yen::new(amount_yen).map_err(|_| UpdatePurchaseItemError::InvalidAmount)?;
        let amounts = allocate(yen, &shares)
            .into_iter()
            .map(|allocation| {
                Ok(ItemAllocation {
                    pool_id: allocation.pool_id().as_str().to_owned(),
                    weight: i64::try_from(allocation.weight())
                        .map_err(|_| UpdatePurchaseItemError::InvalidAllocation)?,
                    amount_yen: allocation.amount().value(),
                })
            })
            .collect::<Result<Vec<_>, UpdatePurchaseItemError>>()?;
        (Some(amounts), basis.rule_id, basis.rule_version)
    } else {
        (None, None, default_basis.rule_version)
    };

    let revision = current
        .revision
        .checked_add(1)
        .ok_or(UpdatePurchaseItemError::InvalidRevision)?;
    let mut field_revisions = current.field_revisions;
    for field in &changed_fields {
        field_revisions.insert((*field).to_owned(), revision);
    }
    Ok(UpdatePurchaseItemDecision {
        revision,
        name,
        amount_yen: amount_yen_change,
        quantity: match command.quantity {
            OptionalValueChange::Unchanged => None,
            OptionalValueChange::Set(value) => Some(Some(value)),
            OptionalValueChange::Clear => Some(None),
        },
        printed_unit_price: match command.printed_unit_price {
            OptionalValueChange::Unchanged => None,
            OptionalValueChange::Set(value) => Some(Some(value)),
            OptionalValueChange::Clear => Some(None),
        },
        category_id: match command.category_id {
            OptionalValueChange::Unchanged => None,
            OptionalValueChange::Set(value) => Some(Some(value)),
            OptionalValueChange::Clear => Some(None),
        },
        product_variant_id: match command.product_variant_id {
            OptionalValueChange::Unchanged => None,
            OptionalValueChange::Set(value) => Some(Some(value)),
            OptionalValueChange::Clear => Some(None),
        },
        product_learning: command.product_learning,
        allocation_override,
        allocations,
        allocation_rule_id,
        allocation_rule_version,
        allocations_changed,
        field_revisions,
        changed_fields: changed_fields.into_iter().map(str::to_owned).collect(),
    })
}

fn validate_optional_id(value: Option<&str>) -> Result<(), UpdatePurchaseItemError> {
    if value.is_some_and(str::is_empty) {
        return Err(UpdatePurchaseItemError::InvalidChangedFields);
    }
    Ok(())
}

fn allocation_shares(
    weights: &[ItemAllocationWeight],
) -> Result<AllocationShares, UpdatePurchaseItemError> {
    let mut domain_weights = Vec::with_capacity(weights.len());
    let mut seen = BTreeSet::new();
    for weight in weights {
        if weight.pool_id.is_empty() || weight.weight <= 0 {
            return Err(UpdatePurchaseItemError::InvalidAllocation);
        }
        if !seen.insert(weight.pool_id.as_str()) {
            return Err(UpdatePurchaseItemError::DuplicateAllocationPool);
        }
        let pool_id = PoolId::new(weight.pool_id.clone())
            .map_err(|_| UpdatePurchaseItemError::InvalidAllocation)?;
        let value =
            u64::try_from(weight.weight).map_err(|_| UpdatePurchaseItemError::InvalidAllocation)?;
        domain_weights.push(
            AllocationWeight::new(pool_id, value)
                .map_err(|_| UpdatePurchaseItemError::InvalidAllocation)?,
        );
    }
    AllocationShares::new(domain_weights).map_err(|_| UpdatePurchaseItemError::InvalidAllocation)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn command() -> UpdatePurchaseItemCommand {
        UpdatePurchaseItemCommand {
            item_id: "item".to_owned(),
            base_revision: 1,
            name: ValueChange::Unchanged,
            amount_yen: ValueChange::Set(5),
            quantity: OptionalValueChange::Unchanged,
            printed_unit_price: OptionalValueChange::Unchanged,
            category_id: OptionalValueChange::Unchanged,
            product_variant_id: OptionalValueChange::Unchanged,
            product_learning: None,
            allocations: ValueChange::Unchanged,
        }
    }

    fn state() -> PurchaseItemState {
        PurchaseItemState {
            revision: 2,
            name: "商品".to_owned(),
            amount_yen: 4,
            category_id: None,
            product_variant_id: None,
            quantity: None,
            printed_unit_price: None,
            allocation_override: false,
            allocation_rule_id: None,
            allocation_rule_version: 1,
            field_revisions: BTreeMap::new(),
            allocations: vec![ItemAllocationWeight {
                pool_id: "pool-a".to_owned(),
                weight: 1,
            }],
        }
    }

    fn basis() -> AllocationBasis {
        AllocationBasis {
            weights: vec![ItemAllocationWeight {
                pool_id: "pool-default".to_owned(),
                weight: 1,
            }],
            rule_id: None,
            rule_version: 3,
        }
    }

    #[test]
    fn reallocates_amount_using_existing_weights_and_records_field_revision() {
        let decision = decide_update_purchase_item(command(), state(), None, basis())
            .expect("valid amount update");

        assert_eq!(decision.revision, 3);
        assert_eq!(decision.field_revisions.get("amountYen"), Some(&3));
        assert_eq!(decision.allocations.as_ref().unwrap()[0].pool_id, "pool-a");
        assert_eq!(decision.allocations.as_ref().unwrap()[0].amount_yen, 5);
    }

    #[test]
    fn rejects_conflicting_field_revisions() {
        let mut conflicting_state = state();
        conflicting_state
            .field_revisions
            .insert("amountYen".to_owned(), 2);
        assert_eq!(
            decide_update_purchase_item(command(), conflicting_state, None, basis()),
            Err(UpdatePurchaseItemError::Conflict)
        );
    }
}
