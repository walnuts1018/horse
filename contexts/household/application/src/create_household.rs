use horse_household_change::UiChangeDescriptor;
pub use household_domain::create_household::{
    ActorId, CreateHouseholdCommand, CreateHouseholdDecision, CreateHouseholdError,
    decide_create_household,
};

pub trait CreateHouseholdOperations {
    type Error: From<CreateHouseholdError>;

    async fn persist(&mut self, decision: CreateHouseholdDecision) -> Result<String, Self::Error>;
}

pub async fn create_household<O: CreateHouseholdOperations>(
    operations: &mut O,
    command: CreateHouseholdCommand,
) -> Result<String, O::Error> {
    let decision = decide_create_household(command)?;
    operations.persist(decision).await
}

pub fn household_created_change() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "household.created",
        "household",
        std::iter::empty::<String>(),
    )
}
