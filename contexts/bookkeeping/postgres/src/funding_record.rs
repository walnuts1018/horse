use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
    upsert::excluded,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use bookkeeping_application::funding_record as application;
use bookkeeping_application::funding_record::{PostingSide, RecordFundingDecision};
use bookkeeping_domain::AccountingPeriodStatus;

use crate::{PgPool, schema::public};

const OPERATION: &str = "recordFunding";
const EVENT_TYPE: &str = "funding.recorded";
const GO_ZERO_TIME_UNIX_SECONDS: i64 = -62_135_596_800;

#[derive(Debug, Clone)]
pub struct PersistRecordFunding {
    pub household_id: String,
    pub actor_id: String,
    pub occurred_at: DateTime<Utc>,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedFunding {
    pub journal_id: String,
    pub ledger_entry_id: String,
}

pub async fn replay_record_funding(
    pool: &PgPool,
    input: &PersistRecordFunding,
    member_id: Uuid,
    pool_id: Uuid,
) -> Result<Option<PersistedFunding>, RecordFundingPersistenceError> {
    if input.idempotency_key.trim().is_empty() || input.idempotency_key.len() > 200 {
        return Err(RecordFundingPersistenceError::InvalidIdempotencyKey);
    }
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| RecordFundingPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<Option<PersistedFunding>, RecordFundingPersistenceError, _>(async |conn| {
            replay_record_funding_in_transaction(conn, input, member_id, pool_id).await
        })
        .await
}

pub async fn replay_record_funding_in_transaction(
    conn: &mut diesel_async::AsyncPgConnection,
    input: &PersistRecordFunding,
    member_id: Uuid,
    pool_id: Uuid,
) -> Result<Option<PersistedFunding>, RecordFundingPersistenceError> {
    if input.idempotency_key.trim().is_empty() || input.idempotency_key.len() > 200 {
        return Err(RecordFundingPersistenceError::InvalidIdempotencyKey);
    }
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    use public::idempotency_records::dsl as idem;

    for id in [actor_id, member_id] {
        public::household_members::table
            .filter(public::household_members::household_id.eq(household_id))
            .filter(public::household_members::member_id.eq(id))
            .filter(public::household_members::left_at.is_null())
            .select(public::household_members::member_id)
            .first::<Uuid>(&mut *conn)
            .await
            .optional()?
            .ok_or(RecordFundingPersistenceError::MemberNotFound)?;
    }
    public::pools::table
        .filter(public::pools::household_id.eq(household_id))
        .filter(public::pools::id.eq(pool_id))
        .select(public::pools::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .ok_or(RecordFundingPersistenceError::PoolNotFound)?;
    let record = idem::idempotency_records
        .filter(idem::household_id.eq(household_id))
        .filter(idem::operation.eq(OPERATION))
        .filter(idem::key.eq(&input.idempotency_key))
        .select((
            idem::request_fingerprint,
            idem::resource_kind,
            idem::resource_id,
        ))
        .first::<(Vec<u8>, String, Uuid)>(&mut *conn)
        .await
        .optional()?;
    let Some((fingerprint, resource_kind, ledger_entry_id)) = record else {
        return Ok(None);
    };
    if fingerprint.as_slice() != input.request_fingerprint || resource_kind != "ledger_entry" {
        return Err(RecordFundingPersistenceError::IdempotencyConflict);
    }
    let journal_id = public::ledger_entries::table
        .filter(public::ledger_entries::household_id.eq(household_id))
        .filter(public::ledger_entries::id.eq(ledger_entry_id))
        .select(public::ledger_entries::journal_id)
        .first::<Uuid>(&mut *conn)
        .await?;
    Ok(Some(PersistedFunding {
        journal_id: journal_id.to_string(),
        ledger_entry_id: ledger_entry_id.to_string(),
    }))
}

#[derive(Debug)]
pub enum RecordFundingPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidIdempotencyKey,
    InvalidOccurredAt,
    IdempotencyConflict,
    InvalidDecision,
    HouseholdNotFound,
    MemberNotFound,
    PoolNotFound,
    AccountNotFound,
    AccountingPeriodClosed,
    AccountOwnershipMismatch,
    InvalidAccountKind,
    NumericOutOfRange(&'static str),
}

impl From<DieselError> for RecordFundingPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

#[derive(Debug)]
pub enum RecordFundingWorkflowAdapterError {
    Context(crate::funding_command_context::FundingCommandContextError),
    Persistence(RecordFundingPersistenceError),
}

pub async fn record_funding_workflow(
    pool: &PgPool,
    command: application::RecordFundingWorkflowCommand,
) -> Result<
    application::RecordedFunding,
    application::RecordFundingWorkflowError<RecordFundingWorkflowAdapterError>,
> {
    let mut pooled = pool.get().await.map_err(|error| {
        application::RecordFundingWorkflowError::Operations(
            RecordFundingWorkflowAdapterError::Persistence(
                RecordFundingPersistenceError::Pool(error.to_string()),
            ),
        )
    })?;
    let result = (&mut *pooled)
        .transaction::<
            Result<_, application::RecordFundingWorkflowError<RecordFundingWorkflowAdapterError>>,
            DieselError,
            _,
        >(async |conn| {
            let mut operations = PgRecordFundingOperations { conn };
            Ok(application::record_funding(&mut operations, command).await)
        })
        .await
        .map_err(|error| {
            application::RecordFundingWorkflowError::Operations(
                RecordFundingWorkflowAdapterError::Persistence(
                    RecordFundingPersistenceError::Database(error),
                ),
            )
        })?;
    result
}

struct PgRecordFundingOperations<'a> {
    conn: &'a mut diesel_async::AsyncPgConnection,
}

impl application::RecordFundingOperations for PgRecordFundingOperations<'_> {
    type Error = RecordFundingWorkflowAdapterError;

