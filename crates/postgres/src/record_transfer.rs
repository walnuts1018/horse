use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use std::collections::BTreeSet;
use uuid::Uuid;

use bookkeeping_workflows::record_transfer::{PostingSide, RecordTransferDecision, TransferKind};

use crate::{PgPool, schema::public};

const OPERATION: &str = "record_transfer";
const EVENT_TYPE: &str = "transfer.recorded";

#[derive(Debug, Clone)]
pub struct PersistRecordTransfer {
    pub household_id: String,
    pub actor_id: String,
    pub occurred_at: DateTime<Utc>,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedTransfer {
    pub transfer_id: String,
    pub journal_id: String,
    pub ledger_entry_id: String,
}

#[derive(Debug)]
pub enum RecordTransferPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidIdempotencyKey,
    IdempotencyConflict,
    NumericOutOfRange(&'static str),
    AccountingPeriodClosed,
    MemberNotFound,
    AccountNotFound,
    PoolNotFound,
    LoanNotFound,
    LoanDirectionMismatch,
    RepaymentExceedsOutstanding,
    InvalidDecision,
}

impl From<DieselError> for RecordTransferPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_record_transfer(
    pool: &PgPool,
    input: PersistRecordTransfer,
    decision: RecordTransferDecision,
) -> Result<PersistedTransfer, RecordTransferPersistenceError> {
    if input.idempotency_key.is_empty() || input.idempotency_key.len() > 200 {
        return Err(RecordTransferPersistenceError::InvalidIdempotencyKey);
    }
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let from_member_id = parse_uuid(decision.from_member_id.as_str(), "from_member_id")?;
    let to_member_id = parse_uuid(decision.to_member_id.as_str(), "to_member_id")?;
    let event_actor_id = parse_uuid(decision.event.actor_id.as_str(), "event actor_id")?;
    let tokyo_offset = FixedOffset::east_opt(9 * 60 * 60)
        .ok_or(RecordTransferPersistenceError::InvalidDecision)?;
    let period_start = input
        .occurred_at
        .with_timezone(&tokyo_offset)
        .date_naive()
        .with_day(1)
        .ok_or(RecordTransferPersistenceError::InvalidDecision)?;
    let loan_id = decision
        .loan_id
        .as_ref()
        .map(|id| parse_uuid(id.as_str(), "loan_id"))
        .transpose()?;
    let payment_account_id = decision
        .payment_account_id
        .as_ref()
        .map(|id| parse_uuid(id.as_str(), "payment_account_id"))
        .transpose()?;
    if actor_id != event_actor_id
        || actor_id != parse_uuid(decision.actor_id.as_str(), "decision actor_id")?
        || decision.amount.value() <= 0
        || decision.amount.value() > 1_000_000_000_000
        || decision.from_member_id == decision.to_member_id
        || (decision.kind == TransferKind::Repayment) != loan_id.is_some()
        || decision.event.revision != 1
        || decision.journal_lines.is_empty()
    {
        return Err(RecordTransferPersistenceError::InvalidDecision);
    }
    validate_decision(&decision)?;

    let transfer_id = Uuid::new_v4();
    let journal_id = Uuid::new_v4();
    let ledger_entry_id = Uuid::new_v4();
    let amount = decision.amount.value();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| RecordTransferPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedTransfer, RecordTransferPersistenceError, _>(async |conn| {
            use public::idempotency_records::dsl as idem;
            use public::journals::dsl as journals;
            use public::ledger_entries::dsl as ledger;

            let inserted = diesel::insert_into(idem::idempotency_records)
                .values((
                    idem::household_id.eq(household_id),
                    idem::operation.eq(OPERATION),
                    idem::key.eq(&input.idempotency_key),
                    idem::request_fingerprint.eq(input.request_fingerprint.to_vec()),
                    idem::resource_kind.eq("transfer"),
                    idem::resource_id.eq(transfer_id),
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
                    .filter(idem::operation.eq(OPERATION))
                    .filter(idem::key.eq(&input.idempotency_key))
                    .select((idem::request_fingerprint, idem::resource_id))
                    .first::<(Vec<u8>, Uuid)>(&mut *conn)
                    .await?;
                if fingerprint.as_slice() != input.request_fingerprint {
                    return Err(RecordTransferPersistenceError::IdempotencyConflict);
                }
                let saved_journal_id = journals::journals
                    .filter(journals::household_id.eq(household_id))
                    .filter(journals::source_kind.eq("transfer"))
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
                return Ok(PersistedTransfer {
                    transfer_id: resource_id.to_string(),
                    journal_id: saved_journal_id.to_string(),
                    ledger_entry_id: saved_ledger_entry_id.to_string(),
                });
            }

            ensure_open_period(&mut *conn, household_id, period_start).await?;
            validate_members_and_references(
                &mut *conn,
                household_id,
                actor_id,
                from_member_id,
                to_member_id,
                &decision,
            )
            .await?;
            if let Some(loan_id) = loan_id {
                validate_repayment(
                    &mut *conn,
                    household_id,
                    loan_id,
                    from_member_id,
                    to_member_id,
                    amount,
                )
                .await?;
            }

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
                    RecordTransferPersistenceError::NumericOutOfRange("journal line_no")
                })?;
                let line_amount = line.amount.value();
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
                        public::journal_entries::debit_yen.eq(if line.side == PostingSide::Debit {
                            line_amount
                        } else {
                            0
                        }),
                        public::journal_entries::credit_yen.eq(
                            if line.side == PostingSide::Credit {
                                line_amount
                            } else {
                                0
                            },
                        ),
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
                    public::transfers::from_member_id.eq(from_member_id),
                    public::transfers::to_member_id.eq(to_member_id),
                    public::transfers::kind.eq(decision.kind.as_str()),
                    public::transfers::amount_yen.eq(amount),
                    public::transfers::description.eq(&decision.description),
                    public::transfers::payment_account_id.eq(payment_account_id),
                    public::transfers::loan_id.eq(loan_id),
                    public::transfers::created_by.eq(actor_id),
                    public::transfers::created_at.eq(now),
                ))
                .execute(&mut *conn)
                .await?;
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
                        public::settlement_adjustments::source_kind.eq(decision.kind.as_str()),
                        public::settlement_adjustments::source_id.eq(transfer_id),
                        public::settlement_adjustments::amount_yen.eq(adjustment.amount_yen),
                        public::settlement_adjustments::created_at.eq(now),
                    ))
                    .execute(&mut *conn)
                    .await?;
            }
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
            diesel::insert_into(public::household_events::table)
                .values((
                    public::household_events::household_id.eq(household_id),
                    public::household_events::event_type.eq(EVENT_TYPE),
                    public::household_events::entity_kind.eq("ledger"),
                    public::household_events::entity_id.eq(ledger_entry_id),
                    public::household_events::revision
                        .eq(i64::try_from(decision.event.revision).map_err(|_| {
                            RecordTransferPersistenceError::NumericOutOfRange("event revision")
                        })?),
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
            Ok(PersistedTransfer {
                transfer_id: transfer_id.to_string(),
                journal_id: journal_id.to_string(),
                ledger_entry_id: ledger_entry_id.to_string(),
            })
        })
        .await
}

