macro_rules! default_allocation_id {
    ($name:ident) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
        pub struct $name(uuid::Uuid);

        impl $name {
            fn parse(value: String) -> Result<Self, uuid::Error> {
                uuid::Uuid::parse_str(&value).map(Self)
            }
            pub const fn value(self) -> uuid::Uuid {
                self.0
            }
        }
    };
}

default_allocation_id!(DefaultAllocationActorId);
default_allocation_id!(DefaultAllocationHouseholdId);
default_allocation_id!(DefaultAllocationPoolId);

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct DefaultAllocationWeightValue(i64);

impl DefaultAllocationWeightValue {
    pub const fn value(self) -> i64 {
        self.0
    }
}

impl TryFrom<i64> for DefaultAllocationWeightValue {
    type Error = SetDefaultAllocationError;

    fn try_from(value: i64) -> Result<Self, Self::Error> {
        if value > 0 {
            Ok(Self(value))
        } else {
            Err(SetDefaultAllocationError::NonPositiveWeight)
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct DefaultAllocationRevision(u64);

impl DefaultAllocationRevision {
    pub const fn value(self) -> u64 {
        self.0
    }

    fn next(self, error: SetDefaultAllocationError) -> Result<Self, SetDefaultAllocationError> {
        self.0.checked_add(1).map(Self).ok_or(error)
    }
}

impl TryFrom<u64> for DefaultAllocationRevision {
    type Error = SetDefaultAllocationError;

    fn try_from(value: u64) -> Result<Self, Self::Error> {
        if value > 0 {
            Ok(Self(value))
        } else {
            Err(SetDefaultAllocationError::InvalidRevision)
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct DefaultAllocationVersion(u64);

impl DefaultAllocationVersion {
    pub const fn value(self) -> u64 {
        self.0
    }

    fn next(self) -> Result<Self, SetDefaultAllocationError> {
        self.0
            .checked_add(1)
            .map(Self)
            .ok_or(SetDefaultAllocationError::InvalidVersion)
    }
}

impl TryFrom<u64> for DefaultAllocationVersion {
    type Error = SetDefaultAllocationError;

    fn try_from(value: u64) -> Result<Self, Self::Error> {
        if value > 0 {
            Ok(Self(value))
        } else {
            Err(SetDefaultAllocationError::InvalidVersion)
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DefaultAllocationWeight {
    pub pool_id: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ValidatedDefaultAllocationWeight {
    pub pool_id: DefaultAllocationPoolId,
    pub weight: DefaultAllocationWeightValue,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetDefaultAllocationCommand {
    pub actor_id: String,
    pub household_id: String,
    pub weights: Vec<DefaultAllocationWeight>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ValidatedDefaultAllocation {
    actor_id: DefaultAllocationActorId,
    household_id: DefaultAllocationHouseholdId,
    weights: Vec<ValidatedDefaultAllocationWeight>,
}

impl ValidatedDefaultAllocation {
    pub fn actor_id(&self) -> uuid::Uuid {
        self.actor_id.value()
    }

    pub fn household_id(&self) -> uuid::Uuid {
        self.household_id.value()
    }

    pub fn weights(&self) -> &[ValidatedDefaultAllocationWeight] {
        &self.weights
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct DefaultAllocationState {
    pub revision: DefaultAllocationRevision,
    pub version: DefaultAllocationVersion,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetDefaultAllocationDecision {
    pub actor_id: DefaultAllocationActorId,
    pub household_id: DefaultAllocationHouseholdId,
    pub revision: DefaultAllocationRevision,
    pub version: DefaultAllocationVersion,
    pub weights: Vec<ValidatedDefaultAllocationWeight>,
    pub changed_fields: Vec<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct SetDefaultAllocationResult {
    pub revision: DefaultAllocationRevision,
    pub version: DefaultAllocationVersion,
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
    let actor_id = DefaultAllocationActorId::parse(command.actor_id)
        .map_err(|_| SetDefaultAllocationError::InvalidActorId)?;
    let household_id = DefaultAllocationHouseholdId::parse(command.household_id)
        .map_err(|_| SetDefaultAllocationError::InvalidHouseholdId)?;
    if command.weights.is_empty() {
        return Err(SetDefaultAllocationError::EmptyWeights);
    }

    let mut pool_ids = std::collections::BTreeSet::new();
    let mut weights = Vec::with_capacity(command.weights.len());
    for weight in command.weights {
        let pool_id = DefaultAllocationPoolId::parse(weight.pool_id)
            .map_err(|_| SetDefaultAllocationError::InvalidPoolId)?;
        let value = DefaultAllocationWeightValue::try_from(weight.weight)?;
        if !pool_ids.insert(pool_id.value()) {
            return Err(SetDefaultAllocationError::DuplicatePoolId);
        }
        weights.push(ValidatedDefaultAllocationWeight {
            pool_id,
            weight: value,
        });
    }

    Ok(ValidatedDefaultAllocation {
        actor_id,
        household_id,
        weights,
    })
}

pub fn decide_set_default_allocation(
    command: ValidatedDefaultAllocation,
    current: DefaultAllocationState,
) -> Result<SetDefaultAllocationDecision, SetDefaultAllocationError> {
    let version = current.version.next()?;
    let revision = current
        .revision
        .next(SetDefaultAllocationError::InvalidRevision)?;

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

    fn id(value: u128) -> String {
        uuid::Uuid::from_u128(value).to_string()
    }

    fn command(weights: Vec<DefaultAllocationWeight>) -> SetDefaultAllocationCommand {
        SetDefaultAllocationCommand {
            actor_id: id(1),
            household_id: id(2),
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
            validate_default_allocation(command(vec![weight(&id(3), 2), weight(&id(4), 3)]))
                .expect("valid weights");
        let decision = decide_set_default_allocation(
            validated,
            DefaultAllocationState {
                revision: DefaultAllocationRevision(7),
                version: DefaultAllocationVersion(4),
            },
        )
        .expect("decision");

        assert_eq!(decision.revision.value(), 8);
        assert_eq!(decision.version.value(), 5);
        assert_eq!(decision.changed_fields, ["defaultAllocation"]);
    }

    #[test]
    fn rejects_empty_duplicate_and_non_positive_weights() {
        assert_eq!(
            validate_default_allocation(command(vec![])),
            Err(SetDefaultAllocationError::EmptyWeights)
        );
        assert_eq!(
            validate_default_allocation(command(vec![weight(&id(3), 1), weight(&id(3), 2)])),
            Err(SetDefaultAllocationError::DuplicatePoolId)
        );
        assert_eq!(
            validate_default_allocation(command(vec![weight(&id(3), 0)])),
            Err(SetDefaultAllocationError::NonPositiveWeight)
        );
    }
}
