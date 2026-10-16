use horse_household_change::UiChangeDescriptor;
use std::collections::BTreeSet;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocationRuleWeight {
    pub pool_id: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AllocationRuleTarget {
    Category(String),
    Merchant(String),
    ProductConcept(String),
    ProductVariant(String),
}

impl AllocationRuleTarget {
    pub fn category_id(&self) -> Option<&str> {
        match self {
            Self::Category(id) => Some(id),
            _ => None,
        }
    }

    pub fn merchant(&self) -> Option<&str> {
        match self {
            Self::Merchant(value) => Some(value),
            _ => None,
        }
    }

    pub fn product_concept_id(&self) -> Option<&str> {
        match self {
            Self::ProductConcept(id) => Some(id),
            _ => None,
        }
    }

    pub fn product_variant_id(&self) -> Option<&str> {
        match self {
            Self::ProductVariant(id) => Some(id),
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
    pub actor_id: String,
    pub household_id: String,
    pub target: AllocationRuleTarget,
    pub weights: Vec<AllocationRuleWeight>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ValidatedSetAllocationRuleCommand {
    actor_id: String,
    household_id: String,
    target: AllocationRuleTarget,
    weights: Vec<AllocationRuleWeight>,
}

impl ValidatedSetAllocationRuleCommand {
    pub fn actor_id(&self) -> &str {
        &self.actor_id
    }

    pub fn household_id(&self) -> &str {
        &self.household_id
    }

    pub fn target(&self) -> &AllocationRuleTarget {
        &self.target
    }

    pub fn weights(&self) -> &[AllocationRuleWeight] {
        &self.weights
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetAllocationRuleDecision {
    pub actor_id: String,
    pub household_id: String,
    pub target: AllocationRuleTarget,
    pub rule_id: Option<String>,
    pub revision: u64,
    pub weights: Vec<AllocationRuleWeight>,
    pub changed_fields: Vec<String>,
}

impl SetAllocationRuleDecision {
    pub fn ui_change(&self) -> UiChangeDescriptor {
        UiChangeDescriptor::new(
            "allocation.rule.updated",
            "allocation_rule",
            self.changed_fields.clone(),
        )
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetAllocationRuleResult {
    pub id: String,
    pub revision: u64,
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
    if command.actor_id.is_empty() {
        return Err(SetAllocationRuleError::InvalidActorId);
    }
    if command.household_id.is_empty() {
        return Err(SetAllocationRuleError::InvalidHouseholdId);
    }
    let target = match command.target {
        AllocationRuleTarget::Category(id) if !id.is_empty() => AllocationRuleTarget::Category(id),
        AllocationRuleTarget::ProductConcept(id) if !id.is_empty() => {
            AllocationRuleTarget::ProductConcept(id)
        }
        AllocationRuleTarget::ProductVariant(id) if !id.is_empty() => {
            AllocationRuleTarget::ProductVariant(id)
        }
        AllocationRuleTarget::Merchant(value) => {
            let normalized = normalize_merchant(&value);
            if normalized.is_empty() || normalized.len() > 240 {
                return Err(SetAllocationRuleError::InvalidMerchant);
            }
            AllocationRuleTarget::Merchant(normalized)
        }
        AllocationRuleTarget::Category(_)
        | AllocationRuleTarget::ProductConcept(_)
        | AllocationRuleTarget::ProductVariant(_) => {
            return Err(SetAllocationRuleError::InvalidTargetId);
        }
    };
    if command.weights.is_empty() {
        return Err(SetAllocationRuleError::EmptyWeights);
    }

    let mut pool_ids = BTreeSet::new();
    for weight in &command.weights {
        if weight.pool_id.is_empty() {
            return Err(SetAllocationRuleError::InvalidPoolId);
        }
        if weight.weight <= 0 {
            return Err(SetAllocationRuleError::NonPositiveWeight);
        }
        if !pool_ids.insert(weight.pool_id.as_str()) {
            return Err(SetAllocationRuleError::DuplicatePoolId);
        }
    }

    Ok(ValidatedSetAllocationRuleCommand {
        actor_id: command.actor_id,
        household_id: command.household_id,
        target,
        weights: command.weights,
    })
}

pub fn decide_set_allocation_rule(
    command: ValidatedSetAllocationRuleCommand,
    existing: Option<(String, u64)>,
) -> Result<SetAllocationRuleDecision, SetAllocationRuleError> {
    let (rule_id, revision) = match existing {
        Some((rule_id, current_revision)) if !rule_id.is_empty() => (
            Some(rule_id),
            current_revision
                .checked_add(1)
                .ok_or(SetAllocationRuleError::InvalidRevision)?,
        ),
        Some(_) => return Err(SetAllocationRuleError::InvalidTargetId),
        None => (None, 1),
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

    fn command(
        target: AllocationRuleTarget,
        weights: Vec<AllocationRuleWeight>,
    ) -> SetAllocationRuleCommand {
        SetAllocationRuleCommand {
            actor_id: "actor".to_owned(),
            household_id: "household".to_owned(),
            target,
            weights,
        }
    }

    fn target_category() -> AllocationRuleTarget {
        AllocationRuleTarget::Category("category".to_owned())
    }

    fn weights() -> Vec<AllocationRuleWeight> {
        vec![
            AllocationRuleWeight {
                pool_id: "pool-a".to_owned(),
                weight: 2,
            },
            AllocationRuleWeight {
                pool_id: "pool-b".to_owned(),
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
        let decision =
            decide_set_allocation_rule(validated, Some(("rule".to_owned(), 3))).expect("decision");
        assert_eq!(decision.revision, 4);
        assert_eq!(decision.rule_id.as_deref(), Some("rule"));
        assert_eq!(decision.changed_fields, ["allocations", "merchant"]);
    }

    #[test]
    fn rejects_empty_target_ids_and_invalid_weights() {
        assert_eq!(
            validate_set_allocation_rule(command(
                AllocationRuleTarget::Category(String::new()),
                weights(),
            )),
            Err(SetAllocationRuleError::InvalidTargetId)
        );
        assert_eq!(
            validate_set_allocation_rule(command(
                AllocationRuleTarget::Merchant("   ".to_owned()),
                weights(),
            )),
            Err(SetAllocationRuleError::InvalidMerchant)
        );
        let invalid_weights = vec![AllocationRuleWeight {
            pool_id: "pool".to_owned(),
            weight: 0,
        }];
        assert_eq!(
            validate_set_allocation_rule(command(target_category(), invalid_weights)),
            Err(SetAllocationRuleError::NonPositiveWeight)
        );
    }
}
