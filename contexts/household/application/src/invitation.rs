use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
struct ActorId(String);

impl ActorId {
    fn new(value: String) -> Result<Self, InvitationError> {
        Uuid::parse_str(&value).map_err(|_| InvitationError::InvalidIdentifier)?;
        Ok(Self(value))
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct HouseholdId(String);

impl HouseholdId {
    fn new(value: String) -> Result<Self, InvitationError> {
        Uuid::parse_str(&value).map_err(|_| InvitationError::InvalidIdentifier)?;
        Ok(Self(value))
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct InvitationId(String);

impl InvitationId {
    fn new(value: String) -> Result<Self, InvitationError> {
        Uuid::parse_str(&value).map_err(|_| InvitationError::InvalidIdentifier)?;
        Ok(Self(value))
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct MemberId(String);

impl MemberId {
    fn new(value: String) -> Result<Self, InvitationError> {
        Uuid::parse_str(&value).map_err(|_| InvitationError::InvalidIdentifier)?;
        Ok(Self(value))
    }
}

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
    NotFound,
    InvalidExpiry,
    Expired,
    Revoked,
    AlreadyAccepted,
    AlreadyMember,
}

pub trait CreateInvitationOperations {
    type Error: From<InvitationError>;
    type Output;

    async fn load_actor_role(
        &mut self,
        household_id: &str,
        actor_id: &str,
    ) -> Result<Option<String>, Self::Error>;
    async fn persist(
        &mut self,
        decision: CreateInvitationDecision,
    ) -> Result<Self::Output, Self::Error>;
}

pub async fn create_invitation<O: CreateInvitationOperations>(
    operations: &mut O,
    command: CreateInvitationCommand,
    now: DateTime<Utc>,
) -> Result<O::Output, O::Error> {
    let actor_id = ActorId::new(command.actor_id.clone())?;
    let household_id = HouseholdId::new(command.household_id.clone())?;
    InvitationId::new(command.invitation_id.clone())?;
    let actor_role = operations
        .load_actor_role(household_id.0.as_str(), actor_id.0.as_str())
        .await?;
    let decision = decide_create_invitation(command, actor_role.as_deref(), now)?;
    operations.persist(decision).await
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AcceptInvitationFacts {
    pub invitation_id: String,
    pub household_id: String,
    pub expires_at: DateTime<Utc>,
    pub revoked: bool,
    pub accepted: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct MembershipStatus {
    pub exists: bool,
    pub active: bool,
}

pub trait AcceptInvitationOperations {
    type Error: From<InvitationError>;

    async fn load_invitation(&mut self) -> Result<AcceptInvitationFacts, Self::Error>;
    async fn lock_household(&mut self, household_id: &str) -> Result<(), Self::Error>;
    async fn load_membership(
        &mut self,
        household_id: &str,
        member_id: &str,
    ) -> Result<MembershipStatus, Self::Error>;
    async fn persist(
        &mut self,
        decision: AcceptInvitationDecision,
        membership: MembershipStatus,
        now: DateTime<Utc>,
    ) -> Result<String, Self::Error>;
}

pub async fn accept_invitation_by_token<O: AcceptInvitationOperations>(
    operations: &mut O,
    member_id: String,
    expected_household_id: Option<&str>,
    now: DateTime<Utc>,
) -> Result<String, O::Error> {
    MemberId::new(member_id.clone())?;
    let facts = operations.load_invitation().await?;
    let household_id = HouseholdId::new(facts.household_id.clone())?;
    let invitation_id = InvitationId::new(facts.invitation_id.clone())?;
    if expected_household_id.is_some_and(|id| id != household_id.0) {
        return Err(InvitationError::NotFound.into());
    }
    operations.lock_household(household_id.0.as_str()).await?;
    let membership = operations
        .load_membership(household_id.0.as_str(), member_id.as_str())
        .await?;
    let decision = decide_accept_invitation(
        AcceptInvitationCommand {
            member_id,
            household_id: household_id.0,
            invitation_id: invitation_id.0,
        },
        facts.expires_at,
        facts.revoked,
        facts.accepted,
        membership.active,
        now,
    )?;
    operations.persist(decision, membership, now).await
}

pub trait RevokeInvitationOperations {
    type Error: From<InvitationError>;
    type Output;

    async fn load_household(&mut self, household_id: &str) -> Result<(), Self::Error>;
    async fn load_actor_role(
        &mut self,
        household_id: &str,
        actor_id: &str,
    ) -> Result<Option<String>, Self::Error>;
    async fn load_invitation(
        &mut self,
        household_id: &str,
        invitation_id: &str,
    ) -> Result<(DateTime<Utc>, DateTime<Utc>, bool, bool), Self::Error>;
    async fn persist(
        &mut self,
        decision: RevokeInvitationDecision,
        now: DateTime<Utc>,
    ) -> Result<Self::Output, Self::Error>;
}

pub async fn revoke_invitation<O: RevokeInvitationOperations>(
    operations: &mut O,
    command: RevokeInvitationCommand,
    now: DateTime<Utc>,
) -> Result<O::Output, O::Error> {
    let actor_id = ActorId::new(command.actor_id.clone())?;
    let household_id = HouseholdId::new(command.household_id.clone())?;
    let invitation_id = InvitationId::new(command.invitation_id.clone())?;
    operations.load_household(household_id.0.as_str()).await?;
    let actor_role = operations
        .load_actor_role(household_id.0.as_str(), actor_id.0.as_str())
        .await?;
    let row = operations
        .load_invitation(household_id.0.as_str(), invitation_id.0.as_str())
        .await?;
    let decision = decide_revoke_invitation(command, actor_role.as_deref(), row.2, row.3)?;
    operations.persist(decision, now).await
}

pub fn decide_create_invitation(
    command: CreateInvitationCommand,
    actor_role: Option<&str>,
    now: DateTime<Utc>,
) -> Result<CreateInvitationDecision, InvitationError> {
    let actor_id = ActorId::new(command.actor_id)?;
    let household_id = HouseholdId::new(command.household_id)?;
    let invitation_id = InvitationId::new(command.invitation_id)?;
    if actor_role != Some("OWNER") {
        return Err(InvitationError::Forbidden);
    }
    let max_expiry = now + chrono::Duration::days(30);
    if command.expires_at <= now || command.expires_at > max_expiry {
        return Err(InvitationError::InvalidExpiry);
    }
    Ok(CreateInvitationDecision {
        actor_id: actor_id.0,
        household_id: household_id.0,
        invitation_id: invitation_id.0,
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
    let member_id = MemberId::new(command.member_id)?;
    let household_id = HouseholdId::new(command.household_id)?;
    let invitation_id = InvitationId::new(command.invitation_id)?;
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
        member_id: member_id.0,
        household_id: household_id.0,
        invitation_id: invitation_id.0,
        restore_membership: true,
    })
}

pub fn decide_revoke_invitation(
    command: RevokeInvitationCommand,
    actor_role: Option<&str>,
    accepted: bool,
    already_revoked: bool,
) -> Result<RevokeInvitationDecision, InvitationError> {
    let actor_id = ActorId::new(command.actor_id)?;
    let household_id = HouseholdId::new(command.household_id)?;
    let invitation_id = InvitationId::new(command.invitation_id)?;
    if actor_role != Some("OWNER") {
        return Err(InvitationError::Forbidden);
    }
    if accepted {
        return Err(InvitationError::AlreadyAccepted);
    }
    Ok(RevokeInvitationDecision {
        actor_id: actor_id.0,
        household_id: household_id.0,
        invitation_id: invitation_id.0,
        already_revoked,
    })
}
