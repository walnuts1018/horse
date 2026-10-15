use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use std::collections::BTreeSet;
use uuid::Uuid;

use bookkeeping_application::{
    Side,
    record_repayment::RecordRepaymentDecision,
    statement_transfer::{
        LinkStatementTransferCommand, StatementTransferObservation, decide_link_statement_transfer,
    },
};
use bookkeeping_domain::AccountingPeriodStatus;

use crate::{PgPool, schema::public};

const OPERATION: &str = "recordTransfer";

#[derive(Debug, Clone)]
pub struct PersistRecordRepayment {
    pub household_id: String,
    pub actor_id: String,
    pub occurred_at: DateTime<Utc>,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
    pub source_document_id: Option<String>,
    pub source_reference: Option<String>,
    pub source_account_id: Option<String>,
}

#[derive(Debug, Clone)]
pub struct FindRecordRepaymentReplay {
    pub household_id: String,
    pub actor_id: String,
    pub from_member_id: String,
    pub to_member_id: String,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
    pub source_document_id: Option<String>,
    pub source_reference: Option<String>,
    pub source_account_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedRepayment {
    pub transfer_id: String,
    pub journal_id: String,
    pub ledger_entry_id: String,
}

#[derive(Debug)]
pub enum RecordRepaymentPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidPeriod,
    InvalidIdempotencyKey,
    IdempotencyConflict,
    NumericOutOfRange(&'static str),
    AccountingPeriodClosed,
    HouseholdNotFound,
    LoanNotFound,
    MemberNotFound,
    LoanDirectionMismatch,
    RepaymentExceedsOutstanding,
    AccountNotFound,
    InvalidAccountKind,
    SourceDocumentNotFound,
    SourceIsNotBankStatement,
    StatementExtractionUnavailable,
    StatementReferenceNotFound,
    StatementAlreadyConfirmed,
    StatementDoesNotMatchTransfer,
    InvalidDecision,
}

impl From<DieselError> for RecordRepaymentPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_record_repayment(
    pool: &PgPool,
    input: PersistRecordRepayment,
    decision: RecordRepaymentDecision,
) -> Result<PersistedRepayment, RecordRepaymentPersistenceError> {
    if input.idempotency_key.is_empty() || input.idempotency_key.len() > 200 {
        return Err(RecordRepaymentPersistenceError::InvalidIdempotencyKey);
    }
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let loan_id = parse_uuid(decision.loan_id.as_str(), "loan_id")?;
    if decision.accounting_period.status() != AccountingPeriodStatus::Open {
        return Err(RecordRepaymentPersistenceError::AccountingPeriodClosed);
    }
    let period_start = NaiveDate::from_ymd_opt(
        i32::from(decision.accounting_period.year_month().year()),
        u32::from(decision.accounting_period.year_month().month()),
        1,
    )
    .ok_or(RecordRepaymentPersistenceError::InvalidPeriod)?;
    let tokyo_offset =
        FixedOffset::east_opt(9 * 60 * 60).ok_or(RecordRepaymentPersistenceError::InvalidPeriod)?;
    if input
        .occurred_at
        .with_timezone(&tokyo_offset)
        .date_naive()
        .with_day(1)
        != Some(period_start)
        || decision.event.loan_id != decision.loan_id
        || decision.amount.value() <= 0
        || decision.journal_lines.is_empty()
        || decision.settlement_adjustments.is_empty()
        || decision.event.event_type.is_empty()
    {
        return Err(RecordRepaymentPersistenceError::InvalidDecision);
    }
    validate_decision(&decision)?;
    validate_statement_input(&input)?;
    let transfer_id = Uuid::new_v4();
    let journal_id = Uuid::new_v4();
    let ledger_entry_id = Uuid::new_v4();
    let amount = decision.amount.value();

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| RecordRepaymentPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedRepayment, RecordRepaymentPersistenceError, _>(async |conn| {
            use public::idempotency_records::dsl as idem;
            use public::journals::dsl as journals;
            use public::ledger_entries::dsl as ledger;

            ensure_open_period(
                &mut *conn,
                household_id,
                period_start,
                decision.accounting_period.revision(),
            )
            .await?;
            public::households::table
                .filter(public::households::id.eq(household_id))
                .select(public::households::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .ok_or(RecordRepaymentPersistenceError::HouseholdNotFound)?;
            validate_active_members(&mut *conn, household_id, actor_id, &decision).await?;
            let inserted = diesel::insert_into(idem::idempotency_records)
                .values((
                    idem::household_id.eq(household_id),
                    idem::operation.eq(OPERATION),
                    idem::key.eq(&input.idempotency_key),
                    idem::request_fingerprint.eq(input.request_fingerprint.to_vec()),
                    idem::resource_kind.eq("ledger_entry"),
                    idem::resource_id.eq(ledger_entry_id),
                ))
                .on_conflict((idem::household_id, idem::operation, idem::key))
                .do_nothing()
                .returning(idem::resource_id)
                .get_result::<Uuid>(&mut *conn)
                .await
                .optional()?;
            if inserted.is_none() {
                let (fingerprint, resource_kind, resource_id) = idem::idempotency_records
                    .filter(idem::household_id.eq(household_id))
                    .filter(idem::operation.eq(OPERATION))
                    .filter(idem::key.eq(&input.idempotency_key))
                    .select((
                        idem::request_fingerprint,
                        idem::resource_kind,
                        idem::resource_id,
                    ))
                    .first::<(Vec<u8>, String, Uuid)>(&mut *conn)
                    .await?;
                if fingerprint.as_slice() != input.request_fingerprint
                    || resource_kind != "ledger_entry"
                {
                    return Err(RecordRepaymentPersistenceError::IdempotencyConflict);
                }
                let (saved_journal_id, saved_transfer_id) = ledger::ledger_entries
                    .filter(ledger::household_id.eq(household_id))
                    .filter(ledger::id.eq(resource_id))
                    .select((ledger::journal_id, ledger::source_id))
                    .first::<(Uuid, Uuid)>(&mut *conn)
                    .await?;
                validate_statement_replay(&mut *conn, household_id, saved_transfer_id, &input)
                    .await?;
                let saved_transfer_id = public::transfers::table
                    .filter(public::transfers::household_id.eq(household_id))
                    .filter(public::transfers::id.eq(saved_transfer_id))
                    .select(public::transfers::id)
                    .first::<Uuid>(&mut *conn)
                    .await?;
                return Ok(PersistedRepayment {
                    transfer_id: saved_transfer_id.to_string(),
                    journal_id: saved_journal_id.to_string(),
                    ledger_entry_id: resource_id.to_string(),
                });
            }

            let (lender_id, borrower_id) =
                ensure_outstanding(&mut *conn, household_id, loan_id, &decision, amount).await?;
            validate_members_and_accounts(&mut *conn, household_id, actor_id, &decision).await?;
            let statement_decision = prepare_statement_transfer(
                &mut *conn,
                household_id,
                actor_id,
                ledger_entry_id,
                input.occurred_at,
                amount,
                &decision,
                &input,
            )
            .await?;
            diesel::insert_into(journals::journals)
                .values((
                    journals::id.eq(journal_id),
                    journals::household_id.eq(household_id),
                    journals::source_kind.eq("transfer"),
                    journals::source_id.eq(transfer_id),
                    journals::occurred_at.eq(input.occurred_at),
                    journals::created_by.eq(actor_id),
                    journals::created_at.eq(now),
                ))
                .execute(&mut *conn)
                .await?;
            for line in &decision.journal_lines {
                let line_no = i32::try_from(line.line_no).map_err(|_| {
                    RecordRepaymentPersistenceError::NumericOutOfRange("journal line_no")
                })?;
                let amount_yen = line.amount.value();
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
                        public::journal_entries::member_id.eq(line
                            .member_id
                            .as_ref()
                            .map(|id| parse_uuid(id.as_str(), "member_id"))
                            .transpose()?),
                        public::journal_entries::debit_yen.eq(if line.side == Side::Debit {
                            amount_yen
                        } else {
                            0
                        }),
                        public::journal_entries::credit_yen.eq(if line.side == Side::Credit {
                            amount_yen
                        } else {
                            0
                        }),
                        public::journal_entries::memo.eq(line.memo),
                    ))
                    .execute(&mut *conn)
                    .await?;
            }
            diesel::insert_into(public::transfers::table)
                .values((
                    public::transfers::id.eq(transfer_id),
                    public::transfers::household_id.eq(household_id),
                    public::transfers::journal_id.eq(journal_id),
                    public::transfers::from_member_id.eq(borrower_id),
                    public::transfers::to_member_id.eq(lender_id),
                    public::transfers::kind.eq("REPAYMENT"),
                    public::transfers::amount_yen.eq(amount),
                    public::transfers::description.eq(&decision.description),
                    public::transfers::loan_id.eq(Some(loan_id)),
                    public::transfers::created_by.eq(actor_id),
                    public::transfers::created_at.eq(now),
                ))
                .execute(&mut *conn)
                .await?;
            diesel::insert_into(ledger::ledger_entries)
                .values((
                    ledger::id.eq(ledger_entry_id),
                    ledger::household_id.eq(household_id),
                    ledger::journal_id.eq(journal_id),
                    ledger::source_kind.eq("transfer"),
                    ledger::source_id.eq(transfer_id),
                    ledger::occurred_at.eq(input.occurred_at),
                    ledger::description.eq(&decision.description),
                    ledger::amount_yen.eq(amount),
                    ledger::created_at.eq(now),
                ))
                .execute(&mut *conn)
                .await?;
            if let Some(statement) = statement_decision {
                insert_statement_confirmation(
                    &mut *conn,
                    household_id,
                    actor_id,
                    transfer_id,
                    statement,
                )
                .await?;
            }
            for adjustment in &decision.settlement_adjustments {
                diesel::insert_into(public::settlement_adjustments::table)
                    .values((
                        public::settlement_adjustments::id.eq(Uuid::new_v4()),
                        public::settlement_adjustments::household_id.eq(household_id),
                        public::settlement_adjustments::period_start.eq(period_start),
                        public::settlement_adjustments::member_id.eq(parse_uuid(
                            adjustment.member_id.as_str(),
                            "adjustment member_id",
                        )?),
                        public::settlement_adjustments::source_kind.eq("REPAYMENT"),
                        public::settlement_adjustments::source_id.eq(transfer_id),
                        public::settlement_adjustments::amount_yen.eq(adjustment.amount_yen),
                        public::settlement_adjustments::created_at.eq(now),
                    ))
                    .execute(&mut *conn)
                    .await?;
            }
            diesel::insert_into(public::household_events::table)
                .values((
                    public::household_events::household_id.eq(household_id),
                    public::household_events::event_type.eq(decision.event.event_type),
                    public::household_events::entity_kind.eq("ledger"),
                    public::household_events::entity_id.eq(ledger_entry_id),
                    public::household_events::revision.eq(1_i64),
                    public::household_events::changed_fields.eq(decision
                        .event
                        .changed_fields
                        .iter()
                        .map(|field| Some((*field).to_owned()))
                        .collect::<Vec<_>>()),
                    public::household_events::actor_id.eq(actor_id),
                    public::household_events::created_at.eq(now),
                ))
                .execute(&mut *conn)
                .await?;
            Ok(PersistedRepayment {
                transfer_id: transfer_id.to_string(),
                journal_id: journal_id.to_string(),
                ledger_entry_id: ledger_entry_id.to_string(),
            })
        })
        .await
}

