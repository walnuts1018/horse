#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePoolCommand {
    pub actor_id: String,
    pub household_id: String,
    pub pool_id: String,
    pub name: String,
    pub kind: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePoolDecision {
    pub actor_id: String,
    pub household_id: String,
    pub pool_id: String,
    pub name: String,
    pub kind: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CreatePoolError {
    InvalidActorId,
    InvalidHouseholdId,
    InvalidPoolId,
    InvalidName,
    InvalidKind,
}

pub fn decide_create_pool(
    command: CreatePoolCommand,
) -> Result<CreatePoolDecision, CreatePoolError> {
    if command.actor_id.is_empty() {
        return Err(CreatePoolError::InvalidActorId);
    }
    if command.household_id.is_empty() {
        return Err(CreatePoolError::InvalidHouseholdId);
    }
    if command.pool_id.is_empty() {
        return Err(CreatePoolError::InvalidPoolId);
    }
    let name = command.name.trim();
    if name.is_empty() || name.chars().count() > 120 {
        return Err(CreatePoolError::InvalidName);
    }
    if !matches!(command.kind.as_str(), "PERSONAL" | "SHARED") {
        return Err(CreatePoolError::InvalidKind);
    }

    Ok(CreatePoolDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        pool_id: command.pool_id,
        name: name.to_owned(),
        kind: command.kind,
    })
}
