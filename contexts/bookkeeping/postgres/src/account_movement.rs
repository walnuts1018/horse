use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use bookkeeping_application::{
    AccountId, MemberId, Side,
    account_movement::{
        AccountKind, AccountMovementKind, LinkStatementMovementCommand,
        LinkStatementMovementDecision, RecordAccountMovementCommand, RecordAccountMovementDecision,
        StatementMovementObservation, StatementMovementSource, decide_link_statement_movement,
        decide_record_account_movement,
    },
};
use bookkeeping_domain::PositiveYen;

use crate::evidence_port::EvidenceOperations;
use crate::{PgPool, schema::public};

const OPERATION: &str = "recordAccountMovement";
#[derive(Debug, Clone)]
pub struct PersistRecordAccountMovement {
    pub household_id: String,
    pub actor_id: String,
    pub occurred_at: DateTime<Utc>,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedAccountMovement {
    pub id: String,
    pub journal_id: String,
    pub kind: String,
    pub from_account_id: String,
    pub to_account_id: String,
    pub amount_yen: i64,
    pub description: String,
}

#[derive(Debug, Clone)]
pub struct PersistLinkStatementMovement {
    pub household_id: String,
    pub actor_id: String,
    pub account_movement_id: String,
    pub statement_account_id: String,
}

#[derive(Debug)]
pub enum AccountMovementPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidIdempotencyKey,
    IdempotencyConflict,
    InvalidDecision,
    HouseholdNotFound,
    MemberNotFound,
    AccountNotFound,
    AccountingPeriodClosed,
    SourceDocumentNotFound,
    StatementExtractionUnavailable,
    StatementReferenceNotFound,
    StatementAlreadyConfirmed,
    StatementIsNotBankMovement,
    StatementMovementMismatch,
}

