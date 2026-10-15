use std::collections::{BTreeMap, BTreeSet};

use bookkeeping_domain::PositiveYen;
use bookkeeping_domain::{AllocationShares, AllocationWeight, Yen};
use bookkeeping_workflows::{
    AccountId, ItemId, PoolId as WorkflowPoolId, PurchaseId, PurchaseStatus, Side,
    correct_purchase::{
        CorrectPurchaseDecision, CorrectionCommand, CorrectionFundingEffects,
        CorrectionFundingRule, CorrectionMemberBurdenSnapshot, CorrectionPaymentFundingSnapshot,
        CorrectionPurchaseBurdenSnapshot, CorrectionPurchasePayment,
        CorrectionSettlementAdjustment, CorrectionSettlementPolicy, CorrectionTarget,
        LinkStatementRefundCorrectionCommand, LinkStatementRefundCorrectionError,
        AccountingPeriod, OriginalPeriodStatus, SignedYenDelta, StatementRefundObservation,
        calculate_correction_funding_effects, decide_correct_purchase,
        decide_link_statement_refund_correction,
    },
};
use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now as recorded_at,
    result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public};

const SOURCE_KIND: &str = "CORRECTION";

#[derive(Debug, Clone)]
pub struct CorrectPurchaseRequest {
    pub household_id: String,
    pub actor_id: String,
    pub purchase_id: String,
    pub item_id: Option<String>,
    pub amount_delta_yen: Option<i64>,
    pub reason: String,
    pub allocations: Vec<(String, i64)>,
    pub payments: Vec<(String, i64)>,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
    pub source_document_id: Option<String>,
    pub source_reference: Option<String>,
}

#[derive(Debug, Clone)]
pub struct PersistCorrectPurchase {
    pub household_id: String,
    pub actor_id: String,
    pub operation: &'static str,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedCorrectPurchase {
    pub correction_id: String,
    pub journal_id: String,
    pub ledger_entry_id: String,
}

#[derive(Debug, Clone)]
pub struct LinkStatementRefundCorrection {
    pub household_id: String,
    pub actor_id: String,
    pub correction_id: String,
    pub source_document_id: String,
    pub source_reference: String,
}

#[derive(Debug, Clone)]
struct LoadedStatementRefund {
    observation: StatementRefundObservation,
    evidence: serde_json::Value,
}

#[derive(Debug)]
pub enum CorrectPurchasePersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidPeriod,
    NumericOutOfRange(&'static str),
    InvalidIdempotencyKey,
    IdempotencyConflict,
    AccountingPeriodClosed,
    PurchaseNotPosted,
    RefundExceedsPurchaseBalance,
    RefundExceedsItemBalance,
    HouseholdMismatch,
    InvalidDecision,
    InvalidFundingSnapshot,
    SourceDocumentNotFound,
    StatementAlreadyConfirmed,
    StatementExtractionUnavailable,
    StatementReferenceNotFound,
    StatementNotRefundable,
    RefundDoesNotMatchCorrection,
    MemberNotFound,
    CorrectionNotFound,
}

