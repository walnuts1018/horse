use chrono::{DateTime, Utc};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, OptionalExtension, QueryDsl, dsl::now,
    result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_household_change::UiChangeDescriptor;
use household_application::authorization::{self, Operation};
use household_application::invitation::{
    AcceptInvitationDecision, AcceptInvitationFacts, AcceptInvitationOperations, ActorId,
    CreateInvitationCommand, CreateInvitationDecision, CreateInvitationOperations, HouseholdId,
    InvitationError, InvitationId, MemberId, MembershipStatus, RevokeInvitationCommand,
    RevokeInvitationDecision, RevokeInvitationOperations,
};
use household_application::invitation::{
    invitation_created_change, invitation_revoked_change, member_joined_change,
};
use household_application::workflow_failure::{WorkflowFailure, WorkflowRejection};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreatedInvitationDto {
    pub id: String,
    pub token: String,
    pub created_at: DateTime<Utc>,
    pub expires_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InvitationStateDto {
    pub id: String,
    pub household_id: String,
    pub created_at: DateTime<Utc>,
    pub expires_at: DateTime<Utc>,
    pub accepted_at: Option<DateTime<Utc>>,
    pub revoked_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdInvitationPageDto {
    pub rows: Vec<InvitationStateDto>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug)]
pub enum HouseholdInvitationError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    Workflow(InvitationError),
    HouseholdNotFound,
    InvitationNotFound,
    MemberNotFound,
    Conflict,
    InvalidPagination,
    InvalidCursor,
}

