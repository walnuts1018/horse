use std::{future::Future, pin::Pin};

use chrono::{DateTime, Utc};
use horse_household_change::UiChangeDescriptor;
pub use household_domain::invitation::{
    AcceptInvitationDecision, ActorId, CreateInvitationDecision, HouseholdId, InvitationError,
    InvitationId, MemberId, RevokeInvitationDecision, decide_accept_invitation,
    decide_create_invitation, decide_revoke_invitation,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateInvitationCommand {
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
pub struct RevokeInvitationCommand {
    pub actor_id: String,
    pub household_id: String,
    pub invitation_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatedInvitation {
    pub id: String,
    pub token: String,
    pub created_at: DateTime<Utc>,
    pub expires_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InvitationState {
    pub id: String,
    pub household_id: String,
    pub created_at: DateTime<Utc>,
    pub expires_at: DateTime<Utc>,
    pub accepted_at: Option<DateTime<Utc>>,
    pub revoked_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InvitationPage {
    pub rows: Vec<InvitationState>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug)]
pub enum InvitationHandlerError {
    Decision(InvitationError),
    NotFound,
    Conflict,
    InvalidPagination,
    InvalidCursor,
    Infrastructure(String),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateInvitationRequest {
    pub command: CreateInvitationCommand,
    pub token: String,
    pub token_hash: [u8; 32],
    pub now: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AcceptInvitationRequest {
    pub member_id: String,
    pub token_hash: [u8; 32],
    pub now: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RevokeInvitationRequest {
    pub command: RevokeInvitationCommand,
    pub now: DateTime<Utc>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ListInvitationsRequest {
    pub actor_id: uuid::Uuid,
    pub household_id: uuid::Uuid,
    pub after_id: Option<uuid::Uuid>,
    pub before_id: Option<uuid::Uuid>,
    pub first: Option<i32>,
    pub last: Option<i32>,
}

pub trait HouseholdInvitationHandler: Send + Sync {
    fn create<'a>(
        &'a self,
        request: CreateInvitationRequest,
    ) -> Pin<Box<dyn Future<Output = Result<CreatedInvitation, InvitationHandlerError>> + Send + 'a>>;

    fn accept<'a>(
        &'a self,
        request: AcceptInvitationRequest,
    ) -> Pin<Box<dyn Future<Output = Result<String, InvitationHandlerError>> + Send + 'a>>;

    fn revoke<'a>(
        &'a self,
        request: RevokeInvitationRequest,
    ) -> Pin<Box<dyn Future<Output = Result<InvitationState, InvitationHandlerError>> + Send + 'a>>;

    fn list<'a>(
        &'a self,
        request: ListInvitationsRequest,
    ) -> Pin<Box<dyn Future<Output = Result<InvitationPage, InvitationHandlerError>> + Send + 'a>>;
}

pub fn invitation_created_change() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "household.invitation.created",
        "invitation",
        ["invitations"],
    )
}

pub fn member_joined_change() -> UiChangeDescriptor {
    UiChangeDescriptor::new("household.member.joined", "member", ["members"])
}

pub fn invitation_revoked_change() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "household.invitation.revoked",
        "invitation",
        ["invitations"],
    )
}

pub trait CreateInvitationOperations {
    type Error: From<InvitationError>;
    type Output;

    async fn load_actor_role(
        &mut self,
        household_id: &HouseholdId,
        actor_id: &ActorId,
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
    let actor_id = ActorId::new(&command.actor_id)?;
    let household_id = HouseholdId::new(&command.household_id)?;
    let actor_role = operations.load_actor_role(&household_id, &actor_id).await?;
    let invitation_id = InvitationId::new(&command.invitation_id)?;
    let decision = decide_create_invitation(
        household_domain::invitation::CreateInvitationCommand {
            actor_id,
            household_id,
            invitation_id,
            expires_at: command.expires_at,
        },
        actor_role.as_deref(),
        now,
    )?;
    operations.persist(decision).await
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AcceptInvitationFacts {
    pub invitation_id: InvitationId,
    pub household_id: HouseholdId,
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
    async fn lock_household(&mut self, household_id: &HouseholdId) -> Result<(), Self::Error>;
    async fn load_membership(
        &mut self,
        household_id: &HouseholdId,
        member_id: &MemberId,
    ) -> Result<MembershipStatus, Self::Error>;
    async fn persist(
        &mut self,
        decision: AcceptInvitationDecision,
        membership: MembershipStatus,
        now: DateTime<Utc>,
    ) -> Result<(), Self::Error>;
}

pub async fn accept_invitation_by_token<O: AcceptInvitationOperations>(
    operations: &mut O,
    member_id: MemberId,
    expected_household_id: Option<HouseholdId>,
    now: DateTime<Utc>,
) -> Result<HouseholdId, O::Error> {
    let facts = operations.load_invitation().await?;
    let household_id = facts.household_id;
    if expected_household_id
        .as_ref()
        .is_some_and(|id| id != &household_id)
    {
        return Err(InvitationError::NotFound.into());
    }
    operations.lock_household(&household_id).await?;
    let membership = operations
        .load_membership(&household_id, &member_id)
        .await?;
    let decision = decide_accept_invitation(
        household_domain::invitation::AcceptInvitationCommand {
            member_id,
            household_id,
            invitation_id: facts.invitation_id,
        },
        facts.expires_at,
        facts.revoked,
        facts.accepted,
        membership.active,
        now,
    )?;
    operations.persist(decision, membership, now).await?;
    Ok(household_id)
}

pub trait RevokeInvitationOperations {
    type Error: From<InvitationError>;
    type Output;

    async fn load_household(&mut self, household_id: &HouseholdId) -> Result<(), Self::Error>;
    async fn load_actor_role(
        &mut self,
        household_id: &HouseholdId,
        actor_id: &ActorId,
    ) -> Result<Option<String>, Self::Error>;
    async fn load_invitation(
        &mut self,
        household_id: &HouseholdId,
        invitation_id: &InvitationId,
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
    let actor_id = ActorId::new(&command.actor_id)?;
    let household_id = HouseholdId::new(&command.household_id)?;
    let invitation_id = InvitationId::new(&command.invitation_id)?;
    operations.load_household(&household_id).await?;
    let actor_role = operations.load_actor_role(&household_id, &actor_id).await?;
    let row = operations
        .load_invitation(&household_id, &invitation_id)
        .await?;
    let decision = decide_revoke_invitation(
        household_domain::invitation::RevokeInvitationCommand {
            actor_id,
            household_id,
            invitation_id,
        },
        actor_role.as_deref(),
        row.2,
        row.3,
    )?;
    operations.persist(decision, now).await
}