pub async fn find_record_repayment_replay(
    pool: &PgPool,
    input: &FindRecordRepaymentReplay,
) -> Result<Option<String>, RecordRepaymentPersistenceError> {
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let from_member_id = parse_uuid(&input.from_member_id, "from_member_id")?;
    let to_member_id = parse_uuid(&input.to_member_id, "to_member_id")?;
    if input.idempotency_key.is_empty() || input.idempotency_key.len() > 200 {
        return Err(RecordRepaymentPersistenceError::InvalidIdempotencyKey);
    }
    validate_statement_input(&PersistRecordRepayment {
        household_id: input.household_id.clone(),
        actor_id: input.actor_id.clone(),
        occurred_at: Utc::now(),
        idempotency_key: input.idempotency_key.clone(),
        request_fingerprint: input.request_fingerprint,
        source_document_id: input.source_document_id.clone(),
        source_reference: input.source_reference.clone(),
        source_account_id: input.source_account_id.clone(),
    })?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| RecordRepaymentPersistenceError::Pool(error.to_string()))?;
    for member_id in [actor_id, from_member_id, to_member_id] {
        public::household_members::table
            .filter(public::household_members::household_id.eq(household_id))
            .filter(public::household_members::member_id.eq(member_id))
            .filter(public::household_members::left_at.is_null())
            .select(public::household_members::member_id)
            .first::<Uuid>(&mut *conn)
            .await
            .optional()?
            .ok_or(RecordRepaymentPersistenceError::MemberNotFound)?;
    }
    let existing = public::idempotency_records::table
        .filter(public::idempotency_records::household_id.eq(household_id))
        .filter(public::idempotency_records::operation.eq(OPERATION))
        .filter(public::idempotency_records::key.eq(&input.idempotency_key))
        .select((
            public::idempotency_records::request_fingerprint,
            public::idempotency_records::resource_kind,
            public::idempotency_records::resource_id,
        ))
        .first::<(Vec<u8>, String, Uuid)>(&mut *conn)
        .await
        .optional()?;
    let Some((fingerprint, resource_kind, resource_id)) = existing else {
        return Ok(None);
    };
    if fingerprint.as_slice() != input.request_fingerprint || resource_kind != "ledger_entry" {
        return Err(RecordRepaymentPersistenceError::IdempotencyConflict);
    }
    let transfer_id = public::ledger_entries::table
        .filter(public::ledger_entries::household_id.eq(household_id))
        .filter(public::ledger_entries::id.eq(resource_id))
        .filter(public::ledger_entries::source_kind.eq("transfer"))
        .select(public::ledger_entries::source_id)
        .first::<Uuid>(&mut *conn)
        .await?;
    let replay_input = PersistRecordRepayment {
        household_id: input.household_id.clone(),
        actor_id: input.actor_id.clone(),
        occurred_at: Utc::now(),
        idempotency_key: input.idempotency_key.clone(),
        request_fingerprint: input.request_fingerprint,
        source_document_id: input.source_document_id.clone(),
        source_reference: input.source_reference.clone(),
        source_account_id: input.source_account_id.clone(),
    };
    validate_statement_replay(&mut conn, household_id, transfer_id, &replay_input).await?;
    Ok(Some(resource_id.to_string()))
}

