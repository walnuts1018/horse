use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, SecondsFormat, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use bookkeeping_application::{
    Side,
    account_income::{
        AccountIncomeError, LinkStatementIncomeCommand, LinkStatementIncomeDecision,
        RecordAccountIncomeDecision, StatementIncomeSource, decide_link_statement_income,
    },
};

use crate::evidence_port::{
    EvidenceOperations, InsertConfirmation, SourceDocumentQuery, SourceReferenceQuery,
};
use crate::{PgPool, schema::public};

const OPERATION: &str = "recordExternalIncome";
#[derive(Debug, Clone)]
pub struct PersistRecordAccountIncome {
    pub household_id: String,
    pub actor_id: String,
    pub occurred_at: DateTime<Utc>,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedAccountIncome {
    pub id: String,
    pub journal_id: String,
    pub account_id: String,
    pub occurred_at: DateTime<Utc>,
    pub amount_yen: i64,
    pub description: String,
}

#[derive(Debug)]
pub enum AccountIncomePersistenceError {
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
    StatementIsNotExternalIncome,
    StatementIncomeMismatch,
}

impl From<DieselError> for AccountIncomePersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_record_account_income(
    pool: &PgPool,
    input: PersistRecordAccountIncome,
    decision: RecordAccountIncomeDecision,
    evidence: &impl EvidenceOperations,
) -> Result<PersistedAccountIncome, AccountIncomePersistenceError> {
    if input.idempotency_key.trim().is_empty() || input.idempotency_key.len() > 200 {
        return Err(AccountIncomePersistenceError::InvalidIdempotencyKey);
    }
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let decision_actor = parse_uuid(decision.actor_id.as_str(), "decision actor_id")?;
    let account_id = parse_uuid(decision.account_id.as_str(), "account_id")?;
    if actor_id != decision_actor
        || decision.amount.value() <= 0
        || decision.amount.value() > 1_000_000_000_000
        || decision.journal_lines[0].line_no != 1
        || decision.journal_lines[0].side != Side::Debit
        || decision.journal_lines[0].account_id.as_ref() != Some(&decision.account_id)
        || decision.journal_lines[1].line_no != 2
        || decision.journal_lines[1].side != Side::Credit
        || decision.journal_lines[1].account_id.is_some()
        || decision
            .journal_lines
            .iter()
            .any(|line| line.amount != decision.amount)
    {
        return Err(AccountIncomePersistenceError::InvalidDecision);
    }
    let tokyo_offset =
        FixedOffset::east_opt(9 * 60 * 60).ok_or(AccountIncomePersistenceError::InvalidDecision)?;
    let period_start = input
        .occurred_at
        .with_timezone(&tokyo_offset)
        .date_naive()
        .with_day(1)
        .ok_or(AccountIncomePersistenceError::InvalidDecision)?;
    let income_id = Uuid::now_v7();
    let journal_id = Uuid::now_v7();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| AccountIncomePersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedAccountIncome, AccountIncomePersistenceError, _>(async |conn| {
            use public::account_incomes::dsl as incomes;
            use public::idempotency_records::dsl as idem;
            use public::journals::dsl as journals;

            let exists = household_postgres::household_access::lock_household_for_update(
                &mut *conn,
                household_id,
            )
            .await?;
            if !exists {
                return Err(AccountIncomePersistenceError::HouseholdNotFound);
            }
            let role = household_postgres::household_access::active_member_role(
                &mut *conn,
                household_id,
                actor_id,
            )
            .await?
            .ok_or(AccountIncomePersistenceError::MemberNotFound)?;
            horse_authorization::authorize(
                Some(&role),
                horse_authorization::Operation::FinancialData,
            )
            .map_err(|_| AccountIncomePersistenceError::MemberNotFound)?;

            let inserted = diesel::insert_into(idem::idempotency_records)
                .values((
                    idem::household_id.eq(household_id),
                    idem::operation.eq(OPERATION),
                    idem::key.eq(&input.idempotency_key),
                    idem::request_fingerprint.eq(input.request_fingerprint.to_vec()),
                    idem::resource_kind.eq("account_income"),
                    idem::resource_id.eq(income_id),
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
                    || resource_kind != "account_income"
                {
                    return Err(AccountIncomePersistenceError::IdempotencyConflict);
                }
                let row = incomes::account_incomes
                    .filter(incomes::household_id.eq(household_id))
                    .filter(incomes::id.eq(resource_id))
                    .select((
                        incomes::id,
                        incomes::journal_id,
                        incomes::account_id,
                        incomes::occurred_at,
                        incomes::amount_yen,
                        incomes::description,
                    ))
                    .first::<(Uuid, Uuid, Uuid, DateTime<Utc>, i64, String)>(&mut *conn)
                    .await?;
                if let Some(source) = &decision.statement_source {
                    require_statement_income_confirmation(
                        evidence,
                        &mut *conn,
                        household_id,
                        resource_id,
                        source,
                    )
                    .await?;
                }
                return Ok(account_income_result(row));
            }

            ensure_open_period(&mut *conn, household_id, period_start).await?;
            let account_kind = public::accounts::table
                .filter(public::accounts::household_id.eq(household_id))
                .filter(public::accounts::id.eq(account_id))
                .for_share()
                .select(public::accounts::kind)
                .first::<String>(&mut *conn)
                .await
                .optional()?
                .ok_or(AccountIncomePersistenceError::AccountNotFound)?;
            if account_kind != decision.account_kind.as_str() || account_kind == "CARD" {
                return Err(AccountIncomePersistenceError::InvalidDecision);
            }
            let statement_document_id = if let Some(source) = &decision.statement_source {
                let statement_document_id = validate_statement_income_source(
                    evidence,
                    &mut *conn,
                    ValidateStatementIncomeSource {
                        household_id,
                        source_document_id: &source.source_document_id,
                        source_reference: &source.source_reference,
                        account_id,
                        amount: decision.amount.value(),
                        occurred_at: input.occurred_at,
                    },
                )
                .await?;
                if source.statement_account_id.as_str() != decision.account_id.as_str() {
                    return Err(AccountIncomePersistenceError::InvalidDecision);
                }
                Some((statement_document_id, source.source_reference.as_str()))
            } else {
                None
            };

            diesel::insert_into(journals::journals)
                .values((
                    journals::id.eq(journal_id),
                    journals::household_id.eq(household_id),
                    journals::source_kind.eq("ACCOUNT_INCOME"),
                    journals::source_id.eq(income_id),
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
                            .map_err(|_| AccountIncomePersistenceError::InvalidDecision)?),
                        public::journal_entries::account_id.eq(line
                            .account_id
                            .as_ref()
                            .map(|id| parse_uuid(id.as_str(), "journal account_id"))
                            .transpose()?),
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
            let row = diesel::insert_into(incomes::account_incomes)
                .values((
                    incomes::id.eq(income_id),
                    incomes::household_id.eq(household_id),
                    incomes::journal_id.eq(journal_id),
                    incomes::account_id.eq(account_id),
                    incomes::occurred_at.eq(input.occurred_at),
                    incomes::amount_yen.eq(decision.amount.value()),
                    incomes::description.eq(&decision.description),
                    incomes::created_at.eq(now),
                ))
                .returning((
                    incomes::id,
                    incomes::journal_id,
                    incomes::account_id,
                    incomes::occurred_at,
                    incomes::amount_yen,
                    incomes::description,
                ))
                .get_result::<(Uuid, Uuid, Uuid, DateTime<Utc>, i64, String)>(&mut *conn)
                .await?;
            if let Some((source_document_id, source_reference)) = statement_document_id {
                insert_statement_income_confirmation(
                    evidence,
                    &mut *conn,
                    household_id,
                    source_document_id,
                    source_reference,
                    income_id,
                    actor_id,
                )
                .await?;
            }
            horse_postgres_common::insert_ui_change(
                &mut *conn,
                household_id,
                income_id,
                1,
                actor_id,
                Utc::now(),
                bookkeeping_application::ui_changes::account_income_recorded(
                    decision.changed_fields.iter().copied(),
                ),
            )
            .await?;
            Ok(account_income_result(row))
        })
        .await
}

#[derive(Debug, Clone)]
pub struct PersistLinkStatementIncome {
    pub household_id: String,
    pub actor_id: String,
    pub account_income_id: String,
    pub statement_account_id: String,
}

pub async fn persist_link_statement_income(
    pool: &PgPool,
    input: PersistLinkStatementIncome,
    decision: LinkStatementIncomeDecision,
    evidence: &impl EvidenceOperations,
) -> Result<PersistedAccountIncome, AccountIncomePersistenceError> {
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let income_id = parse_uuid(&input.account_income_id, "account_income_id")?;
    let statement_account_id = parse_uuid(&input.statement_account_id, "statement_account_id")?;
    let document_id = parse_uuid(&decision.source_document_id, "source_document_id")?;
    let decision_income_account_id =
        parse_uuid(decision.income_account_id.as_str(), "income_account_id")?;
    let decision_statement_account_id = parse_uuid(
        decision.statement_account_id.as_str(),
        "statement_account_id",
    )?;
    if statement_account_id != decision_statement_account_id
        || decision_income_account_id != statement_account_id
        || decision.source_reference.is_empty()
        || decision.source_reference.len() > 256
    {
        return Err(AccountIncomePersistenceError::InvalidDecision);
    }
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| AccountIncomePersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedAccountIncome, AccountIncomePersistenceError, _>(async |conn| {
            let household_exists = household_postgres::household_access::lock_household_for_update(
                &mut *conn,
                household_id,
            )
            .await?;
            if !household_exists {
                return Err(AccountIncomePersistenceError::HouseholdNotFound);
            }
            let role = household_postgres::household_access::active_member_role(
                &mut *conn,
                household_id,
                actor_id,
            )
            .await?
            .ok_or(AccountIncomePersistenceError::MemberNotFound)?;
            horse_authorization::authorize(
                Some(&role),
                horse_authorization::Operation::FinancialData,
            )
            .map_err(|_| AccountIncomePersistenceError::MemberNotFound)?;
            let account_kind = public::accounts::table
                .filter(public::accounts::household_id.eq(household_id))
                .filter(public::accounts::id.eq(statement_account_id))
                .for_share()
                .select(public::accounts::kind)
                .first::<String>(&mut *conn)
                .await
                .optional()?
                .ok_or(AccountIncomePersistenceError::AccountNotFound)?;
            if account_kind != "BANK" {
                return Err(AccountIncomePersistenceError::InvalidDecision);
            }
            let row = public::account_incomes::table
                .filter(public::account_incomes::household_id.eq(household_id))
                .filter(public::account_incomes::id.eq(income_id))
                .for_update()
                .select((
                    public::account_incomes::id,
                    public::account_incomes::journal_id,
                    public::account_incomes::account_id,
                    public::account_incomes::occurred_at,
                    public::account_incomes::amount_yen,
                    public::account_incomes::description,
                ))
                .first::<(Uuid, Uuid, Uuid, DateTime<Utc>, i64, String)>(&mut *conn)
                .await
                .optional()?
                .ok_or(AccountIncomePersistenceError::AccountNotFound)?;
            if row.2 != statement_account_id {
                return Err(AccountIncomePersistenceError::InvalidDecision);
            }
            let entry = load_statement_income_entry(
                evidence,
                &mut *conn,
                SourceReferenceQuery {
                    household_id,
                    document_id,
                    source_reference: &decision.source_reference,
                },
            )
            .await?;
            let statement_occurred_at = DateTime::parse_from_rfc3339(&entry.occurred_at)
                .map_err(|_| AccountIncomePersistenceError::StatementIncomeMismatch)?
                .with_timezone(&Utc);
            let workflow_decision = decide_link_statement_income(LinkStatementIncomeCommand {
                source_document_id: decision.source_document_id.clone(),
                source_reference: decision.source_reference.clone(),
                statement_account_id: decision.statement_account_id.clone(),
                income_account_id: decision.income_account_id.clone(),
                income_amount: bookkeeping_domain::PositiveYen::new(row.4)
                    .map_err(|_| AccountIncomePersistenceError::InvalidDecision)?,
                statement_amount: entry.amount,
                income_occurred_at: row.3.to_rfc3339_opts(SecondsFormat::AutoSi, true),
                statement_occurred_at: statement_occurred_at
                    .to_rfc3339_opts(SecondsFormat::AutoSi, true),
                statement_kind: entry.kind,
                bank_statement: true,
                statement_has_region: entry.has_region,
            })
            .map_err(map_link_workflow_error)?;
            insert_statement_income_confirmation(
                evidence,
                &mut *conn,
                household_id,
                document_id,
                &workflow_decision.source_reference,
                income_id,
                actor_id,
            )
            .await?;
            Ok(account_income_result(row))
        })
        .await
}

#[derive(Debug)]
struct StatementIncomeEntry {
    kind: String,
    occurred_at: String,
    amount: i64,
    has_region: bool,
}

async fn validate_statement_income_source<E: EvidenceOperations + ?Sized>(
    evidence: &E,
    conn: &mut diesel_async::AsyncPgConnection,
    input: ValidateStatementIncomeSource<'_>,
) -> Result<Uuid, AccountIncomePersistenceError> {
    let document_id = parse_uuid(input.source_document_id, "source_document_id")?;
    let entry = load_statement_income_entry(
        evidence,
        conn,
        SourceReferenceQuery {
            household_id: input.household_id,
            document_id,
            source_reference: input.source_reference,
        },
    )
    .await?;
    let statement_occurred_at = DateTime::parse_from_rfc3339(&entry.occurred_at)
        .map_err(|_| AccountIncomePersistenceError::StatementIncomeMismatch)?
        .with_timezone(&Utc);
    let income_amount = bookkeeping_domain::PositiveYen::new(input.amount)
        .map_err(|_| AccountIncomePersistenceError::InvalidDecision)?;
    let account_id_value = input.account_id.to_string();
    let account = bookkeeping_application::AccountId::new(account_id_value)
        .map_err(|_| AccountIncomePersistenceError::InvalidDecision)?;
    let workflow_decision = decide_link_statement_income(LinkStatementIncomeCommand {
        source_document_id: document_id.to_string(),
        source_reference: input.source_reference.to_owned(),
        statement_account_id: account.clone(),
        income_account_id: account,
        income_amount,
        statement_amount: entry.amount,
        income_occurred_at: input
            .occurred_at
            .to_rfc3339_opts(SecondsFormat::AutoSi, true),
        statement_occurred_at: statement_occurred_at.to_rfc3339_opts(SecondsFormat::AutoSi, true),
        statement_kind: entry.kind,
        bank_statement: true,
        statement_has_region: entry.has_region,
    })
    .map_err(map_link_workflow_error)?;
    let account_kind = public::accounts::table
        .filter(public::accounts::household_id.eq(input.household_id))
        .filter(public::accounts::id.eq(input.account_id))
        .select(public::accounts::kind)
        .first::<String>(conn)
        .await
        .optional()?
        .ok_or(AccountIncomePersistenceError::AccountNotFound)?;
    if account_kind != "BANK"
        || workflow_decision.statement_account_id.as_str()
            != workflow_decision.income_account_id.as_str()
    {
        return Err(AccountIncomePersistenceError::InvalidDecision);
    }
    Ok(document_id)
}

struct ValidateStatementIncomeSource<'a> {
    household_id: Uuid,
    source_document_id: &'a str,
    source_reference: &'a str,
    account_id: Uuid,
    amount: i64,
    occurred_at: DateTime<Utc>,
}

async fn load_statement_income_entry<E: EvidenceOperations + ?Sized>(
    evidence: &E,
    conn: &mut diesel_async::AsyncPgConnection,
    query: SourceReferenceQuery<'_>,
) -> Result<StatementIncomeEntry, AccountIncomePersistenceError> {
    let SourceReferenceQuery {
        household_id,
        document_id,
        source_reference,
    } = query;
    let source_kind = evidence
        .source_kind(
            conn,
            SourceDocumentQuery {
                household_id,
                document_id,
                lock: true,
            },
        )
        .await?
        .ok_or(AccountIncomePersistenceError::SourceDocumentNotFound)?;
    if source_kind != "bank" {
        return Err(AccountIncomePersistenceError::StatementIsNotExternalIncome);
    }
    let latest_confirmation = evidence
        .latest_confirmation(
            conn,
            SourceReferenceQuery {
                household_id,
                document_id,
                source_reference,
            },
        )
        .await?;
    if latest_confirmation.is_some_and(|confirmation| confirmation.status == "confirmed") {
        return Err(AccountIncomePersistenceError::StatementAlreadyConfirmed);
    }
    let (_, result) = evidence
        .latest_extraction_with_id(
            conn,
            crate::evidence_port::ExtractionQuery {
                household_id,
                document_id,
            },
        )
        .await?
        .ok_or(AccountIncomePersistenceError::StatementExtractionUnavailable)?;
    let statements = result
        .get("statements")
        .and_then(serde_json::Value::as_array)
        .ok_or(AccountIncomePersistenceError::StatementExtractionUnavailable)?;
    for statement in statements {
        let row = statement.get("row").and_then(serde_json::Value::as_i64);
        let source_ref = statement
            .get("sourceReference")
            .and_then(serde_json::Value::as_str)
            .filter(|value| !value.is_empty())
            .map(str::to_owned)
            .or_else(|| row.map(|row| format!("row:{row}")));
        if source_ref.as_deref() != Some(source_reference) {
            continue;
        }
        let entry = StatementIncomeEntry {
            kind: statement
                .get("kind")
                .and_then(serde_json::Value::as_str)
                .ok_or(AccountIncomePersistenceError::StatementExtractionUnavailable)?
                .to_owned(),
            occurred_at: statement
                .get("occurredAt")
                .and_then(serde_json::Value::as_str)
                .ok_or(AccountIncomePersistenceError::StatementExtractionUnavailable)?
                .to_owned(),
            amount: statement
                .get("amount")
                .and_then(serde_json::Value::as_i64)
                .ok_or(AccountIncomePersistenceError::StatementExtractionUnavailable)?,
            has_region: statement
                .get("region")
                .is_some_and(|region| !region.is_null()),
        };
        return Ok(entry);
    }
    Err(AccountIncomePersistenceError::StatementReferenceNotFound)
}

async fn require_statement_income_confirmation<E: EvidenceOperations + ?Sized>(
    evidence: &E,
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    income_id: Uuid,
    source: &StatementIncomeSource,
) -> Result<(), AccountIncomePersistenceError> {
    let document_id = parse_uuid(&source.source_document_id, "source_document_id")?;
    let confirmation = evidence
        .latest_confirmation(
            conn,
            SourceReferenceQuery {
                household_id,
                document_id,
                source_reference: &source.source_reference,
            },
        )
        .await?;
    if confirmation.is_some_and(|confirmation| {
        confirmation.entity_kind == "account_income"
            && confirmation.entity_id == income_id
            && confirmation.status == "confirmed"
    }) {
        Ok(())
    } else {
        Err(AccountIncomePersistenceError::StatementAlreadyConfirmed)
    }
}

async fn insert_statement_income_confirmation<E: EvidenceOperations + ?Sized>(
    evidence: &E,
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    document_id: Uuid,
    source_reference: &str,
    income_id: Uuid,
    actor_id: Uuid,
) -> Result<(), AccountIncomePersistenceError> {
    evidence
        .insert_confirmation(
            conn,
            InsertConfirmation {
                household_id,
                document_id,
                source_reference: source_reference.to_owned(),
                entity_kind: "account_income".to_owned(),
                entity_id: income_id,
                changed_by: actor_id,
                idempotency_key: format!("account-income-confirmation:{}", Uuid::now_v7()),
                changed_at: None,
            },
        )
        .await
        .map_err(AccountIncomePersistenceError::from)
}

fn map_link_workflow_error(error: AccountIncomeError) -> AccountIncomePersistenceError {
    match error {
        AccountIncomeError::StatementIncomeMismatch
        | AccountIncomeError::StatementIsNotExternalIncome => {
            AccountIncomePersistenceError::StatementIncomeMismatch
        }
        _ => AccountIncomePersistenceError::InvalidDecision,
    }
}

async fn ensure_open_period(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    period_start: NaiveDate,
) -> Result<(), AccountIncomePersistenceError> {
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
        return Err(AccountIncomePersistenceError::AccountingPeriodClosed);
    }
    Ok(())
}

fn account_income_result(
    row: (Uuid, Uuid, Uuid, DateTime<Utc>, i64, String),
) -> PersistedAccountIncome {
    PersistedAccountIncome {
        id: row.0.to_string(),
        journal_id: row.1.to_string(),
        account_id: row.2.to_string(),
        occurred_at: row.3,
        amount_yen: row.4,
        description: row.5,
    }
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, AccountIncomePersistenceError> {
    Uuid::parse_str(value).map_err(|_| AccountIncomePersistenceError::InvalidUuid(field))
}
