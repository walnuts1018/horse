use async_graphql::{Context, ID, InputObject, Result, SimpleObject, Union};
use bookkeeping_application::purchase_mutations::{
    SharedPurchaseDraft as ApplicationSharedPurchaseDraft, SharedPurchaseDraftError,
};
use std::sync::Arc;
use uuid::Uuid;

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
    let draft_id = Uuid::parse_str(id.as_str()).map_err(internal_error)?;
    match ctx
        .data::<Arc<dyn bookkeeping_application::purchase_mutations::PurchaseMutationHandler>>()?
        .get_shared_draft(actor.0, draft_id)
        .await
    {
        Ok(draft) => graphql_draft(draft).map(Some),
        Err(error) => Err(internal_error(format!("{error:?}"))),
    }
}

pub async fn create_shared_purchase_draft(
    ctx: &Context<'_>,
    input: CreateSharedPurchaseDraftInput,
) -> Result<CreateSharedPurchaseDraftResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match Uuid::parse_str(input.household_id.as_str()) {
        Ok(id) if !id.is_nil() => id,
        _ => return Ok(shared_draft_error(MutationErrorCode::BadInput).into()),
    };
    match ctx
        .data::<Arc<dyn bookkeeping_application::purchase_mutations::PurchaseMutationHandler>>()?
        .create_shared_draft(actor.0, household_id, input.payload)
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
    let draft_id = match Uuid::parse_str(input.id.as_str()) {
        Ok(id) if !id.is_nil() => id,
        _ => return Ok(shared_draft_update_error(MutationErrorCode::BadInput).into()),
    };
    match ctx
        .data::<Arc<dyn bookkeeping_application::purchase_mutations::PurchaseMutationHandler>>()?
        .update_shared_draft(
            actor.0,
            draft_id,
            i64::from(input.expected_revision),
            input.payload,
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

fn graphql_draft(value: ApplicationSharedPurchaseDraft) -> Result<SharedPurchaseDraft> {
    let revision = i32::try_from(value.revision).map_err(internal_error)?;
    Ok(SharedPurchaseDraft {
        id: ID(value.id.to_string()),
        household_id: ID(value.household_id.to_string()),
        revision,
        payload: value.payload,
        created_at: value.created_at.into(),
        updated_at: value.updated_at.into(),
    })
}

fn mutation_error(error: &SharedPurchaseDraftError) -> Option<SharedPurchaseDraftUserError> {
    let (code, message) = match error {
        SharedPurchaseDraftError::BadInput => {
            (MutationErrorCode::BadInput, "入力を確認してください。")
        }
        SharedPurchaseDraftError::NotFound => {
            (MutationErrorCode::NotFound, "対象が見つかりません。")
        }
        SharedPurchaseDraftError::Infrastructure(_) => return None,
    };
    Some(SharedPurchaseDraftUserError {
        code,
        message: message.to_owned(),
    })
}

fn shared_draft_error(code: MutationErrorCode) -> SharedPurchaseDraftUserError {
    SharedPurchaseDraftUserError {
        code,
        message: match code {
            MutationErrorCode::BadInput => "入力を確認してください。",
            MutationErrorCode::NotFound => "対象が見つかりません。",
            MutationErrorCode::Conflict => "最新の状態を読み込んでから再試行してください。",
            MutationErrorCode::MailForwardingDisabled => "メール転送は無効です。",
        }
        .to_owned(),
    }
}

fn shared_draft_update_error(code: MutationErrorCode) -> SharedPurchaseDraftUserError {
    shared_draft_error(code)
}
