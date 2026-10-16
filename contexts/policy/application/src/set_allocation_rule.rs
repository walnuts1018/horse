pub use policy_domain::set_allocation_rule::decide_set_allocation_rule;
pub use policy_domain::*;
use std::{future::Future, pin::Pin};

pub trait SetAllocationRuleHandler: Send + Sync {
    fn handle<'a>(
        &'a self,
        command: SetAllocationRuleCommand,
    ) -> Pin<
        Box<
            dyn Future<Output = Result<SetAllocationRuleResult, super::WorkflowFailure>>
                + Send
                + 'a,
        >,
    >;
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
    operations
        .persist(policy_domain::set_allocation_rule::validate_set_allocation_rule(command)?)
        .await
}
