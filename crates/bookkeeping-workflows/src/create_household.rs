#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateHouseholdCommand {
    pub actor_id: String,
    pub name: String,
    pub member_names: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateHouseholdDecision {
    pub actor_id: String,
    pub name: String,
    pub member_names: Vec<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CreateHouseholdError {
    InvalidActorId,
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
    if command.actor_id.is_empty() {
        return Err(CreateHouseholdError::InvalidActorId);
    }

    let mut member_names = Vec::with_capacity(command.member_names.len());
    for member_name in command.member_names {
        let member_name = member_name.trim();
        if member_name.is_empty() || member_name.len() > 120 {
            return Err(CreateHouseholdError::InvalidMemberName);
        }
        member_names.push(member_name.to_owned());
    }

    Ok(CreateHouseholdDecision {
        actor_id: command.actor_id,
        name: name.to_owned(),
        member_names,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn command(name: &str, member_names: &[&str]) -> CreateHouseholdCommand {
        CreateHouseholdCommand {
            actor_id: "actor-id".to_owned(),
            name: name.to_owned(),
            member_names: member_names.iter().map(|name| (*name).to_owned()).collect(),
        }
    }

    #[test]
    fn trims_names_and_accepts_twelve_placeholder_members() {
        let members = (0..12).map(|_| " member ").collect::<Vec<_>>();
        let decision = decide_create_household(command(" home ", &members)).unwrap();

        assert_eq!(decision.name, "home");
        assert_eq!(decision.member_names, vec!["member"; 12]);
    }

    #[test]
    fn rejects_invalid_name_and_member_count() {
        assert_eq!(
            decide_create_household(command("  ", &[])),
            Err(CreateHouseholdError::InvalidName)
        );
        assert_eq!(
            decide_create_household(command("h".repeat(121).as_str(), &[])),
            Err(CreateHouseholdError::InvalidName)
        );
        let members = (0..13).map(|_| "member").collect::<Vec<_>>();
        assert_eq!(
            decide_create_household(command("home", &members)),
            Err(CreateHouseholdError::TooManyMembers)
        );
        assert_eq!(
            decide_create_household(command("home", &["  "])),
            Err(CreateHouseholdError::InvalidMemberName)
        );
    }
}
