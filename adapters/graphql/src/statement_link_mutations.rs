use async_graphql::{Context, ID, InputObject, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_application::workflow_failure::{WorkflowFailure, WorkflowRejection};
use cross_context_application::statement_link_mutations::{
    StatementLinkCommand, StatementLinkMutationFailure, StatementLinkMutationsHandler,
    StatementRefundCorrectionLinkCommand, StatementTransferLinkCommand,
};
use uuid::Uuid;

use crate::{MutationErrorCode, account_mutations, ledger_entry};

#[derive(Clone, Debug, InputObject)]
pub struct LinkStatementAccountMovementInput {
    household_id: ID,
    account_movement_id: ID,
    source_document_id: ID,
    source_reference: String,
    source_account_id: ID,
}

#[derive(Clone, Debug, InputObject)]
pub struct LinkStatementAccountIncomeInput {
    household_id: ID,
    account_income_id: ID,
    source_document_id: ID,
    source_reference: String,
    source_account_id: ID,
}

#[derive(Clone, Debug, InputObject)]
pub struct LinkStatementTransferInput {
    household_id: ID,
    ledger_entry_id: ID,
    source_document_id: ID,
    source_reference: String,
    source_account_id: ID,
}

#[derive(Clone, Debug, InputObject)]
pub struct LinkStatementRefundCorrectionInput {
    household_id: ID,
    correction_id: ID,
    source_document_id: ID,
    source_reference: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LinkStatementAccountMovementSuccess {
    result: account_mutations::AccountMovementObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LinkStatementAccountMovementUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum LinkStatementAccountMovementResult {
    Success(Box<LinkStatementAccountMovementSuccess>),
    UserError(LinkStatementAccountMovementUserError),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LinkStatementAccountIncomeSuccess {
    result: account_mutations::AccountIncomeObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LinkStatementAccountIncomeUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum LinkStatementAccountIncomeResult {
    Success(Box<LinkStatementAccountIncomeSuccess>),
    UserError(LinkStatementAccountIncomeUserError),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LinkStatementTransferSuccess {
    result: ledger_entry::LedgerEntry,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LinkStatementTransferUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum LinkStatementTransferResult {
    Success(Box<LinkStatementTransferSuccess>),
    UserError(LinkStatementTransferUserError),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LinkStatementRefundCorrectionSuccess {
    result: crate::purchase_mutations::PurchaseObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LinkStatementRefundCorrectionUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum LinkStatementRefundCorrectionResult {
    Success(Box<LinkStatementRefundCorrectionSuccess>),
    UserError(LinkStatementRefundCorrectionUserError),
}

pub async fn link_statement_account_movement(
    ctx: &Context<'_>,
    input: LinkStatementAccountMovementInput,
) -> Result<LinkStatementAccountMovementResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match decode_node_id(&input.household_id, "Household") {
        Ok(value) => value,
        Err(()) => return Ok(movement_error(MutationErrorCode::BadInput).into()),
    };
    let movement_id = match decode_node_id(&input.account_movement_id, "AccountMovement") {
        Ok(value) => value,
        Err(()) => return Ok(movement_error(MutationErrorCode::BadInput).into()),
    };
    let source_account_id = match decode_node_id(&input.source_account_id, "Account") {
        Ok(value) => value,
        Err(()) => return Ok(movement_error(MutationErrorCode::BadInput).into()),
    };
    let source_document_id = match Uuid::parse_str(input.source_document_id.as_str()) {
        Ok(value) if !value.is_nil() => value,
        _ => return Ok(movement_error(MutationErrorCode::NotFound).into()),
    };
    if input.source_reference.is_empty() || input.source_reference.len() > 256 {
        return Ok(movement_error(MutationErrorCode::BadInput).into());
    }
    let handler = ctx.data::<std::sync::Arc<dyn StatementLinkMutationsHandler>>()?;
    match handler
        .link_movement(StatementLinkCommand {
            household_id,
            actor_id: actor.0,
            target_id: movement_id,
            source_document_id,
            source_reference: input.source_reference,
            source_account_id: Some(source_account_id),
        })
        .await
    {
        Ok(projection) => Ok(Box::new(LinkStatementAccountMovementSuccess {
            result: map_movement(projection)?,
        })
        .into()),
        Err(error) => movement_persistence_error(error),
    }
}

pub async fn link_statement_account_income(
    ctx: &Context<'_>,
    input: LinkStatementAccountIncomeInput,
) -> Result<LinkStatementAccountIncomeResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match decode_node_id(&input.household_id, "Household") {
        Ok(value) => value,
        Err(()) => return Ok(income_error(MutationErrorCode::BadInput).into()),
    };
    let income_id = match decode_node_id(&input.account_income_id, "AccountIncome") {
        Ok(value) => value,
        Err(()) => return Ok(income_error(MutationErrorCode::BadInput).into()),
    };
    let source_account_id = match decode_node_id(&input.source_account_id, "Account") {
        Ok(value) => value,
        Err(()) => return Ok(income_error(MutationErrorCode::BadInput).into()),
    };
    let source_document_id = match Uuid::parse_str(input.source_document_id.as_str()) {
        Ok(value) if !value.is_nil() => value,
        _ => return Ok(income_error(MutationErrorCode::NotFound).into()),
    };
    if input.source_reference.is_empty() || input.source_reference.len() > 256 {
        return Ok(income_error(MutationErrorCode::BadInput).into());
    }
    let handler = ctx.data::<std::sync::Arc<dyn StatementLinkMutationsHandler>>()?;
    match handler
        .link_income(StatementLinkCommand {
            household_id,
            actor_id: actor.0,
            target_id: income_id,
            source_document_id,
            source_reference: input.source_reference,
            source_account_id: Some(source_account_id),
        })
        .await
    {
        Ok(projection) => Ok(Box::new(LinkStatementAccountIncomeSuccess {
            result: map_income(projection),
        })
        .into()),
        Err(error) => income_persistence_error(error),
    }
}

pub async fn link_statement_transfer(
    ctx: &Context<'_>,
    input: LinkStatementTransferInput,
) -> Result<LinkStatementTransferResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match decode_node_id(&input.household_id, "Household") {
        Ok(value) => value,
        Err(()) => return Ok(transfer_error(MutationErrorCode::BadInput).into()),
    };
    let ledger_entry_id = match decode_node_id(&input.ledger_entry_id, "LedgerEntry") {
        Ok(value) => value,
        Err(()) => return Ok(transfer_error(MutationErrorCode::BadInput).into()),
    };
    let source_account_id = match decode_node_id(&input.source_account_id, "Account") {
        Ok(value) => value,
        Err(()) => return Ok(transfer_error(MutationErrorCode::BadInput).into()),
    };
    let source_document_id = match Uuid::parse_str(input.source_document_id.as_str()) {
        Ok(value) if !value.is_nil() => value,
        _ => return Ok(transfer_error(MutationErrorCode::NotFound).into()),
    };
    if input.source_reference.is_empty() || input.source_reference.len() > 256 {
        return Ok(transfer_error(MutationErrorCode::BadInput).into());
    }
    let handler = ctx.data::<std::sync::Arc<dyn StatementLinkMutationsHandler>>()?;
    match handler
        .link_transfer(StatementTransferLinkCommand {
            household_id,
            actor_id: actor.0,
            ledger_entry_id,
            source_document_id,
            source_reference: input.source_reference,
            source_account_id,
        })
        .await
    {
        Ok(projection) => Ok(Box::new(LinkStatementTransferSuccess {
            result: ledger_entry::LedgerEntry::try_from(projection)
                .map_err(crate::internal_error)?,
        })
        .into()),
        Err(error) => transfer_persistence_error(error),
    }
}

pub async fn link_statement_refund_correction(
    ctx: &Context<'_>,
    input: LinkStatementRefundCorrectionInput,
) -> Result<LinkStatementRefundCorrectionResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match decode_node_id(&input.household_id, "Household") {
        Ok(value) => value,
        Err(()) => return Ok(refund_error(MutationErrorCode::BadInput).into()),
    };
    let correction_id = match decode_node_id(&input.correction_id, "PurchaseCorrection") {
        Ok(value) => value,
        Err(()) => return Ok(refund_error(MutationErrorCode::BadInput).into()),
    };
    let source_document_id = match Uuid::parse_str(input.source_document_id.as_str()) {
        Ok(value) if !value.is_nil() => value,
        _ => return Ok(refund_error(MutationErrorCode::NotFound).into()),
    };
    if input.source_reference.is_empty() || input.source_reference.len() > 256 {
        return Ok(refund_error(MutationErrorCode::BadInput).into());
    }
    let handler = ctx.data::<std::sync::Arc<dyn StatementLinkMutationsHandler>>()?;
    match handler
        .link_refund_correction(StatementRefundCorrectionLinkCommand {
            household_id,
            actor_id: actor.0,
            correction_id,
            source_document_id,
            source_reference: input.source_reference,
        })
        .await
    {
        Ok(projection) => Ok(Box::new(LinkStatementRefundCorrectionSuccess {
            result: crate::purchase_mutations::PurchaseObject::try_from(projection)
                .map_err(crate::internal_error)?,
        })
        .into()),
        Err(error) => refund_persistence_error(error),
    }
}

fn map_movement(
    value: bookkeeping_application::account_history_query::AccountMovementProjection,
) -> Result<account_mutations::AccountMovementObject> {
    use account_mutations::AccountMovementKind;

    let kind = match value.kind.as_str() {
        "TRANSFER" => AccountMovementKind::Transfer,
        "CARD_BILL_PAYMENT" => AccountMovementKind::CardBillPayment,
        _ => {
            return Err(crate::internal_error(
                "invalid persisted account movement kind",
            ));
        }
    };
    Ok(account_mutations::AccountMovementObject {
        id: encode_node_id("AccountMovement", value.id),
        household_id: encode_node_id("Household", value.household_id),
        kind,
        occurred_at: value.occurred_at.into(),
        from_account_id: encode_node_id("Account", value.from_account_id),
        from_account: map_account(value.from_account)?,
        to_account_id: encode_node_id("Account", value.to_account_id),
        to_account: map_account(value.to_account)?,
        amount_yen: crate::Long(value.amount_yen),
        currency: "JPY".to_owned(),
        description: value.description,
        created_at: value.created_at.into(),
    })
}

fn map_income(
    value: bookkeeping_application::account_history_query::AccountIncomeProjection,
) -> account_mutations::AccountIncomeObject {
    account_mutations::AccountIncomeObject {
        id: encode_node_id("AccountIncome", value.id),
        household_id: encode_node_id("Household", value.household_id),
        account_id: encode_node_id("Account", value.account_id),
        amount_yen: crate::Long(value.amount_yen),
        currency: "JPY".to_owned(),
        occurred_at: value.occurred_at.into(),
        description: value.description,
        created_at: value.created_at.into(),
    }
}

fn map_account(
    value: bookkeeping_application::account_history_query::AccountProjection,
) -> Result<account_mutations::AccountObject> {
    use account_mutations::{AccountKind, AccountObject};

    let kind = match value.kind.as_str() {
        "BANK" => AccountKind::Bank,
        "CASH" => AccountKind::Cash,
        "CARD" => AccountKind::Card,
        "OTHER" => AccountKind::Other,
        _ => return Err(crate::internal_error("invalid persisted account kind")),
    };
    Ok(AccountObject {
        id: encode_node_id("Account", value.id),
        name: value.name,
        kind,
        card_last4: value.card_last4,
        pool_id: value.pool_id.map(|id| encode_node_id("Pool", id)),
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

fn encode_node_id(kind: &str, id: Uuid) -> ID {
    let mut payload = Vec::with_capacity(kind.len() + 17);
    payload.extend_from_slice(kind.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    ID(URL_SAFE_NO_PAD.encode(payload))
}

fn movement_error(code: MutationErrorCode) -> LinkStatementAccountMovementUserError {
    LinkStatementAccountMovementUserError {
        code,
        message: error_message(code).to_owned(),
    }
}

fn income_error(code: MutationErrorCode) -> LinkStatementAccountIncomeUserError {
    LinkStatementAccountIncomeUserError {
        code,
        message: error_message(code).to_owned(),
    }
}

fn transfer_error(code: MutationErrorCode) -> LinkStatementTransferUserError {
    LinkStatementTransferUserError {
        code,
        message: error_message(code).to_owned(),
    }
}

fn refund_error(code: MutationErrorCode) -> LinkStatementRefundCorrectionUserError {
    LinkStatementRefundCorrectionUserError {
        code,
        message: error_message(code).to_owned(),
    }
}

fn error_message(code: MutationErrorCode) -> &'static str {
    match code {
        MutationErrorCode::BadInput => "入力を確認してください。",
        MutationErrorCode::Conflict => "最新の状態を読み込んでから再試行してください。",
        MutationErrorCode::NotFound => "対象が見つかりません。",
        MutationErrorCode::MailForwardingDisabled => "操作を実行できません。",
    }
}

fn movement_persistence_error(
    error: StatementLinkMutationFailure,
) -> Result<LinkStatementAccountMovementResult> {
    match error {
        WorkflowFailure::NotFound => Ok(movement_error(MutationErrorCode::NotFound).into()),
        WorkflowFailure::Conflict => Ok(movement_error(MutationErrorCode::Conflict).into()),
        WorkflowFailure::Decision(WorkflowRejection::InvalidInput) => {
            Ok(movement_error(MutationErrorCode::BadInput).into())
        }
        WorkflowFailure::Infrastructure(error) => Err(crate::internal_error(error)),
    }
}

fn income_persistence_error(
    error: StatementLinkMutationFailure,
) -> Result<LinkStatementAccountIncomeResult> {
    match error {
        WorkflowFailure::NotFound => Ok(income_error(MutationErrorCode::NotFound).into()),
        WorkflowFailure::Conflict => Ok(income_error(MutationErrorCode::Conflict).into()),
        WorkflowFailure::Decision(WorkflowRejection::InvalidInput) => {
            Ok(income_error(MutationErrorCode::BadInput).into())
        }
        WorkflowFailure::Infrastructure(error) => Err(crate::internal_error(error)),
    }
}

fn transfer_persistence_error(
    error: StatementLinkMutationFailure,
) -> Result<LinkStatementTransferResult> {
    match error {
        WorkflowFailure::NotFound => Ok(transfer_error(MutationErrorCode::NotFound).into()),
        WorkflowFailure::Conflict => Ok(transfer_error(MutationErrorCode::Conflict).into()),
        WorkflowFailure::Decision(WorkflowRejection::InvalidInput) => {
            Ok(transfer_error(MutationErrorCode::BadInput).into())
        }
        WorkflowFailure::Infrastructure(error) => Err(crate::internal_error(error)),
    }
}

fn refund_persistence_error(
    error: StatementLinkMutationFailure,
) -> Result<LinkStatementRefundCorrectionResult> {
    match error {
        WorkflowFailure::NotFound => Ok(refund_error(MutationErrorCode::NotFound).into()),
        WorkflowFailure::Conflict => Ok(refund_error(MutationErrorCode::Conflict).into()),
        WorkflowFailure::Decision(WorkflowRejection::InvalidInput) => {
            Ok(refund_error(MutationErrorCode::BadInput).into())
        }
        WorkflowFailure::Infrastructure(error) => Err(crate::internal_error(error)),
    }
}
