use async_graphql::{Context, ID, InputObject, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use catalog_workflows::category::Category as DomainCategory;
use catalog_workflows::category::{
    CreateCategoryCommand, SetCategoryArchivedCommand, UpdateCategoryNameCommand,
};
use horse_postgres::{PgPool, category as postgres_category};
use uuid::Uuid;

use crate::MutationErrorCode;

#[derive(Clone, Debug, InputObject)]
pub struct CreateCategoryInput {
    household_id: ID,
    name: String,
    parent_id: Option<ID>,
}

#[derive(Clone, Debug, InputObject)]
pub struct UpdateCategoryNameInput {
    category_id: ID,
    name: String,
}

#[derive(Clone, Debug, InputObject)]
pub struct SetCategoryArchivedInput {
    category_id: ID,
    archived: bool,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct Category {
    pub(crate) id: ID,
    pub(crate) name: String,
    pub(crate) parent_id: Option<ID>,
    pub(crate) archived: bool,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CreateCategorySuccess {
    result: Category,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct UpdateCategoryNameSuccess {
    result: Category,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SetCategoryArchivedSuccess {
    result: Category,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CreateCategoryUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct UpdateCategoryNameUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SetCategoryArchivedUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum CreateCategoryResult {
    Success(CreateCategorySuccess),
    UserError(CreateCategoryUserError),
}

#[derive(Union)]
pub enum UpdateCategoryNameResult {
    Success(UpdateCategoryNameSuccess),
    UserError(UpdateCategoryNameUserError),
}

#[derive(Union)]
pub enum SetCategoryArchivedResult {
    Success(SetCategoryArchivedSuccess),
    UserError(SetCategoryArchivedUserError),
}

pub async fn create_category(
    ctx: &Context<'_>,
    input: CreateCategoryInput,
) -> Result<CreateCategoryResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(create_error(MutationErrorCode::BadInput).into()),
    };
    let parent_id = match input.parent_id.as_ref() {
        Some(id) => match decode_node_id(id, "Category") {
            Ok(id) => Some(id.to_string()),
            Err(()) => return Ok(create_error(MutationErrorCode::BadInput).into()),
        },
        None => None,
    };
    match postgres_category::persist_create_category(
        ctx.data::<PgPool>()?,
        CreateCategoryCommand {
            actor_id: actor.0.to_string(),
            household_id: household_id.to_string(),
            category_id: Uuid::now_v7().to_string(),
            parent_id,
            name: input.name,
        },
    )
    .await
    {
        Ok(category) => Ok(CreateCategorySuccess {
            result: category.into(),
        }
        .into()),
        Err(error) => match user_error_code(&error) {
            Some(code) => Ok(create_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn update_category_name(
    ctx: &Context<'_>,
    input: UpdateCategoryNameInput,
) -> Result<UpdateCategoryNameResult> {
    let actor = crate::actor(ctx)?;
    let category_id = match decode_node_id(&input.category_id, "Category") {
        Ok(id) => id,
        Err(()) => return Ok(update_error(MutationErrorCode::BadInput).into()),
    };
    match postgres_category::persist_update_category_name(
        ctx.data::<PgPool>()?,
        UpdateCategoryNameCommand {
            actor_id: actor.0.to_string(),
            category_id: category_id.to_string(),
            name: input.name,
        },
    )
    .await
    {
        Ok(category) => Ok(UpdateCategoryNameSuccess {
            result: category.into(),
        }
        .into()),
        Err(error) => match user_error_code(&error) {
            Some(code) => Ok(update_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn set_category_archived(
    ctx: &Context<'_>,
    input: SetCategoryArchivedInput,
) -> Result<SetCategoryArchivedResult> {
    let actor = crate::actor(ctx)?;
    let category_id = match decode_node_id(&input.category_id, "Category") {
        Ok(id) => id,
        Err(()) => return Ok(archive_error(MutationErrorCode::BadInput).into()),
    };
    match postgres_category::persist_set_category_archived(
        ctx.data::<PgPool>()?,
        SetCategoryArchivedCommand {
            actor_id: actor.0.to_string(),
            category_id: category_id.to_string(),
            archived: input.archived,
        },
    )
    .await
    {
        Ok(category) => Ok(SetCategoryArchivedSuccess {
            result: category.into(),
        }
        .into()),
        Err(error) => match user_error_code(&error) {
            Some(code) => Ok(archive_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

impl From<DomainCategory> for Category {
    fn from(value: DomainCategory) -> Self {
        Self {
            id: encode_node_id("Category", &value.id)
                .expect("persisted category id must be a UUID")
                .into(),
            name: value.name,
            parent_id: value
                .parent_id
                .as_deref()
                .and_then(|id| encode_node_id("Category", id).ok())
                .map(Into::into),
            archived: value.archived,
        }
    }
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

fn encode_node_id(kind: &str, raw_id: &str) -> std::result::Result<String, ()> {
    let id = Uuid::parse_str(raw_id).map_err(|_| ())?;
    if id.is_nil() {
        return Err(());
    }
    let mut payload = Vec::with_capacity(kind.len() + 17);
    payload.extend_from_slice(kind.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    Ok(URL_SAFE_NO_PAD.encode(payload))
}

fn user_error_code(
    error: &postgres_category::CategoryPersistenceError,
) -> Option<MutationErrorCode> {
    use postgres_category::CategoryPersistenceError as E;
    match error {
        E::InvalidUuid(_) | E::InvalidWorkflow(_) => Some(MutationErrorCode::BadInput),
        E::HouseholdNotFound | E::CategoryNotFound => Some(MutationErrorCode::NotFound),
        E::Pool(_) | E::Database(_) | E::CorruptStoredData => None,
    }
}

fn create_error(code: MutationErrorCode) -> CreateCategoryUserError {
    CreateCategoryUserError {
        code,
        message: error_message(code),
    }
}

fn update_error(code: MutationErrorCode) -> UpdateCategoryNameUserError {
    UpdateCategoryNameUserError {
        code,
        message: error_message(code),
    }
}

fn archive_error(code: MutationErrorCode) -> SetCategoryArchivedUserError {
    SetCategoryArchivedUserError {
        code,
        message: error_message(code),
    }
}

fn error_message(code: MutationErrorCode) -> String {
    match code {
        MutationErrorCode::BadInput => "入力を確認してください。".to_owned(),
        MutationErrorCode::NotFound => "対象が見つかりません。".to_owned(),
        _ => "操作を完了できませんでした。".to_owned(),
    }
}
