use std::collections::{BTreeMap, BTreeSet};

use bookkeeping_domain::{AllocationShares, AllocationWeight, PoolId, Yen, allocate};

use crate::create_purchase::Measure;

const MAX_MEASURE_NUMERATOR: i64 = 1_000_000_000_000;
const MAX_MEASURE_DENOMINATOR: i64 = 1_000_000;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdatePurchaseItemCommand {
    pub item_id: String,
    pub base_revision: u64,
    pub changed_fields: Vec<String>,
    pub name: Option<String>,
    pub amount_yen: Option<i64>,
    pub quantity_set: bool,
    pub quantity: Option<Measure>,
    pub printed_unit_price_set: bool,
    pub printed_unit_price: Option<PrintedUnitPrice>,
    pub category_id_set: bool,
    pub category_id: Option<String>,
    pub product_variant_id_set: bool,
    pub product_variant_id: Option<String>,
    pub product_learning: Option<ProductLearning>,
    pub allocations_set: bool,
    pub allocations: Vec<ItemAllocationWeight>,
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
    pub status: String,
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
    DuplicateChangedField,
    MissingChangedValue,
    ExtraneousChangedValue,
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
    ClosedPurchase,
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
    if !matches!(current.status.as_str(), "DRAFT" | "POSTED") {
        return Err(UpdatePurchaseItemError::ClosedPurchase);
    }
    if command.base_revision == 0 || command.base_revision > current.revision {
        return Err(UpdatePurchaseItemError::Conflict);
    }
    if command.changed_fields.is_empty() {
        return Err(UpdatePurchaseItemError::InvalidChangedFields);
    }

    let allowed = [
        "name",
        "amountYen",
        "quantity",
        "printedUnitPrice",
        "categoryId",
        "productVariantId",
        "allocations",
    ];
    let mut seen = BTreeSet::new();
    for field in &command.changed_fields {
        if !allowed.contains(&field.as_str()) {
            return Err(UpdatePurchaseItemError::InvalidChangedFields);
        }
        if !seen.insert(field.as_str()) {
            return Err(UpdatePurchaseItemError::DuplicateChangedField);
        }
        if current
            .field_revisions
            .get(field)
            .copied()
            .unwrap_or_default()
            > command.base_revision
        {
            return Err(UpdatePurchaseItemError::Conflict);
        }
    }

    let has = |field: &str| command.changed_fields.iter().any(|value| value == field);
    for (field, supplied) in [
        ("name", command.name.is_some()),
        ("amountYen", command.amount_yen.is_some()),
        ("quantity", command.quantity_set),
        ("printedUnitPrice", command.printed_unit_price_set),
        ("categoryId", command.category_id_set),
        ("productVariantId", command.product_variant_id_set),
        ("allocations", command.allocations_set),
    ] {
        if has(field) && !supplied {
            return Err(UpdatePurchaseItemError::MissingChangedValue);
        }
        if !has(field) && supplied {
            return Err(UpdatePurchaseItemError::ExtraneousChangedValue);
        }
    }

    let name = command.name.map(|value| value.trim().to_owned());
    if name
        .as_ref()
        .is_some_and(|value| value.is_empty() || value.chars().count() > 240)
    {
        return Err(UpdatePurchaseItemError::InvalidName);
    }
    if command
        .amount_yen
        .is_some_and(|amount| !(0..=bookkeeping_domain::MAX_YEN).contains(&amount))
    {
        return Err(UpdatePurchaseItemError::InvalidAmount);
    }
    if command.quantity_set {
        if let Some(quantity) = &command.quantity {
            validate_measure(quantity)?;
        }
    }
    if command.printed_unit_price_set {
        if let Some(price) = &command.printed_unit_price {
            if !(0..=bookkeeping_domain::MAX_YEN).contains(&price.amount_yen) {
                return Err(UpdatePurchaseItemError::InvalidMeasure);
            }
            validate_measure(&price.basis)?;
        }
    }
    validate_optional_id(command.category_id.as_deref())?;
    validate_optional_id(command.product_variant_id.as_deref())?;
    if (!command.category_id_set && command.category_id.is_some())
        || (!command.product_variant_id_set && command.product_variant_id.is_some())
        || (!command.quantity_set && command.quantity.is_some())
        || (!command.printed_unit_price_set && command.printed_unit_price.is_some())
    {
        return Err(UpdatePurchaseItemError::ExtraneousChangedValue);
    }
    if let Some(learning) = &command.product_learning {
        if !has("productVariantId") {
            return Err(UpdatePurchaseItemError::ProductLearningRequiresVariantChange);
        }
        if !matches!(learning, &ProductLearning::ItemOnly)
            && command
                .product_variant_id
                .as_ref()
                .is_none_or(String::is_empty)
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

    let amount_yen = command.amount_yen.unwrap_or(current.amount_yen);
    let allocations_changed = command.allocations_set
        || amount_yen != current.amount_yen
        || (command.category_id_set || command.product_variant_id_set)
            && !current.allocation_override;
    let allocation_override = current.allocation_override || command.allocations_set;
    let (allocations, allocation_rule_id, allocation_rule_version) = if allocations_changed {
        let basis = if command.allocations_set {
            AllocationBasis {
                weights: command.allocations.clone(),
                rule_id: None,
                rule_version: default_basis.rule_version,
            }
        } else if (command.category_id_set || command.product_variant_id_set)
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
    for field in &command.changed_fields {
        field_revisions.insert(field.clone(), revision);
    }
    Ok(UpdatePurchaseItemDecision {
        revision,
        name,
        amount_yen: command.amount_yen,
        quantity: command.quantity_set.then_some(command.quantity),
        printed_unit_price: command
            .printed_unit_price_set
            .then_some(command.printed_unit_price),
        category_id: command.category_id_set.then_some(command.category_id),
        product_variant_id: command
            .product_variant_id_set
            .then_some(command.product_variant_id),
        product_learning: command.product_learning,
        allocation_override,
        allocations,
        allocation_rule_id,
        allocation_rule_version,
        allocations_changed,
        field_revisions,
        changed_fields: command.changed_fields,
    })
}

fn validate_optional_id(value: Option<&str>) -> Result<(), UpdatePurchaseItemError> {
    if value.is_some_and(str::is_empty) {
        return Err(UpdatePurchaseItemError::InvalidChangedFields);
    }
    Ok(())
}

fn validate_measure(measure: &Measure) -> Result<(), UpdatePurchaseItemError> {
    let numerator = measure.numerator;
    let denominator = measure.denominator;
    if numerator <= 0
        || numerator > MAX_MEASURE_NUMERATOR
        || denominator <= 0
        || denominator > MAX_MEASURE_DENOMINATOR
        || gcd(numerator, denominator) != 1
        || measure.unit.trim().is_empty()
        || measure.unit.len() > 32
    {
        return Err(UpdatePurchaseItemError::InvalidMeasure);
    }
    Ok(())
}

fn gcd(mut left: i64, mut right: i64) -> i64 {
    while right != 0 {
        let remainder = left % right;
        left = right;
        right = remainder;
    }
    left
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
            changed_fields: vec!["amountYen".to_owned()],
            name: None,
            amount_yen: Some(5),
            quantity_set: false,
            quantity: None,
            printed_unit_price_set: false,
            printed_unit_price: None,
            category_id_set: false,
            category_id: None,
            product_variant_id_set: false,
            product_variant_id: None,
            product_learning: None,
            allocations_set: false,
            allocations: Vec::new(),
        }
    }

    fn state() -> PurchaseItemState {
        PurchaseItemState {
            revision: 2,
            status: "DRAFT".to_owned(),
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
    fn rejects_bad_measures_and_conflicting_field_revisions() {
        let mut invalid = command();
        invalid.changed_fields = vec!["quantity".to_owned()];
        invalid.amount_yen = None;
        invalid.quantity_set = true;
        invalid.quantity = Some(Measure {
            numerator: 2,
            denominator: 4,
            unit: "個".to_owned(),
        });
        assert_eq!(
            decide_update_purchase_item(invalid, state(), None, basis()),
            Err(UpdatePurchaseItemError::InvalidMeasure)
        );

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
