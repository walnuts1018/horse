use std::{future::Future, pin::Pin};

use crate::{
    create_household::CreateHouseholdCommand,
    ports::{CreateHouseholdPortError, CreatedHousehold},
};

pub trait CreateHouseholdHandler: Send + Sync {
    fn create<'a>(
        &'a self,
        command: CreateHouseholdCommand,
    ) -> Pin<Box<dyn Future<Output = Result<CreatedHousehold, CreateHouseholdPortError>> + Send + 'a>>;
}
