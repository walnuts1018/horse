use async_graphql::{Context, ID, InputObject, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_postgres::correct_purchase as postgres_correct_purchase;
use horse_postgres_common::PgPool;
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
struct CorrectPurchaseFingerprint {
    household_id: String,
    target: CorrectPurchaseTargetFingerprint,
    amount_delta_yen: i64,
    reason: String,
    allocations: Option<Vec<CorrectPurchaseAllocationFingerprint>>,
    payment_allocations: Option<Vec<CorrectPurchasePaymentFingerprint>>,
    idempotency_key: String,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CorrectPurchaseFromStatementFingerprint {
    household_id: String,
    source_document_id: String,
    source_reference: String,
    target: CorrectPurchaseTargetFingerprint,
    reason: String,
    idempotency_key: String,
}

#[derive(Serialize)]
struct CorrectPurchaseTargetFingerprint {
    purchase: Option<PurchaseTargetFingerprint>,
    item: Option<ItemTargetFingerprint>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct PurchaseTargetFingerprint {
    purchase_id: String,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct ItemTargetFingerprint {
    purchase_id: String,
    item_id: String,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CorrectPurchaseAllocationFingerprint {
    pool_id: String,
    weight: i64,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CorrectPurchasePaymentFingerprint {
    payment_account_id: String,
    amount_yen: i64,
}

pub async fn correct_purchase(
    ctx: &Context<'_>,
    input: CorrectPurchaseInput,
) -> Result<CorrectPurchaseResult> {
    let (household_id, purchase_id, item_id) =
        match parse_target(&input.household_id, &input.target) {
            Ok(value) => value,
            Err(()) => return Ok(bad_input().into()),
        };
    let fingerprint_allocations = match fingerprint_allocations(input.allocations.as_deref()) {
        Ok(value) => value,
        Err(()) => return Ok(bad_input().into()),
    };
    let fingerprint_payments = match fingerprint_payments(input.payment_allocations.as_deref()) {
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
    let target_fingerprint = match fingerprint_target(&input.target) {
        Ok(value) => value,
        Err(()) => return Ok(bad_input().into()),
    };
    let fingerprint = fingerprint(CorrectPurchaseFingerprint {
        household_id: household_id.to_string(),
        target: target_fingerprint,
        amount_delta_yen: input.amount_delta_yen.0,
        reason: input.reason.clone(),
        allocations: fingerprint_allocations,
        payment_allocations: fingerprint_payments,
        idempotency_key: input.idempotency_key.clone(),
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
    let (household_id, purchase_id, item_id) =
        match parse_target(&input.household_id, &input.target) {
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
    let target_fingerprint = match fingerprint_target(&input.target) {
        Ok(value) => value,
        Err(()) => return Ok(bad_input().into()),
    };
    let fingerprint = fingerprint(CorrectPurchaseFromStatementFingerprint {
        household_id: household_id.to_string(),
        source_document_id: input.source_document_id.to_string(),
        source_reference: input.source_reference.clone(),
        target: target_fingerprint,
        reason: input.reason.clone(),
        idempotency_key: input.idempotency_key.clone(),
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
    let purchase_id = request.purchase_id.clone();
    match postgres_correct_purchase::correct_purchase(ctx.data::<PgPool>()?, request).await {
        Ok(_) => Ok(CorrectPurchaseSuccess {
            result: crate::purchase_mutations::purchase_result(
                ctx.data::<PgPool>()?,
                household_id,
                &purchase_id,
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
    target: &CorrectPurchaseTargetInput,
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

fn fingerprint_allocations(
    allocations: Option<&[CorrectPurchaseAllocationInput]>,
) -> std::result::Result<Option<Vec<CorrectPurchaseAllocationFingerprint>>, ()> {
    let Some(allocations) = allocations.filter(|values| !values.is_empty()) else {
        return Ok(None);
    };
    allocations
        .iter()
        .map(|allocation| {
            Ok(CorrectPurchaseAllocationFingerprint {
                pool_id: decode_node_id(&allocation.pool_id, "Pool")?.to_string(),
                weight: allocation.weight.0,
            })
        })
        .collect::<std::result::Result<Vec<_>, _>>()
        .map(Some)
}

fn fingerprint_payments(
    payments: Option<&[CorrectPurchasePaymentInput]>,
) -> std::result::Result<Option<Vec<CorrectPurchasePaymentFingerprint>>, ()> {
    let Some(payments) = payments.filter(|values| !values.is_empty()) else {
        return Ok(None);
    };
    payments
        .iter()
        .map(|payment| {
            Ok(CorrectPurchasePaymentFingerprint {
                payment_account_id: decode_node_id(&payment.payment_account_id, "Account")?
                    .to_string(),
                amount_yen: payment.amount_yen.0,
            })
        })
        .collect::<std::result::Result<Vec<_>, _>>()
        .map(Some)
}

fn fingerprint_target(
    target: &CorrectPurchaseTargetInput,
) -> std::result::Result<CorrectPurchaseTargetFingerprint, ()> {
    match target {
        CorrectPurchaseTargetInput::Purchase(target) => Ok(CorrectPurchaseTargetFingerprint {
            purchase: Some(PurchaseTargetFingerprint {
                purchase_id: decode_node_id(&target.purchase_id, "Purchase")?.to_string(),
            }),
            item: None,
        }),
        CorrectPurchaseTargetInput::Item(target) => Ok(CorrectPurchaseTargetFingerprint {
            purchase: None,
            item: Some(ItemTargetFingerprint {
                purchase_id: decode_node_id(&target.purchase_id, "Purchase")?.to_string(),
                item_id: decode_node_id(&target.item_id, "PurchaseItem")?.to_string(),
            }),
        }),
    }
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
    let bytes = URL_SAFE_NO_PAD.decode(&id.0).map_err(|_| ())?;
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

fn fingerprint(value: impl Serialize) -> Result<[u8; 32]> {
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
        E::PurchaseNotFound
        | E::SourceDocumentNotFound
        | E::StatementReferenceNotFound
        | E::MemberNotFound => MutationErrorCode::NotFound,
        E::IdempotencyConflict
        | E::AccountingPeriodClosed
        | E::PurchaseNotPosted
        | E::RefundExceedsPurchaseBalance
        | E::RefundExceedsItemBalance
        | E::StatementAlreadyConfirmed
        | E::RefundDoesNotMatchCorrection => MutationErrorCode::Conflict,
        E::StatementExtractionUnavailable => MutationErrorCode::BadInput,
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
