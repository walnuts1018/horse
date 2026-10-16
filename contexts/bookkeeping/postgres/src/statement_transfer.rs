use chrono::{DateTime, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use bookkeeping_application::statement_transfer::{
    LinkStatementTransferCommand, StatementTransferObservation, decide_link_statement_transfer,
};

use crate::evidence_port::EvidenceOperations;
use crate::{PgPool, schema::public};

#[derive(Debug, Clone)]
pub struct PersistLinkStatementTransfer {
    pub household_id: String,
    pub actor_id: String,
    pub ledger_entry_id: String,
    pub source_document_id: String,
    pub source_reference: String,
    pub source_account_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedStatementTransfer {
    pub ledger_entry_id: String,
}

#[derive(Debug)]
pub enum StatementTransferPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidSource,
    InvalidDecision,
    HouseholdNotFound,
    MemberNotFound,
    LedgerEntryNotFound,
    TransferNotFound,
    SourceDocumentNotFound,
    SourceIsNotBankStatement,
    StatementExtractionUnavailable,
    StatementReferenceNotFound,
    StatementAlreadyConfirmed,
    StatementDoesNotMatchTransfer,
    AccountNotFound,
    AccountIsNotBank,
    JournalEntryMismatch,
}

impl From<DieselError> for StatementTransferPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_link_statement_transfer(
    pool: &PgPool,
    input: PersistLinkStatementTransfer,
    evidence: &impl EvidenceOperations,
) -> Result<PersistedStatementTransfer, StatementTransferPersistenceError> {
    validate_input(&input)?;
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let ledger_entry_id = parse_uuid(&input.ledger_entry_id, "ledger_entry_id")?;
    let document_id = parse_uuid(&input.source_document_id, "source_document_id")?;
    let source_account_id = parse_uuid(&input.source_account_id, "source_account_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| StatementTransferPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedStatementTransfer, StatementTransferPersistenceError, _>(
            async |conn| {
                let household_exists =
                    household_postgres::household_access::lock_household_for_update(
                        &mut *conn,
                        household_id,
                    )
                    .await?;
                if !household_exists {
                    return Err(StatementTransferPersistenceError::HouseholdNotFound);
                }
                let role = household_postgres::household_access::active_member_role(
                    &mut *conn,
                    household_id,
                    actor_id,
                )
                .await?
                .ok_or(StatementTransferPersistenceError::MemberNotFound)?;
                horse_authorization::authorize(
                    Some(&role),
                    horse_authorization::Operation::FinancialData,
                )
                .map_err(|_| StatementTransferPersistenceError::MemberNotFound)?;

                let (source_kind, statement) = load_statement(
                    evidence,
                    &mut *conn,
                    household_id,
                    document_id,
                    &input.source_reference,
                )
                .await?;
                let ledger = public::ledger_entries::table
                    .filter(public::ledger_entries::household_id.eq(household_id))
                    .filter(public::ledger_entries::id.eq(ledger_entry_id))
                    .for_update()
                    .select((
                        public::ledger_entries::source_kind,
                        public::ledger_entries::source_id,
                        public::ledger_entries::journal_id,
                        public::ledger_entries::occurred_at,
                        public::ledger_entries::amount_yen,
                    ))
                    .first::<(String, Uuid, Uuid, DateTime<Utc>, i64)>(&mut *conn)
                    .await
                    .optional()?
                    .ok_or(StatementTransferPersistenceError::LedgerEntryNotFound)?;
                if ledger.0 != "transfer" {
                    return Err(StatementTransferPersistenceError::TransferNotFound);
                }
                let transfer = public::transfers::table
                    .filter(public::transfers::household_id.eq(household_id))
                    .filter(public::transfers::id.eq(ledger.1))
                    .for_update()
                    .select((
                        public::transfers::id,
                        public::transfers::journal_id,
                        public::transfers::kind,
                        public::transfers::amount_yen,
                        public::transfers::payment_account_id,
                    ))
                    .first::<(Uuid, Uuid, String, i64, Option<Uuid>)>(&mut *conn)
                    .await
                    .optional()?
                    .ok_or(StatementTransferPersistenceError::TransferNotFound)?;
                if transfer.1 != ledger.2 || transfer.3 != ledger.4 {
                    return Err(StatementTransferPersistenceError::StatementDoesNotMatchTransfer);
                }
                let account_kind = public::accounts::table
                    .filter(public::accounts::household_id.eq(household_id))
                    .filter(public::accounts::id.eq(source_account_id))
                    .select(public::accounts::kind)
                    .first::<String>(&mut *conn)
                    .await
                    .optional()?
                    .ok_or(StatementTransferPersistenceError::AccountNotFound)?;
                if account_kind != "BANK" {
                    return Err(StatementTransferPersistenceError::AccountIsNotBank);
                }
                let journal_lines = public::journal_entries::table
                    .filter(public::journal_entries::household_id.eq(household_id))
                    .filter(public::journal_entries::journal_id.eq(transfer.1))
                    .filter(public::journal_entries::account_id.is_not_null())
                    .select((
                        public::journal_entries::account_id,
                        public::journal_entries::debit_yen,
                        public::journal_entries::credit_yen,
                    ))
                    .load::<(Option<Uuid>, i64, i64)>(&mut *conn)
                    .await?;
                let destination_account_id = journal_lines
                    .iter()
                    .find(|(_, debit, credit)| *debit == transfer.3 && *credit == 0)
                    .and_then(|(account, _, _)| *account)
                    .map(|account| account.to_string());
                let matching_lines = journal_lines
                    .iter()
                    .filter(|(account, _, _)| *account == Some(source_account_id))
                    .collect::<Vec<_>>();
                if matching_lines.len() != 1
                    || statement.amount_yen < 0
                        && (matching_lines[0].2 != transfer.3 || matching_lines[0].1 != 0)
                    || statement.amount_yen > 0
                        && (matching_lines[0].1 != transfer.3 || matching_lines[0].2 != 0)
                {
                    return Err(StatementTransferPersistenceError::JournalEntryMismatch);
                }

                let command = LinkStatementTransferCommand {
                    actor_id: actor_id.to_string(),
                    household_id: household_id.to_string(),
                    ledger_entry_id: ledger_entry_id.to_string(),
                    source_document_id: document_id.to_string(),
                    source_reference: input.source_reference.clone(),
                    source_account_id: source_account_id.to_string(),
                };
                let decision = decide_link_statement_transfer(
                    command,
                    StatementTransferObservation {
                        source_kind,
                        kind: statement.kind,
                        amount_yen: statement.amount_yen,
                        occurred_at_unix_micros: statement.occurred_at.timestamp_micros(),
                    },
                    &transfer.2,
                    transfer.3,
                    ledger.3.timestamp_micros(),
                    transfer.4.map(|id| id.to_string()).as_deref(),
                    destination_account_id.as_deref(),
                )
                .map_err(|_| StatementTransferPersistenceError::StatementDoesNotMatchTransfer)?;
                if decision.ledger_entry_id != ledger_entry_id.to_string() {
                    return Err(StatementTransferPersistenceError::InvalidDecision);
                }
                insert_confirmation(
                    evidence,
                    &mut *conn,
                    household_id,
                    actor_id,
                    document_id,
                    &decision.source_reference,
                    transfer.0,
                )
                .await?;
                Ok(PersistedStatementTransfer {
                    ledger_entry_id: ledger_entry_id.to_string(),
                })
            },
        )
        .await
}

struct StatementRow {
    kind: String,
    amount_yen: i64,
    occurred_at: DateTime<Utc>,
}

async fn load_statement<E: EvidenceOperations + ?Sized>(
    evidence: &E,
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    document_id: Uuid,
    source_reference: &str,
) -> Result<(String, StatementRow), StatementTransferPersistenceError> {
    if source_reference.is_empty() || source_reference.len() > 256 {
        return Err(StatementTransferPersistenceError::InvalidSource);
    }
    let source_kind = evidence
        .source_kind(conn, household_id, document_id, true)
        .await?
        .ok_or(StatementTransferPersistenceError::SourceDocumentNotFound)?;
    if source_kind != "bank" {
        return Err(StatementTransferPersistenceError::SourceIsNotBankStatement);
    }
    let already_confirmed = evidence
        .latest_confirmation_status(conn, household_id, document_id, source_reference)
        .await?;
    if already_confirmed.as_deref() == Some("confirmed") {
        return Err(StatementTransferPersistenceError::StatementAlreadyConfirmed);
    }
    let result = evidence
        .latest_extraction(conn, household_id, document_id)
        .await?
        .ok_or(StatementTransferPersistenceError::StatementExtractionUnavailable)?;
    let rows = result
        .get("statements")
        .and_then(serde_json::Value::as_array)
        .ok_or(StatementTransferPersistenceError::StatementExtractionUnavailable)?;
    for row in rows {
        let row_no = row.get("row").and_then(serde_json::Value::as_i64);
        let reference = row
            .get("sourceReference")
            .and_then(serde_json::Value::as_str)
            .filter(|value| !value.is_empty())
            .map(str::to_owned)
            .or_else(|| row_no.map(|number| format!("row:{number}")));
        if reference.as_deref() != Some(source_reference) {
            continue;
        }
        let kind = row
            .get("kind")
            .and_then(serde_json::Value::as_str)
            .ok_or(StatementTransferPersistenceError::StatementExtractionUnavailable)?
            .to_owned();
        let amount_yen = row
            .get("amount")
            .and_then(serde_json::Value::as_i64)
            .ok_or(StatementTransferPersistenceError::StatementExtractionUnavailable)?;
        let occurred_at = row
            .get("occurredAt")
            .and_then(serde_json::Value::as_str)
            .and_then(|value| DateTime::parse_from_rfc3339(value).ok())
            .map(|value| value.with_timezone(&Utc))
            .ok_or(StatementTransferPersistenceError::StatementExtractionUnavailable)?;
        return Ok((
            source_kind,
            StatementRow {
                kind,
                amount_yen,
                occurred_at,
            },
        ));
    }
    Err(StatementTransferPersistenceError::StatementReferenceNotFound)
}

async fn insert_confirmation<E: EvidenceOperations + ?Sized>(
    evidence: &E,
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
    document_id: Uuid,
    source_reference: &str,
    transfer_id: Uuid,
) -> Result<(), StatementTransferPersistenceError> {
    evidence
        .insert_confirmation(
            conn,
            household_id,
            document_id,
            source_reference,
            "transfer",
            transfer_id,
            actor_id,
            format!("transfer-confirmation:{}", Uuid::now_v7()),
            None,
        )
        .await
        .map_err(StatementTransferPersistenceError::from)
}

fn validate_input(
    input: &PersistLinkStatementTransfer,
) -> Result<(), StatementTransferPersistenceError> {
    if input.source_document_id.trim().is_empty()
        || input.source_reference.trim().is_empty()
        || input.source_reference.len() > 256
        || input.source_account_id.trim().is_empty()
    {
        return Err(StatementTransferPersistenceError::InvalidSource);
    }
    Ok(())
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, StatementTransferPersistenceError> {
    Uuid::parse_str(value).map_err(|_| StatementTransferPersistenceError::InvalidUuid(field))
}
