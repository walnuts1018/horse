use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use bookkeeping_application::{
    AccountId, MemberId,
    record_repayment::{
        Loan as RepaymentLoan, LoanId as RepaymentLoanId, PoolKind as RepaymentPoolKind,
        RepaymentSource,
    },
    record_transfer::{PoolKind as TransferPoolKind, TransferAccount},
};
use bookkeeping_domain::{AccountingPeriod, PoolId, PositiveYen, Yen, YearMonth};

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum FinancialCommandContextError {
    Pool(String),
    Database(diesel::result::Error),
    InvalidTokyoOffset,
    InvalidAccountingPeriod(String),
    InvalidPoolKind(String),
    InvalidAccountKind(String),
    InvalidLoan,
    HouseholdNotFound,
    AccountNotFound,
    LoanNotFound,
}

impl From<diesel::result::Error> for FinancialCommandContextError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TransferCommandContext {
    pub accounting_period: AccountingPeriod,
    pub payment_account: Option<TransferAccount>,
    pub destination_account: Option<TransferAccount>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RepaymentCommandContext {
    pub accounting_period: AccountingPeriod,
    pub loan: RepaymentLoan,
    pub repaid_so_far: Yen,
    pub source: Option<RepaymentSource>,
    pub destination_account: Option<AccountId>,
}

pub async fn load_transfer_command_context(
    pool: &PgPool,
    household_id: Uuid,
    from_member_id: Uuid,
    payment_account_id: Option<Uuid>,
    to_member_id: Uuid,
    destination_account_id: Option<Uuid>,
    occurred_at: DateTime<Utc>,
) -> Result<TransferCommandContext, FinancialCommandContextError> {
    let (_, accounting_period) = load_period(pool, household_id, occurred_at).await?;
    let payment_account =
        load_transfer_account(pool, household_id, from_member_id, payment_account_id).await?;
    let destination_account =
        load_transfer_account(pool, household_id, to_member_id, destination_account_id).await?;
    Ok(TransferCommandContext {
        accounting_period,
        payment_account,
        destination_account,
    })
}

pub async fn load_repayment_command_context(
    pool: &PgPool,
    household_id: Uuid,
    loan_id: Uuid,
    payment_account_id: Option<Uuid>,
    destination_account_id: Option<Uuid>,
    from_member_id: Uuid,
    to_member_id: Uuid,
    occurred_at: DateTime<Utc>,
) -> Result<RepaymentCommandContext, FinancialCommandContextError> {
    let (_, accounting_period) = load_period(pool, household_id, occurred_at).await?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| FinancialCommandContextError::Pool(error.to_string()))?;
    let loan_row = public::transfers::table
        .filter(public::transfers::household_id.eq(household_id))
        .filter(public::transfers::id.eq(loan_id))
        .filter(public::transfers::kind.eq("LOAN"))
        .select((
            public::transfers::from_member_id,
            public::transfers::to_member_id,
            public::transfers::amount_yen,
        ))
        .first::<(Uuid, Uuid, i64)>(&mut *conn)
        .await
        .optional()?
        .ok_or(FinancialCommandContextError::LoanNotFound)?;
    let (lender_id, borrower_id, principal) = loan_row;
    let repayments = public::transfers::table
        .filter(public::transfers::household_id.eq(household_id))
        .filter(public::transfers::loan_id.eq(Some(loan_id)))
        .filter(public::transfers::kind.eq("REPAYMENT"))
        .select(public::transfers::amount_yen)
        .load::<i64>(&mut *conn)
        .await?;
    let repaid = repayments.into_iter().try_fold(0_i64, |sum, amount| {
        sum.checked_add(amount)
            .ok_or(FinancialCommandContextError::InvalidLoan)
    })?;
    let principal =
        PositiveYen::new(principal).map_err(|_| FinancialCommandContextError::InvalidLoan)?;
    let repaid = Yen::new(repaid).map_err(|_| FinancialCommandContextError::InvalidLoan)?;
    let loan = RepaymentLoan {
        id: RepaymentLoanId::new(loan_id.to_string())
            .map_err(|_| FinancialCommandContextError::InvalidLoan)?,
        lender: MemberId::new(lender_id.to_string())
            .map_err(|_| FinancialCommandContextError::InvalidLoan)?,
        borrower: MemberId::new(borrower_id.to_string())
            .map_err(|_| FinancialCommandContextError::InvalidLoan)?,
        principal,
    };
    let source_account =
        load_account_details(pool, household_id, from_member_id, payment_account_id).await?;
    let destination_account =
        load_account_details(pool, household_id, to_member_id, destination_account_id).await?;
    let source: Option<RepaymentSource> = source_account
        .map(
            |(account_id, pool_id, kind)| -> Result<RepaymentSource, FinancialCommandContextError> {
                let pool_kind = match kind.as_str() {
                    "PERSONAL" => Ok(RepaymentPoolKind::Personal),
                    "SHARED" => Ok(RepaymentPoolKind::Shared),
                    _ => Err(FinancialCommandContextError::InvalidPoolKind(kind)),
                }?;
                Ok(RepaymentSource {
                    account_id: AccountId::new(account_id.to_string())
                        .map_err(|_| FinancialCommandContextError::InvalidLoan)?,
                    pool_id: PoolId::new(pool_id.to_string())
                        .map_err(|_| FinancialCommandContextError::InvalidLoan)?,
                    pool_kind,
                })
            },
        )
        .transpose()?;
    let destination_account = destination_account
        .map(|(id, _, _)| AccountId::new(id.to_string()))
        .transpose()
        .map_err(|_| FinancialCommandContextError::InvalidLoan)?;
    Ok(RepaymentCommandContext {
        accounting_period,
        loan,
        repaid_so_far: repaid,
        source,
        destination_account,
    })
}

