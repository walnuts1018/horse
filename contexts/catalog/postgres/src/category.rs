use catalog_application::category::{
    ActorId, CategoryError, CategoryEvent, CategoryId, CategoryName, CategoryView,
    CreateCategoryDecision, CreateCategoryOperations, HouseholdId, LoadedCategory,
    SetCategoryArchivedDecision, SetCategoryArchivedOperations, UpdateCategoryNameCommand,
    UpdateCategoryNameDecision, UpdateCategoryNameOperations,
};
use chrono::Utc;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{RunQueryDsl, pooled_connection::bb8::RunError as PoolRunError};
use std::{future::Future, pin::Pin};
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

impl TryFrom<PersistedCategory> for CategoryView {
    type Error = CategoryWorkflowError;

    // DB復元値を無検証で信用せず、Smart Constructorを通して制約を再確認する。
    fn try_from(category: PersistedCategory) -> Result<Self, Self::Error> {
        CategoryView::new(
            category.id.to_string(),
            category.household_id.to_string(),
            category.parent_id.map(|id| id.to_string()),
            category.name,
            category.archived,
        )
        .map_err(|_| CategoryWorkflowError::CorruptStoredData)
    }
}

#[derive(Debug)]
pub enum CategoryWorkflowError {
    // 業務エラー: GraphQLに公開できる。
    InvalidActorId,
    InvalidHouseholdId,
    InvalidCategoryId,
    InvalidParentId,
    InvalidName,
    HouseholdNotFound,
    CategoryNotFound,
    ParentNotFound,
    // 非メンバーであることは存在開示回避のためGraphQLではNotFoundに写像するが、型上は分離する。
    NotAMember,
    // decide通過後のUUID再パース失敗など、ユーザー入力ではなく実装側の不変違反。
    InvariantViolation(&'static str),
    // infra: 非公開。
    Pool(PoolRunError),
    Database(DieselError),
    // 保存済みデータがドメイン制約を満たさない場合。入力不良や競合とは区別する。
    CorruptStoredData,
}

impl From<DieselError> for CategoryWorkflowError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

impl From<PoolRunError> for CategoryWorkflowError {
    fn from(error: PoolRunError) -> Self {
        Self::Pool(error)
    }
}

impl From<CategoryError> for CategoryWorkflowError {
    fn from(error: CategoryError) -> Self {
        match error {
            CategoryError::InvalidActorId => Self::InvalidActorId,
            CategoryError::InvalidHouseholdId => Self::InvalidHouseholdId,
            CategoryError::InvalidCategoryId => Self::InvalidCategoryId,
            CategoryError::InvalidParentId => Self::InvalidParentId,
            CategoryError::InvalidName => Self::InvalidName,
        }
    }
}

pub async fn with_transaction<T>(
    pool: &PgPool,
    operation: impl for<'connection> FnOnce(
        &'connection mut diesel_async::AsyncPgConnection,
    ) -> Pin<
        Box<dyn Future<Output = Result<T, CategoryWorkflowError>> + Send + 'connection>,
    > + Send,
) -> Result<T, CategoryWorkflowError>
where
    T: Send,
{
    let mut pooled = pool.get().await.map_err(CategoryWorkflowError::from)?;
    (&mut *pooled)
        .transaction::<T, CategoryWorkflowError, _>(async |conn| operation(conn).await)
        .await
}

pub struct PostgresCreateCategoryOperations<'a> {
    conn: &'a mut diesel_async::AsyncPgConnection,
}

impl<'a> PostgresCreateCategoryOperations<'a> {
    pub fn new(conn: &'a mut diesel_async::AsyncPgConnection) -> Self {
        Self { conn }
    }
}