pub async fn list_invitations(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    after_id: Option<Uuid>,
    before_id: Option<Uuid>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<HouseholdInvitationPageDto, HouseholdInvitationError> {
    list_invitations_inner(
        pool,
        actor_id,
        household_id,
        after_id,
        before_id,
        first,
        last,
    )
    .await
}

pub async fn list_invitations_workflow(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    after_id: Option<Uuid>,
    before_id: Option<Uuid>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<HouseholdInvitationPageDto, WorkflowFailure<WorkflowRejection, HouseholdInvitationError>>
{
    list_invitations_inner(
        pool,
        actor_id,
        household_id,
        after_id,
        before_id,
        first,
        last,
    )
    .await
    .map_err(map_list_workflow_failure)
}

async fn list_invitations_inner(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    after_id: Option<Uuid>,
    before_id: Option<Uuid>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<HouseholdInvitationPageDto, HouseholdInvitationError> {
    let backward = last.is_some();
    if first.is_some() && last.is_some() {
        return Err(HouseholdInvitationError::InvalidPagination);
    }
    let page_size = usize::try_from(last.or(first).unwrap_or(50))
        .ok()
        .filter(|value| (1..=100).contains(value))
        .ok_or(HouseholdInvitationError::InvalidPagination)?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| HouseholdInvitationError::Pool(error.to_string()))?;
    use public::{household_invitations as invitations, household_members as members};

    let owner = members::table
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .filter(members::left_at.is_null())
        .select(members::role)
        .first::<String>(&mut *conn)
        .await
        .optional()?;
    if authorization::authorize(owner.as_deref(), Operation::ManageInvitations).is_err() {
        return Err(HouseholdInvitationError::HouseholdNotFound);
    }

    let total_count = invitations::table
        .filter(invitations::household_id.eq(household_id))
        .count()
        .get_result::<i64>(&mut *conn)
        .await?;
    let after = if let Some(id) = after_id {
        Some(
            invitations::table
                .filter(invitations::household_id.eq(household_id))
                .filter(invitations::id.eq(id))
                .select((invitations::created_at, invitations::id))
                .first::<(DateTime<Utc>, Uuid)>(&mut *conn)
                .await
                .optional()?
                .ok_or(HouseholdInvitationError::InvalidCursor)?,
        )
    } else {
        None
    };
    let before = if let Some(id) = before_id {
        Some(
            invitations::table
                .filter(invitations::household_id.eq(household_id))
                .filter(invitations::id.eq(id))
                .select((invitations::created_at, invitations::id))
                .first::<(DateTime<Utc>, Uuid)>(&mut *conn)
                .await
                .optional()?
                .ok_or(HouseholdInvitationError::InvalidCursor)?,
        )
    } else {
        None
    };
    let rows = if backward {
        let mut query = invitations::table
            .filter(invitations::household_id.eq(household_id))
            .into_boxed();
        if let Some((created_at, id)) = after {
            query = query.filter(
                invitations::created_at
                    .lt(created_at)
                    .or(invitations::created_at
                        .eq(created_at)
                        .and(invitations::id.gt(id))),
            );
        }
        if let Some((created_at, id)) = before {
            query = query.filter(
                invitations::created_at
                    .gt(created_at)
                    .or(invitations::created_at
                        .eq(created_at)
                        .and(invitations::id.lt(id))),
            );
        }
        let mut rows = query
            .order_by((invitations::created_at.asc(), invitations::id.desc()))
            .limit(i64::try_from(page_size + 1).unwrap_or(i64::MAX))
            .select((
                invitations::id,
                invitations::household_id,
                invitations::created_at,
                invitations::expires_at,
                invitations::accepted_at,
                invitations::revoked_at,
            ))
            .load::<(
                Uuid,
                Uuid,
                DateTime<Utc>,
                DateTime<Utc>,
                Option<DateTime<Utc>>,
                Option<DateTime<Utc>>,
            )>(&mut *conn)
            .await?;
        rows.reverse();
        rows
    } else {
        let mut query = invitations::table
            .filter(invitations::household_id.eq(household_id))
            .into_boxed();
        if let Some((created_at, id)) = after {
            query = query.filter(
                invitations::created_at
                    .lt(created_at)
                    .or(invitations::created_at
                        .eq(created_at)
                        .and(invitations::id.gt(id))),
            );
        }
        if let Some((created_at, id)) = before {
            query = query.filter(
                invitations::created_at
                    .gt(created_at)
                    .or(invitations::created_at
                        .eq(created_at)
                        .and(invitations::id.lt(id))),
            );
        }
        query
            .order_by((invitations::created_at.desc(), invitations::id.asc()))
            .limit(i64::try_from(page_size + 1).unwrap_or(i64::MAX))
            .select((
                invitations::id,
                invitations::household_id,
                invitations::created_at,
                invitations::expires_at,
                invitations::accepted_at,
                invitations::revoked_at,
            ))
            .load::<(
                Uuid,
                Uuid,
                DateTime<Utc>,
                DateTime<Utc>,
                Option<DateTime<Utc>>,
                Option<DateTime<Utc>>,
            )>(&mut *conn)
            .await?
    };
    let has_extra = rows.len() > page_size;
    let rows = rows
        .into_iter()
        .take(page_size)
        .map(|row| InvitationStateDto {
            id: row.0.to_string(),
            household_id: row.1.to_string(),
            created_at: row.2,
            expires_at: row.3,
            accepted_at: row.4,
            revoked_at: row.5,
        })
        .collect();
    Ok(HouseholdInvitationPageDto {
        rows,
        total_count,
        has_next_page: (!backward && has_extra) || (backward && before_id.is_some()),
        has_previous_page: (backward && has_extra) || (!backward && after_id.is_some()),
    })
}

impl From<DieselError> for HouseholdInvitationError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

impl From<InvitationError> for HouseholdInvitationError {
    fn from(error: InvitationError) -> Self {
        map_workflow_error(error)
    }
}

pub async fn create_invitation(
    pool: &PgPool,
    command: CreateInvitationCommand,
    token: String,
    token_hash: [u8; 32],
    now_value: DateTime<Utc>,
) -> Result<CreatedInvitationDto, HouseholdInvitationError> {
    create_invitation_inner(pool, command, token, token_hash, now_value).await
}

pub async fn create_invitation_workflow(
    pool: &PgPool,
    command: CreateInvitationCommand,
    token: String,
    token_hash: [u8; 32],
    now_value: DateTime<Utc>,
) -> Result<CreatedInvitationDto, WorkflowFailure<InvitationError, HouseholdInvitationError>> {
    create_invitation_inner(pool, command, token, token_hash, now_value)
        .await
        .map_err(map_invitation_workflow_failure)
}

async fn create_invitation_inner(
    pool: &PgPool,
    command: CreateInvitationCommand,
    token: String,
    token_hash: [u8; 32],
    now_value: DateTime<Utc>,
) -> Result<CreatedInvitationDto, HouseholdInvitationError> {
    parse_uuid(&command.household_id, "household_id")?;
    parse_uuid(&command.actor_id, "actor_id")?;
    parse_uuid(&command.invitation_id, "invitation_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdInvitationError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<CreatedInvitationDto, HouseholdInvitationError, _>(async |conn| {
            let mut operations = PostgresCreateInvitationOperations {
                conn,
                token,
                token_hash,
            };
            household_application::invitation::create_invitation(
                &mut operations,
                command,
                now_value,
            )
            .await
        })
        .await
}

struct PostgresCreateInvitationOperations<'a> {
    conn: &'a mut crate::AsyncPgConnection,
    token: String,
    token_hash: [u8; 32],
}

