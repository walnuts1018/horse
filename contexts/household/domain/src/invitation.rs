use chrono::{DateTime, Utc};
use horse_authorization::{Operation, authorize};
use uuid::Uuid;

macro_rules! id_type {
    ($name:ident) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
        pub struct $name(Uuid);

        impl $name {
            pub fn new(value: impl AsRef<str>) -> Result<Self, InvitationError> {
                Uuid::parse_str(value.as_ref())
                    .map(Self)
                    .map_err(|_| InvitationError::InvalidIdentifier)
            }

            pub const fn value(self) -> Uuid {
                self.0
            }
        }

        impl From<Uuid> for $name {
            fn from(value: Uuid) -> Self {
                Self(value)
            }
        }
    };
}

id_type!(ActorId);
id_type!(HouseholdId);
id_type!(InvitationId);
id_type!(MemberId);

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateInvitationCommand {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub invitation_id: InvitationId,
    pub expires_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateInvitationDecision {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub invitation_id: InvitationId,
    pub expires_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AcceptInvitationCommand {
    pub member_id: MemberId,
    pub household_id: HouseholdId,
    pub invitation_id: InvitationId,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AcceptInvitationDecision {
    pub member_id: MemberId,
    pub household_id: HouseholdId,
    pub invitation_id: InvitationId,
    pub restore_membership: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RevokeInvitationCommand {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub invitation_id: InvitationId,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RevokeInvitationDecision {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub invitation_id: InvitationId,
    pub already_revoked: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum InvitationError {
    InvalidIdentifier,
    Forbidden,
    NotFound,
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
    let actor_id = command.actor_id;
    let household_id = command.household_id;
    let invitation_id = command.invitation_id;
    if authorize(actor_role, Operation::ManageInvitations).is_err() {
        return Err(InvitationError::Forbidden);
    }
    let max_expiry = now + chrono::Duration::days(30);
    if command.expires_at <= now || command.expires_at > max_expiry {
        return Err(InvitationError::InvalidExpiry);
    }
    Ok(CreateInvitationDecision {
        actor_id,
        household_id,
        invitation_id,
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
    let member_id = command.member_id;
    let household_id = command.household_id;
    let invitation_id = command.invitation_id;
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
        member_id,
        household_id,
        invitation_id,
        restore_membership: true,
    })
}

pub fn decide_revoke_invitation(
    command: RevokeInvitationCommand,
    actor_role: Option<&str>,
    accepted: bool,
    already_revoked: bool,
) -> Result<RevokeInvitationDecision, InvitationError> {
    let actor_id = command.actor_id;
    let household_id = command.household_id;
    let invitation_id = command.invitation_id;
    if authorize(actor_role, Operation::ManageInvitations).is_err() {
        return Err(InvitationError::Forbidden);
    }
    if accepted {
        return Err(InvitationError::AlreadyAccepted);
    }
    Ok(RevokeInvitationDecision {
        actor_id,
        household_id,
        invitation_id,
        already_revoked,
    })
}
