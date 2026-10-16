use async_graphql::{Context, Enum, ID, InputObject, Result, SimpleObject, Union};
use base64::{Engine as _, engine::general_purpose::URL_SAFE_NO_PAD};
use chrono::{DateTime as ChronoDateTime, Datelike, Duration, NaiveDate, Utc};
use cross_context_application::financial_mutations::{
    FinancialMutationError, RecordRepaymentCommand, RecordTransferCommand, TransferKind,
};
use serde::Serialize;
use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::{DateTime, Long, MutationErrorCode, internal_error};

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum TransferInputKind {
    Advance,
    Loan,
    Gift,
}

#[derive(Clone, Debug, InputObject)]
pub struct RecordTransferInput {
    household_id: ID,
    from_member_id: ID,
    to_member_id: ID,
    kind: TransferInputKind,
    payment_account_id: Option<ID>,
    destination_account_id: Option<ID>,
    amount_yen: Long,
    occurred_at: DateTime,
    description: String,
    idempotency_key: String,
    source_document_id: Option<ID>,
    source_reference: Option<String>,
    source_account_id: Option<ID>,
}

#[derive(Clone, Debug, InputObject)]
pub struct RecordRepaymentInput {
    household_id: ID,
    from_member_id: ID,
    to_member_id: ID,
    loan_id: ID,
    payment_account_id: Option<ID>,
    destination_account_id: Option<ID>,
    amount_yen: Long,
    occurred_at: DateTime,
    description: String,
    idempotency_key: String,
    source_document_id: Option<ID>,
    source_reference: Option<String>,
    source_account_id: Option<ID>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RecordTransferSuccess {
    result: crate::ledger_entry::LedgerEntry,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RecordTransferUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum RecordTransferResult {
    Success(Box<RecordTransferSuccess>),
    UserError(RecordTransferUserError),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RecordRepaymentSuccess {
    result: crate::ledger_entry::LedgerEntry,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RecordRepaymentUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum RecordRepaymentResult {
    Success(Box<RecordRepaymentSuccess>),
    UserError(RecordRepaymentUserError),
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct TransferFingerprint<'a> {
    household_id: &'a str,
    from_member_id: &'a str,
    to_member_id: &'a str,
    kind: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    payment_account_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    destination_account_id: Option<&'a str>,
    amount_yen: i64,
    occurred_at: &'a str,
    description: &'a str,
    idempotency_key: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    source_document_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    source_reference: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    source_account_id: Option<&'a str>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct RepaymentFingerprint<'a> {
    household_id: &'a str,
    from_member_id: &'a str,
    to_member_id: &'a str,
    loan_id: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    payment_account_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    destination_account_id: Option<&'a str>,
    amount_yen: i64,
    occurred_at: &'a str,
    description: &'a str,
    idempotency_key: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    source_document_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    source_reference: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    source_account_id: Option<&'a str>,
}

pub async fn record_transfer(
    ctx: &Context<'_>,
    input: RecordTransferInput,
) -> Result<RecordTransferResult> {
    let actor = crate::actor(ctx)?;
    let parsed = match parse_transfer_input(input) {
        Ok(value) => value,
        Err(error) => return Ok(error.into()),
    };
    let command = RecordTransferCommand {
        household_id: parsed.household_id,
        actor_id: actor.0,
        from_member_id: parsed.from_member_id,
        to_member_id: parsed.to_member_id,
        kind: match parsed.kind {
            TransferInputKind::Advance => TransferKind::Advance,
            TransferInputKind::Loan => TransferKind::Loan,
            TransferInputKind::Gift => TransferKind::Gift,
        },
        payment_account_id: parsed.payment_account_id,
        destination_account_id: parsed.destination_account_id,
        amount_yen: parsed.amount_yen,
        occurred_at: parsed.occurred_at,
        description: parsed.description,
        idempotency_key: parsed.idempotency_key,
        source_document_id: parsed.source_document_id,
        source_reference: parsed.source_reference,
        source_account_id: parsed.source_account_id,
        request_fingerprint: parsed.fingerprint,
    };
    let result = match ctx
        .data::<std::sync::Arc<
            dyn cross_context_application::financial_mutations::FinancialMutationsHandler,
        >>()?
        .record_transfer(command)
        .await
    {
        Ok(value) => value,
        Err(error) => return transfer_handler_failure(error),
    };
    let entry = load_ledger_entry(ctx, parsed.household_id, result.ledger_entry_id).await?;
    Ok(Box::new(RecordTransferSuccess { result: entry }).into())
}

pub async fn record_repayment(
    ctx: &Context<'_>,
    input: RecordRepaymentInput,
) -> Result<RecordRepaymentResult> {
    let actor = crate::actor(ctx)?;
    let parsed = match parse_repayment_input(input) {
        Ok(value) => value,
        Err(error) => return Ok(error.into()),
    };
    let command = RecordRepaymentCommand {
        household_id: parsed.household_id,
        actor_id: actor.0,
        from_member_id: parsed.from_member_id,
        to_member_id: parsed.to_member_id,
        loan_id: parsed.loan_id,
        payment_account_id: parsed.payment_account_id,
        destination_account_id: parsed.destination_account_id,
        amount_yen: parsed.amount_yen,
        occurred_at: parsed.occurred_at,
        description: parsed.description,
        idempotency_key: parsed.idempotency_key,
        source_document_id: parsed.source_document_id,
        source_reference: parsed.source_reference,
        source_account_id: parsed.source_account_id,
        request_fingerprint: parsed.fingerprint,
    };
    let result = match ctx
        .data::<std::sync::Arc<
            dyn cross_context_application::financial_mutations::FinancialMutationsHandler,
        >>()?
        .record_repayment(command)
        .await
    {
        Ok(value) => value,
        Err(error) => return repayment_handler_failure(error),
    };
    let entry = load_ledger_entry(ctx, parsed.household_id, result.ledger_entry_id).await?;
    Ok(Box::new(RecordRepaymentSuccess { result: entry }).into())
}

struct ParsedTransferInput {
    household_id: Uuid,
    from_member_id: Uuid,
    to_member_id: Uuid,
    kind: TransferInputKind,
    payment_account_id: Option<Uuid>,
    destination_account_id: Option<Uuid>,
    amount_yen: i64,
    occurred_at: ChronoDateTime<Utc>,
    description: String,
    idempotency_key: String,
    source_document_id: Option<Uuid>,
    source_reference: Option<String>,
    source_account_id: Option<Uuid>,
    fingerprint: [u8; 32],
}

struct ParsedRepaymentInput {
    household_id: Uuid,
    from_member_id: Uuid,
    to_member_id: Uuid,
    loan_id: Uuid,
    payment_account_id: Option<Uuid>,
    destination_account_id: Option<Uuid>,
    amount_yen: i64,
    occurred_at: ChronoDateTime<Utc>,
    description: String,
    idempotency_key: String,
    source_document_id: Option<Uuid>,
    source_reference: Option<String>,
    source_account_id: Option<Uuid>,
    fingerprint: [u8; 32],
}

fn parse_transfer_input(
    input: RecordTransferInput,
) -> std::result::Result<ParsedTransferInput, RecordTransferUserError> {
    let household_id =
        crate::decode_household_id(&input.household_id).map_err(|_| bad_transfer_error())?;
    let from_member_id = decode_node_id(&input.from_member_id, "Member")?;
    let to_member_id = decode_node_id(&input.to_member_id, "Member")?;
    let payment_account_id = decode_optional_node_id(input.payment_account_id.as_ref(), "Account")?;
    let destination_account_id =
        decode_optional_node_id(input.destination_account_id.as_ref(), "Account")?;
    let source_document_id = decode_optional_uuid(input.source_document_id.as_ref())
        .map_err(|_| bad_transfer_error())?;
    let source_account_id = decode_optional_node_id(input.source_account_id.as_ref(), "Account")?;
    let occurred_at = input
        .occurred_at
        .parse_utc()
        .map_err(|_| bad_transfer_error())?;
    let amount = input.amount_yen.0;
    if amount <= 0
        || amount > 1_000_000_000_000
        || input.idempotency_key.trim().is_empty()
        || input.idempotency_key.len() > 200
        || from_member_id == to_member_id
    {
        return Err(bad_transfer_error());
    }
    validate_statement_fields(
        source_document_id,
        input.source_reference.as_deref(),
        source_account_id,
        input.kind,
    )
    .map_err(|_| bad_transfer_error())?;
    let fingerprint_data = TransferFingerprint {
        household_id: input.household_id.as_str(),
        from_member_id: input.from_member_id.as_str(),
        to_member_id: input.to_member_id.as_str(),
        kind: match input.kind {
            TransferInputKind::Advance => "ADVANCE",
            TransferInputKind::Loan => "LOAN",
            TransferInputKind::Gift => "GIFT",
        },
        payment_account_id: input.payment_account_id.as_ref().map(|id| id.0.as_str()),
        destination_account_id: input
            .destination_account_id
            .as_ref()
            .map(|id| id.0.as_str()),
        amount_yen: amount,
        occurred_at: input.occurred_at.as_str(),
        description: &input.description,
        idempotency_key: &input.idempotency_key,
        source_document_id: input.source_document_id.as_ref().map(|id| id.0.as_str()),
        source_reference: input.source_reference.as_deref(),
        source_account_id: input.source_account_id.as_ref().map(|id| id.0.as_str()),
    };
    let fingerprint = fingerprint(&fingerprint_data)?;
    Ok(ParsedTransferInput {
        household_id,
        from_member_id,
        to_member_id,
        kind: input.kind,
        payment_account_id,
        destination_account_id,
        amount_yen: amount,
        occurred_at,
        description: input.description,
        idempotency_key: input.idempotency_key,
        source_document_id,
        source_reference: input.source_reference,
        source_account_id,
        fingerprint,
    })
}

fn parse_repayment_input(
    input: RecordRepaymentInput,
) -> std::result::Result<ParsedRepaymentInput, RecordRepaymentUserError> {
    let household_id =
        crate::decode_household_id(&input.household_id).map_err(|_| bad_repayment_error())?;
    let from_member_id =
        decode_node_id(&input.from_member_id, "Member").map_err(|_| bad_repayment_error())?;
    let to_member_id =
        decode_node_id(&input.to_member_id, "Member").map_err(|_| bad_repayment_error())?;
    let loan_id =
        decode_node_id(&input.loan_id, "LoanBalance").map_err(|_| bad_repayment_error())?;
    let payment_account_id = decode_optional_node_id(input.payment_account_id.as_ref(), "Account")
        .map_err(|_| bad_repayment_error())?;
    let destination_account_id =
        decode_optional_node_id(input.destination_account_id.as_ref(), "Account")
            .map_err(|_| bad_repayment_error())?;
    let source_document_id = decode_optional_uuid(input.source_document_id.as_ref())
        .map_err(|_| bad_repayment_error())?;
    let source_account_id = decode_optional_node_id(input.source_account_id.as_ref(), "Account")
        .map_err(|_| bad_repayment_error())?;
    let occurred_at = input
        .occurred_at
        .parse_utc()
        .map_err(|_| bad_repayment_error())?;
    if input.amount_yen.0 <= 0
        || input.amount_yen.0 > 1_000_000_000_000
        || input.idempotency_key.trim().is_empty()
        || input.idempotency_key.len() > 200
        || from_member_id == to_member_id
    {
        return Err(bad_repayment_error());
    }
    validate_statement_fields(
        source_document_id,
        input.source_reference.as_deref(),
        source_account_id,
        TransferInputKind::Loan,
    )
    .map_err(|_| bad_repayment_error())?;
    let fingerprint_data = RepaymentFingerprint {
        household_id: input.household_id.as_str(),
        from_member_id: input.from_member_id.as_str(),
        to_member_id: input.to_member_id.as_str(),
        loan_id: input.loan_id.as_str(),
        payment_account_id: input.payment_account_id.as_ref().map(|id| id.0.as_str()),
        destination_account_id: input
            .destination_account_id
            .as_ref()
            .map(|id| id.0.as_str()),
        amount_yen: input.amount_yen.0,
        occurred_at: input.occurred_at.as_str(),
        description: &input.description,
        idempotency_key: &input.idempotency_key,
        source_document_id: input.source_document_id.as_ref().map(|id| id.0.as_str()),
        source_reference: input.source_reference.as_deref(),
        source_account_id: input.source_account_id.as_ref().map(|id| id.0.as_str()),
    };
    let fingerprint = fingerprint(&fingerprint_data).map_err(|_| bad_repayment_error())?;
    Ok(ParsedRepaymentInput {
        household_id,
        from_member_id,
        to_member_id,
        loan_id,
        payment_account_id,
        destination_account_id,
        amount_yen: input.amount_yen.0,
        occurred_at,
        description: input.description,
        idempotency_key: input.idempotency_key,
        source_document_id,
        source_reference: input.source_reference,
        source_account_id,
        fingerprint,
    })
}

fn validate_statement_fields(
    source_document_id: Option<Uuid>,
    source_reference: Option<&str>,
    source_account_id: Option<Uuid>,
    kind: TransferInputKind,
) -> Result<(), ()> {
    let provided =
        source_document_id.is_some() || source_reference.is_some() || source_account_id.is_some();
    if provided
        && (source_document_id.is_none()
            || source_reference.is_none_or(str::is_empty)
            || source_account_id.is_none()
            || !matches!(kind, TransferInputKind::Loan)
            || source_reference.is_some_and(|value| value.len() > 256))
    {
        return Err(());
    }
    Ok(())
}

fn decode_optional_node_id(
    value: Option<&ID>,
    expected_kind: &str,
) -> std::result::Result<Option<Uuid>, RecordTransferUserError> {
    value
        .map(|value| decode_node_id(value, expected_kind))
        .transpose()
}

fn decode_optional_uuid(value: Option<&ID>) -> std::result::Result<Option<Uuid>, ()> {
    value
        .map(|value| Uuid::parse_str(value.as_str()).map_err(|_| ()))
        .transpose()
}

fn decode_node_id(
    value: &ID,
    expected_kind: &str,
) -> std::result::Result<Uuid, RecordTransferUserError> {
    let bytes = URL_SAFE_NO_PAD
        .decode(value.as_str())
        .map_err(|_| bad_transfer_error())?;
    let separator = bytes
        .iter()
        .position(|byte| *byte == b':')
        .ok_or_else(bad_transfer_error)?;
    let (kind, payload_with_separator) = bytes.split_at(separator);
    let payload = &payload_with_separator[1..];
    if kind != expected_kind.as_bytes() || payload.len() != 16 {
        return Err(bad_transfer_error());
    }
    let id = Uuid::from_slice(payload).map_err(|_| bad_transfer_error())?;
    if id.is_nil() {
        return Err(bad_transfer_error());
    }
    Ok(id)
}

fn fingerprint(value: &impl Serialize) -> std::result::Result<[u8; 32], RecordTransferUserError> {
    let encoded = serde_json::to_vec(value).map_err(|_| bad_transfer_error())?;
    Ok(Sha256::digest(encoded).into())
}

async fn load_ledger_entry(
    ctx: &Context<'_>,
    household_id: Uuid,
    ledger_entry_id: Uuid,
) -> Result<crate::ledger_entry::LedgerEntry> {
    let now = Utc::now();
    let next_month = if now.month() == 12 {
        NaiveDate::from_ymd_opt(now.year() + 1, 1, 1)
    } else {
        NaiveDate::from_ymd_opt(now.year(), now.month() + 1, 1)
    }
    .ok_or_else(|| internal_error("invalid next month"))?;
    let period_end = chrono::DateTime::<Utc>::from_naive_utc_and_offset(
        next_month
            .checked_add_signed(Duration::days(i64::from(now.day() - 1)))
            .ok_or_else(|| internal_error("invalid next month date"))?
            .and_time(now.time()),
        Utc,
    );
    let projection = ctx
        .data::<std::sync::Arc<
            dyn cross_context_application::node_financial_batch::NodeFinancialBatchQuery,
        >>()?
        .load(
            cross_context_application::node_financial_batch::NodeFinancialBatchInput {
                household_ids: vec![household_id],
                funding_schedule_ids: Vec::new(),
                budget_ids: Vec::new(),
                loan_balance_ids: Vec::new(),
                ledger_entry_ids: vec![ledger_entry_id],
                transfer_ids: Vec::new(),
                period_start: now,
                period_end,
            },
        )
        .await
        .map_err(internal_error)?;
    let projection = projection
        .transfer_ledger_entries
        .get(&ledger_entry_id)
        .cloned()
        .ok_or_else(|| internal_error("transfer ledger entry was not loaded"))?;
    crate::ledger_entry::LedgerEntry::try_from(projection)
        .map_err(|error| internal_error(format!("invalid transfer ledger entry: {error:?}")))
}

fn transfer_handler_failure(error: FinancialMutationError) -> Result<RecordTransferResult> {
    match error {
        FinancialMutationError::NotFound => Ok(not_found_transfer_error().into()),
        FinancialMutationError::Conflict => Ok(conflict_transfer_error().into()),
        FinancialMutationError::InvalidInput => Ok(bad_transfer_error().into()),
        FinancialMutationError::Infrastructure(error) => Err(internal_error(error)),
    }
}

fn repayment_handler_failure(error: FinancialMutationError) -> Result<RecordRepaymentResult> {
    match error {
        FinancialMutationError::NotFound => Ok(not_found_repayment_error().into()),
        FinancialMutationError::Conflict => Ok(conflict_repayment_error().into()),
        FinancialMutationError::InvalidInput => Ok(bad_repayment_error().into()),
        FinancialMutationError::Infrastructure(error) => Err(internal_error(error)),
    }
}

fn bad_transfer_error() -> RecordTransferUserError {
    RecordTransferUserError {
        code: MutationErrorCode::BadInput,
        message: "入力を確認してください。".to_owned(),
    }
}

fn conflict_transfer_error() -> RecordTransferUserError {
    RecordTransferUserError {
        code: MutationErrorCode::Conflict,
        message: "最新の状態を読み込んでから再試行してください。".to_owned(),
    }
}

fn not_found_transfer_error() -> RecordTransferUserError {
    RecordTransferUserError {
        code: MutationErrorCode::NotFound,
        message: "対象が見つかりません。".to_owned(),
    }
}

fn bad_repayment_error() -> RecordRepaymentUserError {
    RecordRepaymentUserError {
        code: MutationErrorCode::BadInput,
        message: "入力を確認してください。".to_owned(),
    }
}

fn conflict_repayment_error() -> RecordRepaymentUserError {
    RecordRepaymentUserError {
        code: MutationErrorCode::Conflict,
        message: "最新の状態を読み込んでから再試行してください。".to_owned(),
    }
}

fn not_found_repayment_error() -> RecordRepaymentUserError {
    RecordRepaymentUserError {
        code: MutationErrorCode::NotFound,
        message: "対象が見つかりません。".to_owned(),
    }
}
