use async_graphql::{ComplexObject, Context, Enum, ID, InputObject, Interface, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use diesel_async::AsyncConnection;
use bookkeeping_workflows::{
    create_purchase::{
        CreatePurchaseAllocation, CreatePurchaseCommand, CreatePurchaseItem,
        CreatePurchasePayment, Measure, PrintedUnitPrice, decide_create_purchase,
    },
    decide_post_purchase,
};
use chrono::Utc;
use horse_postgres::{
    PgPool, create_purchase as postgres_create,
    post_purchase as postgres_post,
    household_access as postgres_household_access,
    post_purchase_loader::{self, PostPurchaseLoadError},
    purchase_command_context::{self, PurchaseItemAllocationInput},
    purchase_command_result,
    purchase_create_and_post,
};
use serde::Serialize;
use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::{DateTime, Long, MutationErrorCode, account_mutations::AccountObject};

#[derive(Clone, Debug, InputObject)]
pub struct CreatePurchaseInput {
    pub idempotency_key: String,
    pub household_id: ID,
    pub occurred_at: DateTime,
    pub date_precision: Option<PurchaseDatePrecision>,
    pub merchant: String,
    pub origin: Option<PurchaseOrigin>,
    pub payment_allocations: Option<Vec<CreatePurchasePaymentInput>>,
    pub source_document_id: Option<String>,
    pub source_reference: Option<String>,
    pub receipt_candidate_id: Option<String>,
    pub manual_amount_override_reason: Option<String>,
    pub items: Vec<PurchaseItemInput>,
}

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq, Serialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum PurchaseDatePrecision {
    DateTime,
    DateOnly,
}

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq, Serialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum PurchaseOrigin {
    Ai,
    Manual,
    Unknown,
}

#[derive(Clone, Debug, InputObject)]
pub struct PurchaseItemInput {
    pub name: String,
    pub amount_yen: Long,
    pub quantity: Option<CreatePurchaseQuantityInput>,
    pub printed_unit_price: Option<CreatePurchasePrintedUnitPriceInput>,
    pub category_id: Option<ID>,
    pub product_variant_id: Option<ID>,
    pub allocations: Option<Vec<CreatePurchaseAllocationInput>>,
}

#[derive(Clone, Debug, InputObject)]
pub struct CreatePurchasePaymentInput {
    pub payment_account_id: ID,
    pub amount_yen: Long,
}

#[derive(Clone, Debug, InputObject)]
pub struct CreatePurchaseQuantityInput {
    pub numerator: String,
    pub denominator: String,
    pub unit: String,
}

#[derive(Clone, Debug, InputObject)]
pub struct CreatePurchasePrintedUnitPriceInput {
    pub amount_yen: Long,
    pub basis: CreatePurchaseQuantityInput,
}

#[derive(Clone, Debug, InputObject)]
pub struct CreatePurchaseAllocationInput {
    pub pool_id: ID,
    pub weight: Long,
}

#[derive(Clone, Debug, InputObject)]
pub struct PostPurchaseInput {
    pub purchase_id: ID,
    pub expected_revision: i32,
    pub idempotency_key: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct PurchaseMutationUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Clone, Debug, SimpleObject)]
#[graphql(name = "Purchase", complex)]
pub struct PurchaseObject {
    pub id: ID,
    pub household_id: ID,
    pub occurred_at: DateTime,
    pub date_precision: PurchaseDatePrecision,
    pub merchant: String,
    pub origin: PurchaseOrigin,
    pub has_evidence: bool,
    pub source_kinds: Vec<SourceKind>,
    pub payment_allocations: Vec<PurchasePayment>,
    pub items: Vec<PurchaseItem>,
    pub correction_amount_yen: Long,
    pub correction_count: i32,
    pub remaining_refundable_amount_yen: Long,
    pub total_yen: Long,
    pub currency: String,
    pub revision: i32,
    pub status: PurchaseStatus,
    pub created_at: DateTime,
    pub updated_at: DateTime,
    #[graphql(skip)]
    pub household_uuid: Uuid,
    #[graphql(skip)]
    pub purchase_uuid: Uuid,
}

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum SourceKind { Receipt, Order, Email, Card, Bank }
#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum PurchaseStatus { Draft, Posted, Closed }

