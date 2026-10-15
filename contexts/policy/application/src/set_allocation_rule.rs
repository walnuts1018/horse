use std::collections::BTreeSet;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocationRuleWeight {
    pub pool_id: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocationRuleTarget {
    pub category_id: Option<String>,
    pub merchant: Option<String>,
    pub product_concept_id: Option<String>,
    pub product_variant_id: Option<String>,
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
    pub actor_id: String,
    pub household_id: String,
    pub target: AllocationRuleTarget,
    pub weights: Vec<AllocationRuleWeight>,
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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetAllocationRuleResult {
    pub id: String,
    pub revision: u64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SetAllocationRuleError {
    InvalidActorId,
    InvalidHouseholdId,
    InvalidTarget,
    InvalidTargetId,
    InvalidMerchant,
    EmptyWeights,
    InvalidPoolId,
    NonPositiveWeight,
    DuplicatePoolId,
    InvalidRevision,
}

pub trait SetAllocationRuleOperations {
    type Error: From<SetAllocationRuleError>;

    async fn persist(
        &mut self,
        command: ValidatedSetAllocationRuleCommand,
    ) -> Result<SetAllocationRuleResult, Self::Error>;
}

pub async fn set_allocation_rule<O: SetAllocationRuleOperations>(
    operations: &mut O,
    command: SetAllocationRuleCommand,
) -> Result<SetAllocationRuleResult, O::Error> {
    operations.persist(validate_set_allocation_rule(command)?).await
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
    let selected_targets = [
        command.target.category_id.as_ref(),
        command.target.merchant.as_ref(),
        command.target.product_concept_id.as_ref(),
        command.target.product_variant_id.as_ref(),
    ]
    .into_iter()
    .flatten()
    .count();
    if selected_targets != 1 {
        return Err(SetAllocationRuleError::InvalidTarget);
    }
    for target_id in [
        command.target.category_id.as_deref(),
        command.target.product_concept_id.as_deref(),
        command.target.product_variant_id.as_deref(),
    ]
    .into_iter()
    .flatten()
    {
        if target_id.is_empty() {
            return Err(SetAllocationRuleError::InvalidTargetId);
        }
    }
    if let Some(merchant) = &command.target.merchant {
        let normalized = normalize_merchant(merchant);
        if normalized.is_empty() || normalized.len() > 240 {
            return Err(SetAllocationRuleError::InvalidMerchant);
        }
    }
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

    let mut target = command.target;
    if let Some(merchant) = target.merchant.take() {
        target.merchant = Some(normalize_merchant(&merchant));
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
    let target_field = if command.target.product_variant_id.is_some() {
        "productVariantId"
    } else if command.target.product_concept_id.is_some() {
        "productConceptId"
    } else if command.target.category_id.is_some() {
        "categoryId"
    } else {
        "merchant"
    };
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
        AllocationRuleTarget {
            category_id: Some("category".to_owned()),
            merchant: None,
            product_concept_id: None,
            product_variant_id: None,
        }
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
            AllocationRuleTarget {
                category_id: None,
                merchant: Some("  FOO   BAR ".to_owned()),
                product_concept_id: None,
                product_variant_id: None,
            },
            weights(),
        ))
        .expect("valid rule");
        assert_eq!(validated.target.merchant.as_deref(), Some("foo bar"));
        let decision =
            decide_set_allocation_rule(validated, Some(("rule".to_owned(), 3))).expect("decision");
        assert_eq!(decision.revision, 4);
        assert_eq!(decision.rule_id.as_deref(), Some("rule"));
        assert_eq!(decision.changed_fields, ["allocations", "merchant"]);
    }

    #[test]
    fn rejects_multiple_targets_and_invalid_weights() {
        assert_eq!(
            validate_set_allocation_rule(command(
                AllocationRuleTarget {
                    category_id: Some("category".to_owned()),
                    merchant: Some("shop".to_owned()),
                    product_concept_id: None,
                    product_variant_id: None,
                },
                weights(),
            )),
            Err(SetAllocationRuleError::InvalidTarget)
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
