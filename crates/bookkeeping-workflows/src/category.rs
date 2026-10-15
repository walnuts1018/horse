#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Category {
    pub id: String,
    pub household_id: String,
    pub parent_id: Option<String>,
    pub name: String,
    pub archived: bool,
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
    pub actor_id: String,
    pub household_id: String,
    pub category_id: String,
    pub parent_id: Option<String>,
    pub name: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateCategoryNameCommand {
    pub actor_id: String,
    pub category_id: String,
    pub name: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateCategoryNameDecision {
    pub actor_id: String,
    pub category_id: String,
    pub name: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetCategoryArchivedCommand {
    pub actor_id: String,
    pub category_id: String,
    pub archived: bool,
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
    validate_id(&command.actor_id).map_err(|_| CategoryError::InvalidActorId)?;
    validate_id(&command.household_id).map_err(|_| CategoryError::InvalidHouseholdId)?;
    validate_id(&command.category_id).map_err(|_| CategoryError::InvalidCategoryId)?;
    if command.parent_id.as_deref().is_some_and(|id| validate_id(id).is_err()) {
        return Err(CategoryError::InvalidParentId);
    }
    let name = validated_name(command.name)?;
    Ok(CreateCategoryDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        category_id: command.category_id,
        parent_id: command.parent_id,
        name,
    })
}

pub fn decide_update_category_name(
    command: UpdateCategoryNameCommand,
) -> Result<UpdateCategoryNameDecision, CategoryError> {
    validate_id(&command.actor_id).map_err(|_| CategoryError::InvalidActorId)?;
    validate_id(&command.category_id).map_err(|_| CategoryError::InvalidCategoryId)?;
    Ok(UpdateCategoryNameDecision {
        actor_id: command.actor_id,
        category_id: command.category_id,
        name: validated_name(command.name)?,
    })
}

pub fn decide_set_category_archived(
    actor_id: String,
    category_id: String,
    archived: bool,
) -> Result<SetCategoryArchivedCommand, CategoryError> {
    validate_id(&actor_id).map_err(|_| CategoryError::InvalidActorId)?;
    validate_id(&category_id).map_err(|_| CategoryError::InvalidCategoryId)?;
    Ok(SetCategoryArchivedCommand { actor_id, category_id, archived })
}

fn validated_name(value: String) -> Result<String, CategoryError> {
    let name = value.trim();
    if name.is_empty() || name.chars().count() > 120 {
        return Err(CategoryError::InvalidName);
    }
    Ok(name.to_owned())
}

fn validate_id(value: &str) -> Result<(), ()> {
    if value.is_empty() { Err(()) } else { Ok(()) }
}