    async fn replay(
        &mut self,
        command: &application::RecordFundingWorkflowCommand,
    ) -> Result<Option<application::RecordedFunding>, Self::Error> {
        let member_id = parse_uuid(&command.record.member_id, "member_id")
            .map_err(RecordFundingWorkflowAdapterError::Persistence)?;
        let pool_id = parse_uuid(&command.record.pool_id, "pool_id")
            .map_err(RecordFundingWorkflowAdapterError::Persistence)?;
        replay_record_funding_in_transaction(
            &mut *self.conn,
            &PersistRecordFunding {
                household_id: command.household_id.clone(),
                actor_id: command.record.actor_id.clone(),
                occurred_at: command.occurred_at,
                idempotency_key: command.idempotency_key.clone(),
                request_fingerprint: command.request_fingerprint,
            },
            member_id,
            pool_id,
        )
        .await
        .map_err(RecordFundingWorkflowAdapterError::Persistence)
        .map(|result| {
            result.map(|value| application::RecordedFunding {
                journal_id: value.journal_id,
                ledger_entry_id: value.ledger_entry_id,
            })
        })
    }

    async fn load(
        &mut self,
        command: &application::RecordFundingWorkflowCommand,
    ) -> Result<application::FundingCommandContext, Self::Error> {
        let household_id = parse_uuid(&command.household_id, "household_id")
            .map_err(RecordFundingWorkflowAdapterError::Persistence)?;
        let pool_id = parse_uuid(&command.record.pool_id, "pool_id")
            .map_err(RecordFundingWorkflowAdapterError::Persistence)?;
        let source_account_id = command
            .source_account_id
            .as_deref()
            .map(|id| parse_uuid(id, "source_account_id"))
            .transpose()
            .map_err(RecordFundingWorkflowAdapterError::Persistence)?;
        let destination_account_id = command
            .destination_account_id
            .as_deref()
            .map(|id| parse_uuid(id, "destination_account_id"))
            .transpose()
            .map_err(RecordFundingWorkflowAdapterError::Persistence)?;
        crate::funding_command_context::load_funding_command_context_in_transaction(
            &mut *self.conn,
            household_id,
            pool_id,
            source_account_id,
            destination_account_id,
            command.occurred_at,
        )
        .await
        .map_err(RecordFundingWorkflowAdapterError::Context)
    }

    async fn persist(
        &mut self,
        command: &application::RecordFundingWorkflowCommand,
        decision: RecordFundingDecision,
    ) -> Result<application::RecordedFunding, Self::Error> {
        persist_record_funding_in_transaction(
            &mut *self.conn,
            PersistRecordFunding {
                household_id: command.household_id.clone(),
                actor_id: command.record.actor_id.clone(),
                occurred_at: command.occurred_at,
                idempotency_key: command.idempotency_key.clone(),
                request_fingerprint: command.request_fingerprint,
            },
            decision,
        )
        .await
        .map(|value| application::RecordedFunding {
            journal_id: value.journal_id,
            ledger_entry_id: value.ledger_entry_id,
        })
        .map_err(RecordFundingWorkflowAdapterError::Persistence)
    }
}

pub async fn persist_record_funding(
    pool: &PgPool,
    input: PersistRecordFunding,
    decision: RecordFundingDecision,
) -> Result<PersistedFunding, RecordFundingPersistenceError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| RecordFundingPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedFunding, RecordFundingPersistenceError, _>(async |conn| {
            persist_record_funding_in_transaction(conn, input, decision).await
        })
        .await
}