async fn ensure_open_period(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    period_start: NaiveDate,
) -> Result<(), RecordTransferPersistenceError> {
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
        .first::<String>(conn)
        .await
        .optional()?;
    if status.as_deref() != Some("OPEN") {
        return Err(RecordTransferPersistenceError::AccountingPeriodClosed);
    }
    Ok(())
}

async fn validate_members_and_references(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
    from_member_id: Uuid,
    to_member_id: Uuid,
    decision: &RecordTransferDecision,
) -> Result<(), RecordTransferPersistenceError> {
    use public::household_members::dsl as members;
    for member_id in [actor_id, from_member_id, to_member_id] {
        members::household_members
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(member_id))
            .filter(members::left_at.is_null())
            .select(members::member_id)
            .first::<Uuid>(conn)
            .await
            .optional()?
            .ok_or(RecordTransferPersistenceError::MemberNotFound)?;
    }
    for line in &decision.journal_lines {
        if let Some(id) = &line.member_id {
            members::household_members
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(parse_uuid(id.as_str(), "journal member_id")?))
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
                .await
                .optional()?
                .ok_or(RecordTransferPersistenceError::AccountNotFound)?;
        }
        if let Some(id) = &line.pool_id {
            public::pools::table
                .filter(public::pools::household_id.eq(household_id))
                .filter(public::pools::id.eq(parse_uuid(id.as_str(), "pool_id")?))
                .select(public::pools::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .ok_or(RecordTransferPersistenceError::PoolNotFound)?;
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

async fn validate_repayment(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    loan_id: Uuid,
    borrower_id: Uuid,
    lender_id: Uuid,
    amount: i64,
) -> Result<(), RecordTransferPersistenceError> {
    use public::transfers::dsl as transfers;
    let (principal, expected_lender, expected_borrower) = transfers::transfers
        .filter(transfers::household_id.eq(household_id))
        .filter(transfers::id.eq(loan_id))
        .filter(transfers::kind.eq(TransferKind::Loan.as_str()))
        .for_update()
        .select((
            transfers::amount_yen,
            transfers::from_member_id,
            transfers::to_member_id,
        ))
        .first::<(i64, Uuid, Uuid)>(conn)
        .await
        .optional()?
        .ok_or(RecordTransferPersistenceError::LoanNotFound)?;
    if expected_borrower != borrower_id || expected_lender != lender_id {
        return Err(RecordTransferPersistenceError::LoanDirectionMismatch);
    }
    let repayments = transfers::transfers
        .filter(transfers::household_id.eq(household_id))
        .filter(transfers::loan_id.eq(Some(loan_id)))
        .filter(transfers::kind.eq(TransferKind::Repayment.as_str()))
        .select(transfers::amount_yen)
        .load::<i64>(conn)
        .await?;
    let repaid = repayments.into_iter().try_fold(0_i64, |sum, value| {
        sum.checked_add(value)
            .ok_or(RecordTransferPersistenceError::RepaymentExceedsOutstanding)
    })?;
    if repaid > principal || amount > principal - repaid {
        return Err(RecordTransferPersistenceError::RepaymentExceedsOutstanding);
    }
    Ok(())
}

fn validate_decision(
    decision: &RecordTransferDecision,
) -> Result<(), RecordTransferPersistenceError> {
    if decision.journal_lines.len() < 2
        || (decision.kind == TransferKind::Gift && !decision.settlement_adjustments.is_empty())
        || (decision.kind != TransferKind::Gift && decision.settlement_adjustments.is_empty())
    {
        return Err(RecordTransferPersistenceError::InvalidDecision);
    }
    let mut line_numbers = BTreeSet::new();
    let mut debits = 0_i64;
    let mut credits = 0_i64;
    for line in &decision.journal_lines {
        let line_no = i32::try_from(line.line_no)
            .map_err(|_| RecordTransferPersistenceError::NumericOutOfRange("journal line_no"))?;
        let amount = line.amount.value();
        if line_no <= 0
            || !line_numbers.insert(line_no)
            || amount <= 0
            || amount > 1_000_000_000_000
        {
            return Err(RecordTransferPersistenceError::InvalidDecision);
        }
        let total = match line.side {
            PostingSide::Debit => &mut debits,
            PostingSide::Credit => &mut credits,
        };
        *total = total
            .checked_add(amount)
            .ok_or(RecordTransferPersistenceError::InvalidDecision)?;
    }
    if debits != credits
        || decision.journal_lines[0].line_no != 1
        || decision.journal_lines[1].line_no != 2
        || decision.journal_lines[0].amount != decision.amount
        || decision.journal_lines[1].amount != decision.amount
        || decision.journal_lines.iter().any(|line| {
            line.memo.is_empty()
                || [
                    line.account_id.is_some(),
                    line.pool_id.is_some(),
                    line.member_id.is_some(),
                ]
                .into_iter()
                .filter(|present| *present)
                .count()
                    > 1
        })
        || decision.settlement_adjustments.iter().any(|adjustment| {
            adjustment.amount_yen == 0 || adjustment.amount_yen.unsigned_abs() > 1_000_000_000_000
        })
    {
        return Err(RecordTransferPersistenceError::InvalidDecision);
    }
    Ok(())
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, RecordTransferPersistenceError> {
    Uuid::parse_str(value).map_err(|_| RecordTransferPersistenceError::InvalidUuid(field))
}
