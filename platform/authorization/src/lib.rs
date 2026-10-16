#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Role {
    Owner,
    Member,
}

impl Role {
    pub fn parse(value: &str) -> Option<Self> {
        match value {
            "OWNER" => Some(Self::Owner),
            "MEMBER" => Some(Self::Member),
            _ => None,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Operation {
    ManageHousehold,
    ManageMembers,
    ManageInvitations,
    ManageMailForwarding,
    FinancialData,
    LeaveHousehold,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct AuthorizationError;

pub fn authorize(role: Option<&str>, operation: Operation) -> Result<Role, AuthorizationError> {
    let role = role.and_then(Role::parse).ok_or(AuthorizationError)?;
    let owner_only = matches!(
        operation,
        Operation::ManageHousehold
            | Operation::ManageMembers
            | Operation::ManageInvitations
            | Operation::ManageMailForwarding
    );
    if owner_only && role != Role::Owner {
        return Err(AuthorizationError);
    }
    Ok(role)
}