struct StatementConfirmation {
    source_document_id: Uuid,
    source_reference: String,
}

async fn validate_active_members(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
    decision: &RecordRepaymentDecision,
) -> Result<(), RecordRepaymentPersistenceError> {
    let lender = decision
        .journal_lines
        .as_slice()
        .first()
        .and_then(|line| line.member_id.as_ref())
        .map(|id| parse_uuid(id.as_str(), "lender_id"))
        .transpose()?
        .ok_or(RecordRepaymentPersistenceError::InvalidDecision)?;
    let borrower = decision
        .journal_lines
        .as_slice()
        .get(1)
        .and_then(|line| line.member_id.as_ref())
        .map(|id| parse_uuid(id.as_str(), "borrower_id"))
        .transpose()?
        .ok_or(RecordRepaymentPersistenceError::InvalidDecision)?;
    for member_id in [actor_id, borrower, lender] {
        public::household_members::table
            .filter(public::household_members::household_id.eq(household_id))
            .filter(public::household_members::member_id.eq(member_id))
            .filter(public::household_members::left_at.is_null())
            .select(public::household_members::member_id)
            .first::<Uuid>(conn)
            .await
            .optional()?
            .ok_or(RecordRepaymentPersistenceError::MemberNotFound)?;
    }
    Ok(())
}