impl CreateCategoryOperations for PostgresCreateCategoryOperations<'_> {
    type Error = CategoryWorkflowError;

    async fn load_household(&mut self, household_id: &HouseholdId) -> Result<(), Self::Error> {
        use public::households::dsl as households;

        let household_id = parse_uuid(household_id.as_str(), "household_id")?;
        households::households
            .filter(households::id.eq(household_id))
            .for_update()
            .select(households::id)
            .first::<Uuid>(self.conn)
            .await
            .optional()?
            .ok_or(CategoryWorkflowError::HouseholdNotFound)?;
        Ok(())
    }

    async fn require_member(
        &mut self,
        household_id: &HouseholdId,
        actor_id: &ActorId,
    ) -> Result<(), Self::Error> {
        require_member(
            self.conn,
            parse_uuid(household_id.as_str(), "household_id")?,
            parse_uuid(actor_id.as_str(), "actor_id")?,
        )
        .await
    }

    async fn load_parent(
        &mut self,
        household_id: &HouseholdId,
        parent_id: &CategoryId,
    ) -> Result<(), Self::Error> {
        use public::categories::dsl as categories;

        let household_id = parse_uuid(household_id.as_str(), "household_id")?;
        let parent_id = parse_uuid(parent_id.as_str(), "parent_id")?;
        categories::categories
            .filter(categories::id.eq(parent_id))
            .filter(categories::household_id.eq(household_id))
            .select(categories::id)
            .first::<Uuid>(self.conn)
            .await
            .optional()?
            .ok_or(CategoryWorkflowError::ParentNotFound)?;
        Ok(())
    }

    async fn persist(
        &mut self,
        decision: CreateCategoryDecision,
    ) -> Result<CategoryView, Self::Error> {
        use public::categories::dsl as categories;
        use public::household_events::dsl as events;

        let actor_id = parse_uuid(decision.actor_id.as_str(), "actor_id")?;
        let household_id = parse_uuid(decision.household_id.as_str(), "household_id")?;
        let category_id = parse_uuid(decision.category_id.as_str(), "category_id")?;
        let parent_id = decision
            .parent_id
            .as_ref()
            .map(|value| parse_uuid(value.as_str(), "parent_id"))
            .transpose()?;

        diesel::insert_into(categories::categories)
            .values((
                categories::id.eq(category_id),
                categories::household_id.eq(household_id),
                categories::parent_id.eq(parent_id),
                categories::name.eq(decision.name.as_str()),
            ))
            .execute(self.conn)
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
                events::revision.eq(1_i64),
                events::changed_fields.eq(event_fields(&decision.event)),
                events::actor_id.eq(actor_id),
                events::created_at.eq(Utc::now()),
            ))
            .execute(self.conn)
            .await?;

        Ok(CategoryView::try_from(PersistedCategory {
            id: category_id,
            household_id,
            parent_id,
            name: decision.name.as_str().to_owned(),
            archived: false,
        })?)
    }
}

pub struct PostgresUpdateCategoryNameOperations<'a> {
    conn: &'a mut diesel_async::AsyncPgConnection,
}

impl<'a> PostgresUpdateCategoryNameOperations<'a> {
    pub fn new(conn: &'a mut diesel_async::AsyncPgConnection) -> Self {
        Self { conn }
    }
}

impl UpdateCategoryNameOperations for PostgresUpdateCategoryNameOperations<'_> {
    type Error = CategoryWorkflowError;

    async fn load_category(
        &mut self,
        category_id: &CategoryId,
    ) -> Result<LoadedCategory, Self::Error> {
        use public::categories::dsl as categories;

        let category_id = parse_uuid(category_id.as_str(), "category_id")?;
        let row = categories::categories
            .filter(categories::id.eq(category_id))
            .for_update()
            .select((
                categories::id,
                categories::household_id,
                categories::parent_id,
                categories::name,
                categories::archived,
            ))
            .first::<(Uuid, Uuid, Option<Uuid>, String, bool)>(self.conn)
            .await
            .optional()?
            .ok_or(CategoryWorkflowError::CategoryNotFound)?;
        loaded_category(row)
    }

    async fn require_member(
        &mut self,
        household_id: &HouseholdId,
        actor_id: &ActorId,
    ) -> Result<(), Self::Error> {
        require_member(
            self.conn,
            parse_uuid(household_id.as_str(), "household_id")?,
            parse_uuid(actor_id.as_str(), "actor_id")?,
        )
        .await
    }

    async fn persist(
        &mut self,
        category: LoadedCategory,
        decision: UpdateCategoryNameDecision,
    ) -> Result<CategoryView, Self::Error> {
        use public::categories::dsl as categories;
        use public::household_events::dsl as events;

        let category_id = parse_uuid(category.id.as_str(), "category_id")?;
        let household_id = parse_uuid(category.household_id.as_str(), "household_id")?;
        let actor_id = parse_uuid(decision.actor_id.as_str(), "actor_id")?;
        diesel::update(
            categories::categories
                .filter(categories::id.eq(category_id))
                .filter(categories::household_id.eq(household_id)),
        )
        .set(categories::name.eq(decision.name.as_str()))
        .execute(self.conn)
        .await?;
        let event_revision = next_event_revision(self.conn, category_id).await?;
        diesel::insert_into(events::household_events)
            .values((
                events::household_id.eq(household_id),
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
            .execute(self.conn)
            .await?;

        CategoryView::new(
            category.id.as_str(),
            category.household_id.as_str(),
            category.parent_id.map(|id| id.as_str().to_owned()),
            decision.name.as_str(),
            category.status.is_archived(),
        )
        .map_err(|_| CategoryWorkflowError::InvariantViolation("category"))
    }
}

pub struct PostgresSetCategoryArchivedOperations<'a> {
    conn: &'a mut diesel_async::AsyncPgConnection,
}