#[derive(Clone, Debug, SimpleObject)]
pub struct PurchasePayment { pub payment_account_id: ID, pub account: AccountObject, pub amount_yen: Long }
#[derive(Clone, Debug, SimpleObject)]
pub struct PurchaseItem {
    pub id: ID,
    pub name: String,
    pub amount_yen: Long,
    pub remaining_refundable_amount_yen: Long,
    pub currency: String,
    pub quantity: Option<PurchaseQuantity>,
    pub printed_unit_price: Option<PrintedUnitPriceObject>,
    pub effective_unit_price: Option<EffectiveUnitPrice>,
    pub category_id: Option<ID>,
    pub product_variant_id: Option<ID>,
    pub allocations: Vec<PurchaseItemAllocation>,
    pub tags: Vec<crate::catalog_mutations::Tag>,
    pub revision: i32,
}
#[derive(Clone, Debug, SimpleObject)]
pub struct PurchaseQuantity { pub numerator: String, pub denominator: String, pub unit: String }
#[derive(Clone, Debug, SimpleObject)]
pub struct PrintedUnitPriceObject { pub amount_yen: Long, pub basis: PurchaseQuantity }
#[derive(Clone, Debug, SimpleObject)]
pub struct EffectiveUnitPrice { pub numerator_yen: String, pub denominator_yen: String }
#[derive(Clone, Debug, SimpleObject)]
pub struct PurchaseItemAllocation { pub pool_id: ID, pub pool_name: String, pub weight: Long, pub amount_yen: Long }
#[derive(Clone, Debug, SimpleObject)]
pub struct PurchaseCorrectionConnection { pub edges: Vec<PurchaseCorrectionEdge>, pub page_info: PurchasePageInfo, pub total_count: i32 }
#[derive(Clone, Debug, SimpleObject)]
pub struct PurchaseCorrectionEdge { pub cursor: String, pub node: PurchaseCorrection }
#[derive(Clone, Debug, Interface)]
#[graphql(field(name = "id", ty = "&ID"), field(name = "amount_delta_yen", ty = "&Long"), field(name = "reason", ty = "&String"), field(name = "occurred_at", ty = "&DateTime"), field(name = "settlement_month", ty = "&crate::policy_mutations::YearMonth"), field(name = "payment_allocations", ty = "&Vec<PurchasePayment>"))]
pub enum PurchaseCorrection { Wide(PurchaseWideCorrection), Item(PurchaseItemCorrection) }
#[derive(Clone, Debug, SimpleObject)]
#[graphql(name = "PurchaseWideCorrection")]
pub struct PurchaseWideCorrection { pub id: ID, pub amount_delta_yen: Long, pub reason: String, pub occurred_at: DateTime, pub settlement_month: crate::policy_mutations::YearMonth, pub payment_allocations: Vec<PurchasePayment> }
#[derive(Clone, Debug, SimpleObject)]
#[graphql(name = "PurchaseItemCorrection")]
pub struct PurchaseItemCorrection { pub id: ID, pub item_id: ID, pub amount_delta_yen: Long, pub reason: String, pub occurred_at: DateTime, pub settlement_month: crate::policy_mutations::YearMonth, pub payment_allocations: Vec<PurchasePayment> }
#[derive(Clone, Debug, SimpleObject)]
#[graphql(name = "PageInfo")]
pub struct PurchasePageInfo { pub has_next_page: bool, pub has_previous_page: bool, pub start_cursor: Option<String>, pub end_cursor: Option<String> }

