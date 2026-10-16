use crate::ports::{CreateHouseholdPortError, CreatedHousehold, HouseholdStore};
use horse_household_change::UiChangeDescriptor;
pub use household_domain::create_household::{
    ActorId, CreateHouseholdCommand, CreateHouseholdDecision, CreateHouseholdError,
    decide_create_household,
};

pub async fn create_household(
    store: &dyn HouseholdStore,
    command: CreateHouseholdCommand,
) -> Result<CreatedHousehold, CreateHouseholdPortError> {
    let decision =
        decide_create_household(command).map_err(CreateHouseholdPortError::from_workflow)?;
    store.persist(decision).await
}

pub fn household_created_change() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "household.created",
        "household",
        std::iter::empty::<String>(),
    )
}
