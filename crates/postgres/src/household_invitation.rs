use chrono::{DateTime, Utc};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, OptionalExtension, QueryDsl, dsl::now,
    result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use household_workflows::invitation::{
    AcceptInvitationCommand, CreateInvitationCommand, InvitationError, RevokeInvitationCommand,
    decide_accept_invitation, decide_create_invitation, decide_revoke_invitation,
};
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
    if owner.as_deref() != Some("OWNER") {
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
            .order_by((invitations::created_at.asc(), invitations::id.asc()))
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

pub async fn create_invitation(
    pool: &PgPool,
    command: CreateInvitationCommand,
    token: String,
    token_hash: [u8; 32],
    now_value: DateTime<Utc>,
) -> Result<CreatedInvitationDto, HouseholdInvitationError> {
    let household_id = parse_uuid(&command.household_id, "household_id")?;
    let actor_id = parse_uuid(&command.actor_id, "actor_id")?;
    let invitation_id = parse_uuid(&command.invitation_id, "invitation_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdInvitationError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<CreatedInvitationDto, HouseholdInvitationError, _>(async |conn| {
            use public::{
                household_invitations as invitations, household_members as members, households,
            };
            households::table
                .filter(households::id.eq(household_id))
                .for_update()
                .select(households::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .ok_or(HouseholdInvitationError::HouseholdNotFound)?;
            let actor_role = members::table
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(actor_id))
                .filter(members::left_at.is_null())
                .select(members::role)
                .first::<String>(&mut *conn)
                .await
                .optional()?;
            let decision = decide_create_invitation(command, actor_role.as_deref(), now_value)
                .map_err(map_workflow_error)?;
            let created = diesel::insert_into(invitations::table)
                .values((
                    invitations::id.eq(invitation_id),
                    invitations::household_id.eq(household_id),
                    invitations::token_hash.eq(token_hash.to_vec()),
                    invitations::expires_at.eq(decision.expires_at),
                    invitations::created_by.eq(actor_id),
                ))
                .returning((invitations::created_at, invitations::expires_at))
                .get_result::<(DateTime<Utc>, DateTime<Utc>)>(&mut *conn)
                .await?;
            record_change(
                conn,
                household_id,
                invitation_id,
                actor_id,
                "invitation",
                "household.invitation.created",
                vec![Some("invitations".to_owned())],
            )
            .await?;
            Ok(CreatedInvitationDto {
                id: invitation_id.to_string(),
                token,
                created_at: created.0,
                expires_at: created.1,
            })
        })
        .await
}

pub async fn accept_invitation(
    pool: &PgPool,
    command: AcceptInvitationCommand,
    token_hash: [u8; 32],
    now_value: DateTime<Utc>,
) -> Result<String, HouseholdInvitationError> {
    let member_id = parse_uuid(&command.member_id, "member_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdInvitationError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<String, HouseholdInvitationError, _>(async |conn| {
            use public::{
                household_invitations as invitations, household_members as members, households,
            };
            let row = invitations::table
                .filter(invitations::token_hash.eq(token_hash.to_vec()))
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
                )>(&mut *conn)
                .await
                .optional()?
                .ok_or(HouseholdInvitationError::InvitationNotFound)?;
            let (invitation_id, household_id, expires_at, revoked_at, accepted_at) = row;
            if command.household_id != household_id.to_string() {
                return Err(HouseholdInvitationError::InvitationNotFound);
            }
            households::table
                .filter(households::id.eq(household_id))
                .for_update()
                .select(households::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .ok_or(HouseholdInvitationError::HouseholdNotFound)?;
            let existing = members::table
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(member_id))
                .for_update()
                .select(members::left_at)
                .first::<Option<DateTime<Utc>>>(&mut *conn)
                .await
                .optional()?;
            let decision = decide_accept_invitation(
                command,
                expires_at,
                revoked_at.is_some(),
                accepted_at.is_some(),
                existing == Some(None),
                now_value,
            )
            .map_err(map_workflow_error)?;
            if decision.restore_membership {
                if existing.is_some() {
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
                    .execute(&mut *conn)
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
                        .get_result::<Uuid>(&mut *conn)
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
                .execute(&mut *conn)
                .await?;
            record_change(
                conn,
                household_id,
                member_id,
                member_id,
                "member",
                "household.member.joined",
                vec![Some("members".to_owned())],
            )
            .await?;
            Ok(household_id.to_string())
        })
        .await
}

