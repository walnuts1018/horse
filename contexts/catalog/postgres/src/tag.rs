use catalog_application::tag::{
    CreateTagCommand, SetTagArchivedCommand, Tag, TagError, UpdateTagCommand, decide_create_tag,
    decide_set_tag_archived, decide_update_tag, validate_tag_name,
};
use chrono::Utc;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
struct PersistedTag {
    id: Uuid,
    household_id: Uuid,
    name: String,
    archived: bool,
    revision: i64,
}

impl From<PersistedTag> for Tag {
    fn from(tag: PersistedTag) -> Self {
        Self {
            id: tag.id.to_string(),
            household_id: tag.household_id.to_string(),
            name: tag.name,
            archived: tag.archived,
            revision: tag.revision,
        }
    }
}

#[derive(Debug)]
pub enum TagPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidWorkflow(TagError),
    HouseholdNotFound,
    TagNotFound,
    RevisionConflict,
}

impl From<DieselError> for TagPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_create_tag(
    pool: &PgPool,
    command: CreateTagCommand,
) -> Result<Tag, TagPersistenceError> {
    validate_tag_name(&command.name).map_err(TagPersistenceError::InvalidWorkflow)?;
    let tag_id = Uuid::now_v7();
    let decision = decide_create_tag(command, tag_id.to_string())
        .map_err(TagPersistenceError::InvalidWorkflow)?;
    let actor_id = parse_uuid(&decision.actor_id, "actor_id")?;
    let household_id = parse_uuid(&decision.household_id, "household_id")?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| TagPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<Tag, TagPersistenceError, _>(async move |conn| {
            use public::household_events::dsl as events;
            use public::households::dsl as households;
            use public::tags::dsl as tags;

            households::households
                .filter(households::id.eq(household_id))
                .for_update()
                .select(households::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .ok_or(TagPersistenceError::HouseholdNotFound)?;

            diesel::insert_into(tags::tags)
                .values((
                    tags::id.eq(tag_id),
                    tags::household_id.eq(household_id),
                    tags::name.eq(&decision.name),
                    tags::revision.eq(decision.revision),
                ))
                .execute(&mut *conn)
                .await?;

            diesel::insert_into(events::household_events)
                .values((
                    events::household_id.eq(household_id),
                    events::event_type.eq(decision.event_type),
                    events::entity_kind.eq("tag"),
                    events::entity_id.eq(tag_id),
                    events::revision.eq(decision.revision),
                    events::changed_fields.eq(decision
                        .changed_fields
                        .into_iter()
                        .map(Some)
                        .collect::<Vec<_>>()),
                    events::actor_id.eq(actor_id),
                    events::created_at.eq(Utc::now()),
                ))
                .execute(&mut *conn)
                .await?;

            Ok(PersistedTag {
                id: tag_id,
                household_id,
                name: decision.name,
                archived: false,
                revision: decision.revision,
            }
            .into())
        })
        .await
}

pub async fn persist_update_tag(
    pool: &PgPool,
    command: UpdateTagCommand,
) -> Result<Tag, TagPersistenceError> {
    validate_tag_name(&command.name).map_err(TagPersistenceError::InvalidWorkflow)?;
    let actor_id = parse_uuid(&command.actor_id, "actor_id")?;
    let tag_id = parse_uuid(&command.tag_id, "tag_id")?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| TagPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<Tag, TagPersistenceError, _>(async move |conn| {
            use public::tags::dsl as tags;

            let row = tags::tags
                .filter(tags::id.eq(tag_id))
                .for_update()
                .select((
                    tags::id,
                    tags::household_id,
                    tags::name,
                    tags::archived,
                    tags::revision,
                ))
                .first::<(Uuid, Uuid, String, bool, i64)>(conn)
                .await
                .optional()?
                .ok_or(TagPersistenceError::TagNotFound)?;
            require_member(conn, row.1, actor_id).await?;

            let current = PersistedTag {
                id: row.0,
                household_id: row.1,
                name: row.2,
                archived: row.3,
                revision: row.4,
            }
            .into();
            let decision = decide_update_tag(command, current).map_err(map_workflow_error)?;
            let new_revision = decision.tag.revision;

            let updated = diesel::update(tags::tags.filter(tags::id.eq(tag_id)))
                .set((
                    tags::name.eq(&decision.tag.name),
                    tags::revision.eq(new_revision),
                ))
                .returning((
                    tags::id,
                    tags::household_id,
                    tags::name,
                    tags::archived,
                    tags::revision,
                ))
                .get_result::<(Uuid, Uuid, String, bool, i64)>(&mut *conn)
                .await?;

            record_event(
                conn,
                updated.1,
                updated.0,
                actor_id,
                new_revision,
                decision.event_type,
                decision.changed_fields,
            )
            .await?;

            Ok(PersistedTag {
                id: updated.0,
                household_id: updated.1,
                name: updated.2,
                archived: updated.3,
                revision: updated.4,
            }
            .into())
        })
        .await
}

pub async fn persist_set_tag_archived(
    pool: &PgPool,
    command: SetTagArchivedCommand,
) -> Result<Tag, TagPersistenceError> {
    let actor_id = parse_uuid(&command.actor_id, "actor_id")?;
    let tag_id = parse_uuid(&command.tag_id, "tag_id")?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| TagPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<Tag, TagPersistenceError, _>(async move |conn| {
            use public::tags::dsl as tags;

            let row = tags::tags
                .filter(tags::id.eq(tag_id))
                .for_update()
                .select((
                    tags::id,
                    tags::household_id,
                    tags::name,
                    tags::archived,
                    tags::revision,
                ))
                .first::<(Uuid, Uuid, String, bool, i64)>(conn)
                .await
                .optional()?
                .ok_or(TagPersistenceError::TagNotFound)?;
            require_member(conn, row.1, actor_id).await?;

            let current = PersistedTag {
                id: row.0,
                household_id: row.1,
                name: row.2,
                archived: row.3,
                revision: row.4,
            }
            .into();
            let decision = decide_set_tag_archived(command, current).map_err(map_workflow_error)?;
            let new_revision = decision.tag.revision;

            let updated = diesel::update(tags::tags.filter(tags::id.eq(tag_id)))
                .set((
                    tags::archived.eq(decision.tag.archived),
                    tags::revision.eq(new_revision),
                ))
                .returning((
                    tags::id,
                    tags::household_id,
                    tags::name,
                    tags::archived,
                    tags::revision,
                ))
                .get_result::<(Uuid, Uuid, String, bool, i64)>(&mut *conn)
                .await?;

            record_event(
                conn,
                updated.1,
                updated.0,
                actor_id,
                new_revision,
                decision.event_type,
                decision.changed_fields,
            )
            .await?;

            Ok(PersistedTag {
                id: updated.0,
                household_id: updated.1,
                name: updated.2,
                archived: updated.3,
                revision: updated.4,
            }
            .into())
        })
        .await
}

async fn require_member(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<(), TagPersistenceError> {
    use public::household_members::dsl as members;

    let is_member = diesel::select(diesel::dsl::exists(
        members::household_members
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(actor_id))
            .filter(members::left_at.is_null()),
    ))
    .first::<bool>(conn)
    .await?;
    if !is_member {
        return Err(TagPersistenceError::TagNotFound);
    }
    Ok(())
}

async fn record_event(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    tag_id: Uuid,
    actor_id: Uuid,
    revision: i64,
    event_type: &'static str,
    changed_fields: Vec<String>,
) -> Result<(), DieselError> {
    use public::household_events::dsl as events;

    diesel::insert_into(events::household_events)
        .values((
            events::household_id.eq(household_id),
            events::event_type.eq(event_type),
            events::entity_kind.eq("tag"),
            events::entity_id.eq(tag_id),
            events::revision.eq(revision),
            events::changed_fields.eq(changed_fields.into_iter().map(Some).collect::<Vec<_>>()),
            events::actor_id.eq(actor_id),
            events::created_at.eq(Utc::now()),
        ))
        .execute(&mut *conn)
        .await?;
    Ok(())
}

fn map_workflow_error(error: TagError) -> TagPersistenceError {
    match error {
        TagError::RevisionConflict => TagPersistenceError::RevisionConflict,
        other => TagPersistenceError::InvalidWorkflow(other),
    }
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, TagPersistenceError> {
    Uuid::parse_str(value).map_err(|_| TagPersistenceError::InvalidUuid(field))
}
