use async_graphql::{Context, ID, InputObject, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use catalog_domain::{
    product_alias::SetProductAliasCommand,
    product_alias::decide_set_product_alias,
    product_creation::{CreateConceptCommand, CreateVariantCommand},
    tag::{CreateTagCommand, SetTagArchivedCommand, Tag as DomainTag, UpdateTagCommand},
};
use catalog_postgres::{
    create_product as postgres_product, product_alias as postgres_alias, tag as postgres_tag,
};
use horse_postgres_common::PgPool;
use uuid::Uuid;

use crate::MutationErrorCode;

#[derive(Clone, Debug, InputObject)]
pub struct CreateTagInput {
    household_id: ID,
    name: String,
}

#[derive(Clone, Debug, InputObject)]
pub struct UpdateTagInput {
    tag_id: ID,
    base_revision: i32,
    name: String,
}

#[derive(Clone, Debug, InputObject)]
pub struct ArchiveTagInput {
    tag_id: ID,
    base_revision: i32,
    archived: bool,
}

#[derive(Clone, Debug, InputObject)]
pub struct CreateProductConceptInput {
    household_id: ID,
    name: String,
    category_id: Option<ID>,
}

#[derive(Clone, Debug, InputObject)]
pub struct CreateProductVariantInput {
    household_id: ID,
    concept_id: ID,
    name: String,
    brand: Option<String>,
    package_size: Option<String>,
    barcode: Option<String>,
}

#[derive(Clone, Debug, InputObject)]
pub struct SetProductAliasInput {
    household_id: ID,
    variant_id: ID,
    merchant: String,
    alias: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct Tag {
    pub(crate) id: ID,
    pub(crate) household_id: ID,
    pub(crate) name: String,
    pub(crate) archived: bool,
    pub(crate) revision: i32,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ProductConcept {
    pub(crate) id: ID,
    pub(crate) household_id: ID,
    pub(crate) name: String,
    pub(crate) normalized_name: String,
    pub(crate) category_id: Option<ID>,
    pub(crate) archived: bool,
    pub(crate) revision: i32,
    pub(crate) variants: Vec<ProductVariant>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ProductVariant {
    pub(crate) id: ID,
    pub(crate) household_id: ID,
    pub(crate) concept_id: ID,
    pub(crate) name: String,
    pub(crate) normalized_name: String,
    pub(crate) brand: Option<String>,
    pub(crate) package_size: Option<String>,
    pub(crate) barcode: Option<String>,
    pub(crate) archived: bool,
    pub(crate) revision: i32,
    pub(crate) aliases: Vec<ProductAlias>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ProductAlias {
    pub(crate) id: ID,
    pub(crate) household_id: ID,
    pub(crate) variant_id: ID,
    pub(crate) merchant_key: String,
    pub(crate) alias: String,
    pub(crate) normalized_alias: String,
    pub(crate) revision: i32,
}

#[derive(Clone, Debug, SimpleObject)]
#[graphql(name = "MerchantAllocationRuleTarget")]
pub struct MerchantAllocationRuleTarget {
    name: String,
}

impl MerchantAllocationRuleTarget {
    pub fn new(name: impl Into<String>) -> Self {
        Self { name: name.into() }
    }
}

#[derive(Clone, Debug, async_graphql::Interface)]
#[graphql(field(name = "name", ty = "&String"))]
pub enum AllocationRuleTarget {
    MerchantAllocationRuleTarget(MerchantAllocationRuleTarget),
    Category(crate::category::Category),
    ProductConcept(ProductConcept),
    ProductVariant(ProductVariant),
}

impl AllocationRuleTarget {
    pub fn merchant(name: impl Into<String>) -> Self {
        Self::MerchantAllocationRuleTarget(MerchantAllocationRuleTarget::new(name))
    }

    pub fn category(value: crate::category::Category) -> Self {
        Self::Category(value)
    }

    pub fn product_concept(value: ProductConcept) -> Self {
        Self::ProductConcept(value)
    }

    pub fn product_variant(value: ProductVariant) -> Self {
        Self::ProductVariant(value)
    }
}

pub fn allocation_rule_target_from_projection(
    value: cross_context_postgres::pool_mutations::AllocationRuleTargetProjection,
) -> Result<AllocationRuleTarget> {
    use cross_context_postgres::pool_mutations::AllocationRuleTargetProjection as Projection;

    match value {
        Projection::Merchant { name } => Ok(AllocationRuleTarget::merchant(name)),
        Projection::Category {
            id,
            parent_id,
            name,
            archived,
            ..
        } => Ok(AllocationRuleTarget::category(crate::category::Category {
            id: encode_node_id("Category", &id.to_string()).map_err(crate::internal_error)?,
            name,
            parent_id: parent_id
                .map(|id| {
                    encode_node_id("Category", &id.to_string()).map_err(crate::internal_error)
                })
                .transpose()?,
            archived,
        })),
        Projection::ProductConcept(value) => {
            let revision = graphql_revision(value.revision, "product concept")?;
            let variants = value
                .variants
                .into_iter()
                .map(map_projected_product_variant)
                .collect::<Result<Vec<_>>>()?;
            Ok(AllocationRuleTarget::product_concept(ProductConcept {
                id: encode_node_id("ProductConcept", &value.id.to_string())
                    .map_err(crate::internal_error)?,
                household_id: encode_node_id("Household", &value.household_id.to_string())
                    .map_err(crate::internal_error)?,
                name: value.name,
                normalized_name: value.normalized_name,
                category_id: value
                    .category_id
                    .map(|id| {
                        encode_node_id("Category", &id.to_string()).map_err(crate::internal_error)
                    })
                    .transpose()?,
                archived: value.archived,
                revision,
                variants,
            }))
        }
        Projection::ProductVariant(value) => Ok(AllocationRuleTarget::product_variant(
            map_projected_product_variant(value)?,
        )),
    }
}

fn map_projected_product_variant(
    value: cross_context_postgres::pool_mutations::ProductVariantProjection,
) -> Result<ProductVariant> {
    Ok(ProductVariant {
        id: encode_node_id("ProductVariant", &value.id.to_string())
            .map_err(crate::internal_error)?,
        household_id: encode_node_id("Household", &value.household_id.to_string())
            .map_err(crate::internal_error)?,
        concept_id: encode_node_id("ProductConcept", &value.concept_id.to_string())
            .map_err(crate::internal_error)?,
        name: value.name,
        normalized_name: value.normalized_name,
        brand: nonempty(value.brand),
        package_size: nonempty(value.package_size),
        barcode: value.barcode,
        archived: value.archived,
        revision: graphql_revision(value.revision, "product variant")?,
        aliases: value
            .aliases
            .into_iter()
            .map(|alias| {
                Ok(ProductAlias {
                    id: encode_node_id("ProductAlias", &alias.id.to_string())
                        .map_err(crate::internal_error)?,
                    household_id: encode_node_id("Household", &alias.household_id.to_string())
                        .map_err(crate::internal_error)?,
                    variant_id: encode_node_id("ProductVariant", &alias.variant_id.to_string())
                        .map_err(crate::internal_error)?,
                    merchant_key: alias.merchant_key,
                    alias: alias.alias,
                    normalized_alias: alias.normalized_alias,
                    revision: graphql_revision(alias.revision, "product alias")?,
                })
            })
            .collect::<Result<Vec<_>>>()?,
    })
}

fn graphql_revision(value: i64, field: &str) -> Result<i32> {
    i32::try_from(value)
        .map_err(|_| crate::internal_error(format!("{field} revision exceeds GraphQL Int")))
}

fn nonempty(value: String) -> Option<String> {
    (!value.is_empty()).then_some(value)
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CreateTagSuccess {
    result: Tag,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct UpdateTagSuccess {
    result: Tag,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ArchiveTagSuccess {
    result: Tag,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CreateProductConceptSuccess {
    result: ProductConcept,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CreateProductVariantSuccess {
    result: ProductVariant,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SetProductAliasSuccess {
    result: ProductAlias,
}

macro_rules! user_error {
    ($name:ident) => {
        #[derive(Clone, Debug, SimpleObject)]
        pub struct $name {
            pub code: MutationErrorCode,
            pub message: String,
        }

        impl $name {
            fn new(code: MutationErrorCode) -> Self {
                Self {
                    code,
                    message: error_message(code).to_owned(),
                }
            }
        }
    };
}

user_error!(CreateTagUserError);
user_error!(UpdateTagUserError);
user_error!(ArchiveTagUserError);
user_error!(CreateProductConceptUserError);
user_error!(CreateProductVariantUserError);
user_error!(SetProductAliasUserError);

#[derive(Union)]
pub enum CreateTagResult {
    Success(CreateTagSuccess),
    UserError(CreateTagUserError),
}

#[derive(Union)]
pub enum UpdateTagResult {
    Success(UpdateTagSuccess),
    UserError(UpdateTagUserError),
}

#[derive(Union)]
pub enum ArchiveTagResult {
    Success(ArchiveTagSuccess),
    UserError(ArchiveTagUserError),
}

#[derive(Union)]
pub enum CreateProductConceptResult {
    Success(CreateProductConceptSuccess),
    UserError(CreateProductConceptUserError),
}

#[derive(Union)]
pub enum CreateProductVariantResult {
    Success(CreateProductVariantSuccess),
    UserError(CreateProductVariantUserError),
}

#[derive(Union)]
pub enum SetProductAliasResult {
    Success(SetProductAliasSuccess),
    UserError(SetProductAliasUserError),
}

pub async fn create_tag(ctx: &Context<'_>, input: CreateTagInput) -> Result<CreateTagResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(create_tag_error(MutationErrorCode::BadInput).into()),
    };
    match postgres_tag::persist_create_tag(
        ctx.data::<PgPool>()?,
        CreateTagCommand {
            actor_id: actor.0.to_string(),
            household_id: household_id.to_string(),
            name: input.name,
        },
    )
    .await
    {
        Ok(tag) => Ok(CreateTagSuccess {
            result: map_tag(tag).map_err(crate::internal_error)?,
        }
        .into()),
        Err(error) => match tag_error_code(&error) {
            Some(code) => Ok(create_tag_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn update_tag(ctx: &Context<'_>, input: UpdateTagInput) -> Result<UpdateTagResult> {
    let actor = crate::actor(ctx)?;
    let tag_id = match decode_node_id(&input.tag_id, "Tag") {
        Ok(id) => id,
        Err(()) => return Ok(update_tag_error(MutationErrorCode::BadInput).into()),
    };
    match postgres_tag::persist_update_tag(
        ctx.data::<PgPool>()?,
        UpdateTagCommand {
            actor_id: actor.0.to_string(),
            tag_id: tag_id.to_string(),
            base_revision: input.base_revision,
            name: input.name,
        },
    )
    .await
    {
        Ok(tag) => Ok(UpdateTagSuccess {
            result: map_tag(tag).map_err(crate::internal_error)?,
        }
        .into()),
        Err(error) => match tag_error_code(&error) {
            Some(code) => Ok(update_tag_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn archive_tag(ctx: &Context<'_>, input: ArchiveTagInput) -> Result<ArchiveTagResult> {
    let actor = crate::actor(ctx)?;
    let tag_id = match decode_node_id(&input.tag_id, "Tag") {
        Ok(id) => id,
        Err(()) => return Ok(archive_tag_error(MutationErrorCode::BadInput).into()),
    };
    match postgres_tag::persist_set_tag_archived(
        ctx.data::<PgPool>()?,
        SetTagArchivedCommand {
            actor_id: actor.0.to_string(),
            tag_id: tag_id.to_string(),
            base_revision: input.base_revision,
            archived: input.archived,
        },
    )
    .await
    {
        Ok(tag) => Ok(ArchiveTagSuccess {
            result: map_tag(tag).map_err(crate::internal_error)?,
        }
        .into()),
        Err(error) => match tag_error_code(&error) {
            Some(code) => Ok(archive_tag_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn create_product_concept(
    ctx: &Context<'_>,
    input: CreateProductConceptInput,
) -> Result<CreateProductConceptResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(create_product_concept_error(MutationErrorCode::BadInput).into()),
    };
    let category_id = match input.category_id.as_ref() {
        Some(id) => match decode_node_id(id, "Category") {
            Ok(id) => Some(id.to_string()),
            Err(()) => {
                return Ok(create_product_concept_error(MutationErrorCode::BadInput).into());
            }
        },
        None => None,
    };
    let command = CreateConceptCommand {
        actor_id: actor.0.to_string(),
        household_id: household_id.to_string(),
        name: input.name,
        category_id,
    };
    match postgres_product::create_concept(ctx.data::<PgPool>()?, command).await {
        Ok((id, decision)) => Ok(CreateProductConceptSuccess {
            result: ProductConcept {
                id: encode_node_id("ProductConcept", &id).map_err(crate::internal_error)?,
                household_id: encode_node_id("Household", decision.household_id.as_str())
                    .map_err(crate::internal_error)?,
                name: decision.name.as_str().to_owned(),
                normalized_name: decision.normalized_name().as_str().to_owned(),
                category_id: decision
                    .category_id
                    .as_ref()
                    .map(|id| {
                        encode_node_id("Category", id.as_str()).map_err(crate::internal_error)
                    })
                    .transpose()?,
                archived: false,
                revision: 1,
                variants: Vec::new(),
            },
        }
        .into()),
        Err(error) => match create_product_error_code(&error) {
            Some(code) => Ok(create_product_concept_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn create_product_variant(
    ctx: &Context<'_>,
    input: CreateProductVariantInput,
) -> Result<CreateProductVariantResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(create_product_variant_error(MutationErrorCode::BadInput).into()),
    };
    let concept_id = match decode_node_id(&input.concept_id, "ProductConcept") {
        Ok(id) => id,
        Err(()) => return Ok(create_product_variant_error(MutationErrorCode::BadInput).into()),
    };
    let command = CreateVariantCommand {
        actor_id: actor.0.to_string(),
        household_id: household_id.to_string(),
        concept_id: concept_id.to_string(),
        name: input.name,
        brand: input.brand,
        package_size: input.package_size,
        barcode: input.barcode,
    };
    match postgres_product::create_variant(ctx.data::<PgPool>()?, command).await {
        Ok((id, decision)) => Ok(CreateProductVariantSuccess {
            result: ProductVariant {
                id: encode_node_id("ProductVariant", &id).map_err(crate::internal_error)?,
                household_id: encode_node_id("Household", decision.household_id.as_str())
                    .map_err(crate::internal_error)?,
                concept_id: encode_node_id("ProductConcept", decision.concept_id.as_str())
                    .map_err(crate::internal_error)?,
                name: decision.name.as_str().to_owned(),
                normalized_name: decision.normalized_name().as_str().to_owned(),
                brand: decision.brand.filter(|value| !value.is_empty()),
                package_size: decision.package_size.filter(|value| !value.is_empty()),
                barcode: decision.barcode,
                archived: false,
                revision: 1,
                aliases: Vec::new(),
            },
        }
        .into()),
        Err(error) => match create_product_error_code(&error) {
            Some(code) => Ok(create_product_variant_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn set_product_alias(
    ctx: &Context<'_>,
    input: SetProductAliasInput,
) -> Result<SetProductAliasResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(set_product_alias_error(MutationErrorCode::BadInput).into()),
    };
    let variant_id = match decode_node_id(&input.variant_id, "ProductVariant") {
        Ok(id) => id,
        Err(()) => return Ok(set_product_alias_error(MutationErrorCode::BadInput).into()),
    };
    let command = SetProductAliasCommand {
        actor_id: actor.0.to_string(),
        household_id: household_id.to_string(),
        variant_id: variant_id.to_string(),
        merchant: input.merchant,
        alias: input.alias,
    };
    let decision = match decide_set_product_alias(command.clone()) {
        Ok(decision) => decision,
        Err(_) => return Ok(set_product_alias_error(MutationErrorCode::BadInput).into()),
    };
    match postgres_alias::persist_set_product_alias(ctx.data::<PgPool>()?, command).await {
        Ok(alias) => {
            let Ok(revision) = i32::try_from(alias.revision) else {
                return Err(crate::internal_error(
                    "ProductAlias revision exceeds GraphQL Int",
                ));
            };
            Ok(SetProductAliasSuccess {
                result: ProductAlias {
                    id: encode_node_id("ProductAlias", &alias.id).map_err(crate::internal_error)?,
                    household_id: encode_node_id("Household", &decision.household_id)
                        .map_err(crate::internal_error)?,
                    variant_id: encode_node_id("ProductVariant", &decision.variant_id)
                        .map_err(crate::internal_error)?,
                    merchant_key: decision.merchant_key,
                    alias: decision.alias_text,
                    normalized_alias: decision.normalized_alias,
                    revision,
                },
            }
            .into())
        }
        Err(error) => match product_alias_error_code(&error) {
            Some(code) => Ok(set_product_alias_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub(crate) fn map_tag(value: DomainTag) -> Result<Tag> {
    let revision = i32::try_from(value.revision)
        .map_err(|_| crate::internal_error("Tag revision exceeds GraphQL Int"))?;
    Ok(Tag {
        id: encode_node_id("Tag", &value.id).map_err(crate::internal_error)?,
        household_id: encode_node_id("Household", &value.household_id)
            .map_err(crate::internal_error)?,
        name: value.name,
        archived: value.archived,
        revision,
    })
}

fn decode_node_id(value: &ID, expected_kind: &str) -> std::result::Result<Uuid, ()> {
    let bytes = URL_SAFE_NO_PAD.decode(value.as_str()).map_err(|_| ())?;
    let separator = bytes.iter().position(|byte| *byte == b':').ok_or(())?;
    let (kind, payload_with_separator) = bytes.split_at(separator);
    let payload = &payload_with_separator[1..];
    if kind != expected_kind.as_bytes() || payload.len() != 16 {
        return Err(());
    }
    let id = Uuid::from_slice(payload).map_err(|_| ())?;
    if id.is_nil() {
        return Err(());
    }
    Ok(id)
}

pub(crate) fn encode_node_id(kind: &str, raw_id: &str) -> std::result::Result<ID, ()> {
    let id = Uuid::parse_str(raw_id).map_err(|_| ())?;
    if id.is_nil() {
        return Err(());
    }
    let mut payload = Vec::with_capacity(kind.len() + 17);
    payload.extend_from_slice(kind.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    Ok(ID(URL_SAFE_NO_PAD.encode(payload)))
}

fn tag_error_code(error: &postgres_tag::TagPersistenceError) -> Option<MutationErrorCode> {
    use postgres_tag::TagPersistenceError as Error;
    match error {
        Error::InvalidUuid(_) | Error::InvalidWorkflow(_) => Some(MutationErrorCode::BadInput),
        Error::HouseholdNotFound | Error::TagNotFound => Some(MutationErrorCode::NotFound),
        Error::RevisionConflict => Some(MutationErrorCode::Conflict),
        Error::Pool(_) | Error::Database(_) => None,
    }
}

fn create_product_error_code(
    error: &postgres_product::CreateProductPersistenceError,
) -> Option<MutationErrorCode> {
    use postgres_product::CreateProductPersistenceError as Error;
    match error {
        Error::InvalidUuid(_) | Error::InvalidWorkflow(_) => Some(MutationErrorCode::BadInput),
        Error::HouseholdNotFound
        | Error::MemberNotFound
        | Error::CategoryNotFound
        | Error::ConceptNotFound => Some(MutationErrorCode::NotFound),
        Error::Pool(_) | Error::Database(_) => None,
    }
}

fn product_alias_error_code(
    error: &postgres_alias::SetProductAliasPersistenceError,
) -> Option<MutationErrorCode> {
    use postgres_alias::SetProductAliasPersistenceError as Error;
    match error {
        Error::InvalidUuid(_) | Error::InvalidDecision | Error::InvalidWorkflow(_) => {
            Some(MutationErrorCode::BadInput)
        }
        Error::HouseholdNotFound | Error::ActorNotFound | Error::VariantNotFound => {
            Some(MutationErrorCode::NotFound)
        }
        Error::NumericOutOfRange(_) => Some(MutationErrorCode::BadInput),
        Error::Pool(_) | Error::Database(_) => None,
    }
}

fn create_tag_error(code: MutationErrorCode) -> CreateTagUserError {
    CreateTagUserError::new(code)
}

fn update_tag_error(code: MutationErrorCode) -> UpdateTagUserError {
    UpdateTagUserError::new(code)
}

fn archive_tag_error(code: MutationErrorCode) -> ArchiveTagUserError {
    ArchiveTagUserError::new(code)
}

fn create_product_concept_error(code: MutationErrorCode) -> CreateProductConceptUserError {
    CreateProductConceptUserError::new(code)
}

fn create_product_variant_error(code: MutationErrorCode) -> CreateProductVariantUserError {
    CreateProductVariantUserError::new(code)
}

fn set_product_alias_error(code: MutationErrorCode) -> SetProductAliasUserError {
    SetProductAliasUserError::new(code)
}

fn error_message(code: MutationErrorCode) -> &'static str {
    match code {
        MutationErrorCode::BadInput => "入力を確認してください。",
        MutationErrorCode::Conflict => "最新の状態を読み込んでから再試行してください。",
        MutationErrorCode::NotFound => "対象が見つかりません。",
        MutationErrorCode::MailForwardingDisabled => "メール転送は無効です。",
    }
}
