use household_application::{
    create_household::CreateHouseholdCommand,
    create_household_handler::CreateHouseholdHandler,
    ports::{CreateHouseholdPortError, CreatedHousehold, HouseholdStore},
};

use crate::{PgPool, create_household::PostgresHouseholds};

pub struct PostgresCreateHouseholdHandler {
    store: PostgresHouseholds,
}

impl PostgresCreateHouseholdHandler {
    pub fn new(pool: PgPool) -> Self {
        Self {
            store: PostgresHouseholds::new(pool),
        }
    }
}

impl CreateHouseholdHandler for PostgresCreateHouseholdHandler {
    fn create<'a>(
        &'a self,
        command: CreateHouseholdCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<CreatedHousehold, CreateHouseholdPortError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move { self.store.create_household(command).await })
    }
}