pub async fn persist_record_funding_in_transaction(
    conn: &mut diesel_async::AsyncPgConnection,
    input: PersistRecordFunding,
    decision: RecordFundingDecision,
) -> Result<PersistedFunding, RecordFundingPersistenceError> {
    if input.idempotency_key.trim().is_empty() || input.idempotency_key.len() > 200 {
        return Err(RecordFundingPersistenceError::InvalidIdempotencyKey);
    }
    if input.occurred_at.timestamp() == GO_ZERO_TIME_UNIX_SECONDS
        && input.occurred_at.timestamp_subsec_nanos() == 0
    {
        return Err(RecordFundingPersistenceError::InvalidOccurredAt);
    }
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let decision_actor_id = parse_uuid(decision.actor_id.as_str(), "decision actor_id")?;
    let member_id = parse_uuid(decision.member_id.as_str(), "member_id")?;
    let pool_id = parse_uuid(decision.pool_id.as_str(), "pool_id")?;
    let event_actor_id = parse_uuid(decision.event.actor_id.as_str(), "event actor_id")?;
    if actor_id != decision_actor_id
        || actor_id != event_actor_id
        || decision.amount_yen <= 0
        || decision.amount_yen > 1_000_000_000_000
        || decision.event.revision != 1
        || decision.journal_lines.len()
            != (if decision.source_account_id.is_some() || decision.destination_account_id.is_some()
            {
                4
            } else {
                2
            })
    {
        return Err(RecordFundingPersistenceError::InvalidDecision);
    }
    validate_decision(&decision)?;
    let tokyo_offset =
        FixedOffset::east_opt(9 * 60 * 60).ok_or(RecordFundingPersistenceError::InvalidDecision)?;
    let period_start = input
        .occurred_at
        .with_timezone(&tokyo_offset)
        .date_naive()
        .with_day(1)
        .ok_or(RecordFundingPersistenceError::InvalidDecision)?;
    let decision_period_start = NaiveDate::from_ymd_opt(
        i32::from(decision.accounting_period.year_month().year()),
        u32::from(decision.accounting_period.year_month().month()),
        1,
    )
    .ok_or(RecordFundingPersistenceError::InvalidDecision)?;
    if period_start != decision_period_start
        || decision.accounting_period.status() != AccountingPeriodStatus::Open
    {
        return Err(RecordFundingPersistenceError::InvalidDecision);
    }
    let ledger_entry_id = Uuid::now_v7();
    let journal_id = Uuid::now_v7();
    let amount = decision.amount_yen;
    let source_account_id = decision
        .source_account_id
        .as_deref()
        .map(|id| parse_uuid(id, "source_account_id"))
        .transpose()?;
    let destination_account_id = decision
        .destination_account_id
        .as_deref()
        .map(|id| parse_uuid(id, "destination_account_id"))
        .transpose()?;

    use public::idempotency_records::dsl as idem;
    use public::journals::dsl as journals;

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
        .ok_or(RecordFundingPersistenceError::HouseholdNotFound)?;
    for id in [actor_id, member_id] {
        public::household_members::table
            .filter(public::household_members::household_id.eq(household_id))
            .filter(public::household_members::member_id.eq(id))
            .filter(public::household_members::left_at.is_null())
            .select(public::household_members::member_id)
            .first::<Uuid>(&mut *conn)
            .await
            .optional()?
            .ok_or(RecordFundingPersistenceError::MemberNotFound)?;
    }
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
        let (fingerprint, resource_kind, saved_ledger_id) = idem::idempotency_records
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
        if fingerprint.as_slice() != input.request_fingerprint || resource_kind != "ledger_entry" {
            return Err(RecordFundingPersistenceError::IdempotencyConflict);
        }
        let (saved_journal_id, _) = public::ledger_entries::table
            .filter(public::ledger_entries::household_id.eq(household_id))
            .filter(public::ledger_entries::id.eq(saved_ledger_id))
            .select((
                public::ledger_entries::journal_id,
                public::ledger_entries::id,
            ))
            .first::<(Uuid, Uuid)>(&mut *conn)
            .await?;
        return Ok(PersistedFunding {
            journal_id: saved_journal_id.to_string(),
            ledger_entry_id: saved_ledger_id.to_string(),
        });
    }

    public::pools::table
        .filter(public::pools::household_id.eq(household_id))
        .filter(public::pools::id.eq(pool_id))
        .select(public::pools::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .ok_or(RecordFundingPersistenceError::PoolNotFound)?;
    validate_account(
        &mut *conn,
        household_id,
        member_id,
        source_account_id,
        true,
        pool_id,
    )
    .await?;
    validate_account(
        &mut *conn,
        household_id,
        member_id,
        destination_account_id,
        false,
        pool_id,
    )
    .await?;

    let schedule_id = public::funding_schedules::table
        .filter(public::funding_schedules::household_id.eq(household_id))
        .filter(public::funding_schedules::member_id.eq(member_id))
        .filter(public::funding_schedules::pool_id.eq(pool_id))
        .filter(public::funding_schedules::active.eq(true))
        .order(public::funding_schedules::created_at.desc())
        .select(public::funding_schedules::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?;
    diesel::insert_into(public::funding_obligations::table)
        .values((
            public::funding_obligations::id.eq(Uuid::now_v7()),
            public::funding_obligations::household_id.eq(household_id),
            public::funding_obligations::schedule_id.eq(schedule_id),
            public::funding_obligations::member_id.eq(member_id),
            public::funding_obligations::pool_id.eq(pool_id),
            public::funding_obligations::period_start.eq(period_start),
            public::funding_obligations::amount_yen.eq(0_i64),
            public::funding_obligations::paid_yen.eq(amount),
            public::funding_obligations::created_at.eq(now),
        ))
        .on_conflict((
            public::funding_obligations::household_id,
            public::funding_obligations::member_id,
            public::funding_obligations::pool_id,
            public::funding_obligations::period_start,
        ))
        .do_update()
        .set(
            public::funding_obligations::paid_yen.eq(public::funding_obligations::paid_yen
                + excluded(public::funding_obligations::paid_yen)),
        )
        .execute(&mut *conn)
        .await?;

    diesel::insert_into(journals::journals)
        .values((
            journals::id.eq(journal_id),
            journals::household_id.eq(household_id),
            journals::source_kind.eq("FUNDING"),
            journals::source_id.eq(ledger_entry_id),
            journals::idempotency_key.eq(&input.idempotency_key),
            journals::occurred_at.eq(input.occurred_at),
            journals::created_by.eq(member_id),
            journals::created_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;
    for line in &decision.journal_lines {
        diesel::insert_into(public::journal_entries::table)
            .values((
                public::journal_entries::household_id.eq(household_id),
                public::journal_entries::journal_id.eq(journal_id),
                public::journal_entries::line_no.eq(i32::try_from(line.line_no).map_err(|_| {
                    RecordFundingPersistenceError::NumericOutOfRange("journal line_no")
                })?),
                public::journal_entries::account_id.eq(line
                    .account_id
                    .as_deref()
                    .map(|id| parse_uuid(id, "journal account_id"))
                    .transpose()?),
                public::journal_entries::pool_id.eq(line
                    .pool_id
                    .as_deref()
                    .map(|id| parse_uuid(id, "journal pool_id"))
                    .transpose()?),
                public::journal_entries::member_id.eq(None::<Uuid>),
                public::journal_entries::debit_yen.eq(if line.side == PostingSide::Debit {
                    line.amount_yen
                } else {
                    0
                }),
                public::journal_entries::credit_yen.eq(if line.side == PostingSide::Credit {
                    line.amount_yen
                } else {
                    0
                }),
                public::journal_entries::memo.eq(line.memo),
            ))
            .execute(&mut *conn)
            .await?;
    }
    diesel::insert_into(public::ledger_entries::table)
        .values((
            public::ledger_entries::id.eq(ledger_entry_id),
            public::ledger_entries::household_id.eq(household_id),
            public::ledger_entries::journal_id.eq(journal_id),
            public::ledger_entries::source_kind.eq("FUNDING"),
            public::ledger_entries::source_id.eq(ledger_entry_id),
            public::ledger_entries::occurred_at.eq(input.occurred_at),
            public::ledger_entries::description.eq("Pool funding"),
            public::ledger_entries::amount_yen.eq(amount),
            public::ledger_entries::from_member_id.eq(Some(member_id)),
            public::ledger_entries::pool_id.eq(Some(pool_id)),
            public::ledger_entries::created_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;
    diesel::insert_into(public::household_events::table)
        .values((
            public::household_events::household_id.eq(household_id),
            public::household_events::event_type.eq(EVENT_TYPE),
            public::household_events::entity_kind.eq("ledger"),
            public::household_events::entity_id.eq(ledger_entry_id),
            public::household_events::revision.eq(i64::try_from(decision.event.revision)
                .map_err(|_| RecordFundingPersistenceError::NumericOutOfRange("event revision"))?),
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
    Ok(PersistedFunding {
        journal_id: journal_id.to_string(),
        ledger_entry_id: ledger_entry_id.to_string(),
    })
}
async fn ensure_open_period(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    period_start: NaiveDate,
    expected_revision: u64,
) -> Result<(), RecordFundingPersistenceError> {
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
    let (status, revision) = public::accounting_periods::table
        .filter(public::accounting_periods::household_id.eq(household_id))
        .filter(public::accounting_periods::period_start.eq(period_start))
        .for_update()
        .select((
            public::accounting_periods::status,
            public::accounting_periods::revision,
        ))
        .first::<(String, i64)>(conn)
        .await?;
    let expected_revision = i64::try_from(expected_revision)
        .map_err(|_| RecordFundingPersistenceError::InvalidDecision)?;
    if status != "OPEN" || revision != expected_revision {
        return Err(RecordFundingPersistenceError::AccountingPeriodClosed);
    }
    Ok(())
}

async fn validate_account(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    member_id: Uuid,
    account_id: Option<Uuid>,
    source: bool,
    funded_pool_id: Uuid,
) -> Result<(), RecordFundingPersistenceError> {
    let Some(account_id) = account_id else {
        return Ok(());
    };
    let (kind, pool_id) = public::accounts::table
        .filter(public::accounts::household_id.eq(household_id))
        .filter(public::accounts::id.eq(account_id))
        .select((public::accounts::kind, public::accounts::pool_id))
        .first::<(String, Option<Uuid>)>(conn)
        .await
        .optional()?
        .ok_or(RecordFundingPersistenceError::AccountNotFound)?;
    if kind == "CARD" {
        return Err(RecordFundingPersistenceError::InvalidAccountKind);
    }
    if !source && pool_id != Some(funded_pool_id) {
        return Err(RecordFundingPersistenceError::AccountOwnershipMismatch);
    }
    if let Some(account_pool_id) = pool_id {
        let (account_pool_kind, funding_version) = public::pools::table
            .filter(public::pools::household_id.eq(household_id))
            .filter(public::pools::id.eq(account_pool_id))
            .select((public::pools::kind, public::pools::current_funding_version))
            .first::<(String, Option<i32>)>(conn)
            .await
            .optional()?
            .ok_or(RecordFundingPersistenceError::PoolNotFound)?;
        if source && account_pool_kind == "PERSONAL" {
            let version =
                funding_version.ok_or(RecordFundingPersistenceError::AccountOwnershipMismatch)?;
            let owner = public::pool_funding_rules::table
                .filter(public::pool_funding_rules::household_id.eq(household_id))
                .filter(public::pool_funding_rules::pool_id.eq(account_pool_id))
                .filter(public::pool_funding_rules::version.eq(version))
                .select(public::pool_funding_rules::owner_member_id)
                .first::<Option<Uuid>>(conn)
                .await
                .optional()?
                .flatten();
            if owner != Some(member_id) {
                return Err(RecordFundingPersistenceError::AccountOwnershipMismatch);
            }
        }
    } else if !source {
        return Err(RecordFundingPersistenceError::AccountOwnershipMismatch);
    }
    Ok(())
}

fn validate_decision(
    decision: &RecordFundingDecision,
) -> Result<(), RecordFundingPersistenceError> {
    let amount = decision.amount_yen;
    if decision
        .journal_lines
        .iter()
        .enumerate()
        .any(|(index, line)| {
            line.line_no != (index as u32 + 1)
                || line.amount_yen != amount
                || (index < 2 && line.pool_id.as_deref() != Some(decision.pool_id.as_str()))
                || (index >= 2 && line.pool_id.is_some())
        })
    {
        return Err(RecordFundingPersistenceError::InvalidDecision);
    }
    if decision.journal_lines[0].side != PostingSide::Debit
        || decision.journal_lines[1].side != PostingSide::Credit
        || decision
            .journal_lines
            .get(2)
            .is_some_and(|line| line.side != PostingSide::Debit)
        || decision
            .journal_lines
            .get(3)
            .is_some_and(|line| line.side != PostingSide::Credit)
        || decision
            .journal_lines
            .get(2)
            .map(|line| line.account_id.as_deref())
            != Some(decision.destination_account_id.as_deref())
        || decision
            .journal_lines
            .get(3)
            .map(|line| line.account_id.as_deref())
            != Some(decision.source_account_id.as_deref())
    {
        return Err(RecordFundingPersistenceError::InvalidDecision);
    }
    Ok(())
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, RecordFundingPersistenceError> {
    Uuid::parse_str(value).map_err(|_| RecordFundingPersistenceError::InvalidUuid(field))
}
