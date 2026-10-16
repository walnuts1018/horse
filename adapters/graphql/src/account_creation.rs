use async_graphql::{Context, ID, InputObject, Result, SimpleObject, Union};
use bookkeeping_application::{
    create_account::{CreateAccountHandler, CreateAccountOpeningBalance, CreateAccountRequest},
    workflow_failure::WorkflowFailure,
};
use std::sync::Arc;
use uuid::Uuid;

use crate::{
    DateTime, Long,
    account_mutations::{AccountKind, AccountObject},
};

#[derive(Clone, Debug, InputObject)]
pub struct CreateAccountInput {
    household_id: ID,
    name: String,
    kind: AccountKind,
    card_last4: Option<String>,
    pool_id: Option<ID>,
    opening_balance: Option<AccountOpeningBalanceInput>,
}

#[derive(Clone, Debug, InputObject)]
pub struct AccountOpeningBalanceInput {
    amount_yen: Long,
    at: DateTime,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CreateAccountSuccess {
    result: AccountObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CreateAccountUserError {
    pub code: crate::MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum CreateAccountResult {
    Success(CreateAccountSuccess),
    UserError(CreateAccountUserError),
}

pub async fn create_account(
    ctx: &Context<'_>,
    input: CreateAccountInput,
) -> Result<CreateAccountResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(user_error(crate::MutationErrorCode::BadInput).into()),
    };
    let pool_id = match input.pool_id.as_ref() {
        Some(id) => match decode_node_id(id, "Pool") {
            Ok(id) => Some(id),
            Err(()) => return Ok(user_error(crate::MutationErrorCode::BadInput).into()),
        },
        None => None,
    };
    let opening_balance = match input.opening_balance {
        Some(opening) => Some(CreateAccountOpeningBalance {
            amount_yen: opening.amount_yen.0,
            at: opening.at.as_str().to_owned(),
        }),
        None => None,
    };
    let kind = match input.kind {
        AccountKind::Bank => "BANK",
        AccountKind::Cash => "CASH",
        AccountKind::Card => "CARD",
        AccountKind::Other => "OTHER",
    };
    match ctx
        .data::<Arc<dyn CreateAccountHandler>>()?
        .handle(CreateAccountRequest {
            actor_id: actor.0,
            household_id,
            name: input.name,
            kind: kind.to_owned(),
            card_last4: input.card_last4,
            pool_id,
            opening_balance,
        })
        .await
    {
        Ok(account) => {
            let kind = match account.kind.as_str() {
                "BANK" => AccountKind::Bank,
                "CASH" => AccountKind::Cash,
                "CARD" => AccountKind::Card,
                "OTHER" => AccountKind::Other,
                _ => return Err(crate::internal_error("invalid persisted account kind")),
            };
            Ok(CreateAccountSuccess {
                result: AccountObject {
                    id: encode_node_id("Account", &account.id.to_string())?.into(),
                    name: account.name,
                    kind,
                    card_last4: account.card_last4,
                    pool_id: account
                        .pool_id
                        .map(|id| encode_node_id("Pool", &id.to_string()).map(Into::into))
                        .transpose()?,
                },
            }
            .into())
        }
        Err(error) => match error {
            WorkflowFailure::Decision(_) => {
                Ok(user_error(crate::MutationErrorCode::BadInput).into())
            }
            WorkflowFailure::NotFound => Ok(user_error(crate::MutationErrorCode::NotFound).into()),
            WorkflowFailure::Conflict => Ok(user_error(crate::MutationErrorCode::Conflict).into()),
            WorkflowFailure::Infrastructure(error) => Err(crate::internal_error(error)),
        },
    }
}

fn decode_node_id(value: &ID, expected_kind: &str) -> std::result::Result<Uuid, ()> {
    let bytes = base64::Engine::decode(
        &base64::engine::general_purpose::URL_SAFE_NO_PAD,
        value.as_str(),
    )
    .map_err(|_| ())?;
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

fn encode_node_id(kind: &str, raw_id: &str) -> std::result::Result<String, async_graphql::Error> {
    let id = Uuid::parse_str(raw_id).map_err(crate::internal_error)?;
    if id.is_nil() {
        return Err(crate::internal_error("persisted node ID must not be nil"));
    }
    let mut payload = Vec::with_capacity(kind.len() + 17);
    payload.extend_from_slice(kind.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    Ok(base64::Engine::encode(
        &base64::engine::general_purpose::URL_SAFE_NO_PAD,
        payload,
    ))
}

fn user_error(code: crate::MutationErrorCode) -> CreateAccountUserError {
    let message = match code {
        crate::MutationErrorCode::BadInput => "入力を確認してください。",
        crate::MutationErrorCode::Conflict => "対象月は締め済みです。",
        crate::MutationErrorCode::NotFound => "対象が見つかりません。",
        crate::MutationErrorCode::MailForwardingDisabled => "この操作は現在利用できません。",
    };
    CreateAccountUserError {
        code,
        message: message.to_owned(),
    }
}