fn validate_statement_input(
    input: &PersistRecordRepayment,
) -> Result<(), RecordRepaymentPersistenceError> {
    let provided = input.source_document_id.is_some()
        || input.source_reference.is_some()
        || input.source_account_id.is_some();
    if !provided {
        return Ok(());
    }
    if input
        .source_document_id
        .as_deref()
        .is_none_or(str::is_empty)
        || input.source_reference.as_deref().is_none_or(str::is_empty)
        || input
            .source_reference
            .as_ref()
            .is_some_and(|value| value.len() > 256)
        || input.source_account_id.as_deref().is_none_or(str::is_empty)
    {
        return Err(RecordRepaymentPersistenceError::InvalidDecision);
    }
    Ok(())
}

async fn prepare_statement_transfer(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
    ledger_entry_id: Uuid,
    occurred_at: DateTime<Utc>,
    amount: i64,
    decision: &RecordRepaymentDecision,
    input: &PersistRecordRepayment,
) -> Result<Option<StatementConfirmation>, RecordRepaymentPersistenceError> {
    let (Some(document_id), Some(reference), Some(source_account_id)) = (
        input.source_document_id.as_deref(),
        input.source_reference.as_deref(),
        input.source_account_id.as_deref(),
    ) else {
        return Ok(None);
    };
    let document_id = parse_uuid(document_id, "source_document_id")?;
    let source_account_id = parse_uuid(source_account_id, "source_account_id")?;
    let source_kind = public::source_documents::table
        .filter(public::source_documents::household_id.eq(household_id))
        .filter(public::source_documents::id.eq(document_id))
        .for_update()
        .select(public::source_documents::source_kind)
        .first::<String>(conn)
        .await
        .optional()?
        .ok_or(RecordRepaymentPersistenceError::SourceDocumentNotFound)?;
    if source_kind != "bank" {
        return Err(RecordRepaymentPersistenceError::SourceIsNotBankStatement);
    }
    let latest_status = public::source_confirmations::table
        .filter(public::source_confirmations::household_id.eq(household_id))
        .filter(public::source_confirmations::source_document_id.eq(document_id))
        .filter(public::source_confirmations::source_reference.eq(reference))
        .order_by(public::source_confirmations::changed_at.desc())
        .select(public::source_confirmations::status)
        .first::<String>(conn)
        .await
        .optional()?;
    if latest_status.as_deref() == Some("confirmed") {
        return Err(RecordRepaymentPersistenceError::StatementAlreadyConfirmed);
    }
    let result = public::extraction_jobs::table
        .filter(public::extraction_jobs::household_id.eq(household_id))
        .filter(public::extraction_jobs::source_document_id.eq(document_id))
        .filter(public::extraction_jobs::status.eq("succeeded"))
        .order_by(public::extraction_jobs::version.desc())
        .select(public::extraction_jobs::result)
        .first::<Option<serde_json::Value>>(conn)
        .await
        .optional()?
        .flatten()
        .ok_or(RecordRepaymentPersistenceError::StatementExtractionUnavailable)?;
    let rows = result
        .get("statements")
        .and_then(serde_json::Value::as_array)
        .ok_or(RecordRepaymentPersistenceError::StatementExtractionUnavailable)?;
    let row = rows.iter().find(|row| {
        let row_no = row.get("row").and_then(serde_json::Value::as_i64);
        let row_reference = row
            .get("sourceReference")
            .and_then(serde_json::Value::as_str)
            .filter(|value| !value.is_empty())
            .map(str::to_owned)
            .or_else(|| row_no.map(|number| format!("row:{number}")));
        row_reference.as_deref() == Some(reference)
    });
    let row = row.ok_or(RecordRepaymentPersistenceError::StatementReferenceNotFound)?;
    let kind = row
        .get("kind")
        .and_then(serde_json::Value::as_str)
        .ok_or(RecordRepaymentPersistenceError::StatementExtractionUnavailable)?;
    let amount_yen = row
        .get("amount")
        .and_then(serde_json::Value::as_i64)
        .ok_or(RecordRepaymentPersistenceError::StatementExtractionUnavailable)?;
    let statement_occurred_at = row
        .get("occurredAt")
        .and_then(serde_json::Value::as_str)
        .and_then(|value| DateTime::parse_from_rfc3339(value).ok())
        .map(|value| value.with_timezone(&Utc))
        .ok_or(RecordRepaymentPersistenceError::StatementExtractionUnavailable)?;
    let source_account_kind = public::accounts::table
        .filter(public::accounts::household_id.eq(household_id))
        .filter(public::accounts::id.eq(source_account_id))
        .select(public::accounts::kind)
        .first::<String>(conn)
        .await
        .optional()?
        .ok_or(RecordRepaymentPersistenceError::AccountNotFound)?;
    if source_account_kind != "BANK" {
        return Err(RecordRepaymentPersistenceError::InvalidAccountKind);
    }
    let source_account = decision
        .journal_lines
        .iter()
        .find(|line| line.memo == "Transfer source account")
        .and_then(|line| line.account_id.as_ref())
        .map(|id| id.as_str());
    let destination_account = decision
        .journal_lines
        .iter()
        .find(|line| line.memo == "Transfer destination account")
        .and_then(|line| line.account_id.as_ref())
        .map(|id| id.as_str());
    let command = LinkStatementTransferCommand {
        actor_id: actor_id.to_string(),
        household_id: household_id.to_string(),
        ledger_entry_id: ledger_entry_id.to_string(),
        source_document_id: document_id.to_string(),
        source_reference: reference.to_owned(),
        source_account_id: source_account_id.to_string(),
    };
    decide_link_statement_transfer(
        command,
        StatementTransferObservation {
            source_kind,
            kind: kind.to_owned(),
            amount_yen,
            occurred_at_unix_micros: statement_occurred_at.timestamp_micros(),
        },
        "REPAYMENT",
        amount,
        occurred_at.timestamp_micros(),
        source_account,
        destination_account,
    )
    .map_err(|_| RecordRepaymentPersistenceError::StatementDoesNotMatchTransfer)?;
    Ok(Some(StatementConfirmation {
        source_document_id: document_id,
        source_reference: reference.to_owned(),
    }))
}

