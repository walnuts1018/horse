use chrono::{DateTime, Datelike, FixedOffset, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use bookkeeping_application::funding_record::{
    AccountingPeriodStatus, FundingAccount, FundingAccountKind, FundingPoolKind,
};

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

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct FundingCommandContext {
    pub period_status: AccountingPeriodStatus,
    pub pool_kind: FundingPoolKind,
    pub source_account: Option<FundingAccount>,
    pub destination_account: Option<FundingAccount>,
}

pub async fn load_funding_command_context(
    pool: &PgPool,
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
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| FundingCommandContextError::Pool(error.to_string()))?;

    use public::accounting_periods::dsl as periods;
    use public::pools::dsl as pools;

    public::households::table
        .filter(public::households::id.eq(household_id))
        .select(public::households::id)
        .first::<Uuid>(&mut *pooled)
        .await
        .optional()?
        .ok_or(FundingCommandContextError::HouseholdNotFound)?;

    let pool_kind = pools::pools
        .filter(pools::household_id.eq(household_id))
        .filter(pools::id.eq(pool_id))
        .select(pools::kind)
        .first::<String>(&mut *pooled)
        .await
        .optional()?
        .ok_or(FundingCommandContextError::PoolNotFound)
        .and_then(parse_pool_kind)?;

    let period_status = periods::accounting_periods
        .filter(periods::household_id.eq(household_id))
        .filter(periods::period_start.eq(period_start))
        .select(periods::status)
        .first::<String>(&mut *pooled)
        .await
        .optional()?
        .map(parse_period_status)
        .transpose()?
        .unwrap_or(AccountingPeriodStatus::Open);

    let source_account = load_account(&mut pooled, household_id, source_account_id).await?;
    let destination_account =
        load_account(&mut pooled, household_id, destination_account_id).await?;

    Ok(FundingCommandContext {
        period_status,
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

fn parse_period_status(
    value: String,
) -> Result<AccountingPeriodStatus, FundingCommandContextError> {
    match value.as_str() {
        "OPEN" => Ok(AccountingPeriodStatus::Open),
        "CLOSED" => Ok(AccountingPeriodStatus::Closed),
        _ => Err(FundingCommandContextError::InvalidAccountingPeriodStatus(
            value,
        )),
    }
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