async fn load_period(
    pool: &PgPool,
    household_id: Uuid,
    occurred_at: DateTime<Utc>,
) -> Result<(NaiveDate, AccountingPeriod), FinancialCommandContextError> {
    let tokyo = FixedOffset::east_opt(9 * 60 * 60)
        .ok_or(FinancialCommandContextError::InvalidTokyoOffset)?;
    let period_start = occurred_at
        .with_timezone(&tokyo)
        .date_naive()
        .with_day(1)
        .ok_or(FinancialCommandContextError::InvalidTokyoOffset)?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| FinancialCommandContextError::Pool(error.to_string()))?;
    public::households::table
        .filter(public::households::id.eq(household_id))
        .select(public::households::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .ok_or(FinancialCommandContextError::HouseholdNotFound)?;
    let persisted_period = public::accounting_periods::table
        .filter(public::accounting_periods::household_id.eq(household_id))
        .filter(public::accounting_periods::period_start.eq(period_start))
        .select((public::accounting_periods::status, public::accounting_periods::revision))
        .first::<(String, i64)>(&mut *conn)
        .await
        .optional()?;
    let year_month = YearMonth::new(
        u16::try_from(period_start.year()).map_err(|_| {
            FinancialCommandContextError::InvalidAccountingPeriod(period_start.to_string())
        })?,
        u8::try_from(period_start.month()).map_err(|_| {
            FinancialCommandContextError::InvalidAccountingPeriod(period_start.to_string())
        })?,
    )
    .map_err(|_| FinancialCommandContextError::InvalidAccountingPeriod(period_start.to_string()))?;
    let accounting_period = match persisted_period {
        None => AccountingPeriod::open(year_month, 1),
        Some((status, revision)) => {
            let revision = u64::try_from(revision).map_err(|_| {
                FinancialCommandContextError::InvalidAccountingPeriod(revision.to_string())
            })?;
            match status.as_str() {
                "OPEN" => AccountingPeriod::open(year_month, revision),
                "CLOSED" => AccountingPeriod::closed(year_month, revision),
                value => {
                    return Err(FinancialCommandContextError::InvalidAccountingPeriod(
                        value.to_owned(),
                    ));
                }
            }
        }
    }
    .map_err(|_| FinancialCommandContextError::InvalidAccountingPeriod(period_start.to_string()))?;
    Ok((period_start, accounting_period))
}

async fn load_transfer_account(
    pool: &PgPool,
    household_id: Uuid,
    owner_member_id: Uuid,
    account_id: Option<Uuid>,
) -> Result<Option<TransferAccount>, FinancialCommandContextError> {
    let Some((id, pool_id, kind)) =
        load_account_details(pool, household_id, owner_member_id, account_id).await?
    else {
        return Ok(None);
    };
    let pool_kind = match kind.as_str() {
        "PERSONAL" => TransferPoolKind::Personal,
        "SHARED" => TransferPoolKind::Shared,
        "OTHER" => TransferPoolKind::Other,
        _ => return Err(FinancialCommandContextError::InvalidPoolKind(kind)),
    };
    Ok(Some(TransferAccount {
        id: AccountId::new(id.to_string())
            .map_err(|_| FinancialCommandContextError::InvalidLoan)?,
        pool_id: PoolId::new(pool_id.to_string())
            .map_err(|_| FinancialCommandContextError::InvalidLoan)?,
        pool_kind,
    }))
}

async fn load_account_details(
    pool: &PgPool,
    household_id: Uuid,
    owner_member_id: Uuid,
    account_id: Option<Uuid>,
) -> Result<Option<(Uuid, Uuid, String)>, FinancialCommandContextError> {
    let Some(account_id) = account_id else {
        return Ok(None);
    };
    let mut conn = pool
        .get()
        .await
        .map_err(|error| FinancialCommandContextError::Pool(error.to_string()))?;
    let (kind, maybe_pool_id) = public::accounts::table
        .filter(public::accounts::household_id.eq(household_id))
        .filter(public::accounts::id.eq(account_id))
        .select((public::accounts::kind, public::accounts::pool_id))
        .first::<(String, Option<Uuid>)>(&mut *conn)
        .await
        .optional()?
        .ok_or(FinancialCommandContextError::AccountNotFound)?;
    if kind == "CARD" {
        return Err(FinancialCommandContextError::InvalidAccountKind(kind));
    }
    let pool_id = maybe_pool_id.ok_or(FinancialCommandContextError::AccountNotFound)?;
    let (pool_kind, funding_version) = public::pools::table
        .filter(public::pools::household_id.eq(household_id))
        .filter(public::pools::id.eq(pool_id))
        .select((public::pools::kind, public::pools::current_funding_version))
        .first::<(String, Option<i32>)>(&mut *conn)
        .await
        .optional()?
        .ok_or(FinancialCommandContextError::AccountNotFound)?;
    if pool_kind == "PERSONAL" {
        let owner = if let Some(version) = funding_version {
            public::pool_funding_rules::table
                .filter(public::pool_funding_rules::household_id.eq(household_id))
                .filter(public::pool_funding_rules::pool_id.eq(pool_id))
                .filter(public::pool_funding_rules::version.eq(version))
                .select(public::pool_funding_rules::owner_member_id)
                .first::<Option<Uuid>>(&mut *conn)
                .await
                .optional()?
                .flatten()
        } else {
            None
        };
        if owner != Some(owner_member_id) {
            return Err(FinancialCommandContextError::InvalidAccountKind(
                "PERSONAL_OWNER_MISMATCH".to_owned(),
            ));
        }
    }
    Ok(Some((account_id, pool_id, pool_kind)))
}
