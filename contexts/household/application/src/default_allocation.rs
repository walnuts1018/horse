pub use household_domain::default_allocation::decide_set_default_allocation;
pub use household_domain::default_allocation::*;

pub trait SetDefaultAllocationOperations {
    type Error: From<SetDefaultAllocationError>;
    async fn persist(
        &mut self,
        command: ValidatedDefaultAllocation,
    ) -> Result<SetDefaultAllocationResult, Self::Error>;
}

pub async fn set_default_allocation<O: SetDefaultAllocationOperations>(
    operations: &mut O,
    command: SetDefaultAllocationCommand,
) -> Result<SetDefaultAllocationResult, O::Error> {
    operations
        .persist(household_domain::default_allocation::validate_default_allocation(command)?)
        .await
}
