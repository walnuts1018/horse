use async_graphql::{Context, Enum, ID, InputObject, Result, SimpleObject, Union};
use base64::{Engine as _, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_domain::PositiveYen;
use bookkeeping_workflows::{
    MemberId,
    record_repayment::{RecordRepaymentCommand, RepaymentError, decide_record_repayment},
    record_transfer::{
        RecordTransferCommand, RecordTransferError, TransferBase, decide_record_transfer,
    },
};
use chrono::{DateTime as ChronoDateTime, Utc};
use horse_postgres::{
    PgPool, financial_command_context as postgres_context,
    financial_command_result as postgres_result, record_repayment as postgres_repayment,
    record_transfer as postgres_transfer,
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
    Success(RecordTransferSuccess),
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
    Success(RecordRepaymentSuccess),
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
    for member_id in [actor.0, parsed.from_member_id, parsed.to_member_id] {
        match postgres_access(ctx.data::<PgPool>()?, parsed.household_id, member_id).await {
            Ok(true) => {}
            Ok(false) => return Ok(not_found_transfer_error().into()),
            Err(error) => return Err(internal_error(error)),
        }
    }
    let replay_input = postgres_transfer::FindRecordTransferReplay {
        household_id: parsed.household_id.to_string(),
        actor_id: actor.0.to_string(),
        from_member_id: parsed.from_member_id.to_string(),
        to_member_id: parsed.to_member_id.to_string(),
        idempotency_key: parsed.idempotency_key.clone(),
        request_fingerprint: parsed.fingerprint,
        source_document_id: parsed.source_document_id.map(|id| id.to_string()),
        source_reference: parsed.source_reference.clone(),
        source_account_id: parsed.source_account_id.map(|id| id.to_string()),
    };
    let replay =
        match postgres_transfer::find_record_transfer_replay(ctx.data::<PgPool>()?, &replay_input)
            .await
        {
            Ok(value) => value,
            Err(error) => return transfer_persistence_error(error),
        };
    if let Some(ledger_entry_id) = replay {
        let entry =
            load_ledger_entry(ctx.data::<PgPool>()?, parsed.household_id, &ledger_entry_id).await?;
        return Ok(RecordTransferSuccess { result: entry }.into());
    }
    let command_context = match postgres_context::load_transfer_command_context(
        ctx.data::<PgPool>()?,
        parsed.household_id,
        parsed.from_member_id,
        parsed.payment_account_id,
        parsed.to_member_id,
        parsed.destination_account_id,
        parsed.occurred_at,
    )
    .await
    {
        Ok(value) => value,
        Err(error) => return transfer_context_error(error),
    };
    let amount = match PositiveYen::new(parsed.amount_yen) {
        Ok(value) => value,
        Err(_) => return Ok(bad_transfer_error().into()),
    };
    let base = TransferBase {
        actor_id: member_id(actor.0)?,
        from_member_id: member_id(parsed.from_member_id)?,
        to_member_id: member_id(parsed.to_member_id)?,
        amount,
        description: parsed.description.clone(),
        period_status: command_context.period_status,
        payment_account: command_context.payment_account,
        destination_account: command_context.destination_account,
    };
    // RecordTransferInputの種別はAdvance/Loan/Giftのみで、返済はRecordRepaymentInputで扱うため、LoanBasisなしの組み合わせはここでは構築できない。
    let command = match parsed.kind {
        TransferInputKind::Advance => RecordTransferCommand::Advance(base),
        TransferInputKind::Loan => RecordTransferCommand::Loan(base),
        TransferInputKind::Gift => RecordTransferCommand::Gift(base),
    };
    let decision = match decide_record_transfer(command) {
        Ok(value) => value,
        Err(error) => return Ok(transfer_workflow_error(error).into()),
    };
    let result = match postgres_transfer::persist_record_transfer(
        ctx.data::<PgPool>()?,
        postgres_transfer::PersistRecordTransfer {
            household_id: parsed.household_id.to_string(),
            actor_id: actor.0.to_string(),
            occurred_at: parsed.occurred_at,
            idempotency_key: parsed.idempotency_key,
            request_fingerprint: parsed.fingerprint,
            source_document_id: parsed.source_document_id.map(|id| id.to_string()),
            source_reference: parsed.source_reference,
            source_account_id: parsed.source_account_id.map(|id| id.to_string()),
        },
        decision,
    )
    .await
    {
        Ok(value) => value,
        Err(error) => return transfer_persistence_error(error),
    };
    let entry = load_ledger_entry(
        ctx.data::<PgPool>()?,
        parsed.household_id,
        &result.ledger_entry_id,
    )
    .await?;
    Ok(RecordTransferSuccess { result: entry }.into())
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
    for member_id in [actor.0, parsed.from_member_id, parsed.to_member_id] {
        match postgres_access(ctx.data::<PgPool>()?, parsed.household_id, member_id).await {
            Ok(true) => {}
            Ok(false) => return Ok(not_found_repayment_error().into()),
            Err(error) => return Err(internal_error(error)),
        }
    }
    let replay_input = postgres_repayment::FindRecordRepaymentReplay {
        household_id: parsed.household_id.to_string(),
        actor_id: actor.0.to_string(),
        from_member_id: parsed.from_member_id.to_string(),
        to_member_id: parsed.to_member_id.to_string(),
        idempotency_key: parsed.idempotency_key.clone(),
        request_fingerprint: parsed.fingerprint,
        source_document_id: parsed.source_document_id.map(|id| id.to_string()),
        source_reference: parsed.source_reference.clone(),
        source_account_id: parsed.source_account_id.map(|id| id.to_string()),
    };
    let replay = match postgres_repayment::find_record_repayment_replay(
        ctx.data::<PgPool>()?,
        &replay_input,
    )
    .await
    {
        Ok(value) => value,
        Err(error) => return repayment_persistence_error(error),
    };
    if let Some(ledger_entry_id) = replay {
        let entry =
            load_ledger_entry(ctx.data::<PgPool>()?, parsed.household_id, &ledger_entry_id).await?;
        return Ok(RecordRepaymentSuccess { result: entry }.into());
    }
    let command_context = match postgres_context::load_repayment_command_context(
        ctx.data::<PgPool>()?,
        parsed.household_id,
        parsed.loan_id,
        parsed.payment_account_id,
        parsed.destination_account_id,
        parsed.from_member_id,
        parsed.to_member_id,
        parsed.occurred_at,
    )
    .await
    {
        Ok(value) => value,
        Err(error) => return repayment_context_error(error),
    };
    let amount = match PositiveYen::new(parsed.amount_yen) {
        Ok(value) => value,
        Err(_) => return Ok(bad_repayment_error().into()),
    };
    let decision = match decide_record_repayment(RecordRepaymentCommand {
        loan: command_context.loan,
        repaid_so_far: command_context.repaid_so_far,
        from_member: member_id(parsed.from_member_id)?,
        to_member: member_id(parsed.to_member_id)?,
        amount,
        accounting_period: command_context.accounting_period,
        accounting_period_open: command_context.accounting_period_open,
        source: command_context.source,
        destination_account: command_context.destination_account,
        description: parsed.description.clone(),
    }) {
        Ok(value) => value,
        Err(error) => return Ok(repayment_workflow_error(error).into()),
    };
    let result = match postgres_repayment::persist_record_repayment(
        ctx.data::<PgPool>()?,
        postgres_repayment::PersistRecordRepayment {
            household_id: parsed.household_id.to_string(),
            actor_id: actor.0.to_string(),
            occurred_at: parsed.occurred_at,
            idempotency_key: parsed.idempotency_key,
            request_fingerprint: parsed.fingerprint,
            source_document_id: parsed.source_document_id.map(|id| id.to_string()),
            source_reference: parsed.source_reference,
            source_account_id: parsed.source_account_id.map(|id| id.to_string()),
        },
        decision,
    )
    .await
    {
        Ok(value) => value,
        Err(error) => return repayment_persistence_error(error),
    };
    let entry = load_ledger_entry(
        ctx.data::<PgPool>()?,
        parsed.household_id,
        &result.ledger_entry_id,
    )
    .await?;
    Ok(RecordRepaymentSuccess { result: entry }.into())
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

fn member_id(id: Uuid) -> Result<MemberId> {
    MemberId::new(id.to_string()).map_err(internal_error)
}

fn fingerprint(value: &impl Serialize) -> std::result::Result<[u8; 32], RecordTransferUserError> {
    let encoded = serde_json::to_vec(value).map_err(|_| bad_transfer_error())?;
    Ok(Sha256::digest(encoded).into())
}

async fn postgres_access(
    pool: &PgPool,
    household_id: Uuid,
    member_id: Uuid,
) -> Result<bool, impl std::fmt::Debug> {
    horse_postgres::household_access::is_active_member(pool, household_id, member_id).await
}

async fn load_ledger_entry(
    pool: &PgPool,
    household_id: Uuid,
    ledger_entry_id: &str,
) -> Result<crate::ledger_entry::LedgerEntry> {
    let ledger_entry_id = Uuid::parse_str(ledger_entry_id).map_err(internal_error)?;
    let projection =
        postgres_result::load_transfer_ledger_entry_result(pool, household_id, ledger_entry_id)
            .await
            .map_err(internal_error)?;
    crate::ledger_entry::LedgerEntry::try_from(projection).map_err(internal_error)
}

fn transfer_workflow_error(error: RecordTransferError) -> RecordTransferUserError {
    match error {
        RecordTransferError::AccountingPeriodClosed => conflict_transfer_error(),
        _ => bad_transfer_error(),
    }
}

fn repayment_workflow_error(error: RepaymentError) -> RecordRepaymentUserError {
    match error {
        RepaymentError::AccountingPeriodClosed => conflict_repayment_error(),
        _ => bad_repayment_error(),
    }
}

fn transfer_context_error(
    error: postgres_context::FinancialCommandContextError,
) -> Result<RecordTransferResult> {
    use postgres_context::FinancialCommandContextError as E;
    match error {
        E::HouseholdNotFound | E::AccountNotFound | E::LoanNotFound => {
            Ok(not_found_transfer_error().into())
        }
        E::InvalidAccountKind(_)
        | E::InvalidPoolKind(_)
        | E::InvalidAccountingPeriod(_)
        | E::InvalidLoan => Ok(bad_transfer_error().into()),
        E::InvalidTokyoOffset => Err(internal_error("invalid Tokyo offset")),
        E::Pool(_) | E::Database(_) => Err(internal_error(error)),
    }
}

fn repayment_context_error(
    error: postgres_context::FinancialCommandContextError,
) -> Result<RecordRepaymentResult> {
    use postgres_context::FinancialCommandContextError as E;
    match error {
        E::HouseholdNotFound | E::AccountNotFound | E::LoanNotFound => {
            Ok(not_found_repayment_error().into())
        }
        E::InvalidAccountKind(_)
        | E::InvalidPoolKind(_)
        | E::InvalidAccountingPeriod(_)
        | E::InvalidLoan => Ok(bad_repayment_error().into()),
        E::InvalidTokyoOffset => Err(internal_error("invalid Tokyo offset")),
        E::Pool(_) | E::Database(_) => Err(internal_error(error)),
    }
}

fn transfer_persistence_error(
    error: postgres_transfer::RecordTransferPersistenceError,
) -> Result<RecordTransferResult> {
    use postgres_transfer::RecordTransferPersistenceError as E;
    match error {
        E::IdempotencyConflict | E::AccountingPeriodClosed => Ok(conflict_transfer_error().into()),
        E::HouseholdNotFound
        | E::MemberNotFound
        | E::AccountNotFound
        | E::PoolNotFound
        | E::LoanNotFound
        | E::SourceDocumentNotFound
        | E::StatementReferenceNotFound => Ok(not_found_transfer_error().into()),
        E::StatementAlreadyConfirmed => Ok(conflict_transfer_error().into()),
        E::InvalidUuid(_)
        | E::InvalidIdempotencyKey
        | E::NumericOutOfRange(_)
        | E::LoanDirectionMismatch
        | E::RepaymentExceedsOutstanding
        | E::InvalidAccountKind
        | E::AccountWithoutPool
        | E::AccountOwnershipMismatch
        | E::SourceIsNotBankStatement
        | E::StatementExtractionUnavailable
        | E::StatementDoesNotMatchTransfer
        | E::InvalidDecision => Ok(bad_transfer_error().into()),
        E::Pool(_) | E::Database(_) => Err(internal_error(error)),
    }
}

fn repayment_persistence_error(
    error: postgres_repayment::RecordRepaymentPersistenceError,
) -> Result<RecordRepaymentResult> {
    use postgres_repayment::RecordRepaymentPersistenceError as E;
    match error {
        E::IdempotencyConflict | E::AccountingPeriodClosed => Ok(conflict_repayment_error().into()),
        E::StatementAlreadyConfirmed => Ok(conflict_repayment_error().into()),
        E::HouseholdNotFound
        | E::LoanNotFound
        | E::MemberNotFound
        | E::SourceDocumentNotFound
        | E::StatementReferenceNotFound => Ok(not_found_repayment_error().into()),
        E::InvalidUuid(_)
        | E::InvalidPeriod
        | E::InvalidIdempotencyKey
        | E::NumericOutOfRange(_)
        | E::LoanDirectionMismatch
        | E::RepaymentExceedsOutstanding
        | E::AccountNotFound
        | E::InvalidAccountKind
        | E::SourceIsNotBankStatement
        | E::StatementExtractionUnavailable
        | E::StatementDoesNotMatchTransfer
        | E::InvalidDecision => Ok(bad_repayment_error().into()),
        E::Pool(_) | E::Database(_) => Err(internal_error(error)),
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
