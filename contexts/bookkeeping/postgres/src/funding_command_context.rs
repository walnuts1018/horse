use chrono::{DateTime, Datelike, FixedOffset, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

pub use bookkeeping_application::funding_record::FundingCommandContext;
use bookkeeping_application::funding_record::{
    FundingAccount, FundingAccountKind, FundingPoolKind,
};
use bookkeeping_domain::{AccountingPeriod, YearMonth};

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum FundingCommandContextError {
    Pool(String),
    Database(diesel::result::Error),
    InvalidTokyoOffset,
    HouseholdNotFound,
    PoolNotFound,
    AccountNotFound(Uuid),
    InvalidAccountingPeriodStatus(String),
    InvalidPoolKind(String),
    InvalidAccountKind(String),
}

impl From<diesel::result::Error> for FundingCommandContextError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

pub async fn load_funding_command_context(
    pool: &PgPool,
    household_id: Uuid,
    pool_id: Uuid,
    source_account_id: Option<Uuid>,
    destination_account_id: Option<Uuid>,
    occurred_at: DateTime<Utc>,
) -> Result<FundingCommandContext, FundingCommandContextError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| FundingCommandContextError::Pool(error.to_string()))?;
    load_funding_command_context_in_transaction(
        &mut *pooled,
        household_id,
        pool_id,
        source_account_id,
        destination_account_id,
        occurred_at,
    )
    .await
}

pub async fn load_funding_command_context_in_transaction(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    pool_id: Uuid,
    source_account_id: Option<Uuid>,
    destination_account_id: Option<Uuid>,
    occurred_at: DateTime<Utc>,
) -> Result<FundingCommandContext, FundingCommandContextError> {
    let tokyo_offset =
        FixedOffset::east_opt(9 * 60 * 60).ok_or(FundingCommandContextError::InvalidTokyoOffset)?;
    let period_start = occurred_at
        .with_timezone(&tokyo_offset)
        .date_naive()
        .with_day(1)
        .ok_or(FundingCommandContextError::InvalidTokyoOffset)?;
    use public::accounting_periods::dsl as periods;
    use public::pools::dsl as pools;

    public::households::table
        .filter(public::households::id.eq(household_id))
        .select(public::households::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .ok_or(FundingCommandContextError::HouseholdNotFound)?;

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
    let persisted_period = periods::accounting_periods
        .filter(periods::household_id.eq(household_id))
        .filter(periods::period_start.eq(period_start))
        .for_update()
        .select((periods::status, periods::revision))
        .first::<(String, i64)>(&mut *conn)
        .await
        .optional()?;
    let local_date = occurred_at.with_timezone(&tokyo_offset).date_naive();
    let year_month = YearMonth::new(
        u16::try_from(local_date.year()).map_err(|_| {
            FundingCommandContextError::InvalidAccountingPeriodStatus(local_date.to_string())
        })?,
        u8::try_from(local_date.month()).map_err(|_| {
            FundingCommandContextError::InvalidAccountingPeriodStatus(local_date.to_string())
        })?,
    )
    .map_err(|_| {
        FundingCommandContextError::InvalidAccountingPeriodStatus(local_date.to_string())
    })?;
    let accounting_period = match persisted_period {
        None => AccountingPeriod::open(year_month, 1),
        Some((status, revision)) => {
            let revision = u64::try_from(revision).map_err(|_| {
                FundingCommandContextError::InvalidAccountingPeriodStatus(revision.to_string())
            })?;
            match status.as_str() {
                "OPEN" => AccountingPeriod::open(year_month, revision),
                "CLOSED" => AccountingPeriod::closed(year_month, revision),
                value => {
                    return Err(FundingCommandContextError::InvalidAccountingPeriodStatus(
                        value.to_owned(),
                    ));
                }
            }
        }
    }
    .map_err(|_| {
        FundingCommandContextError::InvalidAccountingPeriodStatus(local_date.to_string())
    })?;

    let pool_kind = pools::pools
        .filter(pools::household_id.eq(household_id))
        .filter(pools::id.eq(pool_id))
        .select(pools::kind)
        .first::<String>(&mut *conn)
        .await
        .optional()?
        .ok_or(FundingCommandContextError::PoolNotFound)
        .and_then(parse_pool_kind)?;

    let source_account = load_account(&mut *conn, household_id, source_account_id).await?;
    let destination_account =
        load_account(&mut *conn, household_id, destination_account_id).await?;

    Ok(FundingCommandContext {
        accounting_period,
        pool_kind,
        source_account,
        destination_account,
    })
}

async fn load_account(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    account_id: Option<Uuid>,
) -> Result<Option<FundingAccount>, FundingCommandContextError> {
    let Some(account_id) = account_id else {
        return Ok(None);
    };

    let row = public::accounts::table
        .filter(public::accounts::household_id.eq(household_id))
        .filter(public::accounts::id.eq(account_id))
        .select((
            public::accounts::id,
            public::accounts::kind,
            public::accounts::pool_id,
        ))
        .first::<(Uuid, String, Option<Uuid>)>(conn)
        .await
        .optional()?
        .ok_or(FundingCommandContextError::AccountNotFound(account_id))?;
    let (id, kind, account_pool_id) = row;
    let kind = parse_account_kind(kind)?;
    let (pool_kind, personal_owner_id) = match account_pool_id {
        Some(account_pool_id) => {
            let pool_details = public::pools::table
                .filter(public::pools::household_id.eq(household_id))
                .filter(public::pools::id.eq(account_pool_id))
                .select((public::pools::kind, public::pools::current_funding_version))
                .first::<(String, Option<i32>)>(conn)
                .await
                .optional()?
                .ok_or(FundingCommandContextError::PoolNotFound)?;
            let pool_kind = parse_pool_kind(pool_details.0)?;
            let personal_owner_id = if pool_kind == FundingPoolKind::Personal {
                if let Some(version) = pool_details.1 {
                    public::pool_funding_rules::table
                        .filter(public::pool_funding_rules::household_id.eq(household_id))
                        .filter(public::pool_funding_rules::pool_id.eq(account_pool_id))
                        .filter(public::pool_funding_rules::version.eq(version))
                        .select(public::pool_funding_rules::owner_member_id)
                        .first::<Option<Uuid>>(conn)
                        .await
                        .optional()?
                        .flatten()
                } else {
                    None
                }
            } else {
                None
            };
            (Some(pool_kind), personal_owner_id)
        }
        None => (None, None),
    };

    Ok(Some(FundingAccount {
        id: id.to_string(),
        pool_id: account_pool_id.map(|id| id.to_string()),
        kind,
        pool_kind,
        personal_owner_id: personal_owner_id.map(|id| id.to_string()),
    }))
}

fn parse_pool_kind(value: String) -> Result<FundingPoolKind, FundingCommandContextError> {
    match value.as_str() {
        "PERSONAL" => Ok(FundingPoolKind::Personal),
        "SHARED" => Ok(FundingPoolKind::Shared),
        "OTHER" => Ok(FundingPoolKind::Other),
        _ => Err(FundingCommandContextError::InvalidPoolKind(value)),
    }
}

fn parse_account_kind(value: String) -> Result<FundingAccountKind, FundingCommandContextError> {
    match value.as_str() {
        "BANK" => Ok(FundingAccountKind::Bank),
        "CASH" => Ok(FundingAccountKind::Cash),
        "CARD" => Ok(FundingAccountKind::Card),
        "OTHER" => Ok(FundingAccountKind::Other),
        _ => Err(FundingCommandContextError::InvalidAccountKind(value)),
    }
}
