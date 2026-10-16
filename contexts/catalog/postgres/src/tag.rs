use catalog_application::workflow_failure::WorkflowFailure;
use catalog_domain::tag::{
    CreateTagCommand, SetTagArchivedCommand, Tag, TagError, TagName, UpdateTagCommand,
    decide_create_tag, decide_set_tag_archived, decide_update_tag,
};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use horse_household_change::UiChangeDescriptor;
use horse_postgres_common::insert_ui_change;
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

impl TryFrom<PersistedTag> for Tag {
    type Error = TagPersistenceError;

    fn try_from(tag: PersistedTag) -> Result<Self, Self::Error> {
        Self::reconstitute(
            tag.id.to_string(),
            tag.household_id.to_string(),
            tag.name,
            tag.archived,
            tag.revision,
        )
        .map_err(|_| TagPersistenceError::CorruptStoredData)
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
    CorruptStoredData,
}

impl From<DieselError> for TagPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_create_tag(
    pool: &PgPool,
    command: CreateTagCommand,
) -> Result<Tag, WorkflowFailure<TagError, TagPersistenceError>> {
    TagName::new(&command.name).map_err(TagPersistenceError::InvalidWorkflow)?;
    let tag_id = Uuid::now_v7();
    let decision =
        decide_create_tag(command, tag_id.into()).map_err(TagPersistenceError::InvalidWorkflow)?;
    let actor_id = parse_uuid(decision.actor_id.as_str(), "actor_id")?;
    let household_id = parse_uuid(decision.household_id.as_str(), "household_id")?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| TagPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<Tag, TagPersistenceError, _>(async move |conn| {
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
            require_member(conn, household_id, actor_id).await?;

            diesel::insert_into(tags::tags)
                .values((
                    tags::id.eq(tag_id),
                    tags::household_id.eq(household_id),
                    tags::name.eq(decision.name.as_str()),
                    tags::revision.eq(decision.revision.as_i64()),
                ))
                .execute(&mut *conn)
                .await?;

            insert_ui_change(
                conn,
                household_id,
                tag_id,
                decision.revision.as_i64(),
                actor_id,
                chrono::Utc::now(),
                decision.ui_change(),
            )
            .await?;

            PersistedTag {
                id: tag_id,
                household_id,
                name: decision.name.as_str().to_owned(),
                archived: false,
                revision: decision.revision.as_i64(),
            }
            .try_into()
        })
        .await
        .map_err(map_workflow_failure)
}

pub async fn persist_update_tag(
    pool: &PgPool,
    command: UpdateTagCommand,
) -> Result<Tag, WorkflowFailure<TagError, TagPersistenceError>> {
    TagName::new(&command.name).map_err(TagPersistenceError::InvalidWorkflow)?;
    let actor_id = parse_uuid(command.actor_id.as_str(), "actor_id")?;
    let tag_id = parse_uuid(command.tag_id.as_str(), "tag_id")?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| TagPersistenceError::Pool(error.to_string()))?;
    (*pooled)
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

            let current: Tag = PersistedTag {
                id: row.0,
                household_id: row.1,
                name: row.2,
                archived: row.3,
                revision: row.4,
            }
            .try_into()?;
            let decision = decide_update_tag(command, current).map_err(map_workflow_error)?;
            let new_revision = decision.tag.revision.as_i64();
            let change = decision.ui_change();

            let updated = diesel::update(tags::tags.filter(tags::id.eq(tag_id)))
                .set((
                    tags::name.eq(decision.tag.name.as_str()),
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

            record_event(conn, updated.1, updated.0, actor_id, new_revision, change).await?;

            PersistedTag {
                id: updated.0,
                household_id: updated.1,
                name: updated.2,
                archived: updated.3,
                revision: updated.4,
            }
            .try_into()
        })
        .await
        .map_err(map_workflow_failure)
}

pub async fn persist_set_tag_archived(
    pool: &PgPool,
    command: SetTagArchivedCommand,
) -> Result<Tag, WorkflowFailure<TagError, TagPersistenceError>> {
    let actor_id = parse_uuid(command.actor_id.as_str(), "actor_id")?;
    let tag_id = parse_uuid(command.tag_id.as_str(), "tag_id")?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| TagPersistenceError::Pool(error.to_string()))?;
    (*pooled)
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

            let current: Tag = PersistedTag {
                id: row.0,
                household_id: row.1,
                name: row.2,
                archived: row.3,
                revision: row.4,
            }
            .try_into()?;
            let decision = decide_set_tag_archived(command, current).map_err(map_workflow_error)?;
            let new_revision = decision.tag.revision.as_i64();
            let change = decision.ui_change();

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

            record_event(conn, updated.1, updated.0, actor_id, new_revision, change).await?;

            PersistedTag {
                id: updated.0,
                household_id: updated.1,
                name: updated.2,
                archived: updated.3,
                revision: updated.4,
            }
            .try_into()
        })
        .await
        .map_err(map_workflow_failure)
}

async fn require_member(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<(), TagPersistenceError> {
    use public::household_members::dsl as members;

    let role = members::household_members
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .filter(members::left_at.is_null())
        .select(members::role)
        .first::<String>(conn)
        .await
        .optional()?;
    if authorize(role.as_deref(), Operation::FinancialData).is_err() {
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
    change: UiChangeDescriptor,
) -> Result<(), DieselError> {
    insert_ui_change(
        conn,
        household_id,
        tag_id,
        revision,
        actor_id,
        chrono::Utc::now(),
        change,
    )
    .await?;
    Ok(())
}

fn map_workflow_error(error: TagError) -> TagPersistenceError {
    match error {
        TagError::RevisionConflict => TagPersistenceError::RevisionConflict,
        other => TagPersistenceError::InvalidWorkflow(other),
    }
}

fn map_workflow_failure(
    error: TagPersistenceError,
) -> WorkflowFailure<TagError, TagPersistenceError> {
    match error {
        TagPersistenceError::InvalidUuid(_) => WorkflowFailure::InvalidInput,
        TagPersistenceError::InvalidWorkflow(error) => WorkflowFailure::Decision(error),
        TagPersistenceError::HouseholdNotFound | TagPersistenceError::TagNotFound => {
            WorkflowFailure::NotFound
        }
        TagPersistenceError::RevisionConflict => WorkflowFailure::Conflict,
        error @ (TagPersistenceError::Pool(_)
        | TagPersistenceError::Database(_)
        | TagPersistenceError::CorruptStoredData) => WorkflowFailure::Infrastructure(error),
    }
}

impl From<TagPersistenceError> for WorkflowFailure<TagError, TagPersistenceError> {
    fn from(error: TagPersistenceError) -> Self {
        map_workflow_failure(error)
    }
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, TagPersistenceError> {
    Uuid::parse_str(value).map_err(|_| TagPersistenceError::InvalidUuid(field))
}
