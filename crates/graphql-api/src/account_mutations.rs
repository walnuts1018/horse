use async_graphql::{Context, Enum, ID, InputObject, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_domain::PositiveYen;
use bookkeeping_workflows::{
    AccountId, MemberId,
    account_income::{
        RecordAccountIncomeCommand, StatementIncomeSource, decide_record_account_income,
    },
    account_movement::{
        AccountMovementKind as DomainAccountMovementKind, RecordAccountMovementCommand,
        StatementMovementSource, decide_record_account_movement,
    },
};
use chrono::{DateTime as ChronoDateTime, Utc};
use horse_postgres::{
    PgPool, account_command_context, account_command_result,
    account_income::{
        AccountIncomePersistenceError, PersistRecordAccountIncome, persist_record_account_income,
    },
    account_movement::{
        AccountMovementPersistenceError, PersistRecordAccountMovement,
        persist_record_account_movement,
    },
    household_access,
};
use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::Long;

#[derive(Clone, Debug, InputObject)]
pub struct RecordAccountMovementInput {
    pub household_id: ID,
    pub kind: AccountMovementKind,
    pub from_account_id: ID,
    pub to_account_id: ID,
    pub amount_yen: Long,
    pub occurred_at: crate::DateTime,
    pub description: String,
    pub idempotency_key: String,
    pub source_document_id: Option<ID>,
    pub source_reference: Option<String>,
    pub source_account_id: Option<ID>,
}

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum AccountMovementKind {
    Transfer,
    CardBillPayment,
}

impl From<AccountMovementKind> for DomainAccountMovementKind {
    fn from(value: AccountMovementKind) -> Self {
        match value {
            AccountMovementKind::Transfer => Self::Transfer,
            AccountMovementKind::CardBillPayment => Self::CardBillPayment,
        }
    }
}

#[derive(Clone, Debug, InputObject)]
pub struct RecordExternalIncomeInput {
    pub household_id: ID,
    pub account_id: ID,
    pub amount_yen: Long,
    pub occurred_at: crate::DateTime,
    pub description: String,
    pub idempotency_key: String,
    pub source_document_id: Option<String>,
    pub source_reference: Option<String>,
}

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum AccountKind {
    Bank,
    Cash,
    Card,
    Other,
}

#[derive(Clone, Debug, SimpleObject)]
#[graphql(name = "Account")]
pub struct AccountObject {
    pub id: ID,
    pub name: String,
    pub kind: AccountKind,
    pub card_last4: Option<String>,
    pub pool_id: Option<ID>,
}

#[derive(Clone, Debug, SimpleObject)]
#[graphql(name = "AccountMovement")]
pub struct AccountMovementObject {
    pub id: ID,
    pub household_id: ID,
    pub kind: AccountMovementKind,
    pub occurred_at: crate::DateTime,
    pub from_account_id: ID,
    pub from_account: AccountObject,
    pub to_account_id: ID,
    pub to_account: AccountObject,
    pub amount_yen: Long,
    pub currency: String,
    pub description: String,
    pub created_at: crate::DateTime,
}

#[derive(Clone, Debug, SimpleObject)]
#[graphql(name = "AccountIncome")]
pub struct AccountIncomeObject {
    pub id: ID,
    pub household_id: ID,
    pub account_id: ID,
    pub amount_yen: Long,
    pub currency: String,
    pub occurred_at: crate::DateTime,
    pub description: String,
    pub created_at: crate::DateTime,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RecordAccountMovementUserError {
    pub code: crate::MutationErrorCode,
    pub message: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RecordExternalIncomeUserError {
    pub code: crate::MutationErrorCode,
    pub message: String,
}

pub async fn graphql_record_account_movement(
    ctx: &Context<'_>,
    input: RecordAccountMovementInput,
) -> Result<RecordAccountMovementResult> {
    let actor = crate::actor(ctx)?;
    let pool = ctx.data::<PgPool>()?;
    match record_account_movement(pool, actor.0, input).await? {
        AccountMutationOutcome::UserError(error) => {
            Ok(RecordAccountMovementUserError::from(error).into())
        }
        AccountMutationOutcome::Persisted(value) => {
            let household_id = Uuid::parse_str(&value.household_id)
                .map_err(|error| crate::internal_error(error))?;
            let movement_id = Uuid::parse_str(&value.id).map_err(crate::internal_error)?;
            let result = account_command_result::load_account_movement_command_result(
                pool,
                household_id,
                movement_id,
            )
            .await
            .map_err(crate::internal_error)?;
            let kind = match result.kind.as_str() {
                "TRANSFER" => AccountMovementKind::Transfer,
                "CARD_BILL_PAYMENT" => AccountMovementKind::CardBillPayment,
                _ => {
                    return Err(crate::internal_error(
                        "invalid persisted account movement kind",
                    ));
                }
            };
            Ok(RecordAccountMovementSuccess {
                result: AccountMovementObject {
                    id: encode_node_id("AccountMovement", &result.id.to_string()),
                    household_id: encode_node_id("Household", &result.household_id.to_string()),
                    kind,
                    occurred_at: result.occurred_at.into(),
                    from_account_id: encode_node_id("Account", &result.from_account_id.to_string()),
                    from_account: account_object(result.from_account)?,
                    to_account_id: encode_node_id("Account", &result.to_account_id.to_string()),
                    to_account: account_object(result.to_account)?,
                    amount_yen: Long(result.amount_yen),
                    currency: "JPY".to_owned(),
                    description: result.description,
                    created_at: result.created_at.into(),
                },
            }
            .into())
        }
    }
}

pub async fn graphql_record_external_income(
    ctx: &Context<'_>,
    input: RecordExternalIncomeInput,
) -> Result<RecordExternalIncomeResult> {
    let actor = crate::actor(ctx)?;
    let pool = ctx.data::<PgPool>()?;
    match record_external_income(pool, actor.0, input).await? {
        AccountMutationOutcome::UserError(error) => {
            Ok(RecordExternalIncomeUserError::from(error).into())
        }
        AccountMutationOutcome::Persisted(value) => {
            let household_id =
                Uuid::parse_str(&value.household_id).map_err(crate::internal_error)?;
            let income_id = Uuid::parse_str(&value.id).map_err(crate::internal_error)?;
            let result = account_command_result::load_account_income_command_result(
                pool,
                household_id,
                income_id,
            )
            .await
            .map_err(crate::internal_error)?;
            Ok(RecordExternalIncomeSuccess {
                result: AccountIncomeObject {
                    id: encode_node_id("AccountIncome", &result.id.to_string()),
                    household_id: encode_node_id("Household", &result.household_id.to_string()),
                    account_id: encode_node_id("Account", &result.account_id.to_string()),
                    amount_yen: Long(result.amount_yen),
                    currency: "JPY".to_owned(),
                    occurred_at: result.occurred_at.into(),
                    description: result.description,
                    created_at: result.created_at.into(),
                },
            }
            .into())
        }
    }
}

fn account_object(account: account_command_result::AccountCommandAccount) -> Result<AccountObject> {
    let kind = match account.kind.as_str() {
        "BANK" => AccountKind::Bank,
        "CASH" => AccountKind::Cash,
        "CARD" => AccountKind::Card,
        "OTHER" => AccountKind::Other,
        _ => return Err(crate::internal_error("invalid persisted account kind")),
    };
    Ok(AccountObject {
        id: encode_node_id("Account", &account.id.to_string()),
        name: account.name,
        kind,
        card_last4: account.card_last4,
        pool_id: account
            .pool_id
            .map(|id| encode_node_id("Pool", &id.to_string())),
    })
}

#[derive(Union)]
pub enum RecordAccountMovementResult {
    Success(RecordAccountMovementSuccess),
    UserError(RecordAccountMovementUserError),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RecordAccountMovementSuccess {
    pub result: AccountMovementObject,
}

#[derive(Union)]
pub enum RecordExternalIncomeResult {
    Success(RecordExternalIncomeSuccess),
    UserError(RecordExternalIncomeUserError),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RecordExternalIncomeSuccess {
    pub result: AccountIncomeObject,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PersistedAccountMovementResult {
    pub id: String,
    pub household_id: String,
    pub kind: AccountMovementKind,
    pub occurred_at: crate::DateTime,
    pub from_account_id: String,
    pub to_account_id: String,
    pub amount_yen: i64,
    pub description: String,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PersistedAccountIncomeResult {
    pub id: String,
    pub household_id: String,
    pub account_id: String,
    pub occurred_at: ChronoDateTime<Utc>,
    pub amount_yen: i64,
    pub description: String,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AccountMutationFailure {
    BadInput,
    Conflict,
    NotFound,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AccountMutationOutcome<T> {
    Persisted(T),
    UserError(AccountMutationFailure),
}

impl From<AccountMutationFailure> for RecordAccountMovementUserError {
    fn from(value: AccountMutationFailure) -> Self {
        let (code, message) = match value {
            AccountMutationFailure::BadInput => (
                crate::MutationErrorCode::BadInput,
                "入力を確認してください。",
            ),
            AccountMutationFailure::Conflict => (
                crate::MutationErrorCode::Conflict,
                "最新の状態を読み込んでから再試行してください。",
            ),
            AccountMutationFailure::NotFound => {
                (crate::MutationErrorCode::NotFound, "対象が見つかりません。")
            }
        };
        Self {
            code,
            message: message.to_owned(),
        }
    }
}

impl From<AccountMutationFailure> for RecordExternalIncomeUserError {
    fn from(value: AccountMutationFailure) -> Self {
        let user_error = RecordAccountMovementUserError::from(value);
        Self {
            code: user_error.code,
            message: user_error.message,
        }
    }
}

pub async fn record_account_movement(
    pool: &PgPool,
    actor_id: Uuid,
    input: RecordAccountMovementInput,
) -> Result<AccountMutationOutcome<PersistedAccountMovementResult>, async_graphql::Error> {
    let occurred_at = match input.occurred_at.parse_utc() {
        Ok(value) => value,
        Err(_) => {
            return Ok(AccountMutationOutcome::UserError(
                AccountMutationFailure::BadInput,
            ));
        }
    };
    let (Ok(household_id), Ok(from_account_id), Ok(to_account_id)) = (
        decode_node_id(&input.household_id, "Household"),
        decode_node_id(&input.from_account_id, "Account"),
        decode_node_id(&input.to_account_id, "Account"),
    ) else {
        return Ok(AccountMutationOutcome::UserError(
            AccountMutationFailure::BadInput,
        ));
    };
    let source_account_id = match input.source_account_id.as_ref() {
        Some(id) => match decode_node_id(id, "Account") {
            Ok(value) => Some(value),
            Err(_) => {
                return Ok(AccountMutationOutcome::UserError(
                    AccountMutationFailure::BadInput,
                ));
            }
        },
        None => None,
    };
    let amount = match PositiveYen::new(input.amount_yen.0) {
        Ok(value) if value.value() <= 1_000_000_000_000 => value,
        _ => {
            return Ok(AccountMutationOutcome::UserError(
                AccountMutationFailure::BadInput,
            ));
        }
    };
    if input.idempotency_key.trim().is_empty() || input.idempotency_key.len() > 200 {
        return Ok(AccountMutationOutcome::UserError(
            AccountMutationFailure::BadInput,
        ));
    }
    let source_document_id = input
        .source_document_id
        .as_ref()
        .map(|id| id.to_string())
        .filter(|value| !value.is_empty());
    let source_reference = input
        .source_reference
        .clone()
        .filter(|value| !value.is_empty());
    let statement_source = match (source_document_id, source_reference, source_account_id) {
        (None, None, None) => None,
        (Some(source_document_id), Some(source_reference), Some(statement_account_id)) => {
            Some(StatementMovementSource {
                source_document_id,
                source_reference,
                statement_account_id: match AccountId::new(statement_account_id) {
                    Ok(value) => value,
                    Err(_) => {
                        return Ok(AccountMutationOutcome::UserError(
                            AccountMutationFailure::BadInput,
                        ));
                    }
                },
            })
        }
        _ => {
            return Ok(AccountMutationOutcome::UserError(
                AccountMutationFailure::BadInput,
            ));
        }
    };
    let household_uuid = parse_uuid(&household_id)?;
    let from_account_uuid = parse_uuid(&from_account_id)?;
    let to_account_uuid = parse_uuid(&to_account_id)?;
    let mut account_ids = vec![from_account_uuid, to_account_uuid];
    if let Some(source) = &statement_source {
        account_ids.push(parse_uuid(source.statement_account_id.as_str())?);
    }
    match household_access::is_active_member(pool, household_uuid, actor_id).await {
        Ok(true) => {}
        Ok(false) => {
            return Ok(AccountMutationOutcome::UserError(
                AccountMutationFailure::NotFound,
            ));
        }
        Err(error) => return Err(crate::internal_error(error)),
    }
    let facts = match account_command_context::load_account_command_context(
        pool,
        household_uuid,
        &account_ids,
        occurred_at,
    )
    .await
    {
        Ok(value) => value,
        Err(error) => return Err(crate::internal_error(error)),
    };
    if !facts.missing_account_ids.is_empty() {
        return Ok(AccountMutationOutcome::UserError(
            AccountMutationFailure::NotFound,
        ));
    }
    let (Some(from_account_kind), Some(to_account_kind)) = (
        facts.account_kinds.get(&from_account_uuid),
        facts.account_kinds.get(&to_account_uuid),
    ) else {
        return Ok(AccountMutationOutcome::UserError(
            AccountMutationFailure::NotFound,
        ));
    };
    let command = RecordAccountMovementCommand {
        actor_id: match MemberId::new(actor_id.to_string()) {
            Ok(value) => value,
            Err(_) => {
                return Ok(AccountMutationOutcome::UserError(
                    AccountMutationFailure::BadInput,
                ));
            }
        },
        from_account_id: match AccountId::new(from_account_id.clone()) {
            Ok(value) => value,
            Err(_) => {
                return Ok(AccountMutationOutcome::UserError(
                    AccountMutationFailure::BadInput,
                ));
            }
        },
        from_account_kind: *from_account_kind,
        to_account_id: match AccountId::new(to_account_id.clone()) {
            Ok(value) => value,
            Err(_) => {
                return Ok(AccountMutationOutcome::UserError(
                    AccountMutationFailure::BadInput,
                ));
            }
        },
        to_account_kind: *to_account_kind,
        kind: input.kind.into(),
        occurred_at_unix_micros: occurred_at.timestamp_micros(),
        amount,
        description: input.description.clone(),
        accounting_period_open: facts.accounting_period_open,
        statement_source,
    };
    let decision = match decide_record_account_movement(command) {
        Ok(value) => value,
        Err(_) => {
            return Ok(AccountMutationOutcome::UserError(
                AccountMutationFailure::BadInput,
            ));
        }
    };
    let fingerprint = movement_fingerprint(&input);
    match persist_record_account_movement(
        pool,
        PersistRecordAccountMovement {
            household_id: household_id.clone(),
            actor_id: actor_id.to_string(),
            occurred_at,
            idempotency_key: input.idempotency_key,
            request_fingerprint: fingerprint,
        },
        decision,
    )
    .await
    {
        Ok(value) => Ok(AccountMutationOutcome::Persisted(
            PersistedAccountMovementResult {
                id: value.id,
                household_id,
                kind: input.kind,
                occurred_at: input.occurred_at.clone(),
                from_account_id,
                to_account_id,
                amount_yen: value.amount_yen,
                description: value.description,
            },
        )),
        Err(error) => map_movement_error(error),
    }
}

pub async fn record_external_income(
    pool: &PgPool,
    actor_id: Uuid,
    input: RecordExternalIncomeInput,
) -> Result<AccountMutationOutcome<PersistedAccountIncomeResult>, async_graphql::Error> {
    let occurred_at = match input.occurred_at.parse_utc() {
        Ok(value) => value,
        Err(_) => {
            return Ok(AccountMutationOutcome::UserError(
                AccountMutationFailure::BadInput,
            ));
        }
    };
    let (Ok(household_id), Ok(account_id)) = (
        decode_node_id(&input.household_id, "Household"),
        decode_node_id(&input.account_id, "Account"),
    ) else {
        return Ok(AccountMutationOutcome::UserError(
            AccountMutationFailure::BadInput,
        ));
    };
    let amount = match PositiveYen::new(input.amount_yen.0) {
        Ok(value) if value.value() <= 1_000_000_000_000 => value,
        _ => {
            return Ok(AccountMutationOutcome::UserError(
                AccountMutationFailure::BadInput,
            ));
        }
    };
    if input.idempotency_key.trim().is_empty() || input.idempotency_key.len() > 200 {
        return Ok(AccountMutationOutcome::UserError(
            AccountMutationFailure::BadInput,
        ));
    }
    let description = input.description.trim().to_owned();
    if description.len() > 240 {
        return Ok(AccountMutationOutcome::UserError(
            AccountMutationFailure::BadInput,
        ));
    }
    let description = if description.is_empty() {
        "External income".to_owned()
    } else {
        description
    };
    let source_document_id = input
        .source_document_id
        .as_deref()
        .filter(|value| !value.is_empty());
    let source_reference = input
        .source_reference
        .as_deref()
        .filter(|value| !value.is_empty());
    let statement_source = match (source_document_id, source_reference) {
        (None, None) => None,
        (Some(document_id), Some(reference)) => Some(StatementIncomeSource {
            source_document_id: document_id.to_owned(),
            source_reference: reference.to_owned(),
            statement_account_id: match AccountId::new(account_id.clone()) {
                Ok(value) => value,
                Err(_) => {
                    return Ok(AccountMutationOutcome::UserError(
                        AccountMutationFailure::BadInput,
                    ));
                }
            },
        }),
        _ => {
            return Ok(AccountMutationOutcome::UserError(
                AccountMutationFailure::BadInput,
            ));
        }
    };
    let household_uuid = parse_uuid(&household_id)?;
    let account_uuid = parse_uuid(&account_id)?;
    match household_access::is_active_member(pool, household_uuid, actor_id).await {
        Ok(true) => {}
        Ok(false) => {
            return Ok(AccountMutationOutcome::UserError(
                AccountMutationFailure::NotFound,
            ));
        }
        Err(error) => return Err(crate::internal_error(error)),
    }
    let facts = match account_command_context::load_account_command_context(
        pool,
        household_uuid,
        &[account_uuid],
        occurred_at,
    )
    .await
    {
        Ok(value) => value,
        Err(error) => return Err(crate::internal_error(error)),
    };
    if !facts.missing_account_ids.is_empty() {
        return Ok(AccountMutationOutcome::UserError(
            AccountMutationFailure::NotFound,
        ));
    }
    let Some(account_kind) = facts.income_kind(account_uuid) else {
        return Ok(AccountMutationOutcome::UserError(
            AccountMutationFailure::NotFound,
        ));
    };
    let decision = match decide_record_account_income(RecordAccountIncomeCommand {
        actor_id: match MemberId::new(actor_id.to_string()) {
            Ok(value) => value,
            Err(_) => {
                return Ok(AccountMutationOutcome::UserError(
                    AccountMutationFailure::BadInput,
                ));
            }
        },
        account_id: match AccountId::new(account_id.clone()) {
            Ok(value) => value,
            Err(_) => {
                return Ok(AccountMutationOutcome::UserError(
                    AccountMutationFailure::BadInput,
                ));
            }
        },
        account_kind,
        amount,
        description,
        accounting_period_open: facts.accounting_period_open,
        statement_source,
    }) {
        Ok(value) => value,
        Err(_) => {
            return Ok(AccountMutationOutcome::UserError(
                AccountMutationFailure::BadInput,
            ));
        }
    };
    let request_fingerprint = income_fingerprint(&input);
    match persist_record_account_income(
        pool,
        PersistRecordAccountIncome {
            household_id: household_id.clone(),
            actor_id: actor_id.to_string(),
            occurred_at,
            idempotency_key: input.idempotency_key,
            request_fingerprint,
        },
        decision,
    )
    .await
    {
        Ok(value) => Ok(AccountMutationOutcome::Persisted(
            PersistedAccountIncomeResult {
                id: value.id,
                household_id,
                account_id,
                occurred_at: value.occurred_at,
                amount_yen: value.amount_yen,
                description: value.description,
            },
        )),
        Err(error) => map_income_error(error),
    }
}

fn parse_uuid(value: &str) -> Result<Uuid, async_graphql::Error> {
    Uuid::parse_str(value).map_err(|_| async_graphql::Error::new("invalid UUID"))
}

fn decode_node_id(id: &ID, expected: &str) -> Result<String, async_graphql::Error> {
    let bytes = URL_SAFE_NO_PAD
        .decode(id.as_str())
        .map_err(|_| async_graphql::Error::new("invalid node ID"))?;
    let separator = bytes
        .iter()
        .position(|byte| *byte == b':')
        .ok_or_else(|| async_graphql::Error::new("invalid node ID"))?;
    let (kind, payload_with_separator) = bytes.split_at(separator);
    let payload = &payload_with_separator[1..];
    if kind != expected.as_bytes() || payload.len() != 16 {
        return Err(async_graphql::Error::new("invalid node ID"));
    }
    let uuid =
        Uuid::from_slice(payload).map_err(|_| async_graphql::Error::new("invalid node ID"))?;
    if uuid.is_nil() {
        return Err(async_graphql::Error::new("invalid node ID"));
    }
    Ok(uuid.to_string())
}

fn encode_node_id(kind: &str, value: &str) -> ID {
    let uuid = Uuid::parse_str(value).expect("persisted IDs are UUIDs");
    let mut bytes = Vec::with_capacity(kind.len() + 17);
    bytes.extend_from_slice(kind.as_bytes());
    bytes.push(b':');
    bytes.extend_from_slice(uuid.as_bytes());
    ID(URL_SAFE_NO_PAD.encode(bytes))
}

fn movement_fingerprint(input: &RecordAccountMovementInput) -> [u8; 32] {
    fingerprint(&[
        input.household_id.as_str(),
        match input.kind {
            AccountMovementKind::Transfer => "TRANSFER",
            AccountMovementKind::CardBillPayment => "CARD_BILL_PAYMENT",
        },
        input.from_account_id.as_str(),
        input.to_account_id.as_str(),
        &input.amount_yen.0.to_string(),
        input.occurred_at.as_str(),
        &input.description,
        &input.idempotency_key,
        input
            .source_document_id
            .as_ref()
            .map_or("", |id| id.as_str()),
        input.source_reference.as_deref().unwrap_or(""),
        input
            .source_account_id
            .as_ref()
            .map_or("", |id| id.as_str()),
    ])
}

fn income_fingerprint(input: &RecordExternalIncomeInput) -> [u8; 32] {
    fingerprint(&[
        input.household_id.as_str(),
        input.account_id.as_str(),
        &input.amount_yen.0.to_string(),
        input.occurred_at.as_str(),
        &input.description,
        &input.idempotency_key,
        input.source_document_id.as_deref().unwrap_or(""),
        input.source_reference.as_deref().unwrap_or(""),
    ])
}

fn fingerprint(fields: &[&str]) -> [u8; 32] {
    let mut hash = Sha256::new();
    for field in fields {
        hash.update((field.len() as u64).to_be_bytes());
        hash.update(field.as_bytes());
    }
    hash.finalize().into()
}

fn map_movement_error(
    error: AccountMovementPersistenceError,
) -> Result<AccountMutationOutcome<PersistedAccountMovementResult>, async_graphql::Error> {
    use AccountMovementPersistenceError as E;
    match error {
        E::IdempotencyConflict | E::AccountingPeriodClosed => Ok(
            AccountMutationOutcome::UserError(AccountMutationFailure::Conflict),
        ),
        E::HouseholdNotFound
        | E::MemberNotFound
        | E::AccountNotFound
        | E::SourceDocumentNotFound => Ok(AccountMutationOutcome::UserError(
            AccountMutationFailure::NotFound,
        )),
        E::InvalidIdempotencyKey
        | E::InvalidDecision
        | E::InvalidUuid(_)
        | E::StatementExtractionUnavailable
        | E::StatementReferenceNotFound
        | E::StatementAlreadyConfirmed
        | E::StatementIsNotBankMovement
        | E::StatementMovementMismatch => Ok(AccountMutationOutcome::UserError(
            AccountMutationFailure::BadInput,
        )),
        E::Pool(_) | E::Database(_) => Err(crate::internal_error(error)),
    }
}

fn map_income_error(
    error: AccountIncomePersistenceError,
) -> Result<AccountMutationOutcome<PersistedAccountIncomeResult>, async_graphql::Error> {
    use AccountIncomePersistenceError as E;
    match error {
        E::IdempotencyConflict | E::AccountingPeriodClosed => Ok(
            AccountMutationOutcome::UserError(AccountMutationFailure::Conflict),
        ),
        E::HouseholdNotFound
        | E::MemberNotFound
        | E::AccountNotFound
        | E::SourceDocumentNotFound => Ok(AccountMutationOutcome::UserError(
            AccountMutationFailure::NotFound,
        )),
        E::InvalidIdempotencyKey
        | E::InvalidDecision
        | E::InvalidUuid(_)
        | E::StatementExtractionUnavailable
        | E::StatementReferenceNotFound
        | E::StatementAlreadyConfirmed
        | E::StatementIsNotExternalIncome
        | E::StatementIncomeMismatch => Ok(AccountMutationOutcome::UserError(
            AccountMutationFailure::BadInput,
        )),
        E::Pool(_) | E::Database(_) => Err(crate::internal_error(error)),
    }
}
