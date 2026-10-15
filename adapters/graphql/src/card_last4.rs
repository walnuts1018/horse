use async_graphql::{Context, ID, InputObject, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_postgres::account_card_last4::{self, SetCardLast4PersistenceError};
use horse_postgres_common::PgPool;
use uuid::Uuid;

use crate::{
    MutationErrorCode,
    account_mutations::{AccountKind, AccountObject},
};

#[derive(Clone, Debug, InputObject)]
pub struct SetAccountCardLast4Input {
    account_id: ID,
    card_last4: Option<String>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SetAccountCardLast4Success {
    result: AccountObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SetAccountCardLast4UserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum SetAccountCardLast4Result {
    Success(SetAccountCardLast4Success),
    UserError(SetAccountCardLast4UserError),
}

pub async fn set_account_card_last4(
    ctx: &Context<'_>,
    input: SetAccountCardLast4Input,
) -> Result<SetAccountCardLast4Result> {
    let actor = crate::actor(ctx)?;
    let account_id = match decode_node_id(&input.account_id, "Account") {
        Ok(id) => id,
        Err(()) => return Ok(user_error(MutationErrorCode::BadInput).into()),
    };
    match account_card_last4::persist_set_card_last4(
        ctx.data::<PgPool>()?,
        &actor.0.to_string(),
        &account_id.to_string(),
        input.card_last4,
    )
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
            Ok(SetAccountCardLast4Success {
                result: AccountObject {
                    id: encode_node_id("Account", &account.id)?.into(),
                    name: account.name,
                    kind,
                    card_last4: account.card_last4,
                    pool_id: account
                        .pool_id
                        .as_deref()
                        .map(|id| encode_node_id("Pool", id).map(Into::into))
                        .transpose()?,
                },
            }
            .into())
        }
        Err(error) => match user_error_code(&error) {
            Some(code) => Ok(user_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
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

fn encode_node_id(kind: &str, raw_id: &str) -> Result<String> {
    let id = Uuid::parse_str(raw_id).map_err(crate::internal_error)?;
    if id.is_nil() {
        return Err(crate::internal_error("persisted node ID must not be nil"));
    }
    let mut payload = Vec::with_capacity(kind.len() + 17);
    payload.extend_from_slice(kind.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    Ok(URL_SAFE_NO_PAD.encode(payload))
}

fn user_error_code(error: &SetCardLast4PersistenceError) -> Option<MutationErrorCode> {
    use SetCardLast4PersistenceError as E;
    match error {
        E::InvalidUuid(_) | E::InvalidWorkflow(_) => Some(MutationErrorCode::BadInput),
        E::AccountNotFound => Some(MutationErrorCode::NotFound),
        E::Pool(_) | E::Database(_) | E::InvalidDecision => None,
    }
}

fn user_error(code: MutationErrorCode) -> SetAccountCardLast4UserError {
    let message = match code {
        MutationErrorCode::BadInput => "入力を確認してください。",
        MutationErrorCode::Conflict => "最新の状態を読み込んでから再試行してください。",
        MutationErrorCode::NotFound => "対象が見つかりません。",
        MutationErrorCode::MailForwardingDisabled => "この操作は現在利用できません。",
    };
    SetAccountCardLast4UserError {
        code,
        message: message.to_owned(),
    }
}