impl<'a> PostgresSetCategoryArchivedOperations<'a> {
    pub fn new(conn: &'a mut diesel_async::AsyncPgConnection) -> Self {
        Self { conn }
    }
}

impl SetCategoryArchivedOperations for PostgresSetCategoryArchivedOperations<'_> {
    type Error = CategoryWorkflowError;

    async fn load_category(
        &mut self,
        category_id: &CategoryId,
    ) -> Result<LoadedCategory, Self::Error> {
        use public::categories::dsl as categories;

        let category_id = parse_uuid(category_id.as_str(), "category_id")?;
        let row = categories::categories
            .filter(categories::id.eq(category_id))
            .for_update()
            .select((
                categories::id,
                categories::household_id,
                categories::parent_id,
                categories::name,
                categories::archived,
            ))
            .first::<(Uuid, Uuid, Option<Uuid>, String, bool)>(self.conn)
            .await
            .optional()?
            .ok_or(CategoryWorkflowError::CategoryNotFound)?;
        loaded_category(row)
    }

    async fn require_member(
        &mut self,
        household_id: &HouseholdId,
        actor_id: &ActorId,
    ) -> Result<(), Self::Error> {
        require_member(
            self.conn,
            parse_uuid(household_id.as_str(), "household_id")?,
            parse_uuid(actor_id.as_str(), "actor_id")?,
        )
        .await
    }

    async fn persist(
        &mut self,
        category: LoadedCategory,
        decision: SetCategoryArchivedDecision,
    ) -> Result<CategoryView, Self::Error> {
        use public::categories::dsl as categories;
        use public::household_events::dsl as events;

        let category_id = parse_uuid(category.id.as_str(), "category_id")?;
        let household_id = parse_uuid(category.household_id.as_str(), "household_id")?;
        let actor_id = parse_uuid(decision.actor_id.as_str(), "actor_id")?;
        diesel::update(
            categories::categories
                .filter(categories::id.eq(category_id))
                .filter(categories::household_id.eq(household_id)),
        )
        .set(categories::archived.eq(decision.archived))
        .execute(self.conn)
        .await?;
        let event_revision = next_event_revision(self.conn, category_id).await?;
        diesel::insert_into(events::household_events)
            .values((
                events::household_id.eq(household_id),
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
            .execute(self.conn)
            .await?;

        CategoryView::new(
            category.id.as_str(),
            category.household_id.as_str(),
            category.parent_id.map(|id| id.as_str().to_owned()),
            category.name.as_str(),
            decision.archived,
        )
        .map_err(|_| CategoryWorkflowError::InvariantViolation("category"))
    }
}

fn loaded_category(
    row: (Uuid, Uuid, Option<Uuid>, String, bool),
) -> Result<LoadedCategory, CategoryWorkflowError> {
    Ok(LoadedCategory {
        id: CategoryId::new(row.0.to_string())
            .map_err(|_| CategoryWorkflowError::CorruptStoredData)?,
        household_id: HouseholdId::new(row.1.to_string())
            .map_err(|_| CategoryWorkflowError::CorruptStoredData)?,
        parent_id: row
            .2
            .map(|id| CategoryId::new(id.to_string()))
            .transpose()
            .map_err(|_| CategoryWorkflowError::CorruptStoredData)?,
        name: CategoryName::new(row.3).map_err(|_| CategoryWorkflowError::CorruptStoredData)?,
        status: if row.4 {
            catalog_application::category::CategoryStatus::Archived
        } else {
            catalog_application::category::CategoryStatus::Active
        },
    })
}

// categoriesテーブルにrevision列がないため、同一エンティティのイベントシーケンスとして採番する。更新対象行をFOR UPDATEでロック済みの同一トランザクション内で取得するため、同時更新との競合は直列化される。
async fn next_event_revision(
    conn: &mut diesel_async::AsyncPgConnection,
    entity_id: Uuid,
) -> Result<i64, CategoryWorkflowError> {
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
) -> Result<(), CategoryWorkflowError> {
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
        return Err(CategoryWorkflowError::NotAMember);
    }
    Ok(())
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, CategoryWorkflowError> {
    Uuid::parse_str(value).map_err(|_| CategoryWorkflowError::InvariantViolation(field))
}

fn event_fields(event: &CategoryEvent) -> Vec<Option<String>> {
    event
        .changed_fields()
        .iter()
        .map(|field| Some((*field).to_owned()))
        .collect()
}