async fn validate_statement_replay(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    transfer_id: Uuid,
    input: &PersistRecordRepayment,
) -> Result<(), RecordRepaymentPersistenceError> {
    let (Some(document_id), Some(reference), Some(_)) = (
        input.source_document_id.as_deref(),
        input.source_reference.as_deref(),
        input.source_account_id.as_deref(),
    ) else {
        return Ok(());
    };
    let document_id = parse_uuid(document_id, "source_document_id")?;
    let confirmation = public::source_confirmations::table
        .filter(public::source_confirmations::household_id.eq(household_id))
        .filter(public::source_confirmations::source_document_id.eq(document_id))
        .filter(public::source_confirmations::source_reference.eq(reference))
        .order_by(public::source_confirmations::changed_at.desc())
        .select((
            public::source_confirmations::status,
            public::source_confirmations::entity_kind,
            public::source_confirmations::entity_id,
        ))
        .first::<(String, String, Uuid)>(conn)
        .await
        .optional()?;
    if confirmation != Some(("confirmed".to_owned(), "transfer".to_owned(), transfer_id)) {
        return Err(RecordRepaymentPersistenceError::IdempotencyConflict);
    }
    Ok(())
}

async fn insert_statement_confirmation(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
    transfer_id: Uuid,
    confirmation: StatementConfirmation,
) -> Result<(), RecordRepaymentPersistenceError> {
    diesel::insert_into(public::source_confirmations::table)
        .values((
            public::source_confirmations::id.eq(Uuid::now_v7()),
            public::source_confirmations::household_id.eq(household_id),
            public::source_confirmations::source_document_id.eq(confirmation.source_document_id),
            public::source_confirmations::observation_id.eq(None::<Uuid>),
            public::source_confirmations::source_reference.eq(confirmation.source_reference),
            public::source_confirmations::entity_kind.eq("transfer"),
            public::source_confirmations::entity_id.eq(transfer_id),
            public::source_confirmations::status.eq("confirmed"),
            public::source_confirmations::idempotency_key
                .eq(format!("transfer-confirmation:{}", Uuid::now_v7())),
            public::source_confirmations::changed_by.eq(actor_id),
            public::source_confirmations::changed_at.eq(now),
        ))
        .execute(conn)
        .await?;
    Ok(())
}

