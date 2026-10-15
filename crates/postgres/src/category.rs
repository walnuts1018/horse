use catalog_workflows::category::{
    Category, CategoryError, CategoryEvent, CategoryName, CreateCategoryCommand,
    SetCategoryArchivedCommand, UpdateCategoryNameCommand, decide_create_category,
    decide_set_category_archived, decide_update_category_name,
};
use chrono::Utc;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
struct PersistedCategory {
    id: Uuid,
    household_id: Uuid,
    parent_id: Option<Uuid>,
    name: String,
    archived: bool,
}

impl TryFrom<PersistedCategory> for Category {
    type Error = CategoryPersistenceError;

    // DB復元値を無検証で信用せず、Smart Constructorを通して制約を再確認する。
    fn try_from(category: PersistedCategory) -> Result<Self, Self::Error> {
        CategoryName::new(&category.name).map_err(|_| CategoryPersistenceError::CorruptStoredData)?;
        Ok(Self {
            id: category.id.to_string(),
            household_id: category.household_id.to_string(),
            parent_id: category.parent_id.map(|id| id.to_string()),
            name: category.name,
            archived: category.archived,
        })
    }
}

#[derive(Debug)]
pub enum CategoryPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidWorkflow(CategoryError),
    HouseholdNotFound,
    CategoryNotFound,
    // 保存済みデータがドメイン制約を満たさない場合。入力不良や競合とは区別する。
    CorruptStoredData,
}

