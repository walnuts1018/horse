use chrono::{DateTime, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use household_workflows::mail_forwarding::{
    self, Command, ForwardingAddress, IssueCommand, Status,
};
use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum MailForwardingError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    Workflow(mail_forwarding::Error),
    HouseholdNotFound,
    ForwardingNotFound,
    Conflict,
}

impl From<DieselError> for MailForwardingError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn active(pool: &PgPool, command: Command) -> Result<Status, MailForwardingError> {
    let household_id = parse_uuid(&command.household_id, "household_id")?;
    let actor_id = parse_uuid(&command.actor_id, "actor_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| MailForwardingError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<Status, MailForwardingError, _>(async |conn| {
            use public::{household_mail_forwardings as forwardings, households};
            households::table
                .filter(households::id.eq(household_id))
                .for_share()
                .select(households::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .ok_or(MailForwardingError::HouseholdNotFound)?;
            require_owner(conn, household_id, actor_id).await?;
            let row = forwardings::table
                .filter(forwardings::household_id.eq(household_id))
                .filter(forwardings::revoked_at.is_null())
                .select((forwardings::id, forwardings::created_at))
                .first::<(Uuid, DateTime<Utc>)>(&mut *conn)
                .await
                .optional()?
                .ok_or(MailForwardingError::ForwardingNotFound)?;
            Ok(Status {
                id: row.0.to_string(),
                active: true,
                created_at: row.1,
            })
        })
        .await
}

pub async fn issue(
    pool: &PgPool,
    command: IssueCommand,
) -> Result<ForwardingAddress, MailForwardingError> {
    create(pool, command, false).await
}

pub async fn rotate(
    pool: &PgPool,
    command: IssueCommand,
) -> Result<ForwardingAddress, MailForwardingError> {
    create(pool, command, true).await
}