impl From<DieselError> for CorrectPurchasePersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn correct_purchase(
    pool: &PgPool,
    request: CorrectPurchaseRequest,
) -> Result<PersistedCorrectPurchase, CorrectPurchasePersistenceError> {
    let household_id = parse_uuid(&request.household_id, "household_id")?;
    let purchase_id = parse_uuid(&request.purchase_id, "purchase_id")?;
    let actor_id = parse_uuid(&request.actor_id, "actor_id")?;
    let item_id = request
        .item_id
        .as_deref()
        .map(|id| parse_uuid(id, "item_id"))
        .transpose()?;
    if request.idempotency_key.is_empty() || request.idempotency_key.len() > 200 {
        return Err(CorrectPurchasePersistenceError::InvalidIdempotencyKey);
    }
    let mut conn = pool
        .get()
        .await
        .map_err(|error| CorrectPurchasePersistenceError::Pool(error.to_string()))?;
    let persist_input = PersistCorrectPurchase {
        household_id: request.household_id.clone(),
        actor_id: request.actor_id.clone(),
        operation: if request.source_document_id.is_some() {
            "correctPurchaseFromStatement"
        } else {
            "correctPurchase"
        },
        idempotency_key: request.idempotency_key.clone(),
        request_fingerprint: request.request_fingerprint,
    };
    let result = (&mut *conn)
        .transaction::<PersistedCorrectPurchase, CorrectPurchasePersistenceError, _>(async |conn| {
            use public::{
                accounting_periods, purchase_allocations, purchase_corrections, purchase_items,
                purchases,
            };

            let (occurred_at, status, legacy_payment_account_id) = purchases::table
                .filter(purchases::household_id.eq(household_id))
                .filter(purchases::id.eq(purchase_id))
                .for_update()
                .select((
                    purchases::occurred_at,
                    purchases::status,
                    purchases::payment_account_id,
                ))
                .first::<(DateTime<Utc>, String, Option<Uuid>)>(conn)
                .await
                .optional()?
                .ok_or(CorrectPurchasePersistenceError::PurchaseNotFound)?;
            validate_actor_membership(conn, household_id, actor_id).await?;
            if let Some(replay) =
                find_correct_purchase_replay(conn, household_id, &persist_input).await?
            {
                return Ok(replay);
            }
            let japan_offset = FixedOffset::east_opt(9 * 60 * 60)
                .ok_or(CorrectPurchasePersistenceError::InvalidPeriod)?;
            let original_period = occurred_at
                .with_timezone(&japan_offset)
                .date_naive()
                .with_day(1)
                .ok_or(CorrectPurchasePersistenceError::InvalidPeriod)?;
            let original_period_status = accounting_periods::table
                .filter(accounting_periods::household_id.eq(household_id))
                .filter(accounting_periods::period_start.eq(original_period))
                .for_update()
                .select(accounting_periods::status)
                .first::<String>(conn)
                .await
                .optional()?;
            let original_period_status = match original_period_status.as_deref() {
                None | Some("OPEN") => OriginalPeriodStatus::Open,
                Some("CLOSED") => OriginalPeriodStatus::Closed,
                _ => OriginalPeriodStatus::Invalid,
            };
            let effective_period = if original_period_status == OriginalPeriodStatus::Closed {
                Utc::now()
                    .with_timezone(&japan_offset)
                    .date_naive()
                    .with_day(1)
                    .ok_or(CorrectPurchasePersistenceError::InvalidPeriod)?
            } else {
                original_period
            };
            let effective_period_open = accounting_periods::table
                .filter(accounting_periods::household_id.eq(household_id))
                .filter(accounting_periods::period_start.eq(effective_period))
                .select(accounting_periods::status)
                .first::<String>(conn)
                .await
                .optional()?
                .is_none_or(|value| value == "OPEN");
            let item_rows = purchase_items::table
                .filter(purchase_items::household_id.eq(household_id))
                .filter(purchase_items::purchase_id.eq(purchase_id))
                .select((purchase_items::id, purchase_items::amount_yen))
                .load::<(Uuid, i64)>(conn)
                .await?;
            let mut purchase_refundable = 0_i64;
            let mut original_total = 0_i64;
            let mut item_refundable = BTreeMap::new();
            for (id, amount) in item_rows {
                original_total = original_total
                    .checked_add(amount)
                    .ok_or(CorrectPurchasePersistenceError::InvalidDecision)?;
                purchase_refundable = purchase_refundable
                    .checked_add(amount)
                    .ok_or(CorrectPurchasePersistenceError::InvalidDecision)?;
                item_refundable.insert(id, amount);
            }
            for (delta, correction_item_id) in purchase_corrections::table
                .filter(purchase_corrections::household_id.eq(household_id))
                .filter(purchase_corrections::purchase_id.eq(purchase_id))
                .select((
                    purchase_corrections::amount_delta_yen,
                    purchase_corrections::item_id,
                ))
                .load::<(i64, Option<Uuid>)>(conn)
                .await?
            {
                purchase_refundable = purchase_refundable
                    .checked_add(delta)
                    .ok_or(CorrectPurchasePersistenceError::InvalidDecision)?;
                if let Some(id) = correction_item_id {
                    let balance = item_refundable
                        .get_mut(&id)
                        .ok_or(CorrectPurchasePersistenceError::InvalidDecision)?;
                    *balance = balance
                        .checked_add(delta)
                        .ok_or(CorrectPurchasePersistenceError::InvalidDecision)?;
                }
            }
            if item_id.is_some_and(|id| !item_refundable.contains_key(&id)) {
                return Err(CorrectPurchasePersistenceError::HouseholdMismatch);
            }
            let allocation_rows = if let Some(item_id) = item_id {
                purchase_allocations::table
                    .filter(purchase_allocations::household_id.eq(household_id))
                    .filter(purchase_allocations::purchase_id.eq(purchase_id))
                    .filter(purchase_allocations::item_id.eq(item_id))
                    .select((
                        purchase_allocations::pool_id,
                        purchase_allocations::weight,
                        purchase_allocations::amount_yen,
                    ))
                    .load::<(Uuid, i64, i64)>(conn)
                    .await?
            } else {
                purchase_allocations::table
                    .filter(purchase_allocations::household_id.eq(household_id))
                    .filter(purchase_allocations::purchase_id.eq(purchase_id))
                    .select((
                        purchase_allocations::pool_id,
                        purchase_allocations::weight,
                        purchase_allocations::amount_yen,
                    ))
                    .load::<(Uuid, i64, i64)>(conn)
                    .await?
            };
            let mut allocation_weights = BTreeMap::<Uuid, i64>::new();
            for (pool_id, weight, amount) in allocation_rows {
                let value = if item_id.is_some() { weight } else { amount };
                let total = allocation_weights.entry(pool_id).or_default();
                *total = total
                    .checked_add(value)
                    .ok_or(CorrectPurchasePersistenceError::InvalidDecision)?;
            }
            let saved_allocations = AllocationShares::new(
                allocation_weights
                    .into_iter()
                    .filter(|(_, weight)| *weight > 0)
                    .map(|(pool_id, weight)| {
                        let pool_id = WorkflowPoolId::new(pool_id.to_string())
                            .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?;
                        let weight = u64::try_from(weight)
                            .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?;
                        AllocationWeight::new(pool_id, weight)
                            .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)
                    })
                    .collect::<Result<Vec<_>, _>>()?,
            )
            .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?;
            let requested_allocations = if request.allocations.is_empty() {
                None
            } else {
                Some(
                    AllocationShares::new(
                        request
                            .allocations
                            .iter()
                            .map(|(pool_id, weight)| {
                                let id = WorkflowPoolId::new(pool_id.clone()).map_err(|_| {
                                    CorrectPurchasePersistenceError::InvalidDecision
                                })?;
                                let weight = u64::try_from(*weight).map_err(|_| {
                                    CorrectPurchasePersistenceError::InvalidDecision
                                })?;
                                AllocationWeight::new(id, weight)
                                    .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)
                            })
                            .collect::<Result<Vec<_>, _>>()?,
                    )
                    .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?,
                )
            };
            let source_observation = if let Some(document_id) = &request.source_document_id {
                let document_id = parse_uuid(document_id, "source_document_id")?;
                let reference = request.source_reference.as_deref().unwrap_or_default();
                let statement =
                    load_statement_refund_entry(conn, household_id, document_id, reference).await?;
                if statement.observation.kind != "REFUND" || statement.observation.amount_yen >= 0 {
                    return Err(CorrectPurchasePersistenceError::StatementNotRefundable);
                }
                Some(statement)
            } else {
                None
            };
            let amount_delta = request.amount_delta_yen.or_else(|| {
                source_observation
                    .as_ref()
                    .map(|statement| statement.observation.amount_yen)
            });
            let delta = SignedYenDelta::new(
                amount_delta.ok_or(CorrectPurchasePersistenceError::InvalidDecision)?,
            )
            .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?;
            let payment_inputs = if request.payments.is_empty() {
                use public::purchase_payments;
                let saved_payments = purchase_payments::table
                    .filter(purchase_payments::household_id.eq(household_id))
                    .filter(purchase_payments::purchase_id.eq(purchase_id))
                    .order(purchase_payments::line_no.asc())
                    .select((purchase_payments::account_id, purchase_payments::amount_yen))
                    .load::<(Uuid, i64)>(conn)
                    .await?;
                let mut total = 0_i64;
                let mut weights = Vec::new();
                for (account_id, amount) in saved_payments {
                    total = total
                        .checked_add(amount)
                        .ok_or(CorrectPurchasePersistenceError::InvalidDecision)?;
                    weights.push((account_id.to_string(), amount));
                }
                if weights.is_empty() {
                    if let Some(account_id) = legacy_payment_account_id {
                        weights.push((account_id.to_string(), original_total));
                        total = original_total;
                    }
                }
                if !weights.is_empty() && total != original_total {
                    return Err(CorrectPurchasePersistenceError::InvalidDecision);
                }
                if weights.is_empty() {
                    Vec::new()
                } else {
                    let shares = AllocationShares::new(
                        weights
                            .into_iter()
                            .map(|(id, amount)| {
                                let id = WorkflowPoolId::new(id).map_err(|_| {
                                    CorrectPurchasePersistenceError::InvalidDecision
                                })?;
                                let amount = u64::try_from(amount).map_err(|_| {
                                    CorrectPurchasePersistenceError::InvalidDecision
                                })?;
                                AllocationWeight::new(id, amount)
                                    .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)
                            })
                            .collect::<Result<Vec<_>, _>>()?,
                    )
                    .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?;
                    bookkeeping_domain::allocate(
                        Yen::new(delta.value().abs())
                            .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?,
                        &shares,
                    )
                    .into_iter()
                    .filter(|payment| payment.amount().value() > 0)
                    .map(|payment| {
                        Ok((
                            payment.pool_id().as_str().to_owned(),
                            payment.amount().value(),
                        ))
                    })
                    .collect::<Result<Vec<_>, CorrectPurchasePersistenceError>>()?
                }
            } else {
                request.payments.clone()
            };
            let payments = payment_inputs
                .iter()
                .map(|(account_id, amount)| {
                    let account_id = AccountId::new(account_id.clone())
                        .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?;
                    let amount = PositiveYen::new(*amount)
                        .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?;
                    Ok(bookkeeping_workflows::correct_purchase::CorrectionPayment {
                        account_id,
                        amount,
                    })
                })
                .collect::<Result<Vec<_>, CorrectPurchasePersistenceError>>()?;
            let item_balance = item_id
                .map(|id| item_refundable.get(&id).copied())
                .flatten()
                .map(Yen::new)
                .transpose()
                .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?;
            let command = CorrectionCommand {
                purchase_id: PurchaseId::new(purchase_id.to_string())
                    .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?,
                purchase_status: match status.as_str() {
                    "DRAFT" => PurchaseStatus::Draft,
                    "POSTED" => PurchaseStatus::Posted,
                    _ => PurchaseStatus::Other,
                },
                target: match item_id {
                    Some(id) => CorrectionTarget::Item(
                        ItemId::new(id.to_string())
                            .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?,
                    ),
                    None => CorrectionTarget::Purchase,
                },
                amount_delta: delta,
                reason: request.reason.clone(),
                original_period: AccountingPeriod::new(original_period.format("%Y-%m").to_string())
                    .map_err(|_| CorrectPurchasePersistenceError::InvalidPeriod)?,
                original_period_status,
                effective_period: AccountingPeriod::new(
                    effective_period.format("%Y-%m").to_string(),
                )
                .map_err(|_| CorrectPurchasePersistenceError::InvalidPeriod)?,
                effective_period_open,
                purchase_refundable: Yen::new(purchase_refundable)
                    .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?,
                item_refundable: item_balance,
                saved_allocations,
                requested_allocations,
                payments,
            };
            let decision = decide_correct_purchase(command)
                .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?;
            let persisted =
                persist_correct_purchase_in_transaction(conn, persist_input.clone(), decision)
                    .await?;
            if let (Some(source_document_id), Some(source_reference)) =
                (&request.source_document_id, &request.source_reference)
            {
                persist_statement_refund_confirmation(
                    conn,
                    household_id,
                    actor_id,
                    parse_uuid(source_document_id, "source_document_id")?,
                    source_reference,
                    Uuid::parse_str(&persisted.correction_id)
                        .map_err(|_| CorrectPurchasePersistenceError::InvalidDecision)?,
                    &source_observation
                        .as_ref()
                        .ok_or(CorrectPurchasePersistenceError::InvalidDecision)?
                        .evidence,
                )
                .await?;
            }
            Ok(persisted)
        })
        .await?;
    Ok(result)
}