impl CreateInvitationOperations for PostgresCreateInvitationOperations<'_> {
    type Error = HouseholdInvitationError;
    type Output = CreatedInvitationDto;

    async fn load_actor_role(
        &mut self,
        household_id: &HouseholdId,
        actor_id: &ActorId,
    ) -> Result<Option<String>, Self::Error> {
        use public::{household_members as members, households};
        let household_id = household_id.value();
        let actor_id = actor_id.value();
        households::table
            .filter(households::id.eq(household_id))
            .for_update()
            .select(households::id)
            .first::<Uuid>(&mut *self.conn)
            .await
            .optional()?
            .ok_or(HouseholdInvitationError::HouseholdNotFound)?;
        Ok(members::table
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(actor_id))
            .filter(members::left_at.is_null())
            .select(members::role)
            .first::<String>(&mut *self.conn)
            .await
            .optional()?)
    }

    async fn persist(
        &mut self,
        decision: CreateInvitationDecision,
    ) -> Result<Self::Output, Self::Error> {
        use public::household_invitations as invitations;
        let actor_id = decision.actor_id.value();
        let household_id = decision.household_id.value();
        let invitation_id = decision.invitation_id.value();
        let created = diesel::insert_into(invitations::table)
            .values((
                invitations::id.eq(invitation_id),
                invitations::household_id.eq(household_id),
                invitations::token_hash.eq(self.token_hash.to_vec()),
                invitations::expires_at.eq(decision.expires_at),
                invitations::created_by.eq(actor_id),
            ))
            .returning((invitations::created_at, invitations::expires_at))
            .get_result::<(DateTime<Utc>, DateTime<Utc>)>(&mut *self.conn)
            .await?;
        record_change(
            self.conn,
            household_id,
            invitation_id,
            actor_id,
            invitation_created_change(),
        )
        .await?;
        Ok(CreatedInvitationDto {
            id: invitation_id.to_string(),
            token: self.token.clone(),
            created_at: created.0,
            expires_at: created.1,
        })
    }
}

pub async fn accept_invitation_by_token(
    pool: &PgPool,
    member_id: MemberId,
    token_hash: [u8; 32],
    now_value: DateTime<Utc>,
) -> Result<HouseholdId, HouseholdInvitationError> {
    accept_invitation_by_token_inner(pool, member_id, token_hash, now_value).await
}

pub async fn accept_invitation_by_token_workflow(
    pool: &PgPool,
    member_id: MemberId,
    token_hash: [u8; 32],
    now_value: DateTime<Utc>,
) -> Result<HouseholdId, WorkflowFailure<InvitationError, HouseholdInvitationError>> {
    accept_invitation_by_token_inner(pool, member_id, token_hash, now_value)
        .await
        .map_err(map_invitation_workflow_failure)
}

async fn accept_invitation_by_token_inner(
    pool: &PgPool,
    member_id: MemberId,
    token_hash: [u8; 32],
    now_value: DateTime<Utc>,
) -> Result<HouseholdId, HouseholdInvitationError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdInvitationError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<HouseholdId, HouseholdInvitationError, _>(async |conn| {
            let mut operations = PostgresAcceptInvitationOperations { conn, token_hash };
            household_application::invitation::accept_invitation_by_token(
                &mut operations,
                member_id,
                None,
                now_value,
            )
            .await
        })
        .await
}

