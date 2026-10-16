use std::collections::BTreeSet;
use uuid::Uuid;

macro_rules! id_type {
    ($name:ident) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
        pub struct $name(Uuid);

        impl $name {
            pub const fn new(value: Uuid) -> Self {
                Self(value)
            }
            pub const fn value(self) -> Uuid {
                self.0
            }
        }
    };
}

id_type!(FundingRuleActorId);
id_type!(FundingRuleHouseholdId);
id_type!(FundingRulePoolId);
id_type!(FundingRuleMemberId);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PoolFundingKind {
    Personal,
    Shared,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SharedSettlementPolicy {
    BalanceToStandard,
    ReimbursePersonal,
}

impl SharedSettlementPolicy {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::BalanceToStandard => "BALANCE_TO_STANDARD",
            Self::ReimbursePersonal => "REIMBURSE_PERSONAL",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PoolFundingShareInput {
    pub member_id: FundingRuleMemberId,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PoolFundingConfiguration {
    Personal {
        owner_member_id: FundingRuleMemberId,
    },
    Shared {
        funding_shares: Vec<PoolFundingShareInput>,
        settlement_policy: SharedSettlementPolicy,
    },
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetPoolFundingRuleCommand {
    pub actor_id: FundingRuleActorId,
    pub household_id: FundingRuleHouseholdId,
    pub pool_id: FundingRulePoolId,
    pub expected_version: i32,
    pub configuration: PoolFundingConfiguration,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PoolFundingRuleContext {
    pub kind: PoolFundingKind,
    pub current_version: i32,
    pub active_member_ids: BTreeSet<FundingRuleMemberId>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SetPoolFundingRuleError {
    InvalidConfiguration,
    VersionConflict,
    VersionOutOfRange,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PoolFundingShare {
    pub member_id: FundingRuleMemberId,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetPoolFundingRuleDecision {
    pub actor_id: FundingRuleActorId,
    pub household_id: FundingRuleHouseholdId,
    pub pool_id: FundingRulePoolId,
    pub version: i32,
    pub configuration: PoolFundingConfiguration,
    pub shares: Vec<PoolFundingShare>,
}

pub fn decide_set_pool_funding_rule(
    command: SetPoolFundingRuleCommand,
    context: PoolFundingRuleContext,
) -> Result<SetPoolFundingRuleDecision, SetPoolFundingRuleError> {
    if command.expected_version < 0 || context.current_version < 0 {
        return Err(SetPoolFundingRuleError::InvalidConfiguration);
    }
    if command.expected_version != context.current_version {
        return Err(SetPoolFundingRuleError::VersionConflict);
    }
    let version = context
        .current_version
        .checked_add(1)
        .ok_or(SetPoolFundingRuleError::VersionOutOfRange)?;
    let mut shares = Vec::new();
    match (&command.configuration, context.kind) {
        (PoolFundingConfiguration::Personal { owner_member_id }, PoolFundingKind::Personal) => {
            if !context.active_member_ids.contains(owner_member_id) {
                return Err(SetPoolFundingRuleError::InvalidConfiguration);
            }
        }
        (PoolFundingConfiguration::Shared { funding_shares, .. }, PoolFundingKind::Shared) => {
            if funding_shares.is_empty() {
                return Err(SetPoolFundingRuleError::InvalidConfiguration);
            }
            let mut seen = BTreeSet::new();
            let mut total = 0_i64;
            for share in funding_shares {
                if share.weight <= 0
                    || !seen.insert(share.member_id)
                    || !context.active_member_ids.contains(&share.member_id)
                {
                    return Err(SetPoolFundingRuleError::InvalidConfiguration);
                }
                total = total
                    .checked_add(share.weight)
                    .ok_or(SetPoolFundingRuleError::InvalidConfiguration)?;
                shares.push(PoolFundingShare {
                    member_id: share.member_id,
                    weight: share.weight,
                });
            }
            shares.sort_by_key(|share| share.member_id);
        }
        _ => return Err(SetPoolFundingRuleError::InvalidConfiguration),
    }
    Ok(SetPoolFundingRuleDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        pool_id: command.pool_id,
        version,
        configuration: command.configuration,
        shares,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ids() -> (
        FundingRuleActorId,
        FundingRuleHouseholdId,
        FundingRulePoolId,
        Uuid,
        Uuid,
    ) {
        (
            FundingRuleActorId::new(Uuid::from_u128(1)),
            FundingRuleHouseholdId::new(Uuid::from_u128(2)),
            FundingRulePoolId::new(Uuid::from_u128(3)),
            Uuid::from_u128(4),
            Uuid::from_u128(5),
        )
    }

    #[test]
    fn shared_funding_rule_requires_unique_active_members_and_positive_weights() {
        let (actor_id, household_id, pool_id, first_member_id, second_member_id) = ids();
        let context = PoolFundingRuleContext {
            kind: PoolFundingKind::Shared,
            current_version: 4,
            active_member_ids: [
                FundingRuleMemberId::new(first_member_id),
                FundingRuleMemberId::new(second_member_id),
            ]
            .into(),
        };
        let command = SetPoolFundingRuleCommand {
            actor_id,
            household_id,
            pool_id,
            expected_version: 4,
            configuration: PoolFundingConfiguration::Shared {
                funding_shares: vec![
                    PoolFundingShareInput {
                        member_id: FundingRuleMemberId::new(second_member_id),
                        weight: 1,
                    },
                    PoolFundingShareInput {
                        member_id: FundingRuleMemberId::new(first_member_id),
                        weight: 3,
                    },
                ],
                settlement_policy: SharedSettlementPolicy::BalanceToStandard,
            },
        };

        let decision = decide_set_pool_funding_rule(command.clone(), context.clone())
            .expect("active members with positive weights are valid");
        assert_eq!(decision.version, 5);
        assert_eq!(
            decision
                .shares
                .iter()
                .map(|share| share.member_id.value())
                .collect::<Vec<_>>(),
            vec![first_member_id, second_member_id]
        );

        let mut duplicate = command.clone();
        if let PoolFundingConfiguration::Shared { funding_shares, .. } =
            &mut duplicate.configuration
        {
            funding_shares[1].member_id = funding_shares[0].member_id;
        }
        assert_eq!(
            decide_set_pool_funding_rule(duplicate, context.clone()),
            Err(SetPoolFundingRuleError::InvalidConfiguration)
        );

        let mut non_positive = command;
        if let PoolFundingConfiguration::Shared { funding_shares, .. } =
            &mut non_positive.configuration
        {
            funding_shares[0].weight = 0;
        }
        assert_eq!(
            decide_set_pool_funding_rule(non_positive, context),
            Err(SetPoolFundingRuleError::InvalidConfiguration)
        );
    }

    #[test]
    fn funding_rule_rejects_stale_versions_and_configuration_for_wrong_pool_kind() {
        let (actor_id, household_id, pool_id, member_id, _) = ids();
        let command = SetPoolFundingRuleCommand {
            actor_id,
            household_id,
            pool_id,
            expected_version: 1,
            configuration: PoolFundingConfiguration::Personal {
                owner_member_id: FundingRuleMemberId::new(member_id),
            },
        };
        let context = PoolFundingRuleContext {
            kind: PoolFundingKind::Shared,
            current_version: 2,
            active_member_ids: [FundingRuleMemberId::new(member_id)].into(),
        };
        assert_eq!(
            decide_set_pool_funding_rule(command.clone(), context.clone()),
            Err(SetPoolFundingRuleError::VersionConflict)
        );

        let mut current_command = command;
        current_command.expected_version = 2;
        assert_eq!(
            decide_set_pool_funding_rule(current_command, context),
            Err(SetPoolFundingRuleError::InvalidConfiguration)
        );
    }
}
