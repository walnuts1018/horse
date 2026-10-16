pub use household_domain::default_allocation::decide_set_default_allocation;
pub use household_domain::default_allocation::*;
use std::{future::Future, pin::Pin};

pub trait DefaultAllocationHandler: Send + Sync {
    fn set<'a>(
        &'a self,
        command: SetDefaultAllocationCommand,
    ) -> Pin<
        Box<
            dyn Future<Output = Result<SetDefaultAllocationResult, DefaultAllocationFailure>>
                + Send
                + 'a,
        >,
    >;
}

#[derive(Debug)]
pub enum DefaultAllocationFailure {
    Decision,
    NotFound,
    Infrastructure(String),
}

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
