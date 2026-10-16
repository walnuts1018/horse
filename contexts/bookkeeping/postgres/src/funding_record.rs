use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
    upsert::excluded,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use uuid::Uuid;

use bookkeeping_application::funding_record as application;
use bookkeeping_application::workflow_failure::WorkflowFailure;
use bookkeeping_domain::funding_record::{PostingSide, RecordFundingDecision, RecordFundingError};

use crate::{PgPool, schema::public};

const OPERATION: &str = "recordFunding";
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

async fn replay_record_funding_in_transaction(
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
    if !household_postgres::household_access::household_exists(&mut *conn, household_id).await? {
        return Err(RecordFundingPersistenceError::HouseholdNotFound);
    }
    let tokyo_offset = FixedOffset::east_opt(9 * 60 * 60)
        .ok_or(RecordFundingPersistenceError::InvalidOccurredAt)?;
    let period_start = input
        .occurred_at
        .with_timezone(&tokyo_offset)
        .date_naive()
        .with_day(1)
        .ok_or(RecordFundingPersistenceError::InvalidOccurredAt)?;
    lock_accounting_period(conn, household_id, period_start).await?;
    use public::idempotency_records::dsl as idem;

    let actor_role = household_postgres::household_access::active_member_role(
        &mut *conn,
        household_id,
        actor_id,
    )
    .await?;
    if authorize(actor_role.as_deref(), Operation::FinancialData).is_err() {
        return Err(RecordFundingPersistenceError::MemberNotFound);
    }
    if household_postgres::household_access::active_member_role(&mut *conn, household_id, member_id)
        .await?
        .is_none()
    {
        return Err(RecordFundingPersistenceError::MemberNotFound);
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
    if fingerprint.len() != 32 || resource_kind != "ledger_entry" {
        return Err(RecordFundingPersistenceError::CorruptPersistedData(
            "idempotency record",
        ));
    }
    if fingerprint.as_slice() != input.request_fingerprint {
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
    CorruptPersistedData(&'static str),
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

enum RecordFundingTransactionError {
    Workflow(application::RecordFundingWorkflowError<RecordFundingWorkflowAdapterError>),
    Database(DieselError),
}

impl From<DieselError> for RecordFundingTransactionError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn record_funding_workflow<P: crate::policy_data::PolicyData>(
    pool: &PgPool,
    command: application::RecordFundingWorkflowCommand,
    policy: P,
) -> Result<
    application::RecordedFunding,
    WorkflowFailure<RecordFundingError, RecordFundingWorkflowAdapterError>,
> {
    let mut pooled = pool.get().await.map_err(|error| {
        WorkflowFailure::Infrastructure(RecordFundingWorkflowAdapterError::Persistence(
            RecordFundingPersistenceError::Pool(error.to_string()),
        ))
    })?;
    match (*pooled)
        .transaction::<_, RecordFundingTransactionError, _>(async |conn| {
            let mut operations = PgRecordFundingOperations { conn, policy };
            application::record_funding(&mut operations, command)
                .await
                .map_err(RecordFundingTransactionError::Workflow)
        })
        .await
    {
        Ok(result) => Ok(result),
        Err(RecordFundingTransactionError::Workflow(
            application::RecordFundingWorkflowError::Decision(error),
        )) => Err(WorkflowFailure::Decision(error)),
        Err(RecordFundingTransactionError::Workflow(
            application::RecordFundingWorkflowError::Operations(error),
        )) => Err(classify_funding_failure(error)),
        Err(RecordFundingTransactionError::Database(error)) => Err(
            WorkflowFailure::Infrastructure(RecordFundingWorkflowAdapterError::Persistence(
                RecordFundingPersistenceError::Database(error),
            )),
        ),
    }
}

fn classify_funding_failure(
    error: RecordFundingWorkflowAdapterError,
) -> WorkflowFailure<RecordFundingError, RecordFundingWorkflowAdapterError> {
    match error {
        RecordFundingWorkflowAdapterError::Context(crate::funding_command_context::FundingCommandContextError::HouseholdNotFound | crate::funding_command_context::FundingCommandContextError::PoolNotFound | crate::funding_command_context::FundingCommandContextError::AccountNotFound(_)) => WorkflowFailure::NotFound,
        RecordFundingWorkflowAdapterError::Context(crate::funding_command_context::FundingCommandContextError::AccountingPeriodClosed) => WorkflowFailure::Conflict,
        RecordFundingWorkflowAdapterError::Context(crate::funding_command_context::FundingCommandContextError::InvalidTokyoOffset | crate::funding_command_context::FundingCommandContextError::InvalidAccountingPeriodStatus(_) | crate::funding_command_context::FundingCommandContextError::InvalidPoolKind(_) | crate::funding_command_context::FundingCommandContextError::InvalidAccountKind(_) | crate::funding_command_context::FundingCommandContextError::Database(_)) => WorkflowFailure::Infrastructure(error),
        RecordFundingWorkflowAdapterError::Persistence(RecordFundingPersistenceError::HouseholdNotFound | RecordFundingPersistenceError::MemberNotFound | RecordFundingPersistenceError::PoolNotFound | RecordFundingPersistenceError::AccountNotFound) => WorkflowFailure::NotFound,
        RecordFundingWorkflowAdapterError::Persistence(RecordFundingPersistenceError::IdempotencyConflict | RecordFundingPersistenceError::AccountingPeriodClosed) => WorkflowFailure::Conflict,
        RecordFundingWorkflowAdapterError::Persistence(RecordFundingPersistenceError::InvalidUuid(_) | RecordFundingPersistenceError::InvalidIdempotencyKey | RecordFundingPersistenceError::InvalidOccurredAt | RecordFundingPersistenceError::InvalidAccountKind | RecordFundingPersistenceError::AccountOwnershipMismatch) => WorkflowFailure::Decision(RecordFundingError::InvalidIdentifier),
        RecordFundingWorkflowAdapterError::Persistence(RecordFundingPersistenceError::Pool(_) | RecordFundingPersistenceError::Database(_) | RecordFundingPersistenceError::InvalidDecision | RecordFundingPersistenceError::CorruptPersistedData(_) | RecordFundingPersistenceError::NumericOutOfRange(_)) => WorkflowFailure::Infrastructure(error),
    }
}

struct PgRecordFundingOperations<'a, P> {
    conn: &'a mut diesel_async::AsyncPgConnection,
    policy: P,
}

impl<P: crate::policy_data::PolicyData> application::RecordFundingOperations
    for PgRecordFundingOperations<'_, P>
{
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
            &mut self.policy,
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
            &mut self.policy,
        )
        .await
        .map(|value| application::RecordedFunding {
            journal_id: value.journal_id,
            ledger_entry_id: value.ledger_entry_id,
        })
        .map_err(RecordFundingWorkflowAdapterError::Persistence)
    }
}

async fn persist_record_funding_in_transaction(
    conn: &mut diesel_async::AsyncPgConnection,
    input: PersistRecordFunding,
    decision: RecordFundingDecision,
    policy: &mut impl crate::policy_data::PolicyData,
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
    let decision_actor_id = parse_decision_uuid(decision.actor_id.as_str())?;
    let member_id = parse_decision_uuid(decision.member_id.as_str())?;
    let pool_id = parse_decision_uuid(decision.pool_id.as_str())?;
    let event_actor_id = parse_decision_uuid(decision.event.actor_id.as_str())?;
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
    if period_start != decision_period_start {
        return Err(RecordFundingPersistenceError::InvalidDecision);
    }
    let ledger_entry_id = Uuid::now_v7();
    let journal_id = Uuid::now_v7();
    let amount = decision.amount_yen;
    let source_account_id = decision
        .source_account_id
        .as_deref()
        .map(parse_decision_uuid)
        .transpose()?;
    let destination_account_id = decision
        .destination_account_id
        .as_deref()
        .map(parse_decision_uuid)
        .transpose()?;

    use public::idempotency_records::dsl as idem;
    use public::journals::dsl as journals;

    let (status, revision) = lock_accounting_period(conn, household_id, period_start).await?;
    let expected_revision = i64::try_from(decision.accounting_period.revision())
        .map_err(|_| RecordFundingPersistenceError::InvalidDecision)?;
    if status != "OPEN" || revision != expected_revision {
        return Err(RecordFundingPersistenceError::AccountingPeriodClosed);
    }
    if !household_postgres::household_access::household_exists(&mut *conn, household_id).await? {
        return Err(RecordFundingPersistenceError::HouseholdNotFound);
    }
    let actor_role = household_postgres::household_access::active_member_role(
        &mut *conn,
        household_id,
        actor_id,
    )
    .await?;
    if authorize(actor_role.as_deref(), Operation::FinancialData).is_err() {
        return Err(RecordFundingPersistenceError::MemberNotFound);
    }
    if household_postgres::household_access::active_member_role(&mut *conn, household_id, member_id)
        .await?
        .is_none()
    {
        return Err(RecordFundingPersistenceError::MemberNotFound);
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
        if fingerprint.len() != 32 || resource_kind != "ledger_entry" {
            return Err(RecordFundingPersistenceError::CorruptPersistedData(
                "idempotency record",
            ));
        }
        if fingerprint.as_slice() != input.request_fingerprint {
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
        policy,
    )
    .await?;
    validate_account(
        &mut *conn,
        household_id,
        member_id,
        destination_account_id,
        false,
        pool_id,
        policy,
    )
    .await?;

    let schedule_id = policy
        .active_funding_schedule(&mut *conn, household_id, member_id, pool_id)
        .await?;
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
                    .map(parse_decision_uuid)
                    .transpose()?),
                public::journal_entries::pool_id.eq(line
                    .pool_id
                    .as_deref()
                    .map(parse_decision_uuid)
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
    horse_postgres_common::insert_ui_change(
        &mut *conn,
        household_id,
        ledger_entry_id,
        i64::try_from(decision.event.revision)
            .map_err(|_| RecordFundingPersistenceError::NumericOutOfRange("event revision"))?,
        actor_id,
        Utc::now(),
        bookkeeping_application::ui_changes::funding_recorded(
            decision.event.changed_fields.iter().copied(),
        ),
    )
    .await?;
    Ok(PersistedFunding {
        journal_id: journal_id.to_string(),
        ledger_entry_id: ledger_entry_id.to_string(),
    })
}
async fn lock_accounting_period(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    period_start: NaiveDate,
) -> Result<(String, i64), RecordFundingPersistenceError> {
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
        .await?;
    Ok(period)
}

async fn validate_account(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    member_id: Uuid,
    account_id: Option<Uuid>,
    source: bool,
    funded_pool_id: Uuid,
    policy: &mut impl crate::policy_data::PolicyData,
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
            let owner = policy
                .pool_funding_owner(conn, household_id, account_pool_id, version)
                .await?;
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

fn parse_decision_uuid(value: &str) -> Result<Uuid, RecordFundingPersistenceError> {
    Uuid::parse_str(value).map_err(|_| RecordFundingPersistenceError::InvalidDecision)
}
