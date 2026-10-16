use horse_household_change::UiChangeDescriptor;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DefaultAllocationWeight {
    pub pool_id: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetDefaultAllocationCommand {
    pub actor_id: String,
    pub household_id: String,
    pub weights: Vec<DefaultAllocationWeight>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ValidatedDefaultAllocation {
    actor_id: String,
    household_id: String,
    weights: Vec<DefaultAllocationWeight>,
}

impl ValidatedDefaultAllocation {
    pub fn actor_id(&self) -> &str {
        &self.actor_id
    }

    pub fn household_id(&self) -> &str {
        &self.household_id
    }

    pub fn weights(&self) -> &[DefaultAllocationWeight] {
        &self.weights
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct DefaultAllocationState {
    pub revision: u64,
    pub version: u64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetDefaultAllocationDecision {
    pub actor_id: String,
    pub household_id: String,
    pub revision: u64,
    pub version: u64,
    pub weights: Vec<DefaultAllocationWeight>,
    pub changed_fields: Vec<String>,
}

impl SetDefaultAllocationDecision {
    pub fn ui_change(&self) -> UiChangeDescriptor {
        UiChangeDescriptor::new(
            "allocation.default.updated",
            "household",
            self.changed_fields.clone(),
        )
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct SetDefaultAllocationResult {
    pub revision: u64,
    pub version: u64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SetDefaultAllocationError {
    InvalidActorId,
    InvalidHouseholdId,
    EmptyWeights,
    InvalidPoolId,
    NonPositiveWeight,
    DuplicatePoolId,
    InvalidVersion,
    InvalidRevision,
}

pub fn validate_default_allocation(
    command: SetDefaultAllocationCommand,
) -> Result<ValidatedDefaultAllocation, SetDefaultAllocationError> {
    if command.actor_id.is_empty() {
        return Err(SetDefaultAllocationError::InvalidActorId);
    }
    if command.household_id.is_empty() {
        return Err(SetDefaultAllocationError::InvalidHouseholdId);
    }
    if command.weights.is_empty() {
        return Err(SetDefaultAllocationError::EmptyWeights);
    }

    let mut pool_ids = std::collections::BTreeSet::new();
    for weight in &command.weights {
        if weight.pool_id.is_empty() {
            return Err(SetDefaultAllocationError::InvalidPoolId);
        }
        if weight.weight <= 0 {
            return Err(SetDefaultAllocationError::NonPositiveWeight);
        }
        if !pool_ids.insert(weight.pool_id.as_str()) {
            return Err(SetDefaultAllocationError::DuplicatePoolId);
        }
    }

    Ok(ValidatedDefaultAllocation {
        actor_id: command.actor_id,
        household_id: command.household_id,
        weights: command.weights,
    })
}

pub fn decide_set_default_allocation(
    command: ValidatedDefaultAllocation,
    current: DefaultAllocationState,
) -> Result<SetDefaultAllocationDecision, SetDefaultAllocationError> {
    let version = current
        .version
        .checked_add(1)
        .ok_or(SetDefaultAllocationError::InvalidVersion)?;
    let revision = current
        .revision
        .checked_add(1)
        .ok_or(SetDefaultAllocationError::InvalidRevision)?;

    Ok(SetDefaultAllocationDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        revision,
        version,
        weights: command.weights,
        changed_fields: vec!["defaultAllocation".to_owned()],
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn command(weights: Vec<DefaultAllocationWeight>) -> SetDefaultAllocationCommand {
        SetDefaultAllocationCommand {
            actor_id: "actor".to_owned(),
            household_id: "household".to_owned(),
            weights,
        }
    }

    fn weight(pool_id: &str, weight: i64) -> DefaultAllocationWeight {
        DefaultAllocationWeight {
            pool_id: pool_id.to_owned(),
            weight,
        }
    }

    #[test]
    fn validates_weights_and_advances_versions() {
        let validated =
            validate_default_allocation(command(vec![weight("pool-a", 2), weight("pool-b", 3)]))
                .expect("valid weights");
        let decision = decide_set_default_allocation(
            validated,
            DefaultAllocationState {
                revision: 7,
                version: 4,
            },
        )
        .expect("decision");

        assert_eq!(decision.revision, 8);
        assert_eq!(decision.version, 5);
        assert_eq!(decision.changed_fields, ["defaultAllocation"]);
    }

    #[test]
    fn rejects_empty_duplicate_and_non_positive_weights() {
        assert_eq!(
            validate_default_allocation(command(vec![])),
            Err(SetDefaultAllocationError::EmptyWeights)
        );
        assert_eq!(
            validate_default_allocation(command(vec![weight("pool-a", 1), weight("pool-a", 2)])),
            Err(SetDefaultAllocationError::DuplicatePoolId)
        );
        assert_eq!(
            validate_default_allocation(command(vec![weight("pool-a", 0)])),
            Err(SetDefaultAllocationError::NonPositiveWeight)
        );
    }
}
