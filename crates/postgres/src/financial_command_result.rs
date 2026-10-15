use chrono::{DateTime, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum FinancialCommandResultError {
    Pool(String),
    Database(diesel::result::Error),
    MissingLedgerEntry,
    MissingTransfer,
    MissingMember,
    MissingLoan,
    InvalidTransferKind(String),
    InvalidStoredBalance,
}

impl From<diesel::result::Error> for FinancialCommandResultError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TransferMemberProjection {
    pub id: Uuid,
    pub display_name: String,
    pub role: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LoanBalanceProjection {
    pub id: Uuid,
    pub lender_member_id: Uuid,
    pub borrower_member_id: Uuid,
    pub principal_yen: i64,
    pub repaid_yen: i64,
    pub balance_yen: i64,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TransferLedgerEntryResult {
    pub id: Uuid,
    pub kind: String,
    pub occurred_at: DateTime<Utc>,
    pub description: String,
    pub amount_yen: i64,
    pub currency: String,
    pub created_at: DateTime<Utc>,
    pub from_member: TransferMemberProjection,
    pub to_member: TransferMemberProjection,
    pub loan: Option<LoanBalanceProjection>,
}

pub async fn load_transfer_ledger_entry_result(
    pool: &PgPool,
    household_id: Uuid,
    ledger_entry_id: Uuid,
) -> Result<TransferLedgerEntryResult, FinancialCommandResultError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| FinancialCommandResultError::Pool(error.to_string()))?;
    let (journal_id, occurred_at, description, amount_yen, created_at, source_id) =
        public::ledger_entries::table
            .filter(public::ledger_entries::household_id.eq(household_id))
            .filter(public::ledger_entries::id.eq(ledger_entry_id))
            .filter(public::ledger_entries::source_kind.eq("transfer"))
            .select((
                public::ledger_entries::journal_id,
                public::ledger_entries::occurred_at,
                public::ledger_entries::description,
                public::ledger_entries::amount_yen,
                public::ledger_entries::created_at,
                public::ledger_entries::source_id,
            ))
            .first::<(Uuid, DateTime<Utc>, String, i64, DateTime<Utc>, Uuid)>(
                &mut *conn,
            )
            .await
            .optional()?
            .ok_or(FinancialCommandResultError::MissingLedgerEntry)?;
    let (kind, from_member_id, to_member_id, loan_id) = public::transfers::table
        .filter(public::transfers::household_id.eq(household_id))
        .filter(public::transfers::id.eq(source_id))
        .filter(public::transfers::journal_id.eq(journal_id))
        .select((
            public::transfers::kind,
            public::transfers::from_member_id,
            public::transfers::to_member_id,
            public::transfers::loan_id,
        ))
        .first::<(String, Uuid, Uuid, Option<Uuid>)>(&mut *conn)
        .await
        .optional()?
        .ok_or(FinancialCommandResultError::MissingTransfer)?;
    if !matches!(kind.as_str(), "ADVANCE" | "LOAN" | "REPAYMENT" | "GIFT") {
        return Err(FinancialCommandResultError::InvalidTransferKind(kind));
    }
    let from_member = load_member_projection(&mut conn, household_id, from_member_id).await?;
    let to_member = load_member_projection(&mut conn, household_id, to_member_id).await?;
    let loan_id = match kind.as_str() {
        "LOAN" => Some(source_id),
        "REPAYMENT" => loan_id,
        _ => None,
    };
    let loan = match loan_id {
        Some(loan_id) => Some(load_loan_balance(&mut conn, household_id, loan_id).await?),
        None => None,
    };
    Ok(TransferLedgerEntryResult {
        id: ledger_entry_id,
        kind,
        occurred_at,
        description,
        amount_yen,
        currency: "JPY".to_owned(),
        created_at,
        from_member,
        to_member,
        loan,
    })
}

async fn load_member_projection(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    member_id: Uuid,
) -> Result<TransferMemberProjection, FinancialCommandResultError> {
    let member = public::members::table
        .filter(public::members::id.eq(member_id))
        .select(public::members::display_name)
        .first::<String>(conn)
        .await
        .optional()?
        .ok_or(FinancialCommandResultError::MissingMember)?;
    let role = public::household_members::table
        .filter(public::household_members::household_id.eq(household_id))
        .filter(public::household_members::member_id.eq(member_id))
        .select(public::household_members::role)
        .first::<String>(conn)
        .await
        .optional()?
        .unwrap_or_else(|| "MEMBER".to_owned());
    Ok(TransferMemberProjection {
        id: member_id,
        display_name: member,
        role,
    })
}

async fn load_loan_balance(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    loan_id: Uuid,
) -> Result<LoanBalanceProjection, FinancialCommandResultError> {
    let (lender, borrower, principal, created_at) = public::transfers::table
        .filter(public::transfers::household_id.eq(household_id))
        .filter(public::transfers::id.eq(loan_id))
        .filter(public::transfers::kind.eq("LOAN"))
        .select((
            public::transfers::from_member_id,
            public::transfers::to_member_id,
            public::transfers::amount_yen,
            public::transfers::created_at,
        ))
        .first::<(Uuid, Uuid, i64, DateTime<Utc>)>(conn)
        .await
        .optional()?
        .ok_or(FinancialCommandResultError::MissingLoan)?;
    let repayments = public::transfers::table
        .filter(public::transfers::household_id.eq(household_id))
        .filter(public::transfers::loan_id.eq(Some(loan_id)))
        .filter(public::transfers::kind.eq("REPAYMENT"))
        .select(public::transfers::amount_yen)
        .load::<i64>(conn)
        .await?;
    let repaid = repayments.into_iter().try_fold(0_i64, |total, value| {
        total
            .checked_add(value)
            .ok_or(FinancialCommandResultError::InvalidStoredBalance)
    })?;
    let balance = principal
        .checked_sub(repaid)
        .ok_or(FinancialCommandResultError::InvalidStoredBalance)?;
    if principal < 0 || repaid < 0 || balance < 0 {
        return Err(FinancialCommandResultError::InvalidStoredBalance);
    }
    Ok(LoanBalanceProjection {
        id: loan_id,
        lender_member_id: lender,
        borrower_member_id: borrower,
        principal_yen: principal,
        repaid_yen: repaid,
        balance_yen: balance,
        created_at,
    })
}