#[ComplexObject]
impl PurchaseObject {
    async fn corrections(&self, ctx: &Context<'_>, first: Option<i32>, after: Option<String>) -> Result<PurchaseCorrectionConnection> {
        let pool = ctx.data::<PgPool>()?;
        let projection = horse_postgres::purchase_corrections::load_purchase_corrections(
            pool, self.household_uuid, self.purchase_uuid, first, after.as_deref(),
        ).await.map_err(correction_error)?;
        let mut edges = Vec::with_capacity(projection.edges.len());
        for correction in projection.edges {
            let payments = correction.payment_allocations.into_iter().map(|payment| {
                let kind = account_kind(&payment.account_kind)?;
                Ok(PurchasePayment {
                    payment_account_id: encode_node_id("Account", payment.account_id),
                    account: AccountObject {
                        id: encode_node_id("Account", payment.account_id), name: payment.account_name,
                        kind, card_last4: payment.account_card_last4,
                        pool_id: payment.account_pool_id.map(|id| encode_node_id("Pool", id)),
                    },
                    amount_yen: Long(payment.amount_yen),
                })
            }).collect::<Result<Vec<_>>>()?;
            let id = encode_node_id("PurchaseCorrection", correction.id);
            let settlement_month = crate::policy_mutations::YearMonth::from_naive_date(correction.settlement_month);
            let occurred_at: DateTime = correction.occurred_at.into();
            let node = match correction.item_id {
                Some(item_id) => PurchaseCorrection::Item(PurchaseItemCorrection {
                    id, item_id: encode_node_id("PurchaseItem", item_id), amount_delta_yen: Long(correction.amount_delta_yen),
                    reason: correction.reason, occurred_at, settlement_month, payment_allocations: payments,
                }),
                None => PurchaseCorrection::Wide(PurchaseWideCorrection {
                    id, amount_delta_yen: Long(correction.amount_delta_yen), reason: correction.reason,
                    occurred_at, settlement_month, payment_allocations: payments,
                }),
            };
            let cursor = horse_postgres::purchase_corrections::encode_cursor(&correction);
            edges.push(PurchaseCorrectionEdge { cursor, node });
        }
        let total_count = i32::try_from(projection.total_count).map_err(crate::internal_error)?;
        let start_cursor = edges.first().map(|edge| edge.cursor.clone());
        let end_cursor = edges.last().map(|edge| edge.cursor.clone());
        Ok(PurchaseCorrectionConnection {
            edges,
            page_info: PurchasePageInfo {
                has_next_page: projection.has_next_page,
                has_previous_page: projection.has_previous_page,
                start_cursor,
                end_cursor,
            },
            total_count,
        })
    }
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CreatePurchaseSuccess { pub result: PurchaseObject }
#[derive(Clone, Debug, SimpleObject)]
pub struct CreateAndPostPurchaseSuccess { pub result: PurchaseObject }
#[derive(Clone, Debug, SimpleObject)]
pub struct PostPurchaseSuccess { pub result: PurchaseObject }

#[derive(Union)]
pub enum CreatePurchaseResult { Success(CreatePurchaseSuccess), UserError(CreatePurchaseUserError) }
#[derive(Clone, Debug, SimpleObject)]
pub struct CreatePurchaseUserError { pub code: MutationErrorCode, pub message: String }
#[derive(Union)]
pub enum CreateAndPostPurchaseResult { Success(CreateAndPostPurchaseSuccess), UserError(CreateAndPostPurchaseUserError) }
#[derive(Clone, Debug, SimpleObject)]
pub struct CreateAndPostPurchaseUserError { pub code: MutationErrorCode, pub message: String }
#[derive(Union)]
pub enum PostPurchaseResult { Success(PostPurchaseSuccess), UserError(PostPurchaseUserError) }
#[derive(Clone, Debug, SimpleObject)]
pub struct PostPurchaseUserError { pub code: MutationErrorCode, pub message: String }

pub async fn create_purchase(ctx: &Context<'_>, input: CreatePurchaseInput) -> Result<CreatePurchaseResult> {
    let actor = crate::actor(ctx)?;
    let parsed = match parse_create_input(actor.0, input) { Ok(value) => value, Err(error) => return Ok(error.into()) };
    let pool = ctx.data::<PgPool>()?;
    match postgres_household_access::is_active_member(pool, parsed.household_id, actor.0).await {
        Ok(true) => {}, Ok(false) => return Ok(not_found().into()), Err(error) => return Err(crate::internal_error(error)),
    }
    let allocation_inputs = parsed.command.items.iter().map(|item| PurchaseItemAllocationInput {
        category_id: item.category_id.as_deref().and_then(|id| Uuid::parse_str(id).ok()),
        product_variant_id: item.product_variant_id.as_deref().and_then(|id| Uuid::parse_str(id).ok()),
        merchant_key: normalize_merchant(&parsed.command.merchant),
        allocations: item.allocations.iter().map(|allocation| (Uuid::parse_str(&allocation.pool_id).expect("parsed pool identifier"), allocation.weight)).collect(),
    }).collect::<Vec<_>>();
    let context = match purchase_command_context::load_purchase_command_context(pool, parsed.household_id, &normalize_merchant(&parsed.command.merchant), &allocation_inputs).await {
        Ok(value) => value,
        Err(error) => match context_user_error(&error) { Some(error) => return Ok(error.into()), None => return Err(crate::internal_error(error)) },
    };
    let command = apply_allocation_context(parsed.command, context);
    let decision = match decide_create_purchase(command) { Ok(value) => value, Err(_) => return Ok(bad_input().into()) };
    let persisted = match postgres_create::persist_create_purchase(pool, parsed.persistence, decision).await {
        Ok(value) => value,
        Err(error) => match create_user_error(&error) { Some(error) => return Ok(error.into()), None => return Err(crate::internal_error(error)) },
    };
    Ok(CreatePurchaseSuccess { result: purchase_result(pool, parsed.household_id, &persisted.purchase_id).await? }.into())
}

pub async fn create_and_post_purchase(ctx: &Context<'_>, input: CreatePurchaseInput) -> Result<CreateAndPostPurchaseResult> {
    let actor = crate::actor(ctx)?;
    let parsed = match parse_create_input(actor.0, input) { Ok(value) => value, Err(error) => return Ok(error.into()) };
    let pool = ctx.data::<PgPool>()?;
    match postgres_household_access::is_active_member(pool, parsed.household_id, actor.0).await {
        Ok(true) => {}, Ok(false) => return Ok(not_found().into()), Err(error) => return Err(crate::internal_error(error)),
    }
    let allocation_inputs = parsed.command.items.iter().map(|item| PurchaseItemAllocationInput {
        category_id: item.category_id.as_deref().and_then(|id| Uuid::parse_str(id).ok()),
        product_variant_id: item.product_variant_id.as_deref().and_then(|id| Uuid::parse_str(id).ok()),
        merchant_key: normalize_merchant(&parsed.command.merchant),
        allocations: item.allocations.iter().map(|allocation| (Uuid::parse_str(&allocation.pool_id).expect("parsed pool identifier"), allocation.weight)).collect(),
    }).collect::<Vec<_>>();
    let context = match purchase_command_context::load_purchase_command_context(pool, parsed.household_id, &normalize_merchant(&parsed.command.merchant), &allocation_inputs).await {
        Ok(value) => value,
        Err(error) => match context_user_error(&error) { Some(error) => return Ok(error.into()), None => return Err(crate::internal_error(error)) },
    };
    let post_fingerprint = post_fingerprint(&parsed.command.purchase_id, 1)?;
    let decision = match decide_create_purchase(apply_allocation_context(parsed.command, context)) { Ok(value) => value, Err(_) => return Ok(bad_input().into()) };
    let persisted = match purchase_create_and_post::persist_create_and_post_purchase(pool, parsed.persistence, decision, post_fingerprint).await {
        Ok(value) => value,
        Err(error) => return Err(crate::internal_error(error)),
    };
    Ok(CreateAndPostPurchaseSuccess { result: purchase_result(pool, parsed.household_id, &persisted.purchase_id).await? }.into())
}

pub async fn post_purchase(ctx: &Context<'_>, input: PostPurchaseInput) -> Result<PostPurchaseResult> {
    let actor = crate::actor(ctx)?;
    let purchase_id = match decode_node_id(&input.purchase_id, "Purchase") { Ok(id) => id, Err(()) => return Ok(bad_input().into()) };
    if input.expected_revision < 0 || input.idempotency_key.is_empty() { return Ok(bad_input().into()); }
    let household_id = match purchase_command_result::load_purchase_household_id(ctx.data::<PgPool>()?, purchase_id).await { Ok(Some(id)) => id, Ok(None) => return Ok(not_found().into()), Err(error) => return Err(crate::internal_error(error)) };
    match postgres_household_access::is_active_member(ctx.data::<PgPool>()?, household_id, actor.0).await {
        Ok(true) => {}, Ok(false) => return Ok(not_found().into()), Err(error) => return Err(crate::internal_error(error)),
    }
    let fingerprint = post_fingerprint(&purchase_id.to_string(), input.expected_revision)?;
    let mut conn = ctx.data::<PgPool>()?.get().await.map_err(crate::internal_error)?;
    let projection = purchase_command_result::load_purchase_command_result(ctx.data::<PgPool>()?, household_id, purchase_id).await.map_err(crate::internal_error)?;
    let result = (&mut *conn).transaction::<postgres_post::PersistedPostPurchase, PostMutationError, _>(async |conn| {
        let loaded = post_purchase_loader::load_post_purchase_command(conn, &household_id.to_string(), &purchase_id.to_string(), input.expected_revision as u64)
            .await.map_err(PostMutationError::Load)?;
        let decision = decide_post_purchase(loaded.command).map_err(|_| PostMutationError::Workflow)?;
        postgres_post::persist_post_purchase_in_transaction(conn, postgres_post::PersistPostPurchase {
            household_id: household_id.to_string(), actor_id: actor.0.to_string(), expected_revision: input.expected_revision as u64,
            ledger_description: projection.merchant.clone(), idempotency_key: input.idempotency_key.clone(),
            request_fingerprint: fingerprint, resolved_payments: loaded.resolved_payments,
        }, decision).await.map_err(PostMutationError::Persistence)
    }).await;
    match result {
        Ok(value) => Ok(PostPurchaseSuccess { result: purchase_result(ctx.data::<PgPool>()?, household_id, &value.purchase_id).await? }.into()),
        Err(PostMutationError::Load(error)) => match post_load_user_error(&error) { Some(error) => Ok(error.into()), None => Err(crate::internal_error(error)) },
        Err(PostMutationError::Workflow) => Ok(conflict().into()),
        Err(PostMutationError::Persistence(error)) => match post_user_error(&error) { Some(error) => Ok(error.into()), None => Err(crate::internal_error(error)) },
        Err(PostMutationError::Database(error)) => Err(crate::internal_error(error)),
    }
}

struct ParsedCreatePurchase {
    command: CreatePurchaseCommand,
    persistence: postgres_create::PersistCreatePurchase,
    household_id: Uuid,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CanonicalCreatePurchase {
    idempotency_key: String,
    household_id: String,
    occurred_at: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    date_precision: Option<String>,
    merchant: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    origin: Option<String>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    payment_allocations: Vec<CanonicalPayment>,
    #[serde(skip_serializing_if = "Option::is_none")]
    source_document_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    source_reference: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    receipt_candidate_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    manual_amount_override_reason: Option<String>,
    items: Vec<CanonicalItem>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CanonicalPayment { payment_account_id: String, amount_yen: i64 }
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CanonicalItem {
    name: String,
    amount_yen: i64,
    #[serde(skip_serializing_if = "Option::is_none")]
    quantity: Option<CanonicalQuantity>,
    #[serde(skip_serializing_if = "Option::is_none")]
    printed_unit_price: Option<CanonicalPrintedPrice>,
    #[serde(skip_serializing_if = "Option::is_none")]
    category_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    product_variant_id: Option<String>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    allocations: Vec<CanonicalAllocation>,
}
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CanonicalQuantity { numerator: String, denominator: String, unit: String }
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CanonicalPrintedPrice { amount_yen: i64, basis: CanonicalQuantity }
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct CanonicalAllocation { pool_id: String, weight: i64 }

fn parse_create_input(actor_id: Uuid, input: CreatePurchaseInput) -> std::result::Result<ParsedCreatePurchase, PurchaseMutationUserError> {
    let household_id = crate::decode_household_id(&input.household_id).map_err(|_| bad_input())?;
    let occurred_at = input.occurred_at.parse_utc().map_err(|_| bad_input())?;
    let date_precision = input.date_precision.map(|value| match value { PurchaseDatePrecision::DateTime => "DATE_TIME", PurchaseDatePrecision::DateOnly => "DATE_ONLY" }.to_owned()).unwrap_or_default();
    let origin = input.origin.map(|value| match value { PurchaseOrigin::Ai => "AI", PurchaseOrigin::Manual => "MANUAL", PurchaseOrigin::Unknown => "UNKNOWN" }.to_owned()).unwrap_or_default();
    let purchase_id = Uuid::now_v7().to_string();
    let mut items = Vec::with_capacity(input.items.len());
    for item in &input.items {
        let category_id = item.category_id.as_ref().map(|id| decode_node_id(id, "Category").map(|id| id.to_string())).transpose()?;
        let product_variant_id = item.product_variant_id.as_ref().map(|id| decode_node_id(id, "ProductVariant").map(|id| id.to_string())).transpose()?;
        let quantity = item.quantity.as_ref().map(|value| Ok(Measure { numerator: value.numerator.parse().map_err(|_| bad_input())?, denominator: value.denominator.parse().map_err(|_| bad_input())?, unit: value.unit.clone() })).transpose()?;
        let printed_unit_price = item.printed_unit_price.as_ref().map(|value| Ok(PrintedUnitPrice {
            amount_yen: value.amount_yen.0,
            basis: Measure { numerator: value.basis.numerator.parse().map_err(|_| bad_input())?, denominator: value.basis.denominator.parse().map_err(|_| bad_input())?, unit: value.basis.unit.clone() },
        })).transpose()?;
        let allocations = item.allocations.as_deref().unwrap_or_default().iter().map(|value| {
            decode_node_id(&value.pool_id, "Pool").map(|id| CreatePurchaseAllocation { pool_id: id.to_string(), weight: value.weight.0, rule_version: 1, rule_id: None })
        }).collect::<std::result::Result<Vec<_>, _>>()?;
        items.push(CreatePurchaseItem {
            item_id: Uuid::now_v7().to_string(), name: item.name.clone(), amount_yen: item.amount_yen.0,
            category_id, product_variant_id, quantity, printed_unit_price,
            allocation_override: !allocations.is_empty(), allocations,
        });
    }
    let payments = input.payment_allocations.as_deref().unwrap_or_default().iter().map(|payment| {
        decode_node_id(&payment.payment_account_id, "Account").map(|id| CreatePurchasePayment { account_id: id.to_string(), amount_yen: payment.amount_yen.0 })
    }).collect::<std::result::Result<Vec<_>, _>>()?;
    let canonical = canonical_input(&input);
    let fingerprint: [u8; 32] = Sha256::digest(serde_json::to_vec(&canonical).map_err(|_| bad_input())?).into();
    Ok(ParsedCreatePurchase {
        command: CreatePurchaseCommand {
            purchase_id,
            occurred_at: occurred_at.to_rfc3339(),
            date_precision,
            merchant: input.merchant.clone(),
            origin,
            allocation_rule_version: 1,
            source_document_id: input.source_document_id.clone(),
            source_reference: input.source_reference.clone(),
            receipt_candidate_id: input.receipt_candidate_id.clone(),
            manual_amount_override_reason: input.manual_amount_override_reason.clone(),
            items,
            payments,
        },
        persistence: postgres_create::PersistCreatePurchase {
            household_id: household_id.to_string(), actor_id: actor_id.to_string(), idempotency_key: input.idempotency_key,
            request_fingerprint: fingerprint,
        },
        household_id,
    })
}

fn canonical_input(input: &CreatePurchaseInput) -> CanonicalCreatePurchase {
    CanonicalCreatePurchase {
        idempotency_key: input.idempotency_key.clone(), household_id: input.household_id.to_string(), occurred_at: input.occurred_at.as_str().to_owned(),
        date_precision: input.date_precision.map(|value| match value { PurchaseDatePrecision::DateTime => "DATE_TIME", PurchaseDatePrecision::DateOnly => "DATE_ONLY" }.to_owned()),
        merchant: input.merchant.clone(), origin: input.origin.map(|value| match value { PurchaseOrigin::Ai => "AI", PurchaseOrigin::Manual => "MANUAL", PurchaseOrigin::Unknown => "UNKNOWN" }.to_owned()),
        payment_allocations: input.payment_allocations.as_deref().unwrap_or_default().iter().map(|value| CanonicalPayment { payment_account_id: value.payment_account_id.to_string(), amount_yen: value.amount_yen.0 }).collect(),
        source_document_id: input.source_document_id.clone(), source_reference: input.source_reference.clone(), receipt_candidate_id: input.receipt_candidate_id.clone(),
        manual_amount_override_reason: input.manual_amount_override_reason.clone(),
        items: input.items.iter().map(|value| CanonicalItem {
            name: value.name.clone(), amount_yen: value.amount_yen.0,
            quantity: value.quantity.as_ref().map(|item| CanonicalQuantity { numerator: item.numerator.clone(), denominator: item.denominator.clone(), unit: item.unit.clone() }),
            printed_unit_price: value.printed_unit_price.as_ref().map(|item| CanonicalPrintedPrice { amount_yen: item.amount_yen.0, basis: CanonicalQuantity { numerator: item.basis.numerator.clone(), denominator: item.basis.denominator.clone(), unit: item.basis.unit.clone() } }),
            category_id: value.category_id.as_ref().map(ToString::to_string), product_variant_id: value.product_variant_id.as_ref().map(ToString::to_string),
            allocations: value.allocations.as_deref().unwrap_or_default().iter().map(|allocation| CanonicalAllocation { pool_id: allocation.pool_id.to_string(), weight: allocation.weight.0 }).collect(),
        }).collect(),
    }
}

fn apply_allocation_context(mut command: CreatePurchaseCommand, context: purchase_command_context::PurchaseCommandContext) -> CreatePurchaseCommand {
    command.allocation_rule_version = context.allocation_rule_version;
    for (item, resolved) in command.items.iter_mut().zip(context.items) {
        if item.category_id.is_none() { item.category_id = resolved.category_id.map(|id| id.to_string()); }
        item.allocation_override = resolved.allocation_override;
        item.allocations = resolved.allocations.into_iter().map(|allocation| CreatePurchaseAllocation {
            pool_id: allocation.pool_id.to_string(), weight: allocation.weight, rule_version: allocation.rule_version,
            rule_id: allocation.rule_id.map(|id| id.to_string()),
        }).collect();
    }
    command
}

fn decode_node_id(value: &ID, expected_kind: &str) -> std::result::Result<Uuid, PurchaseMutationUserError> {
    let bytes = URL_SAFE_NO_PAD.decode(value.as_str()).map_err(|_| bad_input())?;
    let separator = bytes.iter().position(|byte| *byte == b':').ok_or_else(bad_input)?;
    let (kind, payload) = bytes.split_at(separator);
    let payload = &payload[1..];
    if kind != expected_kind.as_bytes() || payload.len() != 16 { return Err(bad_input()); }
    let id = Uuid::from_slice(payload).map_err(|_| bad_input())?;
    if id.is_nil() { return Err(bad_input()); }
    Ok(id)
}

fn encode_node_id(kind: &str, id: Uuid) -> ID {
    let mut bytes = Vec::with_capacity(kind.len() + 17);
    bytes.extend_from_slice(kind.as_bytes()); bytes.push(b':'); bytes.extend_from_slice(id.as_bytes());
    ID(URL_SAFE_NO_PAD.encode(bytes))
}

fn normalize_merchant(value: &str) -> String { value.split_whitespace().collect::<Vec<_>>().join(" ").to_lowercase() }

fn post_fingerprint(purchase_id: &str, expected_revision: i32) -> Result<[u8; 32]> {
    #[derive(Serialize)] struct Request { #[serde(rename = "PurchaseID")] purchase_id: String, #[serde(rename = "ExpectedRevision")] expected_revision: i64 }
    let request = Request { purchase_id: purchase_id.to_owned(), expected_revision: i64::from(expected_revision) };
    Ok(Sha256::digest(serde_json::to_vec(&request).map_err(crate::internal_error)?).into())
}

async fn purchase_result(pool: &PgPool, household_id: Uuid, purchase_id: &str) -> Result<PurchaseObject> {
    let purchase_id = Uuid::parse_str(purchase_id).map_err(crate::internal_error)?;
    let projection = purchase_command_result::load_purchase_command_result(pool, household_id, purchase_id).await.map_err(crate::internal_error)?;
    PurchaseObject::try_from(projection).map_err(crate::internal_error)
}

impl TryFrom<purchase_command_result::PurchaseCommandResult> for PurchaseObject {
    type Error = &'static str;
    fn try_from(value: purchase_command_result::PurchaseCommandResult) -> std::result::Result<Self, Self::Error> {
        let date_precision = match value.date_precision.as_str() { "DATE_TIME" => PurchaseDatePrecision::DateTime, "DATE_ONLY" => PurchaseDatePrecision::DateOnly, _ => return Err("invalid purchase date precision") };
        let origin = match value.origin.as_str() { "AI" => PurchaseOrigin::Ai, "MANUAL" => PurchaseOrigin::Manual, "UNKNOWN" => PurchaseOrigin::Unknown, _ => return Err("invalid purchase origin") };
        let status = match value.status.as_str() { "DRAFT" => PurchaseStatus::Draft, "POSTED" => PurchaseStatus::Posted, "CLOSED" => PurchaseStatus::Closed, _ => return Err("invalid purchase status") };
        let source_kinds = value.source_kinds.iter().map(|kind| match kind.as_str() { "RECEIPT" => Ok(SourceKind::Receipt), "ORDER" => Ok(SourceKind::Order), "EMAIL" => Ok(SourceKind::Email), "CARD" => Ok(SourceKind::Card), "BANK" => Ok(SourceKind::Bank), _ => Err("invalid source kind") }).collect::<std::result::Result<Vec<_>, _>>()?;
        let items = value.items.iter().map(|item| {
            let quantity = item.quantity_numerator.zip(item.quantity_denominator).zip(item.quantity_unit.clone()).map(|((numerator, denominator), unit)| PurchaseQuantity { numerator: numerator.to_string(), denominator: denominator.to_string(), unit });
            let printed_unit_price = item.printed_unit_price_yen.zip(item.printed_unit_price_quantity_numerator).zip(item.printed_unit_price_quantity_denominator).zip(item.printed_unit_price_unit.clone()).map(|(((amount_yen, numerator), denominator), unit)| PrintedUnitPriceObject { amount_yen: Long(amount_yen), basis: PurchaseQuantity { numerator: numerator.to_string(), denominator: denominator.to_string(), unit } });
            let effective_unit_price = item.quantity_numerator.zip(item.quantity_denominator).and_then(|(numerator, denominator)| effective_unit_price(item.amount_yen, numerator, denominator));
            Ok(PurchaseItem {
                id: encode_node_id("PurchaseItem", item.id), name: item.name.clone(), amount_yen: Long(item.amount_yen), remaining_refundable_amount_yen: Long(item.remaining_refundable_amount_yen), currency: "JPY".to_owned(), quantity, printed_unit_price, effective_unit_price,
                category_id: item.category_id.map(|id| encode_node_id("Category", id)), product_variant_id: item.product_variant_id.map(|id| encode_node_id("ProductVariant", id)),
                allocations: item.allocations.iter().map(|allocation| PurchaseItemAllocation { pool_id: encode_node_id("Pool", allocation.pool_id), pool_name: allocation.pool_name.clone(), weight: Long(allocation.weight), amount_yen: Long(allocation.amount_yen) }).collect(),
                tags: item.tags.iter().map(|(id, name, archived, revision)| crate::catalog_mutations::map_tag(catalog_workflows::tag::Tag { id: id.to_string(), household_id: value.household_id.to_string(), name: name.clone(), archived: *archived, revision: *revision }).map_err(|_| "invalid tag projection")).collect::<std::result::Result<Vec<_>, &'static str>>()?,
                revision: i32::try_from(item.revision).map_err(|_| "purchase item revision out of range")?,
            })
        }).collect::<std::result::Result<Vec<_>, &'static str>>()?;
        let payment_allocations = value.payments.iter().map(|payment| {
            let kind = match payment.account_kind.as_str() { "BANK" => crate::account_mutations::AccountKind::Bank, "CASH" => crate::account_mutations::AccountKind::Cash, "CARD" => crate::account_mutations::AccountKind::Card, "OTHER" => crate::account_mutations::AccountKind::Other, _ => return Err("invalid account kind") };
            Ok(PurchasePayment { payment_account_id: encode_node_id("Account", payment.account_id), account: AccountObject { id: encode_node_id("Account", payment.account_id), name: payment.account_name.clone(), kind, card_last4: payment.account_card_last4.clone(), pool_id: payment.account_pool_id.map(|id| encode_node_id("Pool", id)) }, amount_yen: Long(payment.amount_yen) })
        }).collect::<std::result::Result<Vec<_>, &'static str>>()?;
        Ok(Self {
            id: encode_node_id("Purchase", value.id), household_id: encode_node_id("Household", value.household_id), occurred_at: value.occurred_at.into(), date_precision, merchant: value.merchant,
            origin, has_evidence: value.has_evidence, source_kinds, payment_allocations, items, correction_amount_yen: Long(value.correction_amount_yen),
            correction_count: i32::try_from(value.correction_count).map_err(|_| "purchase correction count out of range")?, remaining_refundable_amount_yen: Long(value.remaining_refundable_amount_yen),
            total_yen: Long(value.total_yen), currency: "JPY".to_owned(), revision: i32::try_from(value.revision).map_err(|_| "purchase revision out of range")?, status, created_at: value.created_at.into(), updated_at: value.updated_at.into(),
            household_uuid: value.household_id, purchase_uuid: value.id,
        })
    }
}

fn effective_unit_price(amount: i64, numerator: i64, denominator: i64) -> Option<EffectiveUnitPrice> {
    if amount < 0 || numerator <= 0 || denominator <= 0 { return None; }
    let mut n = i128::from(amount).checked_mul(i128::from(denominator))?;
    let mut d = i128::from(numerator);
    let gcd = gcd(n, d);
    n /= gcd; d /= gcd;
    Some(EffectiveUnitPrice { numerator_yen: n.to_string(), denominator_yen: d.to_string() })
}

fn gcd(mut a: i128, mut b: i128) -> i128 { while b != 0 { let r = a % b; a = b; b = r; } a.max(1) }

fn bad_input() -> PurchaseMutationUserError { PurchaseMutationUserError { code: MutationErrorCode::BadInput, message: "入力を確認してください。".to_owned() } }
fn not_found() -> PurchaseMutationUserError { PurchaseMutationUserError { code: MutationErrorCode::NotFound, message: "対象が見つかりません。".to_owned() } }
fn conflict() -> PurchaseMutationUserError { PurchaseMutationUserError { code: MutationErrorCode::Conflict, message: "最新の状態を読み込んでから再試行してください。".to_owned() } }

impl From<PurchaseMutationUserError> for CreatePurchaseResult { fn from(value: PurchaseMutationUserError) -> Self { Self::UserError(CreatePurchaseUserError { code: value.code, message: value.message }) } }
impl From<PurchaseMutationUserError> for CreateAndPostPurchaseResult { fn from(value: PurchaseMutationUserError) -> Self { Self::UserError(CreateAndPostPurchaseUserError { code: value.code, message: value.message }) } }
impl From<PurchaseMutationUserError> for PostPurchaseResult { fn from(value: PurchaseMutationUserError) -> Self { Self::UserError(PostPurchaseUserError { code: value.code, message: value.message }) } }

fn context_user_error(error: &purchase_command_context::PurchaseCommandContextError) -> Option<PurchaseMutationUserError> {
    use purchase_command_context::PurchaseCommandContextError as E;
    match error { E::HouseholdNotFound => Some(not_found()), E::MissingDefaultAllocation | E::MissingProductVariant | E::MissingAllocationRule => Some(bad_input()), E::Pool(_) | E::Database(_) | E::InvalidVersion => None }
}
fn create_user_error(error: &postgres_create::CreatePurchasePersistenceError) -> Option<PurchaseMutationUserError> {
    use postgres_create::CreatePurchasePersistenceError as E;
    match error { E::HouseholdNotFound | E::MemberNotFound | E::ReferenceNotFound(_) => Some(not_found()), E::AccountingPeriodClosed | E::IdempotencyConflict | E::AllocationRuleMismatch => Some(conflict()), E::InvalidUuid(_) | E::InvalidOccurredAt | E::InvalidIdempotencyKey | E::NumericOutOfRange(_) | E::InvalidDecision | E::InvalidSourceReference | E::InvalidReceiptOverrideReason => Some(bad_input()), E::Pool(_) | E::Database(_) => None, _ => Some(bad_input()) }
}
fn post_load_user_error(error: &PostPurchaseLoadError) -> Option<PurchaseMutationUserError> {
    match error { PostPurchaseLoadError::MissingPurchase | PostPurchaseLoadError::HouseholdMismatch(_) => Some(not_found()), PostPurchaseLoadError::MissingAccountingPeriod => Some(conflict()), PostPurchaseLoadError::Database(_) => None, _ => Some(bad_input()) }
}
fn post_user_error(error: &postgres_post::PostPurchasePersistenceError) -> Option<PurchaseMutationUserError> {
    use postgres_post::PostPurchasePersistenceError as E;
    match error { E::AccountingPeriodClosed | E::StalePurchase | E::IdempotencyConflict => Some(conflict()), E::HouseholdMismatch => Some(not_found()), E::Pool(_) | E::Database(_) => None, _ => Some(bad_input()) }
}