struct PostgresAcceptInvitationOperations<'a> {
    conn: &'a mut crate::AsyncPgConnection,
    token_hash: [u8; 32],
}

impl AcceptInvitationOperations for PostgresAcceptInvitationOperations<'_> {
    type Error = HouseholdInvitationError;

    async fn load_invitation(&mut self) -> Result<AcceptInvitationFacts, Self::Error> {
        use public::household_invitations as invitations;
        let row = invitations::table
            .filter(invitations::token_hash.eq(self.token_hash.to_vec()))
            .for_update()
            .select((
                invitations::id,
                invitations::household_id,
                invitations::expires_at,
                invitations::revoked_at,
                invitations::accepted_at,
            ))
            .first::<(
                Uuid,
                Uuid,
                DateTime<Utc>,
                Option<DateTime<Utc>>,
                Option<DateTime<Utc>>,
            )>(&mut *self.conn)
            .await
            .optional()?
            .ok_or(HouseholdInvitationError::InvitationNotFound)?;
        Ok(AcceptInvitationFacts {
            invitation_id: InvitationId::from(row.0),
            household_id: HouseholdId::from(row.1),
            expires_at: row.2,
            revoked: row.3.is_some(),
            accepted: row.4.is_some(),
        })
    }

    async fn lock_household(&mut self, household_id: &HouseholdId) -> Result<(), Self::Error> {
        use public::households;
        let household_id = household_id.value();
        households::table
            .filter(households::id.eq(household_id))
            .for_update()
            .select(households::id)
            .first::<Uuid>(&mut *self.conn)
            .await
            .optional()?
            .ok_or(HouseholdInvitationError::HouseholdNotFound)?;
        Ok(())
    }

    async fn load_membership(
        &mut self,
        household_id: &HouseholdId,
        member_id: &MemberId,
    ) -> Result<MembershipStatus, Self::Error> {
        use public::household_members as members;
        let household_id = household_id.value();
        let member_id = member_id.value();
        let left_at = members::table
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(member_id))
            .for_update()
            .select(members::left_at)
            .first::<Option<DateTime<Utc>>>(&mut *self.conn)
            .await
            .optional()?;
        Ok(MembershipStatus {
            exists: left_at.is_some(),
            active: left_at == Some(None),
        })
    }

    async fn persist(
        &mut self,
        decision: AcceptInvitationDecision,
        membership: MembershipStatus,
        now_value: DateTime<Utc>,
    ) -> Result<(), Self::Error> {
        use public::{household_invitations as invitations, household_members as members};
        let member_id = decision.member_id.value();
        let household_id = decision.household_id.value();
        let invitation_id = decision.invitation_id.value();
        if decision.restore_membership {
            if membership.exists {
                diesel::update(
                    members::table
                        .filter(members::household_id.eq(household_id))
                        .filter(members::member_id.eq(member_id)),
                )
                .set((
                    members::left_at.eq(None::<DateTime<Utc>>),
                    members::joined_at.eq(now_value),
                    members::role.eq("MEMBER"),
                ))
                .execute(&mut *self.conn)
                .await?;
            } else {
                let inserted = diesel::insert_into(members::table)
                    .values((
                        members::household_id.eq(household_id),
                        members::member_id.eq(member_id),
                        members::role.eq("MEMBER"),
                    ))
                    .on_conflict((members::household_id, members::member_id))
                    .do_nothing()
                    .returning(members::member_id)
                    .get_result::<Uuid>(&mut *self.conn)
                    .await
                    .optional()?;
                if inserted.is_none() {
                    return Err(HouseholdInvitationError::Conflict);
                }
            }
        }
        diesel::update(invitations::table.filter(invitations::id.eq(invitation_id)))
            .set((
                invitations::accepted_by.eq(Some(member_id)),
                invitations::accepted_at.eq(Some(now_value)),
            ))
            .execute(&mut *self.conn)
            .await?;
        record_change(
            self.conn,
            household_id,
            member_id,
            member_id,
            member_joined_change(),
        )
        .await?;
        Ok(())
    }
}

pub async fn revoke_invitation(
    pool: &PgPool,
    command: RevokeInvitationCommand,
    now_value: DateTime<Utc>,
) -> Result<InvitationStateDto, HouseholdInvitationError> {
    revoke_invitation_inner(pool, command, now_value).await
}

