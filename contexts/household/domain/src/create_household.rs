use uuid::Uuid;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct ActorId(Uuid);

impl ActorId {
    pub const fn value(self) -> Uuid {
        self.0
    }
}

impl From<Uuid> for ActorId {
    fn from(value: Uuid) -> Self {
        Self(value)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdName(String);

impl HouseholdName {
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MemberName(String);

impl MemberName {
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateHouseholdCommand {
    pub actor_id: ActorId,
    pub name: String,
    pub member_names: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateHouseholdDecision {
    pub actor_id: ActorId,
    pub name: HouseholdName,
    pub member_names: Vec<MemberName>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CreateHouseholdError {
    InvalidName,
    TooManyMembers,
    InvalidMemberName,
}

pub fn decide_create_household(
    command: CreateHouseholdCommand,
) -> Result<CreateHouseholdDecision, CreateHouseholdError> {
    let name = command.name.trim();
    if name.is_empty() || name.len() > 120 {
        return Err(CreateHouseholdError::InvalidName);
    }
    if command.member_names.len() > 12 {
        return Err(CreateHouseholdError::TooManyMembers);
    }
    let mut member_names = Vec::with_capacity(command.member_names.len());
    for member_name in command.member_names {
        let member_name = member_name.trim();
        if member_name.is_empty() || member_name.len() > 120 {
            return Err(CreateHouseholdError::InvalidMemberName);
        }
        member_names.push(MemberName(member_name.to_owned()));
    }

    Ok(CreateHouseholdDecision {
        actor_id: command.actor_id,
        name: HouseholdName(name.to_owned()),
        member_names,
    })
}
