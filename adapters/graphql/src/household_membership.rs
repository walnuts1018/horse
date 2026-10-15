use async_graphql::{Context, ID, InputObject, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use horse_postgres_common::PgPool;
use household_application::membership::{
    LeaveHouseholdCommand, RemoveHouseholdMemberCommand, TransferOwnerCommand,
};
use household_postgres::household_membership as postgres_membership;
use uuid::Uuid;

#[derive(Clone, Debug, InputObject)]
pub struct LeaveHouseholdInput {
    household_id: ID,
}

#[derive(Clone, Debug, InputObject)]
pub struct RemoveHouseholdMemberInput {
    household_id: ID,
    member_id: ID,
}

#[derive(Clone, Debug, InputObject)]
pub struct TransferHouseholdOwnerInput {
    household_id: ID,
    new_owner_id: ID,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LeaveHouseholdSuccess {
    household_id: ID,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RemoveHouseholdMemberSuccess {
    household_id: ID,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct TransferHouseholdOwnerSuccess {
    household_id: ID,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LeaveHouseholdUserError {
    pub code: crate::MutationErrorCode,
    pub message: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RemoveHouseholdMemberUserError {
    pub code: crate::MutationErrorCode,
    pub message: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct TransferHouseholdOwnerUserError {
    pub code: crate::MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum LeaveHouseholdResult {
    Success(LeaveHouseholdSuccess),
    UserError(LeaveHouseholdUserError),
}

#[derive(Union)]
pub enum RemoveHouseholdMemberResult {
    Success(RemoveHouseholdMemberSuccess),
    UserError(RemoveHouseholdMemberUserError),
}

#[derive(Union)]
pub enum TransferHouseholdOwnerResult {
    Success(TransferHouseholdOwnerSuccess),
    UserError(TransferHouseholdOwnerUserError),
}

pub async fn leave_household(
    ctx: &Context<'_>,
    input: LeaveHouseholdInput,
) -> Result<LeaveHouseholdResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(leave_error(crate::MutationErrorCode::BadInput).into()),
    };
    match postgres_membership::leave_household(
        ctx.data::<PgPool>()?,
        LeaveHouseholdCommand {
            member_id: actor.0.to_string(),
            household_id: household_id.to_string(),
        },
    )
    .await
    {
        Ok(change) => Ok(LeaveHouseholdSuccess {
            household_id: encode_node_id("Household", &change.household_id)?.into(),
        }
        .into()),
        Err(error) => match user_error_code(&error) {
            Some(code) => Ok(leave_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn remove_household_member(
    ctx: &Context<'_>,
    input: RemoveHouseholdMemberInput,
) -> Result<RemoveHouseholdMemberResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(remove_error(crate::MutationErrorCode::BadInput).into()),
    };
    let member_id = match decode_node_id(&input.member_id, "Member") {
        Ok(id) => id,
        Err(()) => return Ok(remove_error(crate::MutationErrorCode::BadInput).into()),
    };
    match postgres_membership::remove_household_member(
        ctx.data::<PgPool>()?,
        RemoveHouseholdMemberCommand {
            actor_id: actor.0.to_string(),
            household_id: household_id.to_string(),
            member_id: member_id.to_string(),
        },
    )
    .await
    {
        Ok(change) => Ok(RemoveHouseholdMemberSuccess {
            household_id: encode_node_id("Household", &change.household_id)?.into(),
        }
        .into()),
        Err(error) => match user_error_code(&error) {
            Some(code) => Ok(remove_error(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn transfer_household_owner(
    ctx: &Context<'_>,
    input: TransferHouseholdOwnerInput,
) -> Result<TransferHouseholdOwnerResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(transfer_error(crate::MutationErrorCode::BadInput).into()),
    };
    let new_owner_id = match decode_node_id(&input.new_owner_id, "Member") {
        Ok(id) => id,
        Err(()) => return Ok(transfer_error(crate::MutationErrorCode::BadInput).into()),
    };
    match postgres_membership::transfer_owner(
        ctx.data::<PgPool>()?,
        TransferOwnerCommand {
            actor_id: actor.0.to_string(),
            household_id: household_id.to_string(),
            new_owner_id: new_owner_id.to_string(),
        },
    )
    .await
    {
        Ok(change) => Ok(TransferHouseholdOwnerSuccess {
            household_id: encode_node_id("Household", &change.household_id)?.into(),
        }
        .into()),
        Err(error) => match user_error_code(&error) {
            Some(code) => Ok(transfer_error(code).into()),
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

fn user_error_code(
    error: &postgres_membership::HouseholdMembershipError,
) -> Option<crate::MutationErrorCode> {
    use postgres_membership::HouseholdMembershipError as E;
    match error {
        E::InvalidUuid(_) | E::Workflow(_) => Some(crate::MutationErrorCode::BadInput),
        E::HouseholdNotFound | E::MemberNotFound => Some(crate::MutationErrorCode::NotFound),
        E::Conflict => Some(crate::MutationErrorCode::Conflict),
        E::Pool(_) | E::Database(_) => None,
    }
}

fn error_message(code: crate::MutationErrorCode) -> String {
    match code {
        crate::MutationErrorCode::BadInput => "入力を確認してください。".to_owned(),
        crate::MutationErrorCode::Conflict => "メンバーの状態を確認してください。".to_owned(),
        crate::MutationErrorCode::NotFound => "対象が見つかりません。".to_owned(),
        crate::MutationErrorCode::MailForwardingDisabled => {
            "この操作は現在利用できません。".to_owned()
        }
    }
}

fn leave_error(code: crate::MutationErrorCode) -> LeaveHouseholdUserError {
    LeaveHouseholdUserError {
        code,
        message: error_message(code),
    }
}

fn remove_error(code: crate::MutationErrorCode) -> RemoveHouseholdMemberUserError {
    RemoveHouseholdMemberUserError {
        code,
        message: error_message(code),
    }
}

fn transfer_error(code: crate::MutationErrorCode) -> TransferHouseholdOwnerUserError {
    TransferHouseholdOwnerUserError {
        code,
        message: error_message(code),
    }
}