pub async fn link_statement_refund_correction(
    pool: &PgPool,
    input: LinkStatementRefundCorrection,
) -> Result<String, CorrectPurchasePersistenceError> {
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let correction_id = parse_uuid(&input.correction_id, "correction_id")?;
    let source_document_id = parse_uuid(&input.source_document_id, "source_document_id")?;
    if input.source_reference.is_empty() || input.source_reference.len() > 256 {
        return Err(CorrectPurchasePersistenceError::InvalidDecision);
    }
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CorrectPurchasePersistenceError::Pool(error.to_string()))?;

    (&mut *pooled)
        .transaction::<String, CorrectPurchasePersistenceError, _>(async |conn| {
            use public::evidence_links::dsl as evidence;
            use public::household_members::dsl as members;
            use public::purchase_corrections::dsl as corrections;
            use public::source_confirmations::dsl as confirmations;

            let is_member = members::household_members
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(actor_id))
                .filter(members::left_at.is_null())
                .select(members::member_id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .is_some();
            if !is_member {
                return Err(CorrectPurchasePersistenceError::MemberNotFound);
            }

            let current_statement = load_statement_refund_entry(
                &mut *conn,
                household_id,
                source_document_id,
                &input.source_reference,
            )
            .await?
            .observation;
            let correction = corrections::purchase_corrections
                .filter(corrections::household_id.eq(household_id))
                .filter(corrections::id.eq(correction_id))
                .for_update()
                .select((corrections::purchase_id, corrections::amount_delta_yen))
                .first::<(Uuid, i64)>(&mut *conn)
                .await
                .optional()?
                .ok_or(CorrectPurchasePersistenceError::CorrectionNotFound)?;
            let linked = evidence::evidence_links
                .filter(evidence::household_id.eq(household_id))
                .filter(evidence::correction_id.eq(Some(correction_id)))
                .for_update()
                .select((
                    evidence::source_document_id,
                    evidence::source_reference,
                    evidence::observation,
                ))
                .first::<(Uuid, String, serde_json::Value)>(&mut *conn)
                .await
                .optional()?
                .ok_or(CorrectPurchasePersistenceError::RefundDoesNotMatchCorrection)?;
            let linked_document_exists = public::source_documents::table
                .filter(public::source_documents::household_id.eq(household_id))
                .filter(public::source_documents::id.eq(linked.0))
                .select(public::source_documents::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .is_some();
            if !linked_document_exists {
                return Err(CorrectPurchasePersistenceError::RefundDoesNotMatchCorrection);
            }
            let linked_confirmation = confirmations::source_confirmations
                .filter(confirmations::household_id.eq(household_id))
                .filter(confirmations::source_document_id.eq(linked.0))
                .filter(confirmations::source_reference.eq(&linked.1))
                .order_by(confirmations::changed_at.desc())
                .select((confirmations::correction_id, confirmations::status))
                .first::<(Option<Uuid>, String)>(&mut *conn)
                .await
                .optional()?;
            if !linked_confirmation.is_some_and(|(linked_id, status)| {
                linked_id == Some(correction_id) && status == "confirmed"
            }) {
                return Err(CorrectPurchasePersistenceError::RefundDoesNotMatchCorrection);
            }
            let linked_statement = parse_linked_refund_observation(&linked.2, &linked.1)?;
            let decision =
                decide_link_statement_refund_correction(LinkStatementRefundCorrectionCommand {
                    source_document_id: source_document_id.to_string(),
                    source_reference: input.source_reference.clone(),
                    current_statement,
                    correction_id: correction_id.to_string(),
                    correction_amount_delta_yen: correction.1,
                    linked_source_document_id: linked.0.to_string(),
                    linked_source_reference: linked.1,
                    linked_statement,
                })
                .map_err(map_link_refund_error)?;

            let already_confirmed = confirmations::source_confirmations
                .filter(confirmations::household_id.eq(household_id))
                .filter(confirmations::source_document_id.eq(source_document_id))
                .filter(confirmations::source_reference.eq(&decision.source_reference))
                .filter(confirmations::status.eq("confirmed"))
                .select(confirmations::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .is_some();
            if already_confirmed {
                return Err(CorrectPurchasePersistenceError::StatementAlreadyConfirmed);
            }
            diesel::insert_into(public::source_confirmations::table)
                .values((
                    public::source_confirmations::id.eq(Uuid::now_v7()),
                    public::source_confirmations::household_id.eq(household_id),
                    public::source_confirmations::source_document_id.eq(source_document_id),
                    public::source_confirmations::observation_id.eq(None::<Uuid>),
                    public::source_confirmations::source_reference.eq(&decision.source_reference),
                    public::source_confirmations::entity_kind.eq("purchase_correction"),
                    public::source_confirmations::entity_id.eq(correction_id),
                    public::source_confirmations::status.eq("confirmed"),
                    public::source_confirmations::idempotency_key.eq(format!(
                        "purchase-correction-confirmation:{}",
                        Uuid::now_v7()
                    )),
                    public::source_confirmations::changed_by.eq(actor_id),
                    public::source_confirmations::changed_at.eq(now),
                ))
                .execute(&mut *conn)
                .await
                .map_err(map_confirmation_insert_error)?;
            Ok(correction.0.to_string())
        })
        .await
}

pub async fn persist_correct_purchase(
    pool: &PgPool,
    input: PersistCorrectPurchase,
    decision: CorrectPurchaseDecision,
) -> Result<PersistedCorrectPurchase, CorrectPurchasePersistenceError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CorrectPurchasePersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedCorrectPurchase, CorrectPurchasePersistenceError, _>(async |conn| {
            persist_correct_purchase_in_transaction(conn, input, decision).await
        })
        .await
}

async fn find_correct_purchase_replay(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    input: &PersistCorrectPurchase,
) -> Result<Option<PersistedCorrectPurchase>, CorrectPurchasePersistenceError> {
    use public::{idempotency_records, journals, ledger_entries};

    let existing = idempotency_records::table
        .filter(idempotency_records::household_id.eq(household_id))
        .filter(idempotency_records::operation.eq(input.operation))
        .filter(idempotency_records::key.eq(&input.idempotency_key))
        .select((
            idempotency_records::request_fingerprint,
            idempotency_records::resource_id,
        ))
        .first::<(Vec<u8>, Uuid)>(conn)
        .await
        .optional()?;
    let Some((fingerprint, correction_id)) = existing else {
        return Ok(None);
    };
    if fingerprint.as_slice() != input.request_fingerprint {
        return Err(CorrectPurchasePersistenceError::IdempotencyConflict);
    }
    let journal_id = journals::table
        .filter(journals::household_id.eq(household_id))
        .filter(journals::source_kind.eq(SOURCE_KIND))
        .filter(journals::source_id.eq(correction_id))
        .select(journals::id)
        .first::<Uuid>(conn)
        .await?;
    let ledger_entry_id = ledger_entries::table
        .filter(ledger_entries::household_id.eq(household_id))
        .filter(ledger_entries::journal_id.eq(journal_id))
        .select(ledger_entries::id)
        .first::<Uuid>(conn)
        .await?;
    Ok(Some(PersistedCorrectPurchase {
        correction_id: correction_id.to_string(),
        journal_id: journal_id.to_string(),
        ledger_entry_id: ledger_entry_id.to_string(),
    }))
}

