use catalog_application::create_product::{
    CreateConceptCommand, CreateProductError, CreateVariantCommand, decide_create_concept,
    decide_create_variant,
};
use chrono::Utc;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
struct PersistedConcept {
    id: Uuid,
    household_id: Uuid,
    name: String,
    normalized_name: String,
    category_id: Option<Uuid>,
}

#[derive(Debug)]
struct PersistedVariant {
    id: Uuid,
    household_id: Uuid,
    concept_id: Uuid,
    name: String,
    normalized_name: String,
    brand: String,
    package_size: String,
    barcode: Option<String>,
}

#[derive(Debug)]
pub enum CreateProductPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidWorkflow(CreateProductError),
    HouseholdNotFound,
    MemberNotFound,
    CategoryNotFound,
    ConceptNotFound,
}

impl From<DieselError> for CreateProductPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_create_concept(
    pool: &PgPool,
    command: CreateConceptCommand,
) -> Result<String, CreateProductPersistenceError> {
    let decision =
        decide_create_concept(command).map_err(CreateProductPersistenceError::InvalidWorkflow)?;
    let actor_id = parse_uuid(&decision.actor_id, "actor_id")?;
    let household_id = parse_uuid(&decision.household_id, "household_id")?;
    let concept = PersistedConcept {
        id: Uuid::now_v7(),
        household_id,
        name: decision.name,
        normalized_name: decision.normalized_name,
        category_id: decision
            .category_id
            .as_deref()
            .map(|value| parse_uuid(value, "category_id"))
            .transpose()?,
    };

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CreateProductPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<String, CreateProductPersistenceError, _>(async |conn| {
            use public::categories::dsl as categories;
            use public::household_events::dsl as events;
            use public::household_members::dsl as members;
            use public::households::dsl as households;
            use public::product_concepts::dsl as concepts;

            let household_exists = households::households
                .filter(households::id.eq(household_id))
                .for_update()
                .select(households::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .is_some();
            if !household_exists {
                return Err(CreateProductPersistenceError::HouseholdNotFound);
            }

            let member_exists = diesel::select(diesel::dsl::exists(
                members::household_members
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq(actor_id))
                    .filter(members::left_at.is_null()),
            ))
            .first::<bool>(conn)
            .await?;
            if !member_exists {
                return Err(CreateProductPersistenceError::MemberNotFound);
            }

            if let Some(category_id) = concept.category_id {
                let category_exists = categories::categories
                    .filter(categories::id.eq(category_id))
                    .filter(categories::household_id.eq(concept.household_id))
                    .select(categories::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .is_some();
                if !category_exists {
                    return Err(CreateProductPersistenceError::CategoryNotFound);
                }
            }

            diesel::insert_into(concepts::product_concepts)
                .values((
                    concepts::id.eq(concept.id),
                    concepts::household_id.eq(concept.household_id),
                    concepts::name.eq(&concept.name),
                    concepts::normalized_name.eq(&concept.normalized_name),
                    concepts::category_id.eq(concept.category_id),
                ))
                .execute(&mut *conn)
                .await?;

            diesel::insert_into(events::household_events)
                .values((
                    events::household_id.eq(household_id),
                    events::event_type.eq("product.concept.created"),
                    events::entity_kind.eq("product_concept"),
                    events::entity_id.eq(concept.id),
                    events::revision.eq(1_i64),
                    events::changed_fields
                        .eq(vec![Some("name".to_owned()), Some("categoryId".to_owned())]),
                    events::actor_id.eq(actor_id),
                    events::created_at.eq(Utc::now()),
                ))
                .execute(&mut *conn)
                .await?;
            Ok(concept.id.to_string())
        })
        .await
}

pub async fn persist_create_variant(
    pool: &PgPool,
    command: CreateVariantCommand,
) -> Result<String, CreateProductPersistenceError> {
    let decision =
        decide_create_variant(command).map_err(CreateProductPersistenceError::InvalidWorkflow)?;
    let actor_id = parse_uuid(&decision.actor_id, "actor_id")?;
    let household_id = parse_uuid(&decision.household_id, "household_id")?;
    let variant = PersistedVariant {
        id: Uuid::now_v7(),
        household_id,
        concept_id: parse_uuid(&decision.concept_id, "concept_id")?,
        name: decision.name,
        normalized_name: decision.normalized_name,
        brand: decision.brand.unwrap_or_default(),
        package_size: decision.package_size.unwrap_or_default(),
        barcode: decision.barcode,
    };

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CreateProductPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<String, CreateProductPersistenceError, _>(async |conn| {
            use public::household_events::dsl as events;
            use public::household_members::dsl as members;
            use public::households::dsl as households;
            use public::product_concepts::dsl as concepts;
            use public::product_variants::dsl as variants;

            let household_exists = households::households
                .filter(households::id.eq(household_id))
                .for_update()
                .select(households::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .is_some();
            if !household_exists {
                return Err(CreateProductPersistenceError::HouseholdNotFound);
            }

            let member_exists = diesel::select(diesel::dsl::exists(
                members::household_members
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq(actor_id))
                    .filter(members::left_at.is_null()),
            ))
            .first::<bool>(conn)
            .await?;
            if !member_exists {
                return Err(CreateProductPersistenceError::MemberNotFound);
            }

            let concept_exists = concepts::product_concepts
                .filter(concepts::id.eq(variant.concept_id))
                .filter(concepts::household_id.eq(variant.household_id))
                .filter(concepts::archived.eq(false))
                .select(concepts::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .is_some();
            if !concept_exists {
                return Err(CreateProductPersistenceError::ConceptNotFound);
            }

            diesel::insert_into(variants::product_variants)
                .values((
                    variants::id.eq(variant.id),
                    variants::household_id.eq(variant.household_id),
                    variants::concept_id.eq(variant.concept_id),
                    variants::name.eq(&variant.name),
                    variants::normalized_name.eq(&variant.normalized_name),
                    variants::brand.eq(&variant.brand),
                    variants::package_size.eq(&variant.package_size),
                    variants::barcode.eq(variant.barcode.as_deref()),
                ))
                .execute(&mut *conn)
                .await?;

            diesel::insert_into(events::household_events)
                .values((
                    events::household_id.eq(household_id),
                    events::event_type.eq("product.variant.created"),
                    events::entity_kind.eq("product_variant"),
                    events::entity_id.eq(variant.id),
                    events::revision.eq(1_i64),
                    events::changed_fields
                        .eq(vec![Some("name".to_owned()), Some("conceptId".to_owned())]),
                    events::actor_id.eq(actor_id),
                    events::created_at.eq(Utc::now()),
                ))
                .execute(&mut *conn)
                .await?;
            Ok(variant.id.to_string())
        })
        .await
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, CreateProductPersistenceError> {
    Uuid::parse_str(value).map_err(|_| CreateProductPersistenceError::InvalidUuid(field))
}
