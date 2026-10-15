#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LeaveHouseholdCommand {
    pub member_id: String,
    pub household_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TransferOwnerCommand {
    pub actor_id: String,
    pub household_id: String,
    pub new_owner_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MembershipChangeDecision {
    pub actor_id: String,
    pub household_id: String,
    pub member_id: String,
    pub change: MembershipChange,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MembershipChange {
    Leave,
    TransferOwner { new_owner_was_owner: bool },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MembershipError {
    InvalidIdentifier,
    Forbidden,
    SelfTransfer,
    LastOwner,
    ReferencedMember,
}

pub fn decide_leave_household(
    command: LeaveHouseholdCommand,
    target_role: Option<&str>,
    owner_count: usize,
    has_active_funding_reference: bool,
) -> Result<MembershipChangeDecision, MembershipError> {
    if command.member_id.is_empty() || command.household_id.is_empty() {
        return Err(MembershipError::InvalidIdentifier);
    }
    let Some(target_role) = target_role else {
        return Err(MembershipError::Forbidden);
    };
    if target_role == "OWNER" && owner_count <= 1 {
        return Err(MembershipError::LastOwner);
    }
    if has_active_funding_reference {
        return Err(MembershipError::ReferencedMember);
    }
    Ok(MembershipChangeDecision {
        actor_id: command.member_id.clone(),
        household_id: command.household_id,
        member_id: command.member_id,
        change: MembershipChange::Leave,
    })
}

pub fn decide_transfer_owner(
    command: TransferOwnerCommand,
    actor_role: Option<&str>,
    new_owner_role: Option<&str>,
) -> Result<MembershipChangeDecision, MembershipError> {
    if command.actor_id.is_empty()
        || command.household_id.is_empty()
        || command.new_owner_id.is_empty()
    {
        return Err(MembershipError::InvalidIdentifier);
    }
    if actor_role != Some("OWNER") || new_owner_role.is_none() {
        return Err(MembershipError::Forbidden);
    }
    if command.actor_id == command.new_owner_id {
        return Err(MembershipError::SelfTransfer);
    }
    Ok(MembershipChangeDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        member_id: command.new_owner_id,
        change: MembershipChange::TransferOwner {
            new_owner_was_owner: new_owner_role == Some("OWNER"),
        },
    })
}
