use super::ids::{ActorId, HouseholdId};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePoolCommand {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub pool_id: NewPoolId,
    pub name: String,
    pub kind: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PoolKind {
    Personal,
    Shared,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatePoolDecision {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub pool_id: NewPoolId,
    pub name: String,
    pub kind: PoolKind,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CreatePoolError {
    InvalidName,
    InvalidKind,
}

pub fn decide_create_pool(
    command: CreatePoolCommand,
) -> Result<CreatePoolDecision, CreatePoolError> {
    let name = command.name.trim();
    if name.is_empty() || name.chars().count() > 120 {
        return Err(CreatePoolError::InvalidName);
    }
    let kind = match command.kind.as_str() {
        "PERSONAL" => PoolKind::Personal,
        "SHARED" => PoolKind::Shared,
        _ => return Err(CreatePoolError::InvalidKind),
    };

    Ok(CreatePoolDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        pool_id: command.pool_id,
        name: name.to_owned(),
        kind,
    })
}
use uuid::Uuid;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct NewPoolId(Uuid);

impl NewPoolId {
    pub const fn from_uuid(value: Uuid) -> Self {
        Self(value)
    }

    pub const fn value(self) -> Uuid {
        self.0
    }
}