async fn persist_statement_refund_confirmation(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
    source_document_id: Uuid,
    source_reference: &str,
    correction_id: Uuid,
    observation: &serde_json::Value,
) -> Result<(), CorrectPurchasePersistenceError> {
    use public::{evidence_links, source_confirmations};

    let region = observation
        .get("statement")
        .and_then(|statement| statement.get("region"))
        .cloned();
    diesel::insert_into(source_confirmations::table)
        .values((
            source_confirmations::id.eq(Uuid::now_v7()),
            source_confirmations::household_id.eq(household_id),
            source_confirmations::source_document_id.eq(source_document_id),
            source_confirmations::observation_id.eq(None::<Uuid>),
            source_confirmations::source_reference.eq(source_reference),
            source_confirmations::entity_kind.eq("purchase_correction"),
            source_confirmations::entity_id.eq(correction_id),
            source_confirmations::status.eq("confirmed"),
            source_confirmations::idempotency_key.eq(format!(
                "purchase-correction-confirmation:{}",
                Uuid::now_v7()
            )),
            source_confirmations::changed_by.eq(actor_id),
            source_confirmations::changed_at.eq(now),
        ))
        .execute(&mut *conn)
        .await
        .map_err(map_confirmation_insert_error)?;
    diesel::insert_into(evidence_links::table)
        .values((
            evidence_links::id.eq(Uuid::now_v7()),
            evidence_links::household_id.eq(household_id),
            evidence_links::source_document_id.eq(source_document_id),
            evidence_links::observation_id.eq(None::<Uuid>),
            evidence_links::source_reference.eq(source_reference),
            evidence_links::entity_kind.eq("purchase_correction"),
            evidence_links::entity_id.eq(correction_id),
            evidence_links::item_id.eq(None::<Uuid>),
            evidence_links::evidence_region.eq(region),
            evidence_links::observation.eq(observation.clone()),
            evidence_links::created_by.eq(actor_id),
            evidence_links::created_at.eq(now),
            evidence_links::purchase_id.eq(None::<Uuid>),
            evidence_links::correction_id.eq(Some(correction_id)),
            evidence_links::transfer_id.eq(None::<Uuid>),
        ))
        .execute(&mut *conn)
        .await
        .map_err(map_confirmation_insert_error)?;
    Ok(())
}

