use async_graphql::{Context, ID, InputObject, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use horse_postgres::{PgPool, correct_purchase as postgres_correct_purchase};
use serde::Serialize;
use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::{Long, MutationErrorCode, purchase_mutations::PurchaseObject};

#[derive(Clone, Debug, InputObject)]
pub struct CorrectPurchaseInput {
    pub household_id: ID,
    pub target: CorrectPurchaseTargetInput,
    pub amount_delta_yen: Long,
    pub reason: String,
    pub allocations: Option<Vec<CorrectPurchaseAllocationInput>>,
    pub payment_allocations: Option<Vec<CorrectPurchasePaymentInput>>,
    pub idempotency_key: String,
}

#[derive(Clone, Debug, InputObject)]
pub struct CorrectPurchaseFromStatementInput {
    pub household_id: ID,
    pub source_document_id: ID,
    pub source_reference: String,
    pub target: CorrectPurchaseTargetInput,
    pub reason: String,
    pub idempotency_key: String,
}

#[derive(Clone, Debug, async_graphql::OneofObject)]
#[graphql(name = "CorrectPurchaseTargetInput", rename_fields = "camelCase")]
pub enum CorrectPurchaseTargetInput {
    Purchase(PurchaseCorrectionPurchaseTargetInput),
    Item(PurchaseCorrectionItemTargetInput),
}

#[derive(Clone, Debug, InputObject)]
pub struct PurchaseCorrectionPurchaseTargetInput {
    pub purchase_id: ID,
}

#[derive(Clone, Debug, InputObject)]
pub struct PurchaseCorrectionItemTargetInput {
    pub purchase_id: ID,
    pub item_id: ID,
}

#[derive(Clone, Debug, InputObject)]
pub struct CorrectPurchaseAllocationInput {
    pub pool_id: ID,
    pub weight: Long,
}

#[derive(Clone, Debug, InputObject)]
pub struct CorrectPurchasePaymentInput {
    pub payment_account_id: ID,
    pub amount_yen: Long,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CorrectPurchaseSuccess {
    pub result: PurchaseObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CorrectPurchaseUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum CorrectPurchaseResult {
    Success(CorrectPurchaseSuccess),
    UserError(CorrectPurchaseUserError),
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CorrectPurchaseFingerprint<'a> {
    operation: &'static str,
    household_id: &'a str,
    purchase_id: &'a str,
    item_id: Option<&'a str>,
    amount_delta_yen: Option<i64>,
    reason: &'a str,
    allocations: &'a [(String, i64)],
    payments: &'a [(String, i64)],
    source_document_id: Option<&'a str>,
    source_reference: Option<&'a str>,
}

pub async fn correct_purchase(
    ctx: &Context<'_>,
    input: CorrectPurchaseInput,
) -> Result<CorrectPurchaseResult> {
    let (household_id, purchase_id, item_id) = match parse_target(&input.household_id, input.target)
    {
        Ok(value) => value,
        Err(()) => return Ok(bad_input().into()),
    };
    let allocations = match parse_allocations(input.allocations.unwrap_or_default()) {
        Ok(value) => value,
        Err(()) => return Ok(bad_input().into()),
    };
    let payments = match parse_payments(input.payment_allocations.unwrap_or_default()) {
        Ok(value) => value,
        Err(()) => return Ok(bad_input().into()),
    };
    let fingerprint = fingerprint(CorrectPurchaseFingerprint {
        operation: "correctPurchase",
        household_id: &household_id.to_string(),
        purchase_id: &purchase_id.to_string(),
        item_id: item_id.as_deref(),
        amount_delta_yen: Some(input.amount_delta_yen.0),
        reason: &input.reason,
        allocations: &allocations,
        payments: &payments,
        source_document_id: None,
        source_reference: None,
    })?;
    execute(
        ctx,
        postgres_correct_purchase::CorrectPurchaseRequest {
            household_id: household_id.to_string(),
            actor_id: crate::actor(ctx)?.0.to_string(),
            purchase_id: purchase_id.to_string(),
            item_id: item_id.map(|id| id.to_string()),
            amount_delta_yen: Some(input.amount_delta_yen.0),
            reason: input.reason,
            allocations,
            payments,
            idempotency_key: input.idempotency_key,
            request_fingerprint: fingerprint,
            source_document_id: None,
            source_reference: None,
        },
    )
    .await
}

pub async fn correct_purchase_from_statement(
    ctx: &Context<'_>,
    input: CorrectPurchaseFromStatementInput,
) -> Result<CorrectPurchaseResult> {
    let (household_id, purchase_id, item_id) = match parse_target(&input.household_id, input.target)
    {
        Ok(value) => value,
        Err(()) => return Ok(bad_input().into()),
    };
    let source_document_id = match Uuid::parse_str(input.source_document_id.as_str()) {
        Ok(id) if !id.is_nil() => id,
        _ => return Ok(bad_input().into()),
    };
    if input.source_reference.trim().is_empty()
        || input.source_reference.len() > 256
        || input.idempotency_key.is_empty()
        || input.idempotency_key.len() > 200
        || input.reason.trim().is_empty()
    {
        return Ok(bad_input().into());
    }
    let empty = Vec::new();
    let fingerprint = fingerprint(CorrectPurchaseFingerprint {
        operation: "correctPurchaseFromStatement",
        household_id: &household_id.to_string(),
        purchase_id: &purchase_id.to_string(),
        item_id: item_id.as_deref(),
        amount_delta_yen: None,
        reason: &input.reason,
        allocations: &empty,
        payments: &empty,
        source_document_id: Some(&source_document_id.to_string()),
        source_reference: Some(&input.source_reference),
    })?;
    execute(
        ctx,
        postgres_correct_purchase::CorrectPurchaseRequest {
            household_id: household_id.to_string(),
            actor_id: crate::actor(ctx)?.0.to_string(),
            purchase_id: purchase_id.to_string(),
            item_id: item_id.map(|id| id.to_string()),
            amount_delta_yen: None,
            reason: input.reason,
            allocations: Vec::new(),
            payments: Vec::new(),
            idempotency_key: input.idempotency_key,
            request_fingerprint: fingerprint,
            source_document_id: Some(source_document_id.to_string()),
            source_reference: Some(input.source_reference),
        },
    )
    .await
}

async fn execute(
    ctx: &Context<'_>,
    request: postgres_correct_purchase::CorrectPurchaseRequest,
) -> Result<CorrectPurchaseResult> {
    let household_id = Uuid::parse_str(&request.household_id).map_err(crate::internal_error)?;
    match postgres_correct_purchase::correct_purchase(ctx.data::<PgPool>()?, request).await {
        Ok(value) => Ok(CorrectPurchaseSuccess {
            result: crate::purchase_mutations::purchase_result(
                ctx.data::<PgPool>()?,
                household_id,
                &value.purchase_id,
            )
            .await?,
        }
        .into()),
        Err(error) => match user_error(&error) {
            Some(error) => Ok(error.into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

fn parse_target(
    household_id: &ID,
    target: CorrectPurchaseTargetInput,
) -> std::result::Result<(Uuid, Uuid, Option<Uuid>), ()> {
    let household_id = decode_node_id(household_id, "Household")?;
    let (purchase_id, item_id) = match target {
        CorrectPurchaseTargetInput::Purchase(target) => {
            (decode_node_id(&target.purchase_id, "Purchase")?, None)
        }
        CorrectPurchaseTargetInput::Item(target) => (
            decode_node_id(&target.purchase_id, "Purchase")?,
            Some(decode_node_id(&target.item_id, "PurchaseItem")?),
        ),
    };
    Ok((household_id, purchase_id, item_id))
}

fn parse_allocations(
    allocations: Vec<CorrectPurchaseAllocationInput>,
) -> std::result::Result<Vec<(String, i64)>, ()> {
    allocations
        .into_iter()
        .map(|allocation| {
            let pool_id = decode_node_id(&allocation.pool_id, "Pool")?;
            if allocation.weight.0 <= 0 {
                return Err(());
            }
            Ok((pool_id.to_string(), allocation.weight.0))
        })
        .collect()
}

fn parse_payments(
    payments: Vec<CorrectPurchasePaymentInput>,
) -> std::result::Result<Vec<(String, i64)>, ()> {
    payments
        .into_iter()
        .map(|payment| {
            let account_id = decode_node_id(&payment.payment_account_id, "Account")?;
            if payment.amount_yen.0 <= 0 {
                return Err(());
            }
            Ok((account_id.to_string(), payment.amount_yen.0))
        })
        .collect()
}

fn decode_node_id(id: &ID, expected_kind: &str) -> std::result::Result<Uuid, ()> {
    let bytes = URL_SAFE_NO_PAD.decode(id.as_str()).map_err(|_| ())?;
    let separator = bytes.iter().position(|byte| *byte == b':').ok_or(())?;
    let (kind, rest) = bytes.split_at(separator);
    if kind != expected_kind.as_bytes() || rest.len() != 17 {
        return Err(());
    }
    let id = Uuid::from_slice(&rest[1..]).map_err(|_| ())?;
    if id.is_nil() {
        return Err(());
    }
    Ok(id)
}

fn fingerprint(value: CorrectPurchaseFingerprint<'_>) -> Result<[u8; 32]> {
    Ok(Sha256::digest(serde_json::to_vec(&value).map_err(crate::internal_error)?).into())
}

fn bad_input() -> CorrectPurchaseUserError {
    CorrectPurchaseUserError {
        code: MutationErrorCode::BadInput,
        message: "入力を確認してください。".to_owned(),
    }
}

fn user_error(
    error: &postgres_correct_purchase::CorrectPurchasePersistenceError,
) -> Option<CorrectPurchaseUserError> {
    use postgres_correct_purchase::CorrectPurchasePersistenceError as E;
    let code = match error {
        E::InvalidUuid(_) | E::InvalidIdempotencyKey | E::InvalidDecision => {
            MutationErrorCode::BadInput
        }
        E::StatementNotRefundable => MutationErrorCode::BadInput,
        E::PurchaseNotFound | E::SourceDocumentNotFound | E::MemberNotFound => {
            MutationErrorCode::NotFound
        }
        E::IdempotencyConflict
        | E::AccountingPeriodClosed
        | E::PurchaseNotPosted
        | E::RefundExceedsPurchaseBalance
        | E::RefundExceedsItemBalance
        | E::StatementAlreadyConfirmed
        | E::StatementExtractionUnavailable
        | E::StatementReferenceNotFound
        | E::RefundDoesNotMatchCorrection => MutationErrorCode::Conflict,
        E::HouseholdMismatch => MutationErrorCode::NotFound,
        E::Pool(_)
        | E::Database(_)
        | E::InvalidPeriod
        | E::NumericOutOfRange(_)
        | E::InvalidFundingSnapshot
        | E::CorrectionNotFound => return None,
    };
    Some(CorrectPurchaseUserError {
        code,
        message: match code {
            MutationErrorCode::BadInput => "入力を確認してください。",
            MutationErrorCode::NotFound => "対象が見つかりません。",
            _ => "最新の状態を読み込んでから再試行してください。",
        }
        .to_owned(),
    })
}

impl From<CorrectPurchaseUserError> for CorrectPurchaseResult {
    fn from(value: CorrectPurchaseUserError) -> Self {
        Self::UserError(value)
    }
}
