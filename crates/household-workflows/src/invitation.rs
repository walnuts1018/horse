use chrono::{DateTime, Utc};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateInvitationCommand {
    pub actor_id: String,
    pub household_id: String,
    pub invitation_id: String,
    pub expires_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateInvitationDecision {
    pub actor_id: String,
    pub household_id: String,
    pub invitation_id: String,
    pub expires_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AcceptInvitationCommand {
    pub member_id: String,
    pub household_id: String,
    pub invitation_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AcceptInvitationDecision {
    pub member_id: String,
    pub household_id: String,
    pub invitation_id: String,
    pub restore_membership: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RevokeInvitationCommand {
    pub actor_id: String,
    pub household_id: String,
    pub invitation_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RevokeInvitationDecision {
    pub actor_id: String,
    pub household_id: String,
    pub invitation_id: String,
    pub already_revoked: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum InvitationError {
    InvalidIdentifier,
    Forbidden,
    InvalidExpiry,
    Expired,
    Revoked,
    AlreadyAccepted,
    AlreadyMember,
}

pub fn decide_create_invitation(
    command: CreateInvitationCommand,
    actor_role: Option<&str>,
    now: DateTime<Utc>,
) -> Result<CreateInvitationDecision, InvitationError> {
    if command.actor_id.is_empty()
        || command.household_id.is_empty()
        || command.invitation_id.is_empty()
    {
        return Err(InvitationError::InvalidIdentifier);
    }
    if actor_role != Some("OWNER") {
        return Err(InvitationError::Forbidden);
    }
    let max_expiry = now + chrono::Duration::days(30);
    if command.expires_at <= now || command.expires_at > max_expiry {
        return Err(InvitationError::InvalidExpiry);
    }
    Ok(CreateInvitationDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        invitation_id: command.invitation_id,
        expires_at: command.expires_at,
    })
}

pub fn decide_accept_invitation(
    command: AcceptInvitationCommand,
    expires_at: DateTime<Utc>,
    revoked: bool,
    accepted: bool,
    existing_member_active: bool,
    now: DateTime<Utc>,
) -> Result<AcceptInvitationDecision, InvitationError> {
    if command.member_id.is_empty()
        || command.household_id.is_empty()
        || command.invitation_id.is_empty()
    {
        return Err(InvitationError::InvalidIdentifier);
    }
    if revoked {
        return Err(InvitationError::Revoked);
    }
    if accepted {
        return Err(InvitationError::AlreadyAccepted);
    }
    if expires_at <= now {
        return Err(InvitationError::Expired);
    }
    if existing_member_active {
        return Err(InvitationError::AlreadyMember);
    }
    Ok(AcceptInvitationDecision {
        member_id: command.member_id,
        household_id: command.household_id,
        invitation_id: command.invitation_id,
        restore_membership: true,
    })
}

pub fn decide_revoke_invitation(
    command: RevokeInvitationCommand,
    actor_role: Option<&str>,
    accepted: bool,
    already_revoked: bool,
) -> Result<RevokeInvitationDecision, InvitationError> {
    if command.actor_id.is_empty()
        || command.household_id.is_empty()
        || command.invitation_id.is_empty()
    {
        return Err(InvitationError::InvalidIdentifier);
    }
    if actor_role != Some("OWNER") {
        return Err(InvitationError::Forbidden);
    }
    if accepted {
        return Err(InvitationError::AlreadyAccepted);
    }
    Ok(RevokeInvitationDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        invitation_id: command.invitation_id,
        already_revoked,
    })
}