impl From<DieselError> for CategoryPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_create_category(
    pool: &PgPool,
    command: CreateCategoryCommand,
) -> Result<Category, CategoryPersistenceError> {
    let decision =
        decide_create_category(command).map_err(CategoryPersistenceError::InvalidWorkflow)?;
    let actor_id = parse_uuid(decision.actor_id.as_str(), "actor_id")?;
    let household_id = parse_uuid(decision.household_id.as_str(), "household_id")?;
    let category_id = parse_uuid(decision.category_id.as_str(), "category_id")?;
    let parent_id = decision
        .parent_id
        .as_ref()
        .map(|value| parse_uuid(value.as_str(), "parent_id"))
        .transpose()?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CategoryPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<Category, CategoryPersistenceError, _>(async |conn| {
            use public::categories::dsl as categories;
            use public::household_events::dsl as events;
            use public::households::dsl as households;

            households::households
                .filter(households::id.eq(household_id))
                .for_update()
                .select(households::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .ok_or(CategoryPersistenceError::HouseholdNotFound)?;
            require_member(conn, household_id, actor_id).await?;

            if let Some(parent_id) = parent_id {
                categories::categories
                    .filter(categories::id.eq(parent_id))
                    .filter(categories::household_id.eq(household_id))
                    .select(categories::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .ok_or(CategoryPersistenceError::CategoryNotFound)?;
            }

            diesel::insert_into(categories::categories)
                .values((
                    categories::id.eq(category_id),
                    categories::household_id.eq(household_id),
                    categories::parent_id.eq(parent_id),
                    categories::name.eq(decision.name.as_str()),
                ))
                .execute(&mut *conn)
                .await?;

            diesel::insert_into(events::household_events)
                .values((
                    events::household_id.eq(household_id),
                    events::event_type.eq(decision.event.event_type()),
                    events::entity_kind.eq("category"),
                    events::entity_id.eq(parse_uuid(
                        decision.event.category_id().as_str(),
                        "category_id",
                    )?),
                    // 新規作成時の初回イベントなのでシーケンスは1から始まる。
                    events::revision.eq(1_i64),
                    events::changed_fields.eq(event_fields(&decision.event)),
                    events::actor_id.eq(actor_id),
                    events::created_at.eq(Utc::now()),
                ))
                .execute(&mut *conn)
                .await?;

            Ok(Category::try_from(PersistedCategory {
                id: category_id,
                household_id,
                parent_id,
                name: decision.name.as_str().to_owned(),
                archived: false,
            })?)
        })
        .await
}

pub async fn persist_update_category_name(
    pool: &PgPool,
    command: UpdateCategoryNameCommand,
) -> Result<Category, CategoryPersistenceError> {
    let decision =
        decide_update_category_name(command).map_err(CategoryPersistenceError::InvalidWorkflow)?;
    let actor_id = parse_uuid(decision.actor_id.as_str(), "actor_id")?;
    let category_id = parse_uuid(decision.category_id.as_str(), "category_id")?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CategoryPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<Category, CategoryPersistenceError, _>(async |conn| {
            use public::categories::dsl as categories;
            use public::household_events::dsl as events;

            let row = categories::categories
                .filter(categories::id.eq(category_id))
                .for_update()
                .select((
                    categories::household_id,
                    categories::parent_id,
                    categories::name,
                    categories::archived,
                ))
                .first::<(Uuid, Option<Uuid>, String, bool)>(conn)
                .await
                .optional()?
                .ok_or(CategoryPersistenceError::CategoryNotFound)?;
            require_member(conn, row.0, actor_id).await?;

            if row.2 != decision.name.as_str() {
                diesel::update(
                    categories::categories
                        .filter(categories::id.eq(category_id))
                        .filter(categories::household_id.eq(row.0)),
                )
                .set(categories::name.eq(decision.name.as_str()))
                .execute(&mut *conn)
                .await?;
                let event_revision = next_event_revision(&mut *conn, category_id).await?;
                diesel::insert_into(events::household_events)
                    .values((
                        events::household_id.eq(row.0),
                        events::event_type.eq(decision.event.event_type()),
                        events::entity_kind.eq("category"),
                        events::entity_id.eq(parse_uuid(
                            decision.event.category_id().as_str(),
                            "category_id",
                        )?),
                        events::revision.eq(event_revision),
                        events::changed_fields.eq(event_fields(&decision.event)),
                        events::actor_id.eq(actor_id),
                        events::created_at.eq(Utc::now()),
                    ))
                    .execute(&mut *conn)
                    .await?;
            }

            Ok(Category::try_from(PersistedCategory {
                id: category_id,
                household_id: row.0,
                parent_id: row.1,
                name: decision.name.as_str().to_owned(),
                archived: row.3,
            })?)
        })
        .await
}

pub async fn persist_set_category_archived(
    pool: &PgPool,
    command: SetCategoryArchivedCommand,
) -> Result<Category, CategoryPersistenceError> {
    let decision =
        decide_set_category_archived(command).map_err(CategoryPersistenceError::InvalidWorkflow)?;
    let actor_id = parse_uuid(decision.actor_id.as_str(), "actor_id")?;
    let category_id = parse_uuid(decision.category_id.as_str(), "category_id")?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CategoryPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<Category, CategoryPersistenceError, _>(async |conn| {
            use public::categories::dsl as categories;
            use public::household_events::dsl as events;

            let row = categories::categories
                .filter(categories::id.eq(category_id))
                .for_update()
                .select((
                    categories::household_id,
                    categories::parent_id,
                    categories::name,
                    categories::archived,
                ))
                .first::<(Uuid, Option<Uuid>, String, bool)>(conn)
                .await
                .optional()?
                .ok_or(CategoryPersistenceError::CategoryNotFound)?;
            require_member(conn, row.0, actor_id).await?;

            if row.3 != decision.archived {
                diesel::update(
                    categories::categories
                        .filter(categories::id.eq(category_id))
                        .filter(categories::household_id.eq(row.0)),
                )
                .set(categories::archived.eq(decision.archived))
                .execute(&mut *conn)
                .await?;
                let event_revision = next_event_revision(&mut *conn, category_id).await?;
                diesel::insert_into(events::household_events)
                    .values((
                        events::household_id.eq(row.0),
                        events::event_type.eq(decision.event.event_type()),
                        events::entity_kind.eq("category"),
                        events::entity_id.eq(parse_uuid(
                            decision.event.category_id().as_str(),
                            "category_id",
                        )?),
                        events::revision.eq(event_revision),
                        events::changed_fields.eq(event_fields(&decision.event)),
                        events::actor_id.eq(actor_id),
                        events::created_at.eq(Utc::now()),
                    ))
                    .execute(&mut *conn)
                    .await?;
            }

            Ok(Category::try_from(PersistedCategory {
                id: category_id,
                household_id: row.0,
                parent_id: row.1,
                name: row.2,
                archived: decision.archived,
            })?)
        })
        .await
}

// categoriesテーブルにrevision列がないため、同一エンティティのイベントシーケンスとして採番する。更新対象行をFOR UPDATEでロック済みの同一トランザクション内で取得するため、同時更新との競合は直列化される。
async fn next_event_revision(
    conn: &mut diesel_async::AsyncPgConnection,
    entity_id: Uuid,
) -> Result<i64, CategoryPersistenceError> {
    use public::household_events::dsl as events;

    let max_revision = events::household_events
        .filter(events::entity_kind.eq("category"))
        .filter(events::entity_id.eq(entity_id))
        .select(diesel::dsl::max(events::revision))
        .first::<Option<i64>>(conn)
        .await?;
    Ok(max_revision.unwrap_or(0) + 1)
}

async fn require_member(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<(), CategoryPersistenceError> {
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
        return Err(CategoryPersistenceError::CategoryNotFound);
    }
    Ok(())
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, CategoryPersistenceError> {
    Uuid::parse_str(value).map_err(|_| CategoryPersistenceError::InvalidUuid(field))
}

fn event_fields(event: &CategoryEvent) -> Vec<Option<String>> {
    event
        .changed_fields()
        .iter()
        .map(|field| Some((*field).to_owned()))
        .collect()
}
