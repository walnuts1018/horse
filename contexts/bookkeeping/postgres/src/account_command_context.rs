use std::collections::{BTreeMap, BTreeSet};

use bookkeeping_domain::{AccountingPeriod, AccountingPeriodStatus, YearMonth};
use chrono::{DateTime, Datelike, FixedOffset, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use bookkeeping_application::{account_income::IncomeAccountKind, account_movement::AccountKind};

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum AccountCommandContextError {
    Pool(String),
    Database(diesel::result::Error),
    InvalidTokyoOffset,
    InvalidAccountingPeriodStatus(String),
    InvalidAccountKind(String),
}

impl From<diesel::result::Error> for AccountCommandContextError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AccountCommandContext {
    pub accounting_period: AccountingPeriod,
    pub account_kinds: BTreeMap<Uuid, AccountKind>,
    pub missing_account_ids: BTreeSet<Uuid>,
}

pub async fn load_account_command_context(
    pool: &PgPool,
    household_id: Uuid,
    account_ids: &[Uuid],
    occurred_at: DateTime<Utc>,
) -> Result<AccountCommandContext, AccountCommandContextError> {
    let tokyo_offset =
        FixedOffset::east_opt(9 * 60 * 60).ok_or(AccountCommandContextError::InvalidTokyoOffset)?;
    let period_start = occurred_at
        .with_timezone(&tokyo_offset)
        .date_naive()
        .with_day(1)
        .ok_or(AccountCommandContextError::InvalidTokyoOffset)?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| AccountCommandContextError::Pool(error.to_string()))?;

    use public::accounting_periods::dsl as periods;
    use public::accounts::dsl as accounts;

    let persisted_period = periods::accounting_periods
        .filter(periods::household_id.eq(household_id))
        .filter(periods::period_start.eq(period_start))
        .select((periods::status, periods::revision))
        .first::<(String, i64)>(&mut *pooled)
        .await
        .optional()?;
    let year_month = YearMonth::new(
        u16::try_from(period_start.year()).map_err(|_| {
            AccountCommandContextError::InvalidAccountingPeriodStatus(period_start.to_string())
        })?,
        u8::try_from(period_start.month()).map_err(|_| {
            AccountCommandContextError::InvalidAccountingPeriodStatus(period_start.to_string())
        })?,
    )
    .map_err(|_| {
        AccountCommandContextError::InvalidAccountingPeriodStatus(period_start.to_string())
    })?;
    let accounting_period = match persisted_period {
        None => AccountingPeriod::open(year_month, 1),
        Some((status, revision)) => {
            let revision = u64::try_from(revision).map_err(|_| {
                AccountCommandContextError::InvalidAccountingPeriodStatus(revision.to_string())
            })?;
            match status.as_str() {
                "OPEN" => AccountingPeriod::open(year_month, revision),
                "CLOSED" => AccountingPeriod::closed(year_month, revision),
                value => {
                    return Err(AccountCommandContextError::InvalidAccountingPeriodStatus(
                        value.to_owned(),
                    ));
                }
            }
        }
    }
    .map_err(|_| {
        AccountCommandContextError::InvalidAccountingPeriodStatus(period_start.to_string())
    })?;

    let ids = account_ids.iter().copied().collect::<BTreeSet<_>>();
    let rows = if ids.is_empty() {
        Vec::new()
    } else {
        accounts::accounts
            .filter(accounts::household_id.eq(household_id))
            .filter(accounts::id.eq_any(ids.iter().copied()))
            .select((accounts::id, accounts::kind))
            .load::<(Uuid, String)>(&mut *pooled)
            .await?
    };
    let account_kinds = rows
        .into_iter()
        .map(|(id, kind)| Ok((id, parse_account_kind(kind)?)))
        .collect::<Result<BTreeMap<_, _>, AccountCommandContextError>>()?;
    let loaded_ids = account_kinds.keys().copied().collect::<BTreeSet<_>>();
    let missing_account_ids = ids.difference(&loaded_ids).copied().collect();

    Ok(AccountCommandContext {
        accounting_period,
        account_kinds,
        missing_account_ids,
    })
}

impl AccountCommandContext {
    pub fn income_kind(&self, account_id: Uuid) -> Option<IncomeAccountKind> {
        self.account_kinds.get(&account_id).map(|kind| match kind {
            AccountKind::Bank => IncomeAccountKind::Bank,
            AccountKind::Cash => IncomeAccountKind::Cash,
            AccountKind::Card => IncomeAccountKind::Card,
            AccountKind::Other => IncomeAccountKind::Other,
        })
    }
}

fn parse_account_kind(value: String) -> Result<AccountKind, AccountCommandContextError> {
    match value.as_str() {
        "BANK" => Ok(AccountKind::Bank),
        "CASH" => Ok(AccountKind::Cash),
        "CARD" => Ok(AccountKind::Card),
        "OTHER" => Ok(AccountKind::Other),
        _ => Err(AccountCommandContextError::InvalidAccountKind(value)),
    }
}
