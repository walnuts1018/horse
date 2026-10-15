use async_graphql::{Context, ID, InputObject, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use catalog_workflows::{
    create_product::{
        CreateConceptCommand, CreateVariantCommand, decide_create_concept, decide_create_variant,
    },
    product_alias::{SetProductAliasCommand, decide_set_product_alias},
    tag::{CreateTagCommand, SetTagArchivedCommand, Tag as DomainTag, UpdateTagCommand},
};
use horse_postgres::{
    PgPool, create_product as postgres_product, product_alias as postgres_alias,
    tag as postgres_tag,
};
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
    id: ID,
    household_id: ID,
    name: String,
    archived: bool,
    revision: i32,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ProductConcept {
    id: ID,
    household_id: ID,
    name: String,
    normalized_name: String,
    category_id: Option<ID>,
    archived: bool,
    revision: i32,
    variants: Vec<ProductVariant>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ProductVariant {
    id: ID,
    household_id: ID,
    concept_id: ID,
    name: String,
    normalized_name: String,
    brand: Option<String>,
    package_size: Option<String>,
    barcode: Option<String>,
    archived: bool,
    revision: i32,
    aliases: Vec<ProductAlias>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ProductAlias {
    id: ID,
    household_id: ID,
    variant_id: ID,
    merchant_key: String,
    alias: String,
    normalized_alias: String,
    revision: i32,
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
    let decision = match decide_create_concept(command.clone()) {
        Ok(decision) => decision,
        Err(_) => return Ok(create_product_concept_error(MutationErrorCode::BadInput).into()),
    };
    match postgres_product::persist_create_concept(ctx.data::<PgPool>()?, command).await {
        Ok(id) => Ok(CreateProductConceptSuccess {
            result: ProductConcept {
                id: encode_node_id("ProductConcept", &id).map_err(crate::internal_error)?,
                household_id: encode_node_id("Household", &decision.household_id)
                    .map_err(crate::internal_error)?,
                name: decision.name,
                normalized_name: decision.normalized_name,
                category_id: decision
                    .category_id
                    .as_deref()
                    .map(|id| encode_node_id("Category", id).map_err(crate::internal_error))
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
    let decision = match decide_create_variant(command.clone()) {
        Ok(decision) => decision,
        Err(_) => return Ok(create_product_variant_error(MutationErrorCode::BadInput).into()),
    };
    match postgres_product::persist_create_variant(ctx.data::<PgPool>()?, command).await {
        Ok(id) => Ok(CreateProductVariantSuccess {
            result: ProductVariant {
                id: encode_node_id("ProductVariant", &id).map_err(crate::internal_error)?,
                household_id: encode_node_id("Household", &decision.household_id)
                    .map_err(crate::internal_error)?,
                concept_id: encode_node_id("ProductConcept", &decision.concept_id)
                    .map_err(crate::internal_error)?,
                name: decision.name,
                normalized_name: decision.normalized_name,
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

fn map_tag(value: DomainTag) -> Result<Tag> {
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

fn encode_node_id(kind: &str, raw_id: &str) -> std::result::Result<ID, ()> {
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