async fn ensure_open_period(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    period_start: NaiveDate,
    expected_revision: u64,
) -> Result<(), RecordRepaymentPersistenceError> {
    diesel::insert_into(public::accounting_periods::table)
        .values((
            public::accounting_periods::household_id.eq(household_id),
            public::accounting_periods::period_start.eq(period_start),
            public::accounting_periods::status.eq("OPEN"),
            public::accounting_periods::revision.eq(1_i64),
        ))
        .on_conflict((
            public::accounting_periods::household_id,
            public::accounting_periods::period_start,
        ))
        .do_nothing()
        .execute(&mut *conn)
        .await?;
    let period = public::accounting_periods::table
        .filter(public::accounting_periods::household_id.eq(household_id))
        .filter(public::accounting_periods::period_start.eq(period_start))
        .for_update()
        .select((
            public::accounting_periods::status,
            public::accounting_periods::revision,
        ))
        .first::<(String, i64)>(conn)
        .await
        .optional()?;
    let expected_revision = i64::try_from(expected_revision)
        .map_err(|_| RecordRepaymentPersistenceError::InvalidDecision)?;
    if period
        .as_ref()
        .map(|(status, revision)| (status.as_str(), *revision))
        != Some(("OPEN", expected_revision))
    {
        return Err(RecordRepaymentPersistenceError::AccountingPeriodClosed);
    }
    Ok(())
}

