use std::collections::BTreeSet;

use super::ids::{ActorId, HouseholdId};

macro_rules! allocation_id {
    ($name:ident) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
        pub struct $name(uuid::Uuid);

        impl $name {
            pub const fn from_uuid(value: uuid::Uuid) -> Self {
                Self(value)
            }
            pub const fn value(self) -> uuid::Uuid {
                self.0
            }
        }
    };
}

allocation_id!(AllocationPoolId);
allocation_id!(AllocationCategoryId);
allocation_id!(AllocationProductConceptId);
allocation_id!(AllocationProductVariantId);

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct AllocationRuleId(uuid::Uuid);

impl AllocationRuleId {
    pub const fn value(self) -> uuid::Uuid {
        self.0
    }

    pub fn from_persisted(value: uuid::Uuid) -> Self {
        Self(value)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NormalizedMerchant(String);

impl NormalizedMerchant {
    fn new(value: String) -> Self {
        Self(value)
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct AllocationRuleWeightValue(i64);

impl AllocationRuleWeightValue {
    pub const fn value(self) -> i64 {
        self.0
    }
}

impl TryFrom<i64> for AllocationRuleWeightValue {
    type Error = SetAllocationRuleError;

    fn try_from(value: i64) -> Result<Self, Self::Error> {
        if value > 0 {
            Ok(Self(value))
        } else {
            Err(SetAllocationRuleError::NonPositiveWeight)
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct AllocationRuleRevision(u64);

impl AllocationRuleRevision {
    pub const fn value(self) -> u64 {
        self.0
    }

    fn next(self) -> Result<Self, SetAllocationRuleError> {
        self.0
            .checked_add(1)
            .map(Self)
            .ok_or(SetAllocationRuleError::InvalidRevision)
    }
}

impl TryFrom<u64> for AllocationRuleRevision {
    type Error = SetAllocationRuleError;

    fn try_from(value: u64) -> Result<Self, Self::Error> {
        if value > 0 {
            Ok(Self(value))
        } else {
            Err(SetAllocationRuleError::InvalidRevision)
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocationRuleWeight {
    pub pool_id: AllocationPoolId,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ValidatedAllocationRuleWeight {
    pub pool_id: AllocationPoolId,
    pub weight: AllocationRuleWeightValue,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AllocationRuleTarget {
    Category(AllocationCategoryId),
    Merchant(String),
    ProductConcept(AllocationProductConceptId),
    ProductVariant(AllocationProductVariantId),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ValidatedAllocationRuleTarget {
    Category(AllocationCategoryId),
    Merchant(NormalizedMerchant),
    ProductConcept(AllocationProductConceptId),
    ProductVariant(AllocationProductVariantId),
}

impl ValidatedAllocationRuleTarget {
    pub fn category_id(&self) -> Option<uuid::Uuid> {
        match self {
            Self::Category(id) => Some(id.value()),
            _ => None,
        }
    }
    pub fn merchant(&self) -> Option<&str> {
        match self {
            Self::Merchant(value) => Some(value.as_str()),
            _ => None,
        }
    }
    pub fn product_concept_id(&self) -> Option<uuid::Uuid> {
        match self {
            Self::ProductConcept(id) => Some(id.value()),
            _ => None,
        }
    }
    pub fn product_variant_id(&self) -> Option<uuid::Uuid> {
        match self {
            Self::ProductVariant(id) => Some(id.value()),
            _ => None,
        }
    }
    pub fn changed_field(&self) -> &'static str {
        match self {
            Self::Category(_) => "categoryId",
            Self::Merchant(_) => "merchant",
            Self::ProductConcept(_) => "productConceptId",
            Self::ProductVariant(_) => "productVariantId",
        }
    }
}

impl AllocationRuleTarget {
    pub fn category_id(&self) -> Option<uuid::Uuid> {
        match self {
            Self::Category(id) => Some(id.value()),
            _ => None,
        }
    }

    pub fn merchant(&self) -> Option<&str> {
        match self {
            Self::Merchant(value) => Some(value),
            _ => None,
        }
    }

    pub fn product_concept_id(&self) -> Option<uuid::Uuid> {
        match self {
            Self::ProductConcept(id) => Some(id.value()),
            _ => None,
        }
    }

    pub fn product_variant_id(&self) -> Option<uuid::Uuid> {
        match self {
            Self::ProductVariant(id) => Some(id.value()),
            _ => None,
        }
    }

    pub fn changed_field(&self) -> &'static str {
        match self {
            Self::Category(_) => "categoryId",
            Self::Merchant(_) => "merchant",
            Self::ProductConcept(_) => "productConceptId",
            Self::ProductVariant(_) => "productVariantId",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetAllocationRuleCommand {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub target: AllocationRuleTarget,
    pub weights: Vec<AllocationRuleWeight>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ValidatedSetAllocationRuleCommand {
    actor_id: ActorId,
    household_id: HouseholdId,
    target: ValidatedAllocationRuleTarget,
    weights: Vec<ValidatedAllocationRuleWeight>,
}

impl ValidatedSetAllocationRuleCommand {
    pub fn actor_id(&self) -> uuid::Uuid {
        self.actor_id.value()
    }

    pub fn household_id(&self) -> uuid::Uuid {
        self.household_id.value()
    }

    pub fn target(&self) -> &ValidatedAllocationRuleTarget {
        &self.target
    }

    pub fn weights(&self) -> &[ValidatedAllocationRuleWeight] {
        &self.weights
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetAllocationRuleDecision {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub target: ValidatedAllocationRuleTarget,
    pub rule_id: Option<AllocationRuleId>,
    pub revision: AllocationRuleRevision,
    pub weights: Vec<ValidatedAllocationRuleWeight>,
    pub changed_fields: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetAllocationRuleResult {
    pub id: AllocationRuleId,
    pub revision: AllocationRuleRevision,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SetAllocationRuleError {
    InvalidActorId,
    InvalidHouseholdId,
    InvalidTargetId,
    InvalidMerchant,
    EmptyWeights,
    InvalidPoolId,
    NonPositiveWeight,
    DuplicatePoolId,
    InvalidRevision,
}

pub fn validate_set_allocation_rule(
    command: SetAllocationRuleCommand,
) -> Result<ValidatedSetAllocationRuleCommand, SetAllocationRuleError> {
    let actor_id = command.actor_id;
    let household_id = command.household_id;
    let target = match command.target {
        AllocationRuleTarget::Category(id) => ValidatedAllocationRuleTarget::Category(id),
        AllocationRuleTarget::ProductConcept(id) => {
            ValidatedAllocationRuleTarget::ProductConcept(id)
        }
        AllocationRuleTarget::ProductVariant(id) => {
            ValidatedAllocationRuleTarget::ProductVariant(id)
        }
        AllocationRuleTarget::Merchant(value) => {
            let normalized = normalize_merchant(&value);
            if normalized.is_empty() || normalized.len() > 240 {
                return Err(SetAllocationRuleError::InvalidMerchant);
            }
            ValidatedAllocationRuleTarget::Merchant(NormalizedMerchant::new(normalized))
        }
    };
    if command.weights.is_empty() {
        return Err(SetAllocationRuleError::EmptyWeights);
    }

    let mut pool_ids = BTreeSet::new();
    let mut weights = Vec::with_capacity(command.weights.len());
    for weight in command.weights {
        let pool_id = weight.pool_id;
        let value = AllocationRuleWeightValue::try_from(weight.weight)?;
        if !pool_ids.insert(pool_id.value()) {
            return Err(SetAllocationRuleError::DuplicatePoolId);
        }
        weights.push(ValidatedAllocationRuleWeight {
            pool_id,
            weight: value,
        });
    }

    Ok(ValidatedSetAllocationRuleCommand {
        actor_id,
        household_id,
        target,
        weights,
    })
}

pub fn decide_set_allocation_rule(
    command: ValidatedSetAllocationRuleCommand,
    existing: Option<(uuid::Uuid, u64)>,
) -> Result<SetAllocationRuleDecision, SetAllocationRuleError> {
    let (rule_id, revision) = match existing {
        Some((rule_id, current_revision)) => {
            let revision = AllocationRuleRevision::try_from(current_revision)?.next()?;
            (Some(AllocationRuleId(rule_id)), revision)
        }
        None => (None, AllocationRuleRevision(1)),
    };
    let target_field = command.target.changed_field();
    Ok(SetAllocationRuleDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        target: command.target,
        rule_id,
        revision,
        weights: command.weights,
        changed_fields: vec!["allocations".to_owned(), target_field.to_owned()],
    })
}

fn normalize_merchant(value: &str) -> String {
    value
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .to_lowercase()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn id(value: u128) -> uuid::Uuid {
        uuid::Uuid::from_u128(value)
    }

    fn command(
        target: AllocationRuleTarget,
        weights: Vec<AllocationRuleWeight>,
    ) -> SetAllocationRuleCommand {
        SetAllocationRuleCommand {
            actor_id: ActorId::from_uuid(id(1)),
            household_id: HouseholdId::from_uuid(id(2)),
            target,
            weights,
        }
    }

    fn target_category() -> AllocationRuleTarget {
        AllocationRuleTarget::Category(AllocationCategoryId::from_uuid(id(3)))
    }

    fn weights() -> Vec<AllocationRuleWeight> {
        vec![
            AllocationRuleWeight {
                pool_id: AllocationPoolId::from_uuid(id(4)),
                weight: 2,
            },
            AllocationRuleWeight {
                pool_id: AllocationPoolId::from_uuid(id(5)),
                weight: 3,
            },
        ]
    }

    #[test]
    fn normalizes_merchant_and_decides_revision_and_event_fields() {
        let validated = validate_set_allocation_rule(command(
            AllocationRuleTarget::Merchant("  FOO   BAR ".to_owned()),
            weights(),
        ))
        .expect("valid rule");
        assert_eq!(validated.target.merchant(), Some("foo bar"));
        let decision = decide_set_allocation_rule(validated, Some((uuid::Uuid::from_u128(6), 3)))
            .expect("decision");
        assert_eq!(decision.revision.value(), 4);
        assert_eq!(
            decision.rule_id.map(AllocationRuleId::value),
            Some(uuid::Uuid::from_u128(6))
        );
        assert_eq!(decision.changed_fields, ["allocations", "merchant"]);
    }

    #[test]
    fn rejects_invalid_merchant_and_weights() {
        assert_eq!(
            validate_set_allocation_rule(command(
                AllocationRuleTarget::Merchant("   ".to_owned()),
                weights(),
            )),
            Err(SetAllocationRuleError::InvalidMerchant)
        );
        let invalid_weights = vec![AllocationRuleWeight {
            pool_id: AllocationPoolId::from_uuid(id(4)),
            weight: 0,
        }];
        assert_eq!(
            validate_set_allocation_rule(command(target_category(), invalid_weights)),
            Err(SetAllocationRuleError::NonPositiveWeight)
        );
    }
}
