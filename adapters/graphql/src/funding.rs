use async_graphql::{Context, ID, InputObject, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_application::funding_record::{
    RecordFundingCommand, RecordFundingError, RecordFundingWorkflowCommand,
};
use bookkeeping_domain::{AccountingPeriod, YearMonth};
use bookkeeping_postgres::funding_command_context as postgres_funding_context;
use bookkeeping_postgres::funding_record::{PersistRecordFunding, RecordFundingPersistenceError};
use chrono::{Datelike, FixedOffset};
use cross_context_postgres::{
    funding_command_result as postgres_funding_result, policy_data as postgres_funding,
};
use horse_postgres_common::PgPool;
use serde::Serialize;
use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::{Actor, DateTime, Long, MutationErrorCode, internal_error};

#[derive(Clone, Debug, InputObject)]
pub struct RecordFundingInput {
    household_id: ID,
    member_id: ID,
    pool_id: ID,
    amount_yen: Long,
    occurred_at: DateTime,
    idempotency_key: String,
    source_account_id: Option<ID>,
    destination_account_id: Option<ID>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RecordFundingUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RecordFundingSuccess {
    result: crate::ledger_entry::LedgerEntry,
}

#[derive(Union)]
pub enum RecordFundingResult {
    Success(Box<RecordFundingSuccess>),
    UserError(RecordFundingUserError),
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
struct FundingRequestFingerprint<'a> {
    household_id: &'a str,
    member_id: &'a str,
    pool_id: &'a str,
    amount_yen: i64,
    occurred_at: &'a str,
    idempotency_key: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    source_account_id: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    destination_account_id: Option<&'a str>,
}

pub async fn record_funding(
    ctx: &Context<'_>,
    input: RecordFundingInput,
) -> Result<RecordFundingResult> {
    let actor = crate::actor(ctx)?;
    let parsed = match parse_input(actor, input) {
        Ok(parsed) => parsed,
        Err(error) => return Ok(error.into()),
    };
    let workflow_command = RecordFundingWorkflowCommand {
        household_id: parsed.persistence.household_id.clone(),
        occurred_at: parsed.persistence.occurred_at,
        idempotency_key: parsed.persistence.idempotency_key.clone(),
        request_fingerprint: parsed.persistence.request_fingerprint,
        source_account_id: parsed.source_account_id.map(|id| id.to_string()),
        destination_account_id: parsed.destination_account_id.map(|id| id.to_string()),
        record: parsed.command,
    };
    match postgres_funding::record_funding_workflow(ctx.data::<PgPool>()?, workflow_command).await {
        Ok(result) => Ok(Box::new(
            funding_success(
                ctx.data::<PgPool>()?,
                parsed.household_id,
                result.ledger_entry_id,
            )
            .await?,
        )
        .into()),
        Err(bookkeeping_application::funding_record::RecordFundingWorkflowError::Decision(
            error,
        )) => Ok(workflow_error(error).into()),
        Err(bookkeeping_application::funding_record::RecordFundingWorkflowError::Operations(
            bookkeeping_postgres::funding_record::RecordFundingWorkflowAdapterError::Context(error),
        )) => match command_context_error(&error) {
            Some(error) => Ok(error.into()),
            None => Err(internal_error(error)),
        },
        Err(bookkeeping_application::funding_record::RecordFundingWorkflowError::Operations(
            bookkeeping_postgres::funding_record::RecordFundingWorkflowAdapterError::Persistence(
                error,
            ),
        )) => match persistence_error(&error) {
            Some(error) => Ok(error.into()),
            None => Err(internal_error(error)),
        },
    }
}

async fn funding_success(
    pool: &PgPool,
    household_id: Uuid,
    ledger_entry_id: String,
) -> Result<RecordFundingSuccess> {
    let ledger_entry_id = Uuid::parse_str(&ledger_entry_id).map_err(internal_error)?;
    let projection = postgres_funding_result::load_funding_ledger_entry_result(
        pool,
        household_id,
        ledger_entry_id,
    )
    .await
    .map_err(internal_error)?;
    let entry =
        crate::ledger_entry::FundingLedgerEntry::try_from(projection).map_err(internal_error)?;
    Ok(RecordFundingSuccess {
        result: crate::ledger_entry::LedgerEntry::FundingLedgerEntry(entry),
    })
}

struct ParsedRecordFunding {
    command: RecordFundingCommand,
    persistence: PersistRecordFunding,
    household_id: Uuid,
    source_account_id: Option<Uuid>,
    destination_account_id: Option<Uuid>,
}

fn parse_input(
    actor: Actor,
    input: RecordFundingInput,
) -> std::result::Result<ParsedRecordFunding, RecordFundingUserError> {
    let household_id =
        crate::decode_household_id(&input.household_id).map_err(|_| bad_input_error())?;
    let member_id = decode_node_id(&input.member_id, "Member")?;
    let pool_id = decode_node_id(&input.pool_id, "Pool")?;
    let source_account_uuid = input
        .source_account_id
        .as_ref()
        .map(|id| decode_node_id(id, "Account"))
        .transpose()?;
    let destination_account_uuid = input
        .destination_account_id
        .as_ref()
        .map(|id| decode_node_id(id, "Account"))
        .transpose()?;
    let occurred_at = input
        .occurred_at
        .parse_utc()
        .map_err(|_| bad_input_error())?;
    let tokyo = FixedOffset::east_opt(9 * 60 * 60).ok_or_else(bad_input_error)?;
    let local_date = occurred_at.with_timezone(&tokyo).date_naive();
    let period = YearMonth::new(
        u16::try_from(local_date.year()).map_err(|_| bad_input_error())?,
        u8::try_from(local_date.month()).map_err(|_| bad_input_error())?,
    )
    .map_err(|_| bad_input_error())?;
    let fingerprint_payload = FundingRequestFingerprint {
        household_id: input.household_id.as_str(),
        member_id: input.member_id.as_str(),
        pool_id: input.pool_id.as_str(),
        amount_yen: input.amount_yen.0,
        occurred_at: input.occurred_at.as_str(),
        idempotency_key: &input.idempotency_key,
        source_account_id: input.source_account_id.as_ref().map(|id| id.as_str()),
        destination_account_id: input.destination_account_id.as_ref().map(|id| id.as_str()),
    };
    let fingerprint_json =
        serde_json::to_vec(&fingerprint_payload).map_err(|_| bad_input_error())?;
    let request_fingerprint: [u8; 32] = Sha256::digest(fingerprint_json).into();
    Ok(ParsedRecordFunding {
        command: RecordFundingCommand {
            actor_id: actor.0.to_string(),
            member_id: member_id.to_string(),
            pool_id: pool_id.to_string(),
            amount_yen: input.amount_yen.0,
            accounting_period: AccountingPeriod::open(period, 1).map_err(|_| bad_input_error())?,
            source_account: None,
            destination_account: None,
        },
        persistence: PersistRecordFunding {
            household_id: household_id.to_string(),
            actor_id: actor.0.to_string(),
            occurred_at,
            idempotency_key: input.idempotency_key,
            request_fingerprint,
        },
        household_id,
        source_account_id: source_account_uuid,
        destination_account_id: destination_account_uuid,
    })
}

fn decode_node_id(
    value: &ID,
    expected_kind: &str,
) -> std::result::Result<Uuid, RecordFundingUserError> {
    let bytes = URL_SAFE_NO_PAD
        .decode(value.as_str())
        .map_err(|_| bad_input_error())?;
    let separator = bytes
        .iter()
        .position(|byte| *byte == b':')
        .ok_or_else(bad_input_error)?;
    let (kind, payload_with_separator) = bytes.split_at(separator);
    let payload = &payload_with_separator[1..];
    if kind != expected_kind.as_bytes() || payload.len() != 16 {
        return Err(bad_input_error());
    }
    let id = Uuid::from_slice(payload).map_err(|_| bad_input_error())?;
    if id.is_nil() {
        return Err(bad_input_error());
    }
    Ok(id)
}

fn workflow_error(error: RecordFundingError) -> RecordFundingUserError {
    match error {
        RecordFundingError::AccountingPeriodClosed => conflict_error(),
        _ => bad_input_error(),
    }
}

fn persistence_error(error: &RecordFundingPersistenceError) -> Option<RecordFundingUserError> {
    use RecordFundingPersistenceError as E;
    match error {
        E::InvalidUuid(_)
        | E::InvalidIdempotencyKey
        | E::InvalidOccurredAt
        | E::InvalidAccountKind
        | E::AccountOwnershipMismatch => Some(bad_input_error()),
        E::IdempotencyConflict | E::AccountingPeriodClosed => Some(conflict_error()),
        E::HouseholdNotFound | E::MemberNotFound | E::PoolNotFound | E::AccountNotFound => {
            Some(not_found_error())
        }
        E::Pool(_)
        | E::Database(_)
        | E::InvalidDecision
        | E::CorruptPersistedData(_)
        | E::NumericOutOfRange(_) => None,
    }
}

fn command_context_error(
    error: &postgres_funding_context::FundingCommandContextError,
) -> Option<RecordFundingUserError> {
    use postgres_funding_context::FundingCommandContextError as E;
    match error {
        E::HouseholdNotFound | E::PoolNotFound | E::AccountNotFound(_) => Some(not_found_error()),
        E::Database(_) => None,
        E::InvalidTokyoOffset
        | E::InvalidAccountingPeriodStatus(_)
        | E::InvalidPoolKind(_)
        | E::InvalidAccountKind(_) => None,
    }
}

fn bad_input_error() -> RecordFundingUserError {
    RecordFundingUserError {
        code: MutationErrorCode::BadInput,
        message: "入力を確認してください。".to_owned(),
    }
}

fn conflict_error() -> RecordFundingUserError {
    RecordFundingUserError {
        code: MutationErrorCode::Conflict,
        message: "最新の状態を読み込んでから再試行してください。".to_owned(),
    }
}

fn not_found_error() -> RecordFundingUserError {
    RecordFundingUserError {
        code: MutationErrorCode::NotFound,
        message: "対象が見つかりません。".to_owned(),
    }
}