impl From<DieselError> for AccountMovementPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_record_account_movement(
    pool: &PgPool,
    input: PersistRecordAccountMovement,
    decision: RecordAccountMovementDecision,
    evidence: &impl EvidenceOperations,
) -> Result<PersistedAccountMovement, AccountMovementPersistenceError> {
    if input.idempotency_key.trim().is_empty() || input.idempotency_key.len() > 200 {
        return Err(AccountMovementPersistenceError::InvalidIdempotencyKey);
    }
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let decision_actor = parse_uuid(decision.actor_id.as_str(), "decision actor_id")?;
    let from_account_id = parse_uuid(decision.from_account_id.as_str(), "from_account_id")?;
    let to_account_id = parse_uuid(decision.to_account_id.as_str(), "to_account_id")?;
    if actor_id != decision_actor
        || decision.occurred_at_unix_micros != input.occurred_at.timestamp_micros()
        || decision.amount.value() <= 0
        || decision.amount.value() > 1_000_000_000_000
        || from_account_id == to_account_id
        || decision.journal_lines[0].line_no != 1
        || decision.journal_lines[0].side != Side::Debit
        || decision.journal_lines[0].account_id != decision.to_account_id
        || decision.journal_lines[1].line_no != 2
        || decision.journal_lines[1].side != Side::Credit
        || decision.journal_lines[1].account_id != decision.from_account_id
        || decision
            .journal_lines
            .iter()
            .any(|line| line.amount != decision.amount)
    {
        return Err(AccountMovementPersistenceError::InvalidDecision);
    }
    let tokyo_offset = FixedOffset::east_opt(9 * 60 * 60)
        .ok_or(AccountMovementPersistenceError::InvalidDecision)?;
    let period_start = input
        .occurred_at
        .with_timezone(&tokyo_offset)
        .date_naive()
        .with_day(1)
        .ok_or(AccountMovementPersistenceError::InvalidDecision)?;
    let movement_id = Uuid::now_v7();
    let journal_id = Uuid::now_v7();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| AccountMovementPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedAccountMovement, AccountMovementPersistenceError, _>(async |conn| {
            use public::account_movements::dsl as movements;
            use public::idempotency_records::dsl as idem;
            use public::journals::dsl as journals;

            let exists = household_postgres::household_access::lock_household_for_update(
                &mut *conn,
                household_id,
            )
            .await?;
            if !exists {
                return Err(AccountMovementPersistenceError::HouseholdNotFound);
            }
            let role = household_postgres::household_access::active_member_role(
                &mut *conn,
                household_id,
                actor_id,
            )
            .await?
            .ok_or(AccountMovementPersistenceError::MemberNotFound)?;
            horse_authorization::authorize(
                Some(&role),
                horse_authorization::Operation::FinancialData,
            )
            .map_err(|_| AccountMovementPersistenceError::MemberNotFound)?;

            let inserted = diesel::insert_into(idem::idempotency_records)
                .values((
                    idem::household_id.eq(household_id),
                    idem::operation.eq(OPERATION),
                    idem::key.eq(&input.idempotency_key),
                    idem::request_fingerprint.eq(input.request_fingerprint.to_vec()),
                    idem::resource_kind.eq("account_movement"),
                    idem::resource_id.eq(movement_id),
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
                    || resource_kind != "account_movement"
                {
                    return Err(AccountMovementPersistenceError::IdempotencyConflict);
                }
                if let Some(source) = &decision.statement_source {
                    require_statement_movement_confirmation(
                        evidence,
                        &mut *conn,
                        household_id,
                        resource_id,
                        source,
                    )
                    .await?;
                }
                return movements::account_movements
                    .filter(movements::household_id.eq(household_id))
                    .filter(movements::id.eq(resource_id))
                    .select((
                        movements::id,
                        movements::journal_id,
                        movements::kind,
                        movements::from_account_id,
                        movements::to_account_id,
                        movements::amount_yen,
                        movements::description,
                    ))
                    .first::<(Uuid, Uuid, String, Uuid, Uuid, i64, String)>(&mut *conn)
                    .await
                    .map(account_movement_result)
                    .map_err(Into::into);
            }

            ensure_open_period(&mut *conn, household_id, period_start).await?;
            let from_kind = account_kind(&mut *conn, household_id, from_account_id).await?;
            let to_kind = account_kind(&mut *conn, household_id, to_account_id).await?;
            if from_kind != decision.from_account_kind.as_str()
                || to_kind != decision.to_account_kind.as_str()
            {
                return Err(AccountMovementPersistenceError::InvalidDecision);
            }
            let statement_document_id = if let Some(source) = &decision.statement_source {
                Some(
                    validate_record_statement_source(
                        evidence,
                        &mut *conn,
                        household_id,
                        &input.occurred_at,
                        movement_id,
                        source,
                        &decision,
                    )
                    .await?,
                )
            } else {
                None
            };

            diesel::insert_into(journals::journals)
                .values((
                    journals::id.eq(journal_id),
                    journals::household_id.eq(household_id),
                    journals::source_kind.eq("ACCOUNT_MOVEMENT"),
                    journals::source_id.eq(movement_id),
                    journals::idempotency_key.eq(&input.idempotency_key),
                    journals::occurred_at.eq(input.occurred_at),
                    journals::created_by.eq(actor_id),
                    journals::created_at.eq(now),
                ))
                .execute(&mut *conn)
                .await?;
            for line in &decision.journal_lines {
                diesel::insert_into(public::journal_entries::table)
                    .values((
                        public::journal_entries::household_id.eq(household_id),
                        public::journal_entries::journal_id.eq(journal_id),
                        public::journal_entries::line_no.eq(i32::try_from(line.line_no)
                            .map_err(|_| AccountMovementPersistenceError::InvalidDecision)?),
                        public::journal_entries::account_id.eq(Some(parse_uuid(
                            line.account_id.as_str(),
                            "journal account_id",
                        )?)),
                        public::journal_entries::pool_id.eq::<Option<Uuid>>(None),
                        public::journal_entries::member_id.eq::<Option<Uuid>>(None),
                        public::journal_entries::debit_yen.eq(if line.side == Side::Debit {
                            line.amount.value()
                        } else {
                            0
                        }),
                        public::journal_entries::credit_yen.eq(if line.side == Side::Credit {
                            line.amount.value()
                        } else {
                            0
                        }),
                        public::journal_entries::memo.eq(line.memo),
                    ))
                    .execute(&mut *conn)
                    .await?;
            }
            let row = diesel::insert_into(movements::account_movements)
                .values((
                    movements::id.eq(movement_id),
                    movements::household_id.eq(household_id),
                    movements::journal_id.eq(journal_id),
                    movements::kind.eq(decision.kind.as_str()),
                    movements::occurred_at.eq(input.occurred_at),
                    movements::from_account_id.eq(from_account_id),
                    movements::to_account_id.eq(to_account_id),
                    movements::amount_yen.eq(decision.amount.value()),
                    movements::description.eq(&decision.description),
                    movements::created_at.eq(now),
                ))
                .returning((
                    movements::id,
                    movements::journal_id,
                    movements::kind,
                    movements::from_account_id,
                    movements::to_account_id,
                    movements::amount_yen,
                    movements::description,
                ))
                .get_result::<(Uuid, Uuid, String, Uuid, Uuid, i64, String)>(&mut *conn)
                .await?;
            if let (Some(source), Some(document_id)) =
                (&decision.statement_source, statement_document_id)
            {
                insert_statement_movement_confirmation(
                    evidence,
                    &mut *conn,
                    household_id,
                    document_id,
                    &source.source_reference,
                    movement_id,
                    actor_id,
                )
                .await?;
            }
            horse_postgres_common::insert_ui_change(
                &mut *conn,
                household_id,
                movement_id,
                1,
                actor_id,
                Utc::now(),
                bookkeeping_application::ui_changes::account_movement_recorded(
                    decision.changed_fields.iter().copied(),
                ),
            )
            .await?;
            Ok(account_movement_result(row))
        })
        .await
}

pub async fn persist_link_statement_movement(
    pool: &PgPool,
    input: PersistLinkStatementMovement,
    decision: LinkStatementMovementDecision,
    evidence: &impl EvidenceOperations,
) -> Result<PersistedAccountMovement, AccountMovementPersistenceError> {
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let movement_id = parse_uuid(&input.account_movement_id, "account_movement_id")?;
    let statement_account_id = parse_uuid(&input.statement_account_id, "statement_account_id")?;
    let document_id = parse_uuid(&decision.source_document_id, "source_document_id")?;
    if decision.movement_id != movement_id.to_string()
        || decision.statement_account_id.as_str() != statement_account_id.to_string()
        || decision.source_reference.is_empty()
        || decision.source_reference.len() > 256
    {
        return Err(AccountMovementPersistenceError::InvalidDecision);
    }
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| AccountMovementPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedAccountMovement, AccountMovementPersistenceError, _>(async |conn| {
            let household_exists = household_postgres::household_access::lock_household_for_update(
                &mut *conn,
                household_id,
            )
            .await?;
            if !household_exists {
                return Err(AccountMovementPersistenceError::HouseholdNotFound);
            }
            let role = household_postgres::household_access::active_member_role(
                &mut *conn,
                household_id,
                actor_id,
            )
            .await?
            .ok_or(AccountMovementPersistenceError::MemberNotFound)?;
            horse_authorization::authorize(
                Some(&role),
                horse_authorization::Operation::FinancialData,
            )
            .map_err(|_| AccountMovementPersistenceError::MemberNotFound)?;
            let source = StatementMovementSource {
                source_document_id: decision.source_document_id.clone(),
                source_reference: decision.source_reference.clone(),
                statement_account_id: decision.statement_account_id.clone(),
            };
            let statement = load_statement_movement_entry(
                evidence,
                &mut *conn,
                household_id,
                document_id,
                &decision.source_reference,
            )
            .await?;
            let statement_account_kind =
                account_kind(&mut *conn, household_id, statement_account_id).await?;
            let statement_account_kind = parse_account_kind(&statement_account_kind)
                .ok_or(AccountMovementPersistenceError::InvalidDecision)?;
            let movement = public::account_movements::table
                .filter(public::account_movements::household_id.eq(household_id))
                .filter(public::account_movements::id.eq(movement_id))
                .for_update()
                .select((
                    public::account_movements::id,
                    public::account_movements::journal_id,
                    public::account_movements::kind,
                    public::account_movements::from_account_id,
                    public::account_movements::to_account_id,
                    public::account_movements::amount_yen,
                    public::account_movements::description,
                    public::account_movements::occurred_at,
                ))
                .first::<(Uuid, Uuid, String, Uuid, Uuid, i64, String, DateTime<Utc>)>(&mut *conn)
                .await
                .optional()?
                .ok_or(AccountMovementPersistenceError::AccountNotFound)?;
            let kind = parse_movement_kind(&movement.2)
                .ok_or(AccountMovementPersistenceError::InvalidDecision)?;
            let amount = PositiveYen::new(movement.5)
                .map_err(|_| AccountMovementPersistenceError::InvalidDecision)?;
            let statement = statement.observation()?;
            let expected = decide_link_statement_movement(LinkStatementMovementCommand {
                source_document_id: source.source_document_id.clone(),
                source_reference: source.source_reference.clone(),
                statement_account_id: source.statement_account_id.clone(),
                statement_account_kind,
                movement_id: movement.0.to_string(),
                from_account_id: AccountId::new(movement.3.to_string())
                    .map_err(|_| AccountMovementPersistenceError::InvalidDecision)?,
                to_account_id: AccountId::new(movement.4.to_string())
                    .map_err(|_| AccountMovementPersistenceError::InvalidDecision)?,
                kind,
                amount,
                occurred_at_unix_micros: movement.7.timestamp_micros(),
                statement,
            })
            .map_err(map_statement_workflow_error)?;
            if expected != decision {
                return Err(AccountMovementPersistenceError::InvalidDecision);
            }
            insert_statement_movement_confirmation(
                evidence,
                &mut *conn,
                household_id,
                document_id,
                &decision.source_reference,
                movement_id,
                actor_id,
            )
            .await?;
            Ok(account_movement_result((
                movement.0, movement.1, movement.2, movement.3, movement.4, movement.5, movement.6,
            )))
        })
        .await
}

#[derive(Debug)]
struct StatementMovementEntry {
    kind: String,
    occurred_at: String,
    amount_yen: i64,
    has_region: bool,
}

impl StatementMovementEntry {
    fn observation(&self) -> Result<StatementMovementObservation, AccountMovementPersistenceError> {
        let occurred_at = DateTime::parse_from_rfc3339(&self.occurred_at)
            .map_err(|_| AccountMovementPersistenceError::StatementMovementMismatch)?
            .with_timezone(&Utc);
        Ok(StatementMovementObservation {
            bank_statement: true,
            has_region: self.has_region,
            kind: self.kind.clone(),
            amount_yen: self.amount_yen,
            occurred_at_unix_micros: occurred_at.timestamp_micros(),
        })
    }
}

async fn validate_record_statement_source<E: EvidenceOperations + ?Sized>(
    evidence: &E,
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    occurred_at: &DateTime<Utc>,
    movement_id: Uuid,
    source: &StatementMovementSource,
    decision: &RecordAccountMovementDecision,
) -> Result<Uuid, AccountMovementPersistenceError> {
    let document_id = parse_uuid(&source.source_document_id, "source_document_id")?;
    let entry = load_statement_movement_entry(
        evidence,
        conn,
        household_id,
        document_id,
        &source.source_reference,
    )
    .await?;
    let statement = entry.observation()?;
    let amount = PositiveYen::new(decision.amount.value())
        .map_err(|_| AccountMovementPersistenceError::InvalidDecision)?;
    let recalculated = decide_record_account_movement(RecordAccountMovementCommand {
        actor_id: MemberId::new(decision.actor_id.as_str())
            .map_err(|_| AccountMovementPersistenceError::InvalidDecision)?,
        from_account_id: AccountId::new(decision.from_account_id.as_str())
            .map_err(|_| AccountMovementPersistenceError::InvalidDecision)?,
        from_account_kind: decision.from_account_kind,
        to_account_id: AccountId::new(decision.to_account_id.as_str())
            .map_err(|_| AccountMovementPersistenceError::InvalidDecision)?,
        to_account_kind: decision.to_account_kind,
        kind: decision.kind,
        occurred_at_unix_micros: occurred_at.timestamp_micros(),
        amount,
        description: decision.description.clone(),
        accounting_period: decision.accounting_period.clone(),
        statement_source: Some(source.clone()),
    })
    .map_err(map_statement_workflow_error)?;
    if &recalculated != decision {
        return Err(AccountMovementPersistenceError::InvalidDecision);
    }
    let statement_account_id =
        parse_uuid(source.statement_account_id.as_str(), "statement_account_id")?;
    let statement_account_kind =
        parse_account_kind(&account_kind(conn, household_id, statement_account_id).await?)
            .ok_or(AccountMovementPersistenceError::InvalidDecision)?;
    decide_link_statement_movement(LinkStatementMovementCommand {
        source_document_id: source.source_document_id.clone(),
        source_reference: source.source_reference.clone(),
        statement_account_id: source.statement_account_id.clone(),
        statement_account_kind,
        movement_id: movement_id.to_string(),
        from_account_id: decision.from_account_id.clone(),
        to_account_id: decision.to_account_id.clone(),
        kind: decision.kind,
        amount: decision.amount,
        occurred_at_unix_micros: decision.occurred_at_unix_micros,
        statement,
    })
    .map_err(map_statement_workflow_error)?;
    Ok(document_id)
}

async fn load_statement_movement_entry<E: EvidenceOperations + ?Sized>(
    evidence: &E,
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    document_id: Uuid,
    source_reference: &str,
) -> Result<StatementMovementEntry, AccountMovementPersistenceError> {
    if source_reference.is_empty() || source_reference.len() > 256 {
        return Err(AccountMovementPersistenceError::InvalidDecision);
    }
    let source_kind = evidence
        .source_kind(
            conn,
            crate::evidence_port::SourceDocumentQuery {
                household_id,
                document_id,
                lock: true,
            },
        )
        .await?
        .ok_or(AccountMovementPersistenceError::SourceDocumentNotFound)?;
    if source_kind != "bank" {
        return Err(AccountMovementPersistenceError::StatementIsNotBankMovement);
    }
    let has_confirmation = evidence
        .latest_confirmation_status(
            conn,
            crate::evidence_port::SourceReferenceQuery {
                household_id,
                document_id,
                source_reference,
            },
        )
        .await?;
    if has_confirmation.as_deref() == Some("confirmed") {
        return Err(AccountMovementPersistenceError::StatementAlreadyConfirmed);
    }
    let result = evidence
        .latest_extraction(
            conn,
            crate::evidence_port::ExtractionQuery {
                household_id,
                document_id,
            },
        )
        .await?
        .ok_or(AccountMovementPersistenceError::StatementExtractionUnavailable)?;
    let statements = result
        .get("statements")
        .and_then(serde_json::Value::as_array)
        .ok_or(AccountMovementPersistenceError::StatementExtractionUnavailable)?;
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
        return Ok(StatementMovementEntry {
            kind: statement
                .get("kind")
                .and_then(serde_json::Value::as_str)
                .ok_or(AccountMovementPersistenceError::StatementExtractionUnavailable)?
                .to_owned(),
            occurred_at: statement
                .get("occurredAt")
                .and_then(serde_json::Value::as_str)
                .ok_or(AccountMovementPersistenceError::StatementExtractionUnavailable)?
                .to_owned(),
            amount_yen: statement
                .get("amount")
                .and_then(serde_json::Value::as_i64)
                .ok_or(AccountMovementPersistenceError::StatementExtractionUnavailable)?,
            has_region: statement
                .get("region")
                .is_some_and(|region| !region.is_null()),
        });
    }
    Err(AccountMovementPersistenceError::StatementReferenceNotFound)
}

