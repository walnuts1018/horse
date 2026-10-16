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
            .filter(public::ledger_entries::source_kind.eq_any(["TRANSFER", "transfer"]))
            .select((
                public::ledger_entries::journal_id,
                public::ledger_entries::occurred_at,
                public::ledger_entries::description,
                public::ledger_entries::amount_yen,
                public::ledger_entries::created_at,
                public::ledger_entries::source_id,
            ))
            .first::<(Uuid, DateTime<Utc>, String, i64, DateTime<Utc>, Uuid)>(&mut *conn)
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

pub async fn load_transfer_ledger_entry_results(
    pool: &PgPool,
    household_ids: &[Uuid],
    ledger_entry_ids: &[Uuid],
) -> Result<std::collections::HashMap<Uuid, TransferLedgerEntryResult>, FinancialCommandResultError>
{
    use std::collections::HashMap;

    let mut result = HashMap::with_capacity(ledger_entry_ids.len());
    if household_ids.is_empty() || ledger_entry_ids.is_empty() {
        return Ok(result);
    }

    let mut conn = pool
        .get()
        .await
        .map_err(|error| FinancialCommandResultError::Pool(error.to_string()))?;
    let ledger_rows = public::ledger_entries::table
        .filter(public::ledger_entries::household_id.eq_any(household_ids))
        .filter(public::ledger_entries::id.eq_any(ledger_entry_ids))
        .filter(public::ledger_entries::source_kind.eq_any(["TRANSFER", "transfer"]))
        .select((
            public::ledger_entries::id,
            public::ledger_entries::household_id,
            public::ledger_entries::journal_id,
            public::ledger_entries::occurred_at,
            public::ledger_entries::description,
            public::ledger_entries::amount_yen,
            public::ledger_entries::created_at,
            public::ledger_entries::source_id,
        ))
        .load::<(
            Uuid,
            Uuid,
            Uuid,
            DateTime<Utc>,
            String,
            i64,
            DateTime<Utc>,
            Uuid,
        )>(&mut *conn)
        .await?;
    if ledger_rows.is_empty() {
        return Ok(result);
    }

    let transfer_ids = ledger_rows
        .iter()
        .map(|(_, _, _, _, _, _, _, source_id)| *source_id)
        .collect::<Vec<_>>();
    let journal_ids = ledger_rows
        .iter()
        .map(|(_, _, journal_id, ..)| *journal_id)
        .collect::<Vec<_>>();
    let transfers = public::transfers::table
        .filter(public::transfers::household_id.eq_any(household_ids))
        .filter(public::transfers::id.eq_any(&transfer_ids))
        .filter(public::transfers::journal_id.eq_any(&journal_ids))
        .select((
            public::transfers::id,
            public::transfers::household_id,
            public::transfers::journal_id,
            public::transfers::kind,
            public::transfers::from_member_id,
            public::transfers::to_member_id,
            public::transfers::loan_id,
        ))
        .load::<(Uuid, Uuid, Uuid, String, Uuid, Uuid, Option<Uuid>)>(&mut *conn)
        .await?;
    let transfers_by_id = transfers
        .into_iter()
        .map(
            |(id, household_id, journal_id, kind, from_member_id, to_member_id, loan_id)| {
                (
                    id,
                    (
                        household_id,
                        journal_id,
                        kind,
                        from_member_id,
                        to_member_id,
                        loan_id,
                    ),
                )
            },
        )
        .collect::<HashMap<_, _>>();
    let mut member_ids = Vec::with_capacity(transfers_by_id.len() * 2);
    let mut loan_ids = Vec::new();
    for (transfer_id, (_, _, kind, from_member_id, to_member_id, loan_id)) in &transfers_by_id {
        member_ids.extend([*from_member_id, *to_member_id]);
        match kind.as_str() {
            "LOAN" => loan_ids.push(*transfer_id),
            "REPAYMENT" => {
                if let Some(loan_id) = loan_id {
                    loan_ids.push(*loan_id);
                }
            }
            "ADVANCE" | "GIFT" => {}
            value => {
                return Err(FinancialCommandResultError::InvalidTransferKind(
                    value.to_owned(),
                ));
            }
        }
    }
    member_ids.sort_unstable();
    member_ids.dedup();
    loan_ids.sort_unstable();
    loan_ids.dedup();

    let (members, roles) = crate::household_identity_query::transfer_members_for_households(
        &mut conn,
        household_ids,
        &member_ids,
    )
    .await?;
    let loan_rows = if loan_ids.is_empty() {
        Vec::new()
    } else {
        public::transfers::table
            .filter(public::transfers::household_id.eq_any(household_ids))
            .filter(public::transfers::id.eq_any(&loan_ids))
            .filter(public::transfers::kind.eq("LOAN"))
            .select((
                public::transfers::id,
                public::transfers::household_id,
                public::transfers::from_member_id,
                public::transfers::to_member_id,
                public::transfers::amount_yen,
                public::transfers::created_at,
            ))
            .load::<(Uuid, Uuid, Uuid, Uuid, i64, DateTime<Utc>)>(&mut *conn)
            .await?
    };
    let repayments = if loan_ids.is_empty() {
        Vec::new()
    } else {
        public::transfers::table
            .filter(public::transfers::household_id.eq_any(household_ids))
            .filter(
                public::transfers::loan_id
                    .eq_any(loan_ids.iter().copied().map(Some).collect::<Vec<_>>()),
            )
            .filter(public::transfers::kind.eq("REPAYMENT"))
            .select((
                public::transfers::household_id,
                public::transfers::loan_id,
                public::transfers::amount_yen,
            ))
            .load::<(Uuid, Option<Uuid>, i64)>(&mut *conn)
            .await?
    };
    let mut repaid_by_loan = HashMap::<(Uuid, Uuid), i64>::new();
    for (household_id, loan_id, amount_yen) in repayments {
        if let Some(loan_id) = loan_id {
            let total = repaid_by_loan
                .get(&(household_id, loan_id))
                .copied()
                .unwrap_or_default()
                .checked_add(amount_yen)
                .ok_or(FinancialCommandResultError::InvalidStoredBalance)?;
            repaid_by_loan.insert((household_id, loan_id), total);
        }
    }
    let mut loans_by_key = HashMap::with_capacity(loan_rows.len());
    for (id, household_id, lender_member_id, borrower_member_id, principal_yen, created_at) in
        loan_rows
    {
        let repaid_yen = repaid_by_loan
            .get(&(household_id, id))
            .copied()
            .unwrap_or_default();
        let balance_yen = principal_yen
            .checked_sub(repaid_yen)
            .ok_or(FinancialCommandResultError::InvalidStoredBalance)?;
        if principal_yen < 0 || repaid_yen < 0 || balance_yen < 0 {
            return Err(FinancialCommandResultError::InvalidStoredBalance);
        }
        loans_by_key.insert(
            (household_id, id),
            LoanBalanceProjection {
                id,
                lender_member_id,
                borrower_member_id,
                principal_yen,
                repaid_yen,
                balance_yen,
                created_at,
            },
        );
    }

    for (
        id,
        household_id,
        _journal_id,
        occurred_at,
        description,
        amount_yen,
        created_at,
        source_id,
    ) in ledger_rows
    {
        let (
            transfer_household,
            transfer_journal,
            kind,
            from_member_id,
            to_member_id,
            transfer_loan_id,
        ) = transfers_by_id
            .get(&source_id)
            .ok_or(FinancialCommandResultError::MissingTransfer)?;
        if *transfer_household != household_id || *transfer_journal != _journal_id {
            return Err(FinancialCommandResultError::MissingTransfer);
        }
        let from_member =
            transfer_member_projection(&members, &roles, household_id, *from_member_id)?;
        let to_member = transfer_member_projection(&members, &roles, household_id, *to_member_id)?;
        let loan_id = match kind.as_str() {
            "LOAN" => Some(source_id),
            "REPAYMENT" => *transfer_loan_id,
            "ADVANCE" | "GIFT" => None,
            value => {
                return Err(FinancialCommandResultError::InvalidTransferKind(
                    value.to_owned(),
                ));
            }
        };
        let loan = loan_id
            .map(|loan_id| {
                loans_by_key
                    .get(&(household_id, loan_id))
                    .cloned()
                    .ok_or(FinancialCommandResultError::MissingLoan)
            })
            .transpose()?;
        result.insert(
            id,
            TransferLedgerEntryResult {
                id,
                kind: kind.clone(),
                occurred_at,
                description,
                amount_yen,
                currency: "JPY".to_owned(),
                created_at,
                from_member,
                to_member,
                loan,
            },
        );
    }
    Ok(result)
}

fn transfer_member_projection(
    members: &std::collections::HashMap<Uuid, String>,
    roles: &std::collections::HashMap<(Uuid, Uuid), String>,
    household_id: Uuid,
    member_id: Uuid,
) -> Result<TransferMemberProjection, FinancialCommandResultError> {
    let display_name = members
        .get(&member_id)
        .cloned()
        .ok_or(FinancialCommandResultError::MissingMember)?;
    Ok(TransferMemberProjection {
        id: member_id,
        display_name,
        role: roles
            .get(&(household_id, member_id))
            .cloned()
            .unwrap_or_else(|| "MEMBER".to_owned()),
    })
}

async fn load_member_projection(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    member_id: Uuid,
) -> Result<TransferMemberProjection, FinancialCommandResultError> {
    let (members, roles) = crate::household_identity_query::transfer_members_for_households(
        conn,
        &[household_id],
        &[member_id],
    )
    .await?;
    let member = members
        .get(&member_id)
        .cloned()
        .ok_or(FinancialCommandResultError::MissingMember)?;
    let role = roles
        .get(&(household_id, member_id))
        .cloned()
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
