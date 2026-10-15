use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use bookkeeping_workflows::{Side, account_income::RecordAccountIncomeDecision};

use crate::{PgPool, schema::public};

const OPERATION: &str = "recordExternalIncome";
const EVENT_TYPE: &str = "account.income.recorded";

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
    (&mut *pooled)
        .transaction::<PersistedAccountIncome, AccountIncomePersistenceError, _>(async |conn| {
            use public::account_incomes::dsl as incomes;
            use public::idempotency_records::dsl as idem;
            use public::journals::dsl as journals;

            let exists = public::households::table
                .filter(public::households::id.eq(household_id))
                .for_update()
                .select(public::households::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .is_some();
            if !exists {
                return Err(AccountIncomePersistenceError::HouseholdNotFound);
            }
            let member = public::household_members::table
                .filter(public::household_members::household_id.eq(household_id))
                .filter(public::household_members::member_id.eq(actor_id))
                .filter(public::household_members::left_at.is_null())
                .select(public::household_members::member_id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .is_some();
            if !member {
                return Err(AccountIncomePersistenceError::MemberNotFound);
            }

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
            diesel::insert_into(public::household_events::table)
                .values((
                    public::household_events::household_id.eq(household_id),
                    public::household_events::event_type.eq(EVENT_TYPE),
                    public::household_events::entity_kind.eq("account_income"),
                    public::household_events::entity_id.eq(income_id),
                    public::household_events::revision.eq(1_i64),
                    public::household_events::changed_fields.eq(decision
                        .changed_fields
                        .iter()
                        .map(|field| Some((*field).to_owned()))
                        .collect::<Vec<_>>()),
                    public::household_events::actor_id.eq(actor_id),
                    public::household_events::created_at.eq(now),
                ))
                .execute(&mut *conn)
                .await?;
            Ok(account_income_result(row))
        })
        .await
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