async fn require_statement_movement_confirmation<E: EvidenceOperations + ?Sized>(
    evidence: &E,
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    movement_id: Uuid,
    source: &StatementMovementSource,
) -> Result<(), AccountMovementPersistenceError> {
    let document_id = parse_uuid(&source.source_document_id, "source_document_id")?;
    let confirmation = evidence
        .latest_confirmation(
            conn,
            crate::evidence_port::SourceReferenceQuery {
                household_id,
                document_id,
                source_reference: &source.source_reference,
            },
        )
        .await?;
    if confirmation.is_some_and(|confirmation| {
        confirmation.entity_kind == "account_movement"
            && confirmation.entity_id == movement_id
            && confirmation.status == "confirmed"
    }) {
        Ok(())
    } else {
        Err(AccountMovementPersistenceError::StatementAlreadyConfirmed)
    }
}

async fn insert_statement_movement_confirmation<E: EvidenceOperations + ?Sized>(
    evidence: &E,
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    document_id: Uuid,
    source_reference: &str,
    movement_id: Uuid,
    actor_id: Uuid,
) -> Result<(), AccountMovementPersistenceError> {
    evidence
        .insert_confirmation(
            conn,
            crate::evidence_port::InsertConfirmation {
                household_id,
                document_id,
                source_reference: source_reference.to_owned(),
                entity_kind: "account_movement".to_owned(),
                entity_id: movement_id,
                changed_by: actor_id,
                idempotency_key: format!("account-movement-confirmation:{}", Uuid::now_v7()),
                changed_at: None,
            },
        )
        .await
        .map_err(AccountMovementPersistenceError::from)
}

