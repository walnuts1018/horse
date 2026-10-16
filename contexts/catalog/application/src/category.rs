pub use catalog_domain::category::{
    ActorId, CategoryError, CategoryEvent, CategoryId, CategoryName, CategoryStatus, CategoryView,
    CreateCategoryCommand, CreateCategoryDecision, HouseholdId, LoadedCategory,
    SetCategoryArchivedCommand, SetCategoryArchivedDecision, UpdateCategoryNameCommand,
    UpdateCategoryNameDecision,
};
use catalog_domain::category::{
    decide_create_category, decide_set_category_archived, decide_update_category_name,
};
use std::{future::Future, pin::Pin};

pub trait CreateCategoryOperations {
    type Error: From<CategoryError>;

    async fn load_household(&mut self, household_id: &HouseholdId) -> Result<(), Self::Error>;
    async fn require_member(
        &mut self,
        household_id: &HouseholdId,
        actor_id: &ActorId,
    ) -> Result<(), Self::Error>;
    async fn load_parent(
        &mut self,
        household_id: &HouseholdId,
        parent_id: &CategoryId,
    ) -> Result<(), Self::Error>;
    async fn persist(
        &mut self,
        decision: CreateCategoryDecision,
    ) -> Result<CategoryView, Self::Error>;
}

pub async fn create_category<O: CreateCategoryOperations>(
    operations: &mut O,
    command: CreateCategoryCommand,
) -> Result<CategoryView, O::Error> {
    let decision = decide_create_category(command)?;
    operations.load_household(&decision.household_id).await?;
    operations
        .require_member(&decision.household_id, &decision.actor_id)
        .await?;
    if let Some(parent_id) = &decision.parent_id {
        operations
            .load_parent(&decision.household_id, parent_id)
            .await?;
    }
    operations.persist(decision).await
}

pub trait UpdateCategoryNameOperations {
    type Error: From<CategoryError>;

    async fn load_category(
        &mut self,
        category_id: &CategoryId,
    ) -> Result<LoadedCategory, Self::Error>;
    async fn require_member(
        &mut self,
        household_id: &HouseholdId,
        actor_id: &ActorId,
    ) -> Result<(), Self::Error>;
    async fn persist(
        &mut self,
        category: LoadedCategory,
        decision: UpdateCategoryNameDecision,
    ) -> Result<CategoryView, Self::Error>;
}

pub async fn update_category_name<O: UpdateCategoryNameOperations>(
    operations: &mut O,
    command: UpdateCategoryNameCommand,
) -> Result<CategoryView, O::Error> {
    let decision = decide_update_category_name(command)?;
    let category = operations.load_category(&decision.category_id).await?;
    operations
        .require_member(&category.household_id, &decision.actor_id)
        .await?;
    if category.name == decision.name {
        return Ok(category.into_category());
    }
    operations.persist(category, decision).await
}

pub trait SetCategoryArchivedOperations {
    type Error: From<CategoryError>;

    async fn load_category(
        &mut self,
        category_id: &CategoryId,
    ) -> Result<LoadedCategory, Self::Error>;
    async fn require_member(
        &mut self,
        household_id: &HouseholdId,
        actor_id: &ActorId,
    ) -> Result<(), Self::Error>;
    async fn persist(
        &mut self,
        category: LoadedCategory,
        decision: SetCategoryArchivedDecision,
    ) -> Result<CategoryView, Self::Error>;
}

pub async fn set_category_archived<O: SetCategoryArchivedOperations>(
    operations: &mut O,
    command: SetCategoryArchivedCommand,
) -> Result<CategoryView, O::Error> {
    let decision = decide_set_category_archived(command)?;
    let category = operations.load_category(&decision.category_id).await?;
    operations
        .require_member(&category.household_id, &decision.actor_id)
        .await?;
    if category.status.is_archived() == decision.archived {
        return Ok(category.into_category());
    }
    operations.persist(category, decision).await
}

#[derive(Debug)]
pub enum CategoryMutationError {
    BadInput,
    NotFound,
    InvariantViolation(&'static str),
    Infrastructure(String),
}

pub trait CategoryMutation: Send + Sync {
    fn create<'a>(
        &'a self,
        command: CreateCategoryCommand,
    ) -> Pin<Box<dyn Future<Output = Result<CategoryView, CategoryMutationError>> + Send + 'a>>;

    fn update_name<'a>(
        &'a self,
        command: UpdateCategoryNameCommand,
    ) -> Pin<Box<dyn Future<Output = Result<CategoryView, CategoryMutationError>> + Send + 'a>>;

    fn set_archived<'a>(
        &'a self,
        command: SetCategoryArchivedCommand,
    ) -> Pin<Box<dyn Future<Output = Result<CategoryView, CategoryMutationError>> + Send + 'a>>;
}
