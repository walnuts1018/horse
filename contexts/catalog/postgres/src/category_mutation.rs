use catalog_application::category::{
    self as application_category, CategoryMutation, CategoryMutationError, CreateCategoryCommand,
    SetCategoryArchivedCommand, UpdateCategoryNameCommand,
};
use catalog_domain::category::CategoryView;

use crate::{PgPool, category};

pub struct PostgresCategoryMutation {
    pool: PgPool,
}

impl PostgresCategoryMutation {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl CategoryMutation for PostgresCategoryMutation {
    fn create<'a>(
        &'a self,
        command: CreateCategoryCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<CategoryView, CategoryMutationError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            category::with_transaction(&self.pool, move |conn| {
                Box::pin(async move {
                    let mut operations = category::PostgresCreateCategoryOperations::new(conn);
                    application_category::create_category(&mut operations, command).await
                })
            })
            .await
            .map_err(map_error)
        })
    }

    fn update_name<'a>(
        &'a self,
        command: UpdateCategoryNameCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<CategoryView, CategoryMutationError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            category::with_transaction(&self.pool, move |conn| {
                Box::pin(async move {
                    let mut operations = category::PostgresUpdateCategoryNameOperations::new(conn);
                    application_category::update_category_name(&mut operations, command).await
                })
            })
            .await
            .map_err(map_error)
        })
    }

    fn set_archived<'a>(
        &'a self,
        command: SetCategoryArchivedCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<CategoryView, CategoryMutationError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            category::with_transaction(&self.pool, move |conn| {
                Box::pin(async move {
                    let mut operations = category::PostgresSetCategoryArchivedOperations::new(conn);
                    application_category::set_category_archived(&mut operations, command).await
                })
            })
            .await
            .map_err(map_error)
        })
    }
}

fn map_error(error: category::CategoryWorkflowError) -> CategoryMutationError {
    use category::CategoryWorkflowError as E;
    match error {
        E::InvalidActorId
        | E::InvalidHouseholdId
        | E::InvalidCategoryId
        | E::InvalidParentId
        | E::InvalidName => CategoryMutationError::BadInput,
        E::HouseholdNotFound | E::CategoryNotFound | E::ParentNotFound | E::NotAMember => {
            CategoryMutationError::NotFound
        }
        E::InvariantViolation(message) => CategoryMutationError::InvariantViolation(message),
        E::Pool(error) => CategoryMutationError::Infrastructure(format!("{error:?}")),
        E::Database(error) => CategoryMutationError::Infrastructure(format!("{error:?}")),
        E::CorruptStoredData => {
            CategoryMutationError::Infrastructure("corrupt stored category".into())
        }
    }
}
