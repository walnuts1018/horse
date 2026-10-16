use horse_authorization::{Operation, Role, authorize};
use horse_household_change::UiChangeDescriptor;
use uuid::Uuid;

macro_rules! id_type {
    ($name:ident) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
        pub struct $name(Uuid);

        impl $name {
            pub fn new(value: impl AsRef<str>) -> Result<Self, MembershipError> {
                Uuid::parse_str(value.as_ref())
                    .map(Self)
                    .map_err(|_| MembershipError::InvalidIdentifier)
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
id_type!(MemberId);

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MembershipFacts {
    pub actor_role: Option<String>,
    pub target_role: Option<String>,
    pub owner_count: usize,
    pub has_active_funding_reference: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MembershipChangeDecision {
    pub actor_id: ActorId,
    pub household_id: HouseholdId,
    pub member_id: MemberId,
    pub change: MembershipChange,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MembershipChange {
    Leave,
    Remove,
    TransferOwner { new_owner_was_owner: bool },
}

impl MembershipChange {
    pub fn ui_change(self) -> UiChangeDescriptor {
        match self {
            Self::Leave => UiChangeDescriptor::new("household.member.left", "member", ["members"]),
            Self::Remove => {
                UiChangeDescriptor::new("household.member.removed", "member", ["members"])
            }
            Self::TransferOwner { .. } => UiChangeDescriptor::new(
                "household.owner.transferred",
                "member",
                ["members", "owner"],
            ),
        }
    }
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
    actor_id: ActorId,
    household_id: HouseholdId,
    member_id: MemberId,
    target_role: Option<&str>,
    owner_count: usize,
    has_active_funding_reference: bool,
) -> Result<MembershipChangeDecision, MembershipError> {
    let target_role = authorize(target_role, Operation::LeaveHousehold)
        .map_err(|_| MembershipError::Forbidden)?;
    if target_role == Role::Owner && owner_count <= 1 {
        return Err(MembershipError::LastOwner);
    }
    if has_active_funding_reference {
        return Err(MembershipError::ReferencedMember);
    }
    if actor_id.value() != member_id.value() {
        return Err(MembershipError::Forbidden);
    }
    Ok(MembershipChangeDecision {
        actor_id,
        household_id,
        member_id,
        change: MembershipChange::Leave,
    })
}

pub fn decide_remove_household_member(
    actor_id: ActorId,
    household_id: HouseholdId,
    member_id: MemberId,
    actor_role: Option<&str>,
    target_role: Option<&str>,
    owner_count: usize,
    has_active_funding_reference: bool,
) -> Result<MembershipChangeDecision, MembershipError> {
    if authorize(actor_role, Operation::ManageMembers).is_err()
        || target_role.and_then(Role::parse).is_none()
    {
        return Err(MembershipError::Forbidden);
    }
    if target_role.and_then(Role::parse) == Some(Role::Owner) && owner_count <= 1 {
        return Err(MembershipError::LastOwner);
    }
    if has_active_funding_reference {
        return Err(MembershipError::ReferencedMember);
    }
    Ok(MembershipChangeDecision {
        actor_id,
        household_id,
        member_id,
        change: MembershipChange::Remove,
    })
}

pub fn decide_transfer_owner(
    actor_id: ActorId,
    household_id: HouseholdId,
    new_owner_id: MemberId,
    actor_role: Option<&str>,
    new_owner_role: Option<&str>,
) -> Result<MembershipChangeDecision, MembershipError> {
    if authorize(actor_role, Operation::ManageMembers).is_err() {
        return Err(MembershipError::Forbidden);
    }
    let Some(new_owner_role) = new_owner_role.and_then(Role::parse) else {
        return Err(MembershipError::Forbidden);
    };
    if actor_id.value() == new_owner_id.value() {
        return Err(MembershipError::SelfTransfer);
    }
    Ok(MembershipChangeDecision {
        actor_id,
        household_id,
        member_id: new_owner_id,
        change: MembershipChange::TransferOwner {
            new_owner_was_owner: new_owner_role == Role::Owner,
        },
    })
}