async fn create(
    pool: &PgPool,
    command: IssueCommand,
    rotate: bool,
) -> Result<ForwardingAddress, MailForwardingError> {
    let household_id = parse_uuid(&command.command.household_id, "household_id")?;
    let actor_id = parse_uuid(&command.command.actor_id, "actor_id")?;
    let mut token_bytes = [0_u8; 32];
    for chunk in token_bytes.chunks_exact_mut(8) {
        chunk.copy_from_slice(&Uuid::new_v4().as_bytes()[..8]);
    }
    let token = base32_lower(&token_bytes);
    let token_hash: [u8; 32] = Sha256::digest(token_bytes).into();
    let forwarding_id = Uuid::now_v7();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| MailForwardingError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<ForwardingAddress, MailForwardingError, _>(async |conn| {
            use public::{household_mail_forwardings as forwardings, households};
            households::table
                .filter(households::id.eq(household_id))
                .for_update()
                .select(households::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .ok_or(MailForwardingError::HouseholdNotFound)?;
            let role = owner_role(conn, household_id, actor_id).await?;
            let active = forwardings::table
                .filter(forwardings::household_id.eq(household_id))
                .filter(forwardings::revoked_at.is_null())
                .for_update()
                .select((forwardings::id, forwardings::created_at))
                .first::<(Uuid, DateTime<Utc>)>(&mut *conn)
                .await
                .optional()?;
            let decision = mail_forwarding::decide_issue(
                command.clone(),
                role.as_deref(),
                active.is_some(),
                forwarding_id,
                &token,
                rotate,
            )
            .map_err(map_workflow_error)?;
            if let Some((active_id, _)) = active {
                diesel::update(forwardings::table.filter(forwardings::id.eq(active_id)))
                    .set(forwardings::revoked_at.eq(Some(Utc::now())))
                    .execute(&mut *conn)
                    .await?;
            }
            let created = diesel::insert_into(forwardings::table)
                .values((
                    forwardings::id.eq(forwarding_id),
                    forwardings::household_id.eq(household_id),
                    forwardings::token_hash.eq(token_hash.to_vec()),
                    forwardings::created_by.eq(actor_id),
                ))
                .returning(forwardings::created_at)
                .get_result::<DateTime<Utc>>(&mut *conn)
                .await?;
            record_change(
                conn,
                household_id,
                forwarding_id,
                actor_id,
                decision.event_type,
            )
            .await?;
            Ok(ForwardingAddress {
                id: forwarding_id.to_string(),
                address: decision.address,
                created_at: created,
            })
        })
        .await
}

pub async fn revoke(pool: &PgPool, command: Command) -> Result<Status, MailForwardingError> {
    let household_id = parse_uuid(&command.household_id, "household_id")?;
    let actor_id = parse_uuid(&command.actor_id, "actor_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| MailForwardingError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<Status, MailForwardingError, _>(async |conn| {
            use public::{household_mail_forwardings as forwardings, households};
            households::table
                .filter(households::id.eq(household_id))
                .for_update()
                .select(households::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .ok_or(MailForwardingError::HouseholdNotFound)?;
            let role = owner_role(conn, household_id, actor_id).await?;
            let active = forwardings::table
                .filter(forwardings::household_id.eq(household_id))
                .filter(forwardings::revoked_at.is_null())
                .for_update()
                .select((forwardings::id, forwardings::created_at))
                .first::<(Uuid, DateTime<Utc>)>(&mut *conn)
                .await
                .optional()?;
            mail_forwarding::decide_revoke(command, role.as_deref(), active.is_some())
                .map_err(map_workflow_error)?;
            let (forwarding_id, created_at) =
                active.ok_or(MailForwardingError::ForwardingNotFound)?;
            let affected = diesel::update(
                forwardings::table
                    .filter(forwardings::id.eq(forwarding_id))
                    .filter(forwardings::revoked_at.is_null()),
            )
            .set(forwardings::revoked_at.eq(Some(Utc::now())))
            .execute(&mut *conn)
            .await?;
            if affected != 1 {
                return Err(MailForwardingError::Conflict);
            }
            record_change(
                conn,
                household_id,
                forwarding_id,
                actor_id,
                "household.mail_forwarding.revoked",
            )
            .await?;
            Ok(Status {
                id: forwarding_id.to_string(),
                active: false,
                created_at,
            })
        })
        .await
}

async fn owner_role(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<Option<String>, MailForwardingError> {
    use public::household_members;
    Ok(household_members::table
        .filter(household_members::household_id.eq(household_id))
        .filter(household_members::member_id.eq(actor_id))
        .filter(household_members::left_at.is_null())
        .for_share()
        .select(household_members::role)
        .first::<String>(&mut *conn)
        .await
        .optional()?)
}

async fn require_owner(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<(), MailForwardingError> {
    let role = owner_role(conn, household_id, actor_id).await?;
    mail_forwarding::decide_active(
        Command {
            actor_id: actor_id.to_string(),
            household_id: household_id.to_string(),
        },
        role.as_deref(),
    )
    .map(|_| ())
    .map_err(map_workflow_error)
}

async fn record_change(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    entity_id: Uuid,
    actor_id: Uuid,
    event_type: &str,
) -> Result<(), MailForwardingError> {
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
            events::entity_kind.eq("mail_forwarding"),
            events::entity_id.eq(entity_id),
            events::revision.eq(revision),
            events::changed_fields.eq(vec![Some("mailForwardingAddress".to_owned())]),
            events::actor_id.eq(actor_id),
            events::created_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;
    Ok(())
}

fn map_workflow_error(error: mail_forwarding::Error) -> MailForwardingError {
    match error {
        mail_forwarding::Error::Forbidden | mail_forwarding::Error::NotActive => {
            MailForwardingError::ForwardingNotFound
        }
        mail_forwarding::Error::AlreadyActive => MailForwardingError::Conflict,
        other => MailForwardingError::Workflow(other),
    }
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, MailForwardingError> {
    Uuid::parse_str(value).map_err(|_| MailForwardingError::InvalidUuid(field))
}

fn base32_lower(bytes: &[u8]) -> String {
    const ALPHABET: &[u8; 32] = b"abcdefghijklmnopqrstuvwxyz234567";
    let mut output = String::with_capacity(bytes.len() * 8 / 5 + 1);
    let mut buffer = 0_u16;
    let mut bits = 0_u8;
    for byte in bytes {
        buffer = (buffer << 8) | u16::from(*byte);
        bits += 8;
        while bits >= 5 {
            bits -= 5;
            output.push(ALPHABET[usize::from((buffer >> bits) & 0x1f)] as char);
        }
    }
    if bits > 0 {
        output.push(ALPHABET[usize::from((buffer << (5 - bits)) & 0x1f)] as char);
    }
    output
}