async fn persist_correct_purchase_in_transaction(
    conn: &mut diesel_async::AsyncPgConnection,
    input: PersistCorrectPurchase,
    decision: CorrectPurchaseDecision,
) -> Result<PersistedCorrectPurchase, CorrectPurchasePersistenceError> {
    if input.idempotency_key.is_empty() || input.idempotency_key.len() > 200 {
        return Err(CorrectPurchasePersistenceError::InvalidIdempotencyKey);
    }
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let purchase_id = parse_uuid(decision.purchase_id.as_str(), "purchase_id")?;
    let effective_period = parse_period(decision.effective_period.as_str())?;
    validate_decision(&decision)?;

    let correction_id = Uuid::new_v4();
    let journal_id = Uuid::new_v4();
    let ledger_entry_id = Uuid::new_v4();
    use public::accounting_periods::dsl as periods;
    use public::household_events::dsl as events;
    use public::idempotency_records::dsl as idem;
    use public::journals::dsl as journals;
    use public::ledger_entries::dsl as ledger;
    use public::purchases::dsl as purchases;

    let purchase = purchases::purchases
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::id.eq(purchase_id))
        .for_update()
        .select((purchases::occurred_at, purchases::status))
        .first::<(DateTime<Utc>, String)>(&mut *conn)
        .await?;
    validate_actor_membership(&mut *conn, household_id, actor_id).await?;
    let japan_offset =
        FixedOffset::east_opt(9 * 60 * 60).ok_or(CorrectPurchasePersistenceError::InvalidPeriod)?;
    let original_period = purchase
        .0
        .with_timezone(&japan_offset)
        .date_naive()
        .with_day(1)
        .ok_or(CorrectPurchasePersistenceError::InvalidPeriod)?;
    let inserted = diesel::insert_into(idem::idempotency_records)
        .values((
            idem::household_id.eq(household_id),
            idem::operation.eq(input.operation),
            idem::key.eq(&input.idempotency_key),
            idem::request_fingerprint.eq(input.request_fingerprint.to_vec()),
            idem::resource_kind.eq("purchase_correction"),
            idem::resource_id.eq(correction_id),
        ))
        .on_conflict((idem::household_id, idem::operation, idem::key))
        .do_nothing()
        .returning(idem::resource_id)
        .get_result::<Uuid>(&mut *conn)
        .await
        .optional()?;
    if inserted.is_none() {
        let (fingerprint, resource_id) = idem::idempotency_records
            .filter(idem::household_id.eq(household_id))
            .filter(idem::operation.eq(input.operation))
            .filter(idem::key.eq(&input.idempotency_key))
            .select((idem::request_fingerprint, idem::resource_id))
            .first::<(Vec<u8>, Uuid)>(&mut *conn)
            .await?;
        if fingerprint.as_slice() != input.request_fingerprint {
            return Err(CorrectPurchasePersistenceError::IdempotencyConflict);
        }
        let saved_journal_id = journals::journals
            .filter(journals::household_id.eq(household_id))
            .filter(journals::source_kind.eq(SOURCE_KIND))
            .filter(journals::source_id.eq(resource_id))
            .select(journals::id)
            .first::<Uuid>(&mut *conn)
            .await?;
        let saved_ledger_entry_id = ledger::ledger_entries
            .filter(ledger::household_id.eq(household_id))
            .filter(ledger::journal_id.eq(saved_journal_id))
            .select(ledger::id)
            .first::<Uuid>(&mut *conn)
            .await?;
        return Ok(PersistedCorrectPurchase {
            correction_id: resource_id.to_string(),
            journal_id: saved_journal_id.to_string(),
            ledger_entry_id: saved_ledger_entry_id.to_string(),
        });
    }
    if purchase.1 == "DRAFT" {
        return Err(CorrectPurchasePersistenceError::PurchaseNotPosted);
    }

    let period_dates = if original_period == effective_period {
        vec![original_period]
    } else if original_period < effective_period {
        vec![original_period, effective_period]
    } else {
        vec![effective_period, original_period]
    };
    for period_start in &period_dates {
        diesel::insert_into(periods::accounting_periods)
            .values((
                periods::household_id.eq(household_id),
                periods::period_start.eq(period_start),
                periods::status.eq("OPEN"),
                periods::revision.eq(0_i64),
            ))
            .on_conflict((periods::household_id, periods::period_start))
            .do_nothing()
            .execute(&mut *conn)
            .await?;
    }
    let locked_periods = periods::accounting_periods
        .filter(periods::household_id.eq(household_id))
        .filter(periods::period_start.eq_any(&period_dates))
        .order(periods::period_start.asc())
        .for_update()
        .select((periods::period_start, periods::status))
        .load::<(NaiveDate, String)>(&mut *conn)
        .await?;
    if locked_periods.len() != period_dates.len() {
        return Err(CorrectPurchasePersistenceError::AccountingPeriodClosed);
    }
    let original_status = locked_periods
        .iter()
        .find(|(date, _)| *date == original_period)
        .map(|(_, status)| status.as_str());
    let effective_status = locked_periods
        .iter()
        .find(|(date, _)| *date == effective_period)
        .map(|(_, status)| status.as_str());
    match (original_status, effective_status) {
        (Some("OPEN"), Some("OPEN")) if original_period == effective_period => {}
        (Some("CLOSED"), Some("OPEN")) if original_period != effective_period => {}
        _ => return Err(CorrectPurchasePersistenceError::AccountingPeriodClosed),
    }

    validate_references(&mut *conn, household_id, actor_id, purchase_id, &decision).await?;
    validate_refundable_balance(&mut *conn, household_id, purchase_id, &decision).await?;
    let amount_yen = decision.amount_delta.value().abs();
    diesel::insert_into(journals::journals)
        .values((
            journals::id.eq(journal_id),
            journals::household_id.eq(household_id),
            journals::source_kind.eq(SOURCE_KIND),
            journals::source_id.eq(correction_id),
            journals::occurred_at.eq(now),
            journals::created_by.eq(actor_id),
            journals::created_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;

    for line in &decision.journal_lines {
        let line_no = i32::try_from(line.line_no)
            .map_err(|_| CorrectPurchasePersistenceError::NumericOutOfRange("journal line_no"))?;
        let amount = line.amount.value();
        diesel::insert_into(public::journal_entries::table)
            .values((
                public::journal_entries::household_id.eq(household_id),
                public::journal_entries::journal_id.eq(journal_id),
                public::journal_entries::line_no.eq(line_no),
                public::journal_entries::account_id.eq(line
                    .account_id
                    .as_ref()
                    .map(|id| parse_uuid(id.as_str(), "account_id"))
                    .transpose()?),
                public::journal_entries::pool_id.eq(line
                    .pool_id
                    .as_ref()
                    .map(|id| parse_uuid(id.as_str(), "pool_id"))
                    .transpose()?),
                public::journal_entries::member_id.eq::<Option<Uuid>>(None),
                public::journal_entries::debit_yen.eq(if line.side == Side::Debit {
                    amount
                } else {
                    0
                }),
                public::journal_entries::credit_yen.eq(if line.side == Side::Credit {
                    amount
                } else {
                    0
                }),
                public::journal_entries::memo.eq(&line.memo),
            ))
            .execute(&mut *conn)
            .await?;
    }

    let item_id = match &decision.target {
        CorrectionTarget::Purchase => None,
        CorrectionTarget::Item(id) => Some(parse_uuid(id.as_str(), "item_id")?),
    };
    diesel::insert_into(public::purchase_corrections::table)
        .values((
            public::purchase_corrections::id.eq(correction_id),
            public::purchase_corrections::household_id.eq(household_id),
            public::purchase_corrections::purchase_id.eq(purchase_id),
            public::purchase_corrections::item_id.eq(item_id),
            public::purchase_corrections::amount_delta_yen.eq(decision.amount_delta.value()),
            public::purchase_corrections::reason.eq(&decision.reason),
            public::purchase_corrections::effective_period_start.eq(effective_period),
            public::purchase_corrections::journal_id.eq(journal_id),
            public::purchase_corrections::created_by.eq(actor_id),
            public::purchase_corrections::created_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;

    for (index, allocation) in decision.allocations.iter().enumerate() {
        let line_no = i32::try_from(index + 1).map_err(|_| {
            CorrectPurchasePersistenceError::NumericOutOfRange("allocation line_no")
        })?;
        diesel::insert_into(public::purchase_correction_allocations::table)
            .values((
                public::purchase_correction_allocations::household_id.eq(household_id),
                public::purchase_correction_allocations::correction_id.eq(correction_id),
                public::purchase_correction_allocations::line_no.eq(line_no),
                public::purchase_correction_allocations::pool_id
                    .eq(parse_uuid(allocation.pool_id.as_str(), "pool_id")?),
                public::purchase_correction_allocations::amount_yen.eq(allocation.amount.value()),
            ))
            .execute(&mut *conn)
            .await?;
    }

    let funding_effects =
        load_correction_funding_effects(&mut *conn, household_id, purchase_id, item_id, &decision)
            .await?;
    persist_correction_member_burdens(
        &mut *conn,
        household_id,
        correction_id,
        &funding_effects.member_burdens,
    )
    .await?;

    let mut payment_line_no = 0_i32;
    for line in &decision.journal_lines {
        let Some(account_id) = &line.account_id else {
            continue;
        };
        payment_line_no = payment_line_no.checked_add(1).ok_or(
            CorrectPurchasePersistenceError::NumericOutOfRange("payment line_no"),
        )?;
        diesel::insert_into(public::purchase_correction_payments::table)
            .values((
                public::purchase_correction_payments::household_id.eq(household_id),
                public::purchase_correction_payments::correction_id.eq(correction_id),
                public::purchase_correction_payments::line_no.eq(payment_line_no),
                public::purchase_correction_payments::account_id
                    .eq(parse_uuid(account_id.as_str(), "account_id")?),
                public::purchase_correction_payments::amount_yen.eq(line.amount.value()),
            ))
            .execute(&mut *conn)
            .await?;
    }

    persist_settlement_adjustments(
        &mut *conn,
        household_id,
        correction_id,
        effective_period,
        &funding_effects.settlement_adjustments,
    )
    .await?;

    diesel::insert_into(ledger::ledger_entries)
        .values((
            ledger::id.eq(ledger_entry_id),
            ledger::household_id.eq(household_id),
            ledger::journal_id.eq(journal_id),
            ledger::source_kind.eq(SOURCE_KIND),
            ledger::source_id.eq(correction_id),
            ledger::occurred_at.eq(now),
            ledger::description.eq(&decision.reason),
            ledger::amount_yen.eq(amount_yen),
            ledger::created_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;

    use public::households::dsl as households;
    households::households
        .filter(households::id.eq(household_id))
        .for_update()
        .select(households::id)
        .first::<Uuid>(&mut *conn)
        .await?;
    diesel::insert_into(events::household_events)
        .values((
            events::household_id.eq(household_id),
            events::event_type.eq(decision.event.event_type),
            events::entity_kind.eq("purchase"),
            events::entity_id.eq(purchase_id),
            events::revision.eq(1_i64),
            events::changed_fields.eq(decision
                .event
                .changed_fields
                .iter()
                .map(|field| Some((*field).to_owned()))
                .collect::<Vec<_>>()),
            events::actor_id.eq(actor_id),
            events::created_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;

    Ok(PersistedCorrectPurchase {
        correction_id: correction_id.to_string(),
        journal_id: journal_id.to_string(),
        ledger_entry_id: ledger_entry_id.to_string(),
    })
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, CorrectPurchasePersistenceError> {
    Uuid::parse_str(value).map_err(|_| CorrectPurchasePersistenceError::InvalidUuid(field))
}

async fn load_statement_refund_entry(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    document_id: Uuid,
    source_reference: &str,
) -> Result<LoadedStatementRefund, CorrectPurchasePersistenceError> {
    use public::extraction_jobs::dsl as jobs;
    use public::source_confirmations::dsl as confirmations;
    use public::source_documents::dsl as documents;

    let source_kind = documents::source_documents
        .filter(documents::household_id.eq(household_id))
        .filter(documents::id.eq(document_id))
        .for_update()
        .select((documents::source_kind, documents::sha256))
        .first::<(String, String)>(conn)
        .await
        .optional()?
        .ok_or(CorrectPurchasePersistenceError::SourceDocumentNotFound)?;
    if source_kind.0 != "bank" && source_kind.0 != "card" {
        return Err(CorrectPurchasePersistenceError::StatementReferenceNotFound);
    }
    let confirmed = confirmations::source_confirmations
        .filter(confirmations::household_id.eq(household_id))
        .filter(confirmations::source_document_id.eq(document_id))
        .filter(confirmations::source_reference.eq(source_reference))
        .filter(confirmations::status.eq("confirmed"))
        .select(confirmations::id)
        .first::<Uuid>(conn)
        .await
        .optional()?
        .is_some();
    if confirmed {
        return Err(CorrectPurchasePersistenceError::StatementAlreadyConfirmed);
    }
    let (job_id, result) = jobs::extraction_jobs
        .filter(jobs::household_id.eq(household_id))
        .filter(jobs::source_document_id.eq(document_id))
        .filter(jobs::status.eq("succeeded"))
        .order_by(jobs::version.desc())
        .select((jobs::id, jobs::result))
        .first::<(Uuid, Option<serde_json::Value>)>(conn)
        .await
        .optional()?
        .map(|(job_id, result)| (job_id, result.flatten()))
        .and_then(|(job_id, result)| result.map(|result| (job_id, result)))
        .ok_or(CorrectPurchasePersistenceError::StatementExtractionUnavailable)?;
    let statements = result
        .1
        .get("statements")
        .and_then(serde_json::Value::as_array)
        .ok_or(CorrectPurchasePersistenceError::StatementExtractionUnavailable)?;
    for statement in statements {
        let row = statement.get("row").and_then(serde_json::Value::as_i64);
        let reference = statement
            .get("sourceReference")
            .and_then(serde_json::Value::as_str)
            .filter(|value| !value.is_empty())
            .map(str::to_owned)
            .or_else(|| row.map(|row| format!("row:{row}")));
        if reference.as_deref() != Some(source_reference) {
            continue;
        }
        if statement
            .get("region")
            .is_none_or(serde_json::Value::is_null)
        {
            return Err(CorrectPurchasePersistenceError::StatementNotRefundable);
        }
        return Ok(LoadedStatementRefund {
            observation: statement_observation(statement, source_reference)?,
            evidence: serde_json::json!({
                "jobId": result.0,
                "statement": statement,
                "sourceHash": source_kind.1,
            }),
        });
    }
    Err(CorrectPurchasePersistenceError::StatementReferenceNotFound)
}

fn parse_linked_refund_observation(
    value: &serde_json::Value,
    source_reference: &str,
) -> Result<StatementRefundObservation, CorrectPurchasePersistenceError> {
    let statement = value
        .get("statement")
        .ok_or(CorrectPurchasePersistenceError::RefundDoesNotMatchCorrection)?;
    let row = statement.get("row").and_then(serde_json::Value::as_i64);
    let reference = statement
        .get("sourceReference")
        .and_then(serde_json::Value::as_str)
        .filter(|value| !value.is_empty())
        .map(str::to_owned)
        .or_else(|| row.map(|row| format!("row:{row}")))
        .ok_or(CorrectPurchasePersistenceError::RefundDoesNotMatchCorrection)?;
    let observation = statement_observation(statement, &reference)?;
    if reference != source_reference || observation.source_reference != source_reference {
        return Err(CorrectPurchasePersistenceError::RefundDoesNotMatchCorrection);
    }
    Ok(observation)
}

pub(crate) fn statement_observation(
    value: &serde_json::Value,
    source_reference: &str,
) -> Result<StatementRefundObservation, CorrectPurchasePersistenceError> {
    let kind = value
        .get("kind")
        .and_then(serde_json::Value::as_str)
        .ok_or(CorrectPurchasePersistenceError::StatementReferenceNotFound)?
        .to_owned();
    let occurred_at = value
        .get("occurredAt")
        .and_then(serde_json::Value::as_str)
        .and_then(|value| DateTime::parse_from_rfc3339(value).ok())
        .ok_or(CorrectPurchasePersistenceError::StatementReferenceNotFound)?;
    let occurred_at_unix_nanos = i128::from(occurred_at.timestamp()) * 1_000_000_000
        + i128::from(occurred_at.timestamp_subsec_nanos());
    let merchant = value
        .get("merchant")
        .and_then(serde_json::Value::as_str)
        .unwrap_or_default()
        .to_owned();
    let amount_yen = value
        .get("amount")
        .and_then(serde_json::Value::as_i64)
        .ok_or(CorrectPurchasePersistenceError::StatementReferenceNotFound)?;
    Ok(StatementRefundObservation {
        source_reference: source_reference.to_owned(),
        kind,
        occurred_at_unix_nanos,
        merchant,
        amount_yen,
    })
}

fn map_link_refund_error(
    error: LinkStatementRefundCorrectionError,
) -> CorrectPurchasePersistenceError {
    match error {
        LinkStatementRefundCorrectionError::InvalidSource => {
            CorrectPurchasePersistenceError::StatementReferenceNotFound
        }
        LinkStatementRefundCorrectionError::RefundDoesNotMatchCorrection => {
            CorrectPurchasePersistenceError::RefundDoesNotMatchCorrection
        }
    }
}

fn map_confirmation_insert_error(error: DieselError) -> CorrectPurchasePersistenceError {
    if matches!(
        &error,
        DieselError::DatabaseError(diesel::result::DatabaseErrorKind::UniqueViolation, _)
    ) {
        CorrectPurchasePersistenceError::StatementAlreadyConfirmed
    } else {
        CorrectPurchasePersistenceError::Database(error)
    }
}

fn parse_period(value: &str) -> Result<NaiveDate, CorrectPurchasePersistenceError> {
    let (year, month) = value
        .split_once('-')
        .ok_or(CorrectPurchasePersistenceError::InvalidPeriod)?;
    if year.len() != 4 || month.len() != 2 {
        return Err(CorrectPurchasePersistenceError::InvalidPeriod);
    }
    let year = year
        .parse::<i32>()
        .map_err(|_| CorrectPurchasePersistenceError::InvalidPeriod)?;
    let month = month
        .parse::<u32>()
        .map_err(|_| CorrectPurchasePersistenceError::InvalidPeriod)?;
    NaiveDate::from_ymd_opt(year, month, 1).ok_or(CorrectPurchasePersistenceError::InvalidPeriod)
}

fn validate_decision(
    decision: &CorrectPurchaseDecision,
) -> Result<(), CorrectPurchasePersistenceError> {
    if decision.reason.trim().is_empty()
        || decision.amount_delta.value() == 0
        || decision.amount_delta.value().abs() > 1_000_000_000_000
        || decision.event.purchase_id != decision.purchase_id
        || decision.event.event_type != "purchase.corrected"
        || decision.event.changed_fields != ["correction"]
    {
        return Err(CorrectPurchasePersistenceError::InvalidDecision);
    }
    let mut line_numbers = BTreeSet::new();
    let mut debits = 0_i64;
    let mut credits = 0_i64;
    for line in &decision.journal_lines {
        let line_no = i32::try_from(line.line_no)
            .map_err(|_| CorrectPurchasePersistenceError::NumericOutOfRange("journal line_no"))?;
        if line_no <= 0 || !line_numbers.insert(line_no) {
            return Err(CorrectPurchasePersistenceError::InvalidDecision);
        }
        match line.side {
            Side::Debit => {
                debits = debits.checked_add(line.amount.value()).ok_or(
                    CorrectPurchasePersistenceError::NumericOutOfRange("debit total"),
                )?;
            }
            Side::Credit => {
                credits = credits.checked_add(line.amount.value()).ok_or(
                    CorrectPurchasePersistenceError::NumericOutOfRange("credit total"),
                )?;
            }
        }
    }
    let amount = decision.amount_delta.value().abs();
    if debits != amount || credits != amount || line_numbers.is_empty() {
        return Err(CorrectPurchasePersistenceError::InvalidDecision);
    }
    Ok(())
}

async fn validate_actor_membership(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<(), CorrectPurchasePersistenceError> {
    use public::household_members::dsl as members;

    let member = members::household_members
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .filter(members::left_at.is_null())
        .select(members::member_id)
        .first::<Uuid>(conn)
        .await
        .optional()?;
    if member.is_none() {
        return Err(CorrectPurchasePersistenceError::HouseholdMismatch);
    }
    Ok(())
}

async fn validate_refundable_balance(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    purchase_id: Uuid,
    decision: &CorrectPurchaseDecision,
) -> Result<(), CorrectPurchasePersistenceError> {
    if decision.amount_delta.value() >= 0 {
        return Ok(());
    }

    use public::{purchase_corrections, purchase_items};

    let item_rows = purchase_items::table
        .filter(purchase_items::household_id.eq(household_id))
        .filter(purchase_items::purchase_id.eq(purchase_id))
        .select((purchase_items::id, purchase_items::amount_yen))
        .load::<(Uuid, i64)>(conn)
        .await?;
    let mut purchase_total = 0_i64;
    let mut item_totals = BTreeMap::new();
    for (item_id, amount) in item_rows {
        purchase_total = purchase_total
            .checked_add(amount)
            .ok_or(CorrectPurchasePersistenceError::InvalidDecision)?;
        item_totals.insert(item_id, amount);
    }

    let correction_rows = purchase_corrections::table
        .filter(purchase_corrections::household_id.eq(household_id))
        .filter(purchase_corrections::purchase_id.eq(purchase_id))
        .select((
            purchase_corrections::amount_delta_yen,
            purchase_corrections::item_id,
        ))
        .load::<(i64, Option<Uuid>)>(conn)
        .await?;
    for (delta, item_id) in correction_rows {
        purchase_total = purchase_total
            .checked_add(delta)
            .ok_or(CorrectPurchasePersistenceError::InvalidDecision)?;
        if let Some(item_id) = item_id {
            let total = item_totals
                .get_mut(&item_id)
                .ok_or(CorrectPurchasePersistenceError::InvalidDecision)?;
            *total = total
                .checked_add(delta)
                .ok_or(CorrectPurchasePersistenceError::InvalidDecision)?;
        }
    }

    let refund = decision
        .amount_delta
        .value()
        .checked_abs()
        .ok_or(CorrectPurchasePersistenceError::InvalidDecision)?;
    if purchase_total < 0 || refund > purchase_total {
        return Err(CorrectPurchasePersistenceError::RefundExceedsPurchaseBalance);
    }
    if let CorrectionTarget::Item(item_id) = &decision.target {
        let item_id = parse_uuid(item_id.as_str(), "item_id")?;
        let item_total = item_totals
            .get(&item_id)
            .copied()
            .ok_or(CorrectPurchasePersistenceError::HouseholdMismatch)?;
        if item_total < 0 || refund > item_total {
            return Err(CorrectPurchasePersistenceError::RefundExceedsItemBalance);
        }
    }
    Ok(())
}

async fn validate_references(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
    purchase_id: Uuid,
    decision: &CorrectPurchaseDecision,
) -> Result<(), CorrectPurchasePersistenceError> {
    use public::{accounts, household_members, pools, purchase_items};

    let mut account_ids = BTreeSet::new();
    let mut pool_ids = BTreeSet::new();
    for line in &decision.journal_lines {
        if let Some(id) = &line.account_id {
            account_ids.insert(parse_uuid(id.as_str(), "account_id")?);
        }
        if let Some(id) = &line.pool_id {
            pool_ids.insert(parse_uuid(id.as_str(), "pool_id")?);
        }
    }
    for allocation in &decision.allocations {
        pool_ids.insert(parse_uuid(allocation.pool_id.as_str(), "pool_id")?);
    }
    let item_id = match &decision.target {
        CorrectionTarget::Purchase => None,
        CorrectionTarget::Item(id) => Some(parse_uuid(id.as_str(), "item_id")?),
    };

    let members = household_members::table
        .filter(household_members::household_id.eq(household_id))
        .filter(household_members::member_id.eq(actor_id))
        .filter(household_members::left_at.is_null())
        .select(household_members::member_id)
        .load::<Uuid>(conn)
        .await?;
    if members.len() != 1 {
        return Err(CorrectPurchasePersistenceError::HouseholdMismatch);
    }
    let accounts = accounts::table
        .filter(accounts::household_id.eq(household_id))
        .filter(accounts::id.eq_any(account_ids.iter().copied()))
        .select(accounts::id)
        .load::<Uuid>(conn)
        .await?;
    if accounts.len() != account_ids.len() {
        return Err(CorrectPurchasePersistenceError::HouseholdMismatch);
    }
    let pools = pools::table
        .filter(pools::household_id.eq(household_id))
        .filter(pools::id.eq_any(pool_ids.iter().copied()))
        .select(pools::id)
        .load::<Uuid>(conn)
        .await?;
    if pools.len() != pool_ids.len() {
        return Err(CorrectPurchasePersistenceError::HouseholdMismatch);
    }
    if let Some(item_id) = item_id {
        let items = purchase_items::table
            .filter(purchase_items::household_id.eq(household_id))
            .filter(purchase_items::purchase_id.eq(purchase_id))
            .filter(purchase_items::id.eq(item_id))
            .select(purchase_items::id)
            .load::<Uuid>(conn)
            .await?;
        if items.len() != 1 {
            return Err(CorrectPurchasePersistenceError::HouseholdMismatch);
        }
    }
    Ok(())
}

async fn load_correction_funding_effects(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    purchase_id: Uuid,
    _item_id: Option<Uuid>,
    decision: &CorrectPurchaseDecision,
) -> Result<CorrectionFundingEffects, CorrectPurchasePersistenceError> {
    use public::{
        pool_funding_rules, purchase_member_burdens, purchase_payment_funding_snapshots,
        purchase_payments,
    };

    let burden_rows = purchase_member_burdens::table
        .filter(purchase_member_burdens::household_id.eq(household_id))
        .filter(purchase_member_burdens::purchase_id.eq(purchase_id))
        .select((
            purchase_member_burdens::item_id,
            purchase_member_burdens::pool_id,
            purchase_member_burdens::member_id,
            purchase_member_burdens::funding_rule_version,
            purchase_member_burdens::funding_weight,
            purchase_member_burdens::settlement_policy,
        ))
        .load::<(Uuid, Uuid, Uuid, i32, i64, Option<String>)>(conn)
        .await?;
    let purchase_burdens = burden_rows
        .into_iter()
        .map(|(item_id, pool_id, member_id, version, weight, policy)| {
            Ok(CorrectionPurchaseBurdenSnapshot {
                item_id: item_id.to_string(),
                pool_id: pool_id.to_string(),
                member_id: member_id.to_string(),
                funding_rule_version: u32::try_from(version)
                    .map_err(|_| CorrectPurchasePersistenceError::InvalidFundingSnapshot)?,
                funding_weight: u64::try_from(weight)
                    .map_err(|_| CorrectPurchasePersistenceError::InvalidFundingSnapshot)?,
                settlement_policy: parse_funding_policy(policy.as_deref())?,
            })
        })
        .collect::<Result<Vec<_>, CorrectPurchasePersistenceError>>()?;

    let payment_rows = purchase_payments::table
        .filter(purchase_payments::household_id.eq(household_id))
        .filter(purchase_payments::purchase_id.eq(purchase_id))
        .order(purchase_payments::line_no.asc())
        .select((
            purchase_payments::line_no,
            purchase_payments::account_id,
            purchase_payments::amount_yen,
        ))
        .load::<(i32, Uuid, i64)>(conn)
        .await?;
    let purchase_payments = payment_rows
        .into_iter()
        .map(|(line_no, account_id, amount)| {
            Ok(CorrectionPurchasePayment {
                line_no: u32::try_from(line_no)
                    .map_err(|_| CorrectPurchasePersistenceError::InvalidFundingSnapshot)?,
                account_id: account_id.to_string(),
                amount: PositiveYen::new(amount)
                    .map_err(|_| CorrectPurchasePersistenceError::InvalidFundingSnapshot)?,
            })
        })
        .collect::<Result<Vec<_>, CorrectPurchasePersistenceError>>()?;

    let snapshot_rows = purchase_payment_funding_snapshots::table
        .filter(purchase_payment_funding_snapshots::household_id.eq(household_id))
        .filter(purchase_payment_funding_snapshots::purchase_id.eq(purchase_id))
        .order(purchase_payment_funding_snapshots::payment_line_no.asc())
        .order(purchase_payment_funding_snapshots::member_id.asc())
        .select((
            purchase_payment_funding_snapshots::payment_line_no,
            purchase_payment_funding_snapshots::account_id,
            purchase_payment_funding_snapshots::pool_id,
            purchase_payment_funding_snapshots::member_id,
            purchase_payment_funding_snapshots::funding_rule_version,
            purchase_payment_funding_snapshots::funding_weight,
            purchase_payment_funding_snapshots::settlement_policy,
            purchase_payment_funding_snapshots::snapshot_source,
        ))
        .load::<(i32, Uuid, Uuid, Uuid, i32, i64, Option<String>, String)>(conn)
        .await?;
    let payment_snapshots = snapshot_rows
        .into_iter()
        .map(
            |(
                line_no,
                account_id,
                pool_id,
                member_id,
                version,
                weight,
                policy,
                snapshot_source,
            )| {
                Ok(CorrectionPaymentFundingSnapshot {
                    line_no: u32::try_from(line_no)
                        .map_err(|_| CorrectPurchasePersistenceError::InvalidFundingSnapshot)?,
                    account_id: account_id.to_string(),
                    pool_id: pool_id.to_string(),
                    member_id: member_id.to_string(),
                    funding_rule_version: u32::try_from(version)
                        .map_err(|_| CorrectPurchasePersistenceError::InvalidFundingSnapshot)?,
                    funding_weight: u64::try_from(weight)
                        .map_err(|_| CorrectPurchasePersistenceError::InvalidFundingSnapshot)?,
                    settlement_policy: parse_funding_policy(policy.as_deref())?,
                    snapshot_source,
                })
            },
        )
        .collect::<Result<Vec<_>, CorrectPurchasePersistenceError>>()?;

    let mut rule_keys = BTreeSet::new();
    for burden in &purchase_burdens {
        rule_keys.insert((
            parse_uuid(&burden.pool_id, "pool_id")?,
            i32::try_from(burden.funding_rule_version)
                .map_err(|_| CorrectPurchasePersistenceError::InvalidFundingSnapshot)?,
        ));
    }
    for snapshot in &payment_snapshots {
        rule_keys.insert((
            parse_uuid(&snapshot.pool_id, "pool_id")?,
            i32::try_from(snapshot.funding_rule_version)
                .map_err(|_| CorrectPurchasePersistenceError::InvalidFundingSnapshot)?,
        ));
    }
    let pool_ids = rule_keys
        .iter()
        .map(|(pool_id, _)| *pool_id)
        .collect::<BTreeSet<_>>();
    let versions = rule_keys
        .iter()
        .map(|(_, version)| *version)
        .collect::<BTreeSet<_>>();
    let rule_rows = pool_funding_rules::table
        .filter(pool_funding_rules::household_id.eq(household_id))
        .filter(pool_funding_rules::pool_id.eq_any(pool_ids))
        .filter(pool_funding_rules::version.eq_any(versions))
        .select((
            pool_funding_rules::pool_id,
            pool_funding_rules::version,
            pool_funding_rules::owner_member_id,
            pool_funding_rules::settlement_policy,
        ))
        .load::<(Uuid, i32, Option<Uuid>, Option<String>)>(conn)
        .await?;
    let historical_rules = rule_rows
        .into_iter()
        .map(|(pool_id, version, owner_id, policy)| {
            Ok(CorrectionFundingRule {
                pool_id: pool_id.to_string(),
                version: u32::try_from(version)
                    .map_err(|_| CorrectPurchasePersistenceError::InvalidFundingSnapshot)?,
                owner_member_id: owner_id.map(|id| id.to_string()),
                settlement_policy: parse_funding_policy(policy.as_deref())?,
            })
        })
        .collect::<Result<Vec<_>, CorrectPurchasePersistenceError>>()?;

    calculate_correction_funding_effects(
        decision,
        &purchase_burdens,
        &purchase_payments,
        &payment_snapshots,
        &historical_rules,
    )
    .map_err(|_| CorrectPurchasePersistenceError::InvalidFundingSnapshot)
}

async fn persist_correction_member_burdens(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    correction_id: Uuid,
    member_burdens: &[CorrectionMemberBurdenSnapshot],
) -> Result<(), CorrectPurchasePersistenceError> {
    for burden in member_burdens {
        diesel::insert_into(public::correction_member_burdens::table)
            .values((
                public::correction_member_burdens::household_id.eq(household_id),
                public::correction_member_burdens::correction_id.eq(correction_id),
                public::correction_member_burdens::item_id.eq(burden
                    .item_id
                    .as_deref()
                    .map(|id| parse_uuid(id, "item_id"))
                    .transpose()?),
                public::correction_member_burdens::pool_id
                    .eq(parse_uuid(&burden.pool_id, "pool_id")?),
                public::correction_member_burdens::member_id
                    .eq(parse_uuid(&burden.member_id, "member_id")?),
                public::correction_member_burdens::amount_delta_yen.eq(burden.amount_delta),
                public::correction_member_burdens::funding_rule_version
                    .eq(i32::try_from(burden.funding_rule_version)
                        .map_err(|_| CorrectPurchasePersistenceError::InvalidFundingSnapshot)?),
                public::correction_member_burdens::funding_weight
                    .eq(i64::try_from(burden.funding_weight)
                        .map_err(|_| CorrectPurchasePersistenceError::InvalidFundingSnapshot)?),
            ))
            .execute(&mut *conn)
            .await?;
    }
    Ok(())
}

async fn persist_settlement_adjustments(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    correction_id: Uuid,
    period_start: NaiveDate,
    adjustments: &[CorrectionSettlementAdjustment],
) -> Result<(), CorrectPurchasePersistenceError> {
    for adjustment in adjustments {
        if adjustment.amount == 0 {
            continue;
        }
        diesel::insert_into(public::settlement_adjustments::table)
            .values((
                public::settlement_adjustments::id.eq(Uuid::new_v4()),
                public::settlement_adjustments::household_id.eq(household_id),
                public::settlement_adjustments::period_start.eq(period_start),
                public::settlement_adjustments::member_id
                    .eq(parse_uuid(&adjustment.member_id, "member_id")?),
                public::settlement_adjustments::source_kind.eq(SOURCE_KIND),
                public::settlement_adjustments::source_id.eq(correction_id),
                public::settlement_adjustments::amount_yen.eq(adjustment.amount),
                public::settlement_adjustments::created_at.eq(now),
            ))
            .execute(&mut *conn)
            .await?;
    }
    Ok(())
}

fn parse_funding_policy(
    value: Option<&str>,
) -> Result<Option<CorrectionSettlementPolicy>, CorrectPurchasePersistenceError> {
    match value {
        None => Ok(None),
        Some("BALANCE_TO_STANDARD") => Ok(Some(CorrectionSettlementPolicy::BalanceToStandard)),
        Some("REIMBURSE_PERSONAL") => Ok(Some(CorrectionSettlementPolicy::ReimbursePersonal)),
        Some(_) => Err(CorrectPurchasePersistenceError::InvalidFundingSnapshot),
    }
}