fn parse_movement_kind(value: &str) -> Option<AccountMovementKind> {
    match value {
        "TRANSFER" => Some(AccountMovementKind::Transfer),
        "CARD_BILL_PAYMENT" => Some(AccountMovementKind::CardBillPayment),
        _ => None,
    }
}

fn parse_account_kind(value: &str) -> Option<AccountKind> {
    match value {
        "BANK" => Some(AccountKind::Bank),
        "CASH" => Some(AccountKind::Cash),
        "CARD" => Some(AccountKind::Card),
        "OTHER" => Some(AccountKind::Other),
        _ => None,
    }
}

fn map_statement_workflow_error(
    error: bookkeeping_application::account_movement::AccountMovementError,
) -> AccountMovementPersistenceError {
    use bookkeeping_application::account_movement::AccountMovementError as WorkflowError;

    match error {
        WorkflowError::StatementNotBankMovement => {
            AccountMovementPersistenceError::StatementIsNotBankMovement
        }
        WorkflowError::StatementMovementMismatch | WorkflowError::StatementAccountMismatch => {
            AccountMovementPersistenceError::StatementMovementMismatch
        }
        _ => AccountMovementPersistenceError::InvalidDecision,
    }
}

async fn ensure_open_period(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    period_start: NaiveDate,
) -> Result<(), AccountMovementPersistenceError> {
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
    let status = public::accounting_periods::table
        .filter(public::accounting_periods::household_id.eq(household_id))
        .filter(public::accounting_periods::period_start.eq(period_start))
        .for_update()
        .select(public::accounting_periods::status)
        .first::<String>(&mut *conn)
        .await?;
    if status != "OPEN" {
        return Err(AccountMovementPersistenceError::AccountingPeriodClosed);
    }
    Ok(())
}

async fn account_kind(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    account_id: Uuid,
) -> Result<String, AccountMovementPersistenceError> {
    public::accounts::table
        .filter(public::accounts::household_id.eq(household_id))
        .filter(public::accounts::id.eq(account_id))
        .for_share()
        .select(public::accounts::kind)
        .first::<String>(conn)
        .await
        .optional()?
        .ok_or(AccountMovementPersistenceError::AccountNotFound)
}

fn account_movement_result(
    row: (Uuid, Uuid, String, Uuid, Uuid, i64, String),
) -> PersistedAccountMovement {
    PersistedAccountMovement {
        id: row.0.to_string(),
        journal_id: row.1.to_string(),
        kind: row.2,
        from_account_id: row.3.to_string(),
        to_account_id: row.4.to_string(),
        amount_yen: row.5,
        description: row.6,
    }
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, AccountMovementPersistenceError> {
    Uuid::parse_str(value).map_err(|_| AccountMovementPersistenceError::InvalidUuid(field))
}
