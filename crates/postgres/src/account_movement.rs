use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use bookkeeping_workflows::{Side, account_movement::RecordAccountMovementDecision};

use crate::{PgPool, schema::public};

const OPERATION: &str = "recordAccountMovement";
const EVENT_TYPE: &str = "account.movement.recorded";

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
    (&mut *pooled)
        .transaction::<PersistedAccountMovement, AccountMovementPersistenceError, _>(async |conn| {
            use public::account_movements::dsl as movements;
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
                return Err(AccountMovementPersistenceError::HouseholdNotFound);
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
                return Err(AccountMovementPersistenceError::MemberNotFound);
            }

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
            diesel::insert_into(public::household_events::table)
                .values((
                    public::household_events::household_id.eq(household_id),
                    public::household_events::event_type.eq(EVENT_TYPE),
                    public::household_events::entity_kind.eq("account_movement"),
                    public::household_events::entity_id.eq(movement_id),
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
            account_movement_result(row)
        })
        .await
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