pub async fn revoke_invitation_workflow(
    pool: &PgPool,
    command: RevokeInvitationCommand,
    now_value: DateTime<Utc>,
) -> Result<InvitationStateDto, WorkflowFailure<InvitationError, HouseholdInvitationError>> {
    revoke_invitation_inner(pool, command, now_value)
        .await
        .map_err(map_invitation_workflow_failure)
}

async fn revoke_invitation_inner(
    pool: &PgPool,
    command: RevokeInvitationCommand,
    now_value: DateTime<Utc>,
) -> Result<InvitationStateDto, HouseholdInvitationError> {
    parse_uuid(&command.household_id, "household_id")?;
    parse_uuid(&command.actor_id, "actor_id")?;
    parse_uuid(&command.invitation_id, "invitation_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdInvitationError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<InvitationStateDto, HouseholdInvitationError, _>(async |conn| {
            let mut operations = PostgresRevokeInvitationOperations { conn, row: None };
            household_application::invitation::revoke_invitation(
                &mut operations,
                command,
                now_value,
            )
            .await
        })
        .await
}

fn map_invitation_workflow_failure(
    error: HouseholdInvitationError,
) -> WorkflowFailure<InvitationError, HouseholdInvitationError> {
    match error {
        HouseholdInvitationError::InvalidUuid(_) => {
            WorkflowFailure::Decision(InvitationError::InvalidIdentifier)
        }
        HouseholdInvitationError::Workflow(InvitationError::InvalidIdentifier) => {
            WorkflowFailure::Decision(InvitationError::InvalidIdentifier)
        }
        HouseholdInvitationError::Workflow(InvitationError::Forbidden)
        | HouseholdInvitationError::Workflow(InvitationError::NotFound)
        | HouseholdInvitationError::HouseholdNotFound
        | HouseholdInvitationError::InvitationNotFound
        | HouseholdInvitationError::MemberNotFound => WorkflowFailure::NotFound,
        HouseholdInvitationError::Workflow(
            InvitationError::Expired
            | InvitationError::Revoked
            | InvitationError::AlreadyAccepted
            | InvitationError::AlreadyMember,
        )
        | HouseholdInvitationError::Conflict => WorkflowFailure::Conflict,
        HouseholdInvitationError::Workflow(error) => WorkflowFailure::Decision(error),
        error @ (HouseholdInvitationError::Pool(_) | HouseholdInvitationError::Database(_)) => {
            WorkflowFailure::Infrastructure(error)
        }
        error @ (HouseholdInvitationError::InvalidPagination
        | HouseholdInvitationError::InvalidCursor) => WorkflowFailure::Infrastructure(error),
    }
}

fn map_list_workflow_failure(
    error: HouseholdInvitationError,
) -> WorkflowFailure<WorkflowRejection, HouseholdInvitationError> {
    match error {
        HouseholdInvitationError::HouseholdNotFound | HouseholdInvitationError::MemberNotFound => {
            WorkflowFailure::NotFound
        }
        HouseholdInvitationError::InvalidPagination => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidPagination)
        }
        HouseholdInvitationError::InvalidCursor => {
            WorkflowFailure::Decision(WorkflowRejection::InvalidCursor)
        }
        error => WorkflowFailure::Infrastructure(error),
    }
}

struct PostgresRevokeInvitationOperations<'a> {
    conn: &'a mut crate::AsyncPgConnection,
    row: Option<RevokeInvitationRow>,
}

#[derive(diesel::Queryable)]
struct RevokeInvitationRow {
    created_at: DateTime<Utc>,
    expires_at: DateTime<Utc>,
    accepted_at: Option<DateTime<Utc>>,
    revoked_at: Option<DateTime<Utc>>,
}