pub async fn accept_invitation_by_token(
    pool: &PgPool,
    member_id: String,
    token_hash: [u8; 32],
    now_value: DateTime<Utc>,
) -> Result<String, HouseholdInvitationError> {
    let member_id = parse_uuid(&member_id, "member_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdInvitationError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<String, HouseholdInvitationError, _>(async |conn| {
            use public::{
                household_invitations as invitations, household_members as members, households,
            };
            let row = invitations::table
                .filter(invitations::token_hash.eq(token_hash.to_vec()))
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
                )>(&mut *conn)
                .await
                .optional()?
                .ok_or(HouseholdInvitationError::InvitationNotFound)?;
            let (invitation_id, household_id, expires_at, revoked_at, accepted_at) = row;
            households::table
                .filter(households::id.eq(household_id))
                .for_update()
                .select(households::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .ok_or(HouseholdInvitationError::HouseholdNotFound)?;
            let existing = members::table
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(member_id))
                .for_update()
                .select(members::left_at)
                .first::<Option<DateTime<Utc>>>(&mut *conn)
                .await
                .optional()?;
            let command = AcceptInvitationCommand {
                member_id: member_id.to_string(),
                household_id: household_id.to_string(),
                invitation_id: invitation_id.to_string(),
            };
            let decision = decide_accept_invitation(
                command,
                expires_at,
                revoked_at.is_some(),
                accepted_at.is_some(),
                existing == Some(None),
                now_value,
            )
            .map_err(map_workflow_error)?;
            if decision.restore_membership {
                if existing.is_some() {
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
                    .execute(&mut *conn)
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
                        .get_result::<Uuid>(&mut *conn)
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
                .execute(&mut *conn)
                .await?;
            record_change(
                conn,
                household_id,
                member_id,
                member_id,
                "member",
                "household.member.joined",
                vec![Some("members".to_owned())],
            )
            .await?;
            Ok(household_id.to_string())
        })
        .await
}

pub async fn revoke_invitation(
    pool: &PgPool,
    command: RevokeInvitationCommand,
    now_value: DateTime<Utc>,
) -> Result<InvitationStateDto, HouseholdInvitationError> {
    let household_id = parse_uuid(&command.household_id, "household_id")?;
    let actor_id = parse_uuid(&command.actor_id, "actor_id")?;
    let invitation_id = parse_uuid(&command.invitation_id, "invitation_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdInvitationError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<InvitationStateDto, HouseholdInvitationError, _>(async |conn| {
            use public::{
                household_invitations as invitations, household_members as members, households,
            };
            households::table
                .filter(households::id.eq(household_id))
                .for_update()
                .select(households::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .ok_or(HouseholdInvitationError::HouseholdNotFound)?;
            let actor_role = members::table
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(actor_id))
                .filter(members::left_at.is_null())
                .for_share()
                .select(members::role)
                .first::<String>(&mut *conn)
                .await
                .optional()?;
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
                .first::<(
                    DateTime<Utc>,
                    DateTime<Utc>,
                    Option<DateTime<Utc>>,
                    Option<DateTime<Utc>>,
                )>(&mut *conn)
                .await
                .optional()?
                .ok_or(HouseholdInvitationError::InvitationNotFound)?;
            let decision = decide_revoke_invitation(
                command,
                actor_role.as_deref(),
                row.2.is_some(),
                row.3.is_some(),
            )
            .map_err(map_workflow_error)?;
            if !decision.already_revoked {
                diesel::update(invitations::table.filter(invitations::id.eq(invitation_id)))
                    .set(invitations::revoked_at.eq(Some(now_value)))
                    .execute(&mut *conn)
                    .await?;
                record_change(
                    conn,
                    household_id,
                    invitation_id,
                    actor_id,
                    "invitation",
                    "household.invitation.revoked",
                    vec![Some("invitations".to_owned())],
                )
                .await?;
            } else {
                households::table
                    .filter(households::id.eq(household_id))
                    .select(households::id)
                    .first::<Uuid>(&mut *conn)
                    .await
                    .optional()?
                    .ok_or(HouseholdInvitationError::HouseholdNotFound)?;
            }
            Ok(InvitationStateDto {
                id: invitation_id.to_string(),
                household_id: household_id.to_string(),
                created_at: row.0,
                expires_at: row.1,
                accepted_at: row.2,
                revoked_at: if decision.already_revoked {
                    row.3
                } else {
                    Some(now_value)
                },
            })
        })
        .await
}

async fn record_change(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    entity_id: Uuid,
    actor_id: Uuid,
    entity_kind: &str,
    event_type: &str,
    changed_fields: Vec<Option<String>>,
) -> Result<(), HouseholdInvitationError> {
    use public::{household_events as events, households};
    let revision = diesel::update(households::table.filter(households::id.eq(household_id)))
        .set((
            households::revision.eq(households::revision + 1),
            households::updated_at.eq(now),
        ))
        .returning(households::revision)
        .get_result::<i64>(&mut *conn)
        .await?;
    diesel::insert_into(events::table)
        .values((
            events::household_id.eq(household_id),
            events::event_type.eq(event_type),
            events::entity_kind.eq(entity_kind),
            events::entity_id.eq(entity_id),
            events::revision.eq(revision),
            events::changed_fields.eq(changed_fields),
            events::actor_id.eq(actor_id),
            events::created_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;
    Ok(())
}

fn map_workflow_error(error: InvitationError) -> HouseholdInvitationError {
    match error {
        InvitationError::Forbidden => HouseholdInvitationError::MemberNotFound,
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
