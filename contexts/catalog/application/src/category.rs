use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Category {
    pub id: String,
    pub household_id: String,
    pub parent_id: Option<String>,
    pub name: String,
    pub archived: bool,
}

macro_rules! identifier {
    ($type_name:ident, $error_name:ident) => {
        #[derive(Debug, Clone, PartialEq, Eq)]
        pub struct $type_name(String);

        #[derive(Debug, Clone, Copy, PartialEq, Eq)]
        pub struct $error_name;

        impl $type_name {
            pub fn new(value: impl Into<String>) -> Result<Self, $error_name> {
                let value = value.into();
                if Uuid::parse_str(&value).is_err() {
                    return Err($error_name);
                }
                Ok(Self(value))
            }

            pub fn as_str(&self) -> &str {
                &self.0
            }
        }
    };
}

identifier!(ActorId, InvalidActorId);
identifier!(HouseholdId, InvalidHouseholdId);
identifier!(CategoryId, InvalidCategoryId);

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CategoryName(String);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct InvalidCategoryName;

impl CategoryName {
    pub fn new(value: impl Into<String>) -> Result<Self, InvalidCategoryName> {
        let value = value.into();
        let name = value.trim();
        if name.is_empty() || name.chars().count() > 120 {
            return Err(InvalidCategoryName);
        }
        Ok(Self(name.to_owned()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateCategoryCommand {
    pub actor_id: String,
    pub household_id: String,
    pub category_id: String,
    pub parent_id: Option<String>,
    pub name: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateCategoryDecision {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub category_id: CategoryId,
    pub parent_id: Option<CategoryId>,
    pub name: CategoryName,
    pub event: CategoryEvent,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CategoryEvent {
    Created { category_id: CategoryId },
    NameUpdated { category_id: CategoryId },
    Archived { category_id: CategoryId },
    Unarchived { category_id: CategoryId },
}

impl CategoryEvent {
    pub fn category_id(&self) -> &CategoryId {
        match self {
            Self::Created { category_id }
            | Self::NameUpdated { category_id }
            | Self::Archived { category_id }
            | Self::Unarchived { category_id } => category_id,
        }
    }

    pub fn event_type(&self) -> &'static str {
        match self {
            Self::Created { .. } => "category.created",
            Self::NameUpdated { .. } => "category.name_updated",
            Self::Archived { .. } => "category.archived",
            Self::Unarchived { .. } => "category.unarchived",
        }
    }

    pub fn changed_fields(&self) -> &'static [&'static str] {
        match self {
            Self::Created { .. } => &["name", "parentId"],
            Self::NameUpdated { .. } => &["name"],
            Self::Archived { .. } | Self::Unarchived { .. } => &["archived"],
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateCategoryNameCommand {
    pub actor_id: String,
    pub category_id: String,
    pub name: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateCategoryNameDecision {
    pub actor_id: ActorId,
    pub category_id: CategoryId,
    pub name: CategoryName,
    pub event: CategoryEvent,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetCategoryArchivedCommand {
    pub actor_id: String,
    pub category_id: String,
    pub archived: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetCategoryArchivedDecision {
    pub actor_id: ActorId,
    pub category_id: CategoryId,
    pub archived: bool,
    pub event: CategoryEvent,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CategoryError {
    InvalidActorId,
    InvalidHouseholdId,
    InvalidCategoryId,
    InvalidParentId,
    InvalidName,
}

pub fn decide_create_category(
    command: CreateCategoryCommand,
) -> Result<CreateCategoryDecision, CategoryError> {
    let actor_id = ActorId::new(command.actor_id).map_err(|_| CategoryError::InvalidActorId)?;
    let household_id =
        HouseholdId::new(command.household_id).map_err(|_| CategoryError::InvalidHouseholdId)?;
    let category_id =
        CategoryId::new(command.category_id).map_err(|_| CategoryError::InvalidCategoryId)?;
    let parent_id = command
        .parent_id
        .map(CategoryId::new)
        .transpose()
        .map_err(|_| CategoryError::InvalidParentId)?;
    let name = CategoryName::new(command.name).map_err(|_| CategoryError::InvalidName)?;
    let event = CategoryEvent::Created {
        category_id: category_id.clone(),
    };
    Ok(CreateCategoryDecision {
        actor_id,
        household_id,
        category_id,
        parent_id,
        name,
        event,
    })
}

pub fn decide_update_category_name(
    command: UpdateCategoryNameCommand,
) -> Result<UpdateCategoryNameDecision, CategoryError> {
    let actor_id = ActorId::new(command.actor_id).map_err(|_| CategoryError::InvalidActorId)?;
    let category_id =
        CategoryId::new(command.category_id).map_err(|_| CategoryError::InvalidCategoryId)?;
    let name = CategoryName::new(command.name).map_err(|_| CategoryError::InvalidName)?;
    let event = CategoryEvent::NameUpdated {
        category_id: category_id.clone(),
    };
    Ok(UpdateCategoryNameDecision {
        actor_id,
        category_id,
        name,
        event,
    })
}

pub fn decide_set_category_archived(
    command: SetCategoryArchivedCommand,
) -> Result<SetCategoryArchivedDecision, CategoryError> {
    let actor_id = ActorId::new(command.actor_id).map_err(|_| CategoryError::InvalidActorId)?;
    let category_id =
        CategoryId::new(command.category_id).map_err(|_| CategoryError::InvalidCategoryId)?;
    let event = if command.archived {
        CategoryEvent::Archived {
            category_id: category_id.clone(),
        }
    } else {
        CategoryEvent::Unarchived {
            category_id: category_id.clone(),
        }
    };
    Ok(SetCategoryArchivedDecision {
        actor_id,
        category_id,
        archived: command.archived,
        event,
    })
}

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
    async fn persist(&mut self, decision: CreateCategoryDecision) -> Result<Category, Self::Error>;
}

pub async fn create_category<O: CreateCategoryOperations>(
    operations: &mut O,
    command: CreateCategoryCommand,
) -> Result<Category, O::Error> {
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
