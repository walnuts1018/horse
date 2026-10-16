use async_graphql::{Context, ID, InputObject, Result, SimpleObject, Union};
use bookkeeping_postgres::shared_draft as postgres_shared_draft;
use cross_context_postgres::reference_data as shared_draft_workflow;
use horse_postgres_common::PgPool;

use crate::{DateTime, MutationErrorCode, internal_error};

#[derive(Clone, Debug, SimpleObject)]
#[graphql(rename_fields = "camelCase")]
pub struct SharedPurchaseDraft {
    pub id: ID,
    pub household_id: ID,
    pub revision: i32,
    pub payload: String,
    pub created_at: DateTime,
    pub updated_at: DateTime,
}

#[derive(Clone, Debug, InputObject)]
#[graphql(rename_fields = "camelCase")]
pub struct CreateSharedPurchaseDraftInput {
    pub household_id: ID,
    pub payload: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CreateSharedPurchaseDraftSuccess {
    pub result: SharedPurchaseDraft,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SharedPurchaseDraftUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum CreateSharedPurchaseDraftResult {
    Success(CreateSharedPurchaseDraftSuccess),
    UserError(SharedPurchaseDraftUserError),
}

#[derive(Clone, Debug, InputObject)]
#[graphql(rename_fields = "camelCase")]
pub struct UpdateSharedPurchaseDraftInput {
    pub id: ID,
    pub expected_revision: i32,
    pub payload: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct UpdateSharedPurchaseDraftSuccess {
    pub result: SharedPurchaseDraft,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct UpdateSharedPurchaseDraftConflict {
    pub code: MutationErrorCode,
    pub message: String,
    pub draft: SharedPurchaseDraft,
}

#[derive(Union)]
pub enum UpdateSharedPurchaseDraftResult {
    Success(UpdateSharedPurchaseDraftSuccess),
    Conflict(UpdateSharedPurchaseDraftConflict),
    UserError(SharedPurchaseDraftUserError),
}

pub async fn shared_purchase_draft(
    ctx: &Context<'_>,
    id: ID,
) -> Result<Option<SharedPurchaseDraft>> {
    let actor = crate::actor(ctx)?;
    match shared_draft_workflow::get_shared_purchase_draft(
        ctx.data::<PgPool>()?,
        &actor.0.to_string(),
        id.as_str(),
    )
    .await
    {
        Ok(draft) => graphql_draft(draft).map(Some),
        Err(error) => Err(internal_error(error)),
    }
}

pub async fn create_shared_purchase_draft(
    ctx: &Context<'_>,
    input: CreateSharedPurchaseDraftInput,
) -> Result<CreateSharedPurchaseDraftResult> {
    let actor = crate::actor(ctx)?;
    match shared_draft_workflow::create_shared_purchase_draft(
        ctx.data::<PgPool>()?,
        &actor.0.to_string(),
        input.household_id.as_str(),
        &input.payload,
    )
    .await
    {
        Ok(draft) => Ok(CreateSharedPurchaseDraftSuccess {
            result: graphql_draft(draft)?,
        }
        .into()),
        Err(error) => match mutation_error(&error) {
            Some(error) => Ok(error.into()),
            None => Err(internal_error(error)),
        },
    }
}

pub async fn update_shared_purchase_draft(
    ctx: &Context<'_>,
    input: UpdateSharedPurchaseDraftInput,
) -> Result<UpdateSharedPurchaseDraftResult> {
    let actor = crate::actor(ctx)?;
    match shared_draft_workflow::update_shared_purchase_draft(
        ctx.data::<PgPool>()?,
        &actor.0.to_string(),
        input.id.as_str(),
        i64::from(input.expected_revision),
        &input.payload,
    )
    .await
    {
        Ok(result) if result.conflict => Ok(UpdateSharedPurchaseDraftConflict {
            code: MutationErrorCode::Conflict,
            message: "最新の状態を読み込んでから再試行してください。".to_owned(),
            draft: graphql_draft(result.draft)?,
        }
        .into()),
        Ok(result) => Ok(UpdateSharedPurchaseDraftSuccess {
            result: graphql_draft(result.draft)?,
        }
        .into()),
        Err(error) => match mutation_error(&error) {
            Some(error) => Ok(error.into()),
            None => Err(internal_error(error)),
        },
    }
}

fn graphql_draft(value: postgres_shared_draft::SharedPurchaseDraft) -> Result<SharedPurchaseDraft> {
    let revision = i32::try_from(value.revision).map_err(internal_error)?;
    Ok(SharedPurchaseDraft {
        id: ID(value.id),
        household_id: ID(value.household_id),
        revision,
        payload: value.payload,
        created_at: value.created_at.into(),
        updated_at: value.updated_at.into(),
    })
}

fn mutation_error(
    error: &postgres_shared_draft::SharedPurchaseDraftPersistenceError,
) -> Option<SharedPurchaseDraftUserError> {
    use postgres_shared_draft::SharedPurchaseDraftPersistenceError as E;

    let (code, message) = match error {
        E::InvalidUuid(_) | E::InvalidWorkflow(_) | E::InvalidPayload(_) => {
            (MutationErrorCode::BadInput, "入力を確認してください。")
        }
        E::HouseholdNotFound | E::DraftNotFound => {
            (MutationErrorCode::NotFound, "対象が見つかりません。")
        }
        E::Pool(_) | E::Database(_) | E::RevisionOutOfRange => return None,
    };
    Some(SharedPurchaseDraftUserError {
        code,
        message: message.to_owned(),
    })
}