impl RevokeInvitationOperations for PostgresRevokeInvitationOperations<'_> {
    type Error = HouseholdInvitationError;
    type Output = InvitationStateDto;

    async fn load_household(&mut self, household_id: &HouseholdId) -> Result<(), Self::Error> {
        use public::households;
        let household_id = household_id.value();
        households::table
            .filter(households::id.eq(household_id))
            .for_update()
            .select(households::id)
            .first::<Uuid>(&mut *self.conn)
            .await
            .optional()?
            .ok_or(HouseholdInvitationError::HouseholdNotFound)?;
        Ok(())
    }

    async fn load_actor_role(
        &mut self,
        household_id: &HouseholdId,
        actor_id: &ActorId,
    ) -> Result<Option<String>, Self::Error> {
        use public::household_members as members;
        let household_id = household_id.value();
        let actor_id = actor_id.value();
        Ok(members::table
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(actor_id))
            .filter(members::left_at.is_null())
            .for_share()
            .select(members::role)
            .first::<String>(&mut *self.conn)
            .await
            .optional()?)
    }

    async fn load_invitation(
        &mut self,
        household_id: &HouseholdId,
        invitation_id: &InvitationId,
    ) -> Result<(DateTime<Utc>, DateTime<Utc>, bool, bool), Self::Error> {
        use public::household_invitations as invitations;
        let household_id = household_id.value();
        let invitation_id = invitation_id.value();
        let row = invitations::table
            .filter(invitations::household_id.eq(household_id))
            .filter(invitations::id.eq(invitation_id))
            .for_update()
            .select((
                invitations::created_at,
                invitations::expires_at,
                invitations::accepted_at,
                invitations::revoked_at,
            ))
            .first::<RevokeInvitationRow>(&mut *self.conn)
            .await
            .optional()?
            .ok_or(HouseholdInvitationError::InvitationNotFound)?;
        let facts = (
            row.created_at,
            row.expires_at,
            row.accepted_at.is_some(),
            row.revoked_at.is_some(),
        );
        self.row = Some(row);
        Ok(facts)
    }

    async fn persist(
        &mut self,
        decision: RevokeInvitationDecision,
        now_value: DateTime<Utc>,
    ) -> Result<Self::Output, Self::Error> {
        use public::household_invitations as invitations;
        let household_id = decision.household_id.value();
        let actor_id = decision.actor_id.value();
        let invitation_id = decision.invitation_id.value();
        let row = self
            .row
            .take()
            .ok_or(HouseholdInvitationError::InvitationNotFound)?;
        if !decision.already_revoked {
            diesel::update(invitations::table.filter(invitations::id.eq(invitation_id)))
                .set(invitations::revoked_at.eq(Some(now_value)))
                .execute(&mut *self.conn)
                .await?;
            record_change(
                self.conn,
                household_id,
                invitation_id,
                actor_id,
                invitation_revoked_change(),
            )
            .await?;
        }
        Ok(InvitationStateDto {
            id: invitation_id.to_string(),
            household_id: household_id.to_string(),
            created_at: row.created_at,
            expires_at: row.expires_at,
            accepted_at: row.accepted_at,
            revoked_at: if decision.already_revoked {
                row.revoked_at
            } else {
                Some(now_value)
            },
        })
    }
}

async fn record_change(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    entity_id: Uuid,
    actor_id: Uuid,
    change: UiChangeDescriptor,
) -> Result<(), HouseholdInvitationError> {
    use public::households;
    let revision = diesel::update(households::table.filter(households::id.eq(household_id)))
        .set((
            households::revision.eq(households::revision + 1),
            households::updated_at.eq(now),
        ))
        .returning(households::revision)
        .get_result::<i64>(&mut *conn)
        .await?;
    horse_postgres_common::insert_ui_change(
        conn,
        household_id,
        entity_id,
        revision,
        actor_id,
        Utc::now(),
        change,
    )
    .await?;
    Ok(())
}

fn map_workflow_error(error: InvitationError) -> HouseholdInvitationError {
    match error {
        InvitationError::Forbidden => HouseholdInvitationError::MemberNotFound,
        InvitationError::NotFound => HouseholdInvitationError::InvitationNotFound,
        InvitationError::Expired
        | InvitationError::Revoked
        | InvitationError::AlreadyAccepted
        | InvitationError::AlreadyMember => HouseholdInvitationError::Conflict,
        other => HouseholdInvitationError::Workflow(other),
    }
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, HouseholdInvitationError> {
    Uuid::parse_str(value).map_err(|_| HouseholdInvitationError::InvalidUuid(field))
}
