use bookkeeping_application::workflow_failure::WorkflowFailure;
use bookkeeping_domain::create_account::{
    CreateAccountCommand, CreateAccountError, decide_create_account,
};
use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedCreateAccount {
    pub id: String,
    pub name: String,
    pub kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<String>,
}

#[derive(Debug)]
pub enum CreateAccountPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidOpeningDate,
    HouseholdNotFound,
    MemberNotFound,
    PoolNotFound,
    AccountingPeriodClosed,
    InvalidWorkflow(CreateAccountError),
}

impl From<DieselError> for CreateAccountPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_create_account(
    pool: &PgPool,
    command: CreateAccountCommand,
) -> Result<
    PersistedCreateAccount,
    WorkflowFailure<CreateAccountError, CreateAccountPersistenceError>,
> {
    persist_create_account_inner(pool, command)
        .await
        .map_err(|error| match error {
            CreateAccountPersistenceError::InvalidWorkflow(error) => {
                WorkflowFailure::Decision(error)
            }
            CreateAccountPersistenceError::InvalidOpeningDate => {
                WorkflowFailure::Decision(CreateAccountError::InvalidOpeningDate)
            }
            CreateAccountPersistenceError::HouseholdNotFound
            | CreateAccountPersistenceError::MemberNotFound
            | CreateAccountPersistenceError::PoolNotFound => WorkflowFailure::NotFound,
            CreateAccountPersistenceError::AccountingPeriodClosed => WorkflowFailure::Conflict,
            error => WorkflowFailure::Infrastructure(error),
        })
}

