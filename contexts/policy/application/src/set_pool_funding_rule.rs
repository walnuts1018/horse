pub use policy_domain::set_pool_funding_rule::*;
use std::{future::Future, pin::Pin};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedPoolFundingRule {
    pub pool_id: FundingRulePoolId,
    pub version: i32,
}

pub trait SetPoolFundingRuleHandler: Send + Sync {
    fn set<'a>(
        &'a self,
        command: SetPoolFundingRuleCommand,
    ) -> Pin<
        Box<
            dyn Future<Output = Result<PersistedPoolFundingRule, super::WorkflowFailure>>
                + Send
                + 'a,
        >,
    >;
}

pub trait SetPoolFundingRuleOperations {
    type Error;

    fn classify_error(error: Self::Error) -> super::WorkflowFailure;

    async fn load_context(
        &mut self,
        actor_id: super::ActorId,
        household_id: super::HouseholdId,
        pool_id: FundingRulePoolId,
    ) -> Result<PoolFundingRuleContext, Self::Error>;

    async fn persist(
        &mut self,
        decision: SetPoolFundingRuleDecision,
    ) -> Result<PersistedPoolFundingRule, Self::Error>;
}

pub async fn set_pool_funding_rule<O: SetPoolFundingRuleOperations>(
    operations: &mut O,
    command: SetPoolFundingRuleCommand,
) -> Result<PersistedPoolFundingRule, super::WorkflowFailure> {
    let context = operations
        .load_context(command.actor_id, command.household_id, command.pool_id)
        .await
        .map_err(O::classify_error)?;
    let decision =
        policy_domain::set_pool_funding_rule::decide_set_pool_funding_rule(command, context)
            .map_err(|error| match error {
                SetPoolFundingRuleError::VersionConflict => super::WorkflowFailure::Conflict,
                SetPoolFundingRuleError::VersionOutOfRange => {
                    super::WorkflowFailure::Infrastructure(
                        "funding rule version is out of range".to_owned(),
                    )
                }
                SetPoolFundingRuleError::InvalidConfiguration => super::WorkflowFailure::Decision,
            })?;
    operations
        .persist(decision)
        .await
        .map_err(O::classify_error)
}
