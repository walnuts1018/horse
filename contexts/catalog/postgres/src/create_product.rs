use catalog_application::create_product::CreateProductOperations;
use catalog_application::workflow_failure::WorkflowFailure;
use catalog_domain::product_creation::{
    CreateConceptCommand, CreateConceptDecision, CreateProductError, CreateVariantCommand,
    CreateVariantDecision,
};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use horse_postgres_common::insert_ui_change;
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

pub async fn create_concept(
    pool: &PgPool,
    command: CreateConceptCommand,
) -> Result<
    (String, CreateConceptDecision),
    WorkflowFailure<CreateProductError, CreateProductPersistenceError>,
> {
    let mut operations = PostgresCreateProductOperations { pool };
    catalog_application::create_product::create_concept(&mut operations, command)
        .await
        .map_err(|error| match error {
            catalog_application::create_product::CreateProductWorkflowError::Decision(error) => {
                CreateProductPersistenceError::InvalidWorkflow(error)
            }
            catalog_application::create_product::CreateProductWorkflowError::Operations(error) => {
                error
            }
        })
        .map_err(map_workflow_failure)
}

pub async fn create_variant(
    pool: &PgPool,
    command: CreateVariantCommand,
) -> Result<
    (String, CreateVariantDecision),
    WorkflowFailure<CreateProductError, CreateProductPersistenceError>,
> {
    let mut operations = PostgresCreateProductOperations { pool };
    catalog_application::create_product::create_variant(&mut operations, command)
        .await
        .map_err(|error| match error {
            catalog_application::create_product::CreateProductWorkflowError::Decision(error) => {
                CreateProductPersistenceError::InvalidWorkflow(error)
            }
            catalog_application::create_product::CreateProductWorkflowError::Operations(error) => {
                error
            }
        })
        .map_err(map_workflow_failure)
}

struct PostgresCreateProductOperations<'a> {
    pool: &'a PgPool,
}

impl CreateProductOperations for PostgresCreateProductOperations<'_> {
    type Error = CreateProductPersistenceError;

    async fn persist_concept(
        &mut self,
        decision: CreateConceptDecision,
    ) -> Result<String, Self::Error> {
        persist_create_concept(self.pool, decision).await
    }

    async fn persist_variant(
        &mut self,
        decision: CreateVariantDecision,
    ) -> Result<String, Self::Error> {
        persist_create_variant(self.pool, decision).await
    }
}

impl From<DieselError> for CreateProductPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

fn map_workflow_failure(
    error: CreateProductPersistenceError,
) -> WorkflowFailure<CreateProductError, CreateProductPersistenceError> {
    match error {
        CreateProductPersistenceError::InvalidUuid(_) => WorkflowFailure::InvalidInput,
        CreateProductPersistenceError::InvalidWorkflow(error) => WorkflowFailure::Decision(error),
        CreateProductPersistenceError::HouseholdNotFound
        | CreateProductPersistenceError::MemberNotFound
        | CreateProductPersistenceError::CategoryNotFound
        | CreateProductPersistenceError::ConceptNotFound => WorkflowFailure::NotFound,
        error @ (CreateProductPersistenceError::Pool(_)
        | CreateProductPersistenceError::Database(_)) => WorkflowFailure::Infrastructure(error),
    }
}

async fn persist_create_concept(
    pool: &PgPool,
    decision: CreateConceptDecision,
) -> Result<String, CreateProductPersistenceError> {
    let ui_change = catalog_application::ui_changes::concept_created();
    let actor_id = parse_uuid(decision.actor_id.as_str(), "actor_id")?;
    let household_id = parse_uuid(decision.household_id.as_str(), "household_id")?;
    let concept = PersistedConcept {
        id: Uuid::now_v7(),
        household_id,
        name: decision.name.as_str().to_owned(),
        normalized_name: decision.normalized_name().as_str().to_owned(),
        category_id: decision
            .category_id
            .as_ref()
            .map(|value| parse_uuid(value.as_str(), "category_id"))
            .transpose()?,
    };

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CreateProductPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<String, CreateProductPersistenceError, _>(async |conn| {
            use public::categories::dsl as categories;
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

            let actor_role = members::household_members
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(actor_id))
                .filter(members::left_at.is_null())
                .select(members::role)
                .first::<String>(conn)
                .await
                .optional()?;
            if authorize(actor_role.as_deref(), Operation::FinancialData).is_err() {
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

            insert_ui_change(
                conn,
                household_id,
                concept.id,
                1_i64,
                actor_id,
                chrono::Utc::now(),
                ui_change,
            )
            .await?;
            Ok(concept.id.to_string())
        })
        .await
}

async fn persist_create_variant(
    pool: &PgPool,
    decision: CreateVariantDecision,
) -> Result<String, CreateProductPersistenceError> {
    let ui_change = catalog_application::ui_changes::variant_created();
    let actor_id = parse_uuid(decision.actor_id.as_str(), "actor_id")?;
    let household_id = parse_uuid(decision.household_id.as_str(), "household_id")?;
    let variant = PersistedVariant {
        id: Uuid::now_v7(),
        household_id,
        concept_id: parse_uuid(decision.concept_id.as_str(), "concept_id")?,
        name: decision.name.as_str().to_owned(),
        normalized_name: decision.normalized_name().as_str().to_owned(),
        brand: decision
            .brand
            .map(|value| value.as_str().to_owned())
            .unwrap_or_default(),
        package_size: decision
            .package_size
            .map(|value| value.as_str().to_owned())
            .unwrap_or_default(),
        barcode: decision.barcode.map(|value| value.as_str().to_owned()),
    };

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CreateProductPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<String, CreateProductPersistenceError, _>(async |conn| {
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

            let actor_role = members::household_members
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(actor_id))
                .filter(members::left_at.is_null())
                .select(members::role)
                .first::<String>(conn)
                .await
                .optional()?;
            if authorize(actor_role.as_deref(), Operation::FinancialData).is_err() {
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

            insert_ui_change(
                conn,
                household_id,
                variant.id,
                1_i64,
                actor_id,
                chrono::Utc::now(),
                ui_change,
            )
            .await?;
            Ok(variant.id.to_string())
        })
        .await
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, CreateProductPersistenceError> {
    Uuid::parse_str(value).map_err(|_| CreateProductPersistenceError::InvalidUuid(field))
}