async fn persist_create_account_inner(
    pool: &PgPool,
    command: CreateAccountCommand,
) -> Result<PersistedCreateAccount, CreateAccountPersistenceError> {
    let decision =
        decide_create_account(command).map_err(CreateAccountPersistenceError::InvalidWorkflow)?;
    let household_id = parse_uuid(&decision.household_id, "household_id")?;
    let actor_id = parse_uuid(&decision.actor_id, "actor_id")?;
    let pool_id = decision
        .pool_id
        .as_deref()
        .map(|value| parse_uuid(value, "pool_id"))
        .transpose()?;
    let opening = decision
        .opening_balance
        .as_ref()
        .map(|opening| {
            let occurred_at = DateTime::parse_from_rfc3339(&opening.at)
                .map_err(|_| CreateAccountPersistenceError::InvalidOpeningDate)?
                .with_timezone(&Utc);
            let tokyo_offset = FixedOffset::east_opt(9 * 60 * 60)
                .ok_or(CreateAccountPersistenceError::InvalidOpeningDate)?;
            let local = occurred_at.with_timezone(&tokyo_offset);
            let period_start = NaiveDate::from_ymd_opt(local.year(), local.month(), 1)
                .ok_or(CreateAccountPersistenceError::InvalidOpeningDate)?;
            Ok::<_, CreateAccountPersistenceError>((opening.amount_yen, occurred_at, period_start))
        })
        .transpose()?;
    let account_id = Uuid::now_v7();

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CreateAccountPersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedCreateAccount, CreateAccountPersistenceError, _>(async |conn| {
            use public::accounting_periods::dsl as periods;
            use public::accounts::dsl as accounts;
            use public::journal_entries::dsl as entries;
            use public::journals::dsl as journals;
            use public::pools::dsl as pools;

            let household_exists = household_postgres::household_access::lock_household_for_update(
                &mut *conn,
                household_id,
            )
            .await?;
            if !household_exists {
                return Err(CreateAccountPersistenceError::HouseholdNotFound);
            }

            let actor_role = household_postgres::household_access::active_member_role(
                &mut *conn,
                household_id,
                actor_id,
            )
            .await?;
            if authorize(actor_role.as_deref(), Operation::FinancialData).is_err() {
                return Err(CreateAccountPersistenceError::MemberNotFound);
            }

            if let Some(pool_id) = pool_id {
                let pool_exists = pools::pools
                    .filter(pools::id.eq(pool_id))
                    .filter(pools::household_id.eq(household_id))
                    .select(pools::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .is_some();
                if !pool_exists {
                    return Err(CreateAccountPersistenceError::PoolNotFound);
                }
            }

            if let Some((_, _, period_start)) = opening {
                diesel::insert_into(periods::accounting_periods)
                    .values((
                        periods::household_id.eq(household_id),
                        periods::period_start.eq(period_start),
                        periods::status.eq("OPEN"),
                        periods::revision.eq(1_i64),
                    ))
                    .on_conflict((periods::household_id, periods::period_start))
                    .do_nothing()
                    .execute(&mut *conn)
                    .await?;
                let status = periods::accounting_periods
                    .filter(periods::household_id.eq(household_id))
                    .filter(periods::period_start.eq(period_start))
                    .for_update()
                    .select(periods::status)
                    .first::<String>(&mut *conn)
                    .await?;
                if status == "CLOSED" {
                    return Err(CreateAccountPersistenceError::AccountingPeriodClosed);
                }
            }

            diesel::insert_into(accounts::accounts)
                .values((
                    accounts::id.eq(account_id),
                    accounts::household_id.eq(household_id),
                    accounts::pool_id.eq(pool_id),
                    accounts::name.eq(&decision.name),
                    accounts::kind.eq(&decision.kind),
                    accounts::card_last4.eq(decision.card_last4.as_deref()),
                ))
                .execute(&mut *conn)
                .await?;

            if let Some((amount_yen, occurred_at, _)) = opening {
                let journal_id = Uuid::now_v7();
                diesel::insert_into(journals::journals)
                    .values((
                        journals::id.eq(journal_id),
                        journals::household_id.eq(household_id),
                        journals::source_kind.eq("ACCOUNT_OPENING_BALANCE"),
                        journals::source_id.eq(account_id),
                        journals::occurred_at.eq(occurred_at),
                        journals::created_by.eq(actor_id),
                    ))
                    .execute(&mut *conn)
                    .await?;

                let (account_debit, offset_debit) = if decision.kind == "CARD" {
                    (0, amount_yen)
                } else {
                    (amount_yen, 0)
                };
                let (account_credit, offset_credit) = if decision.kind == "CARD" {
                    (amount_yen, 0)
                } else {
                    (0, amount_yen)
                };
                diesel::insert_into(entries::journal_entries)
                    .values(vec![
                        (
                            entries::household_id.eq(household_id),
                            entries::journal_id.eq(journal_id),
                            entries::line_no.eq(1_i32),
                            entries::account_id.eq(Some(account_id)),
                            entries::pool_id.eq(None::<Uuid>),
                            entries::member_id.eq(None::<Uuid>),
                            entries::debit_yen.eq(account_debit),
                            entries::credit_yen.eq(account_credit),
                            entries::memo.eq("Opening balance"),
                        ),
                        (
                            entries::household_id.eq(household_id),
                            entries::journal_id.eq(journal_id),
                            entries::line_no.eq(2_i32),
                            entries::account_id.eq(None::<Uuid>),
                            entries::pool_id.eq(None::<Uuid>),
                            entries::member_id.eq(None::<Uuid>),
                            entries::debit_yen.eq(offset_debit),
                            entries::credit_yen.eq(offset_credit),
                            entries::memo.eq("Opening balance offset"),
                        ),
                    ])
                    .execute(&mut *conn)
                    .await?;
            }

            horse_postgres_common::insert_ui_change(
                &mut *conn,
                household_id,
                account_id,
                1,
                actor_id,
                Utc::now(),
                bookkeeping_application::ui_changes::account_created(),
            )
            .await?;

            Ok(PersistedCreateAccount {
                id: account_id.to_string(),
                name: decision.name.clone(),
                kind: decision.kind.clone(),
                card_last4: decision.card_last4.clone(),
                pool_id: decision.pool_id.clone(),
            })
        })
        .await
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, CreateAccountPersistenceError> {
    Uuid::parse_str(value).map_err(|_| CreateAccountPersistenceError::InvalidUuid(field))
}