async fn ensure_outstanding(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    loan_id: Uuid,
    decision: &RecordRepaymentDecision,
    amount: i64,
) -> Result<(Uuid, Uuid), RecordRepaymentPersistenceError> {
    use public::transfers::dsl as transfers;
    let (principal, lender, borrower) = transfers::transfers
        .filter(transfers::household_id.eq(household_id))
        .filter(transfers::id.eq(loan_id))
        .filter(transfers::kind.eq("LOAN"))
        .for_update()
        .select((
            transfers::amount_yen,
            transfers::from_member_id,
            transfers::to_member_id,
        ))
        .first::<(i64, Uuid, Uuid)>(conn)
        .await
        .optional()?
        .ok_or(RecordRepaymentPersistenceError::LoanNotFound)?;
    let journal_lender = decision
        .journal_lines
        .get(0)
        .and_then(|line| line.member_id.as_ref())
        .map(|id| parse_uuid(id.as_str(), "lender_id"))
        .transpose()?;
    let journal_borrower = decision
        .journal_lines
        .get(1)
        .and_then(|line| line.member_id.as_ref())
        .map(|id| parse_uuid(id.as_str(), "borrower_id"))
        .transpose()?;
    if decision.journal_lines.len() < 2
        || decision.journal_lines[0].side != Side::Debit
        || decision.journal_lines[1].side != Side::Credit
        || journal_lender != Some(lender)
        || journal_borrower != Some(borrower)
    {
        return Err(RecordRepaymentPersistenceError::LoanDirectionMismatch);
    }
    let repayment_amounts = transfers::transfers
        .filter(transfers::household_id.eq(household_id))
        .filter(transfers::loan_id.eq(Some(loan_id)))
        .filter(transfers::kind.eq("REPAYMENT"))
        .select(transfers::amount_yen)
        .load::<i64>(conn)
        .await?;
    let transfer_repayments =
        repayment_amounts
            .into_iter()
            .try_fold(0_i64, |total, repayment| {
                total
                    .checked_add(repayment)
                    .ok_or(RecordRepaymentPersistenceError::RepaymentExceedsOutstanding)
            })?;
    if transfer_repayments > principal || amount > principal - transfer_repayments {
        return Err(RecordRepaymentPersistenceError::RepaymentExceedsOutstanding);
    }
    Ok((lender, borrower))
}

async fn validate_members_and_accounts(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
    decision: &RecordRepaymentDecision,
) -> Result<(), RecordRepaymentPersistenceError> {
    use public::household_members::dsl as members;
    members::household_members
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .select(members::member_id)
        .first::<Uuid>(conn)
        .await?;
    for line in &decision.journal_lines {
        if let Some(id) = &line.member_id {
            members::household_members
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(parse_uuid(id.as_str(), "member_id")?))
                .select(members::member_id)
                .first::<Uuid>(conn)
                .await?;
        }
        if let Some(id) = &line.account_id {
            public::accounts::table
                .filter(public::accounts::household_id.eq(household_id))
                .filter(public::accounts::id.eq(parse_uuid(id.as_str(), "account_id")?))
                .select(public::accounts::id)
                .first::<Uuid>(conn)
                .await?;
        }
        if let Some(id) = &line.pool_id {
            public::pools::table
                .filter(public::pools::household_id.eq(household_id))
                .filter(public::pools::id.eq(parse_uuid(id.as_str(), "pool_id")?))
                .select(public::pools::id)
                .first::<Uuid>(conn)
                .await?;
        }
    }
    for adjustment in &decision.settlement_adjustments {
        members::household_members
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(parse_uuid(
                adjustment.member_id.as_str(),
                "adjustment member_id",
            )?))
            .select(members::member_id)
            .first::<Uuid>(conn)
            .await?;
    }
    Ok(())
}

fn validate_decision(
    decision: &RecordRepaymentDecision,
) -> Result<(), RecordRepaymentPersistenceError> {
    if decision.journal_lines.len() < 2 {
        return Err(RecordRepaymentPersistenceError::InvalidDecision);
    }
    let mut line_numbers = BTreeSet::new();
    let mut debits = 0_i64;
    let mut credits = 0_i64;
    for line in &decision.journal_lines {
        let line_no = i32::try_from(line.line_no)
            .map_err(|_| RecordRepaymentPersistenceError::NumericOutOfRange("journal line_no"))?;
        if line_no <= 0
            || !line_numbers.insert(line_no)
            || line.amount.value() <= 0
            || line.amount.value() > 1_000_000_000_000
        {
            return Err(RecordRepaymentPersistenceError::InvalidDecision);
        }
        let total = match line.side {
            Side::Debit => &mut debits,
            Side::Credit => &mut credits,
        };
        *total = total
            .checked_add(line.amount.value())
            .ok_or(RecordRepaymentPersistenceError::InvalidDecision)?;
    }
    if debits != credits
        || decision.journal_lines[0].line_no != 1
        || decision.journal_lines[1].line_no != 2
        || decision.journal_lines[0].amount != decision.amount
        || decision.journal_lines[1].amount != decision.amount
        || decision.settlement_adjustments.iter().any(|adjustment| {
            adjustment.amount_yen == 0 || adjustment.amount_yen.unsigned_abs() > 1_000_000_000_000
        })
    {
        return Err(RecordRepaymentPersistenceError::InvalidDecision);
    }
    Ok(())
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, RecordRepaymentPersistenceError> {
    Uuid::parse_str(value).map_err(|_| RecordRepaymentPersistenceError::InvalidUuid(field))
}
