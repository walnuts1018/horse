use chrono::{DateTime, Utc};
use diesel::{ExpressionMethods, JoinOnDsl, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LoanBalanceRow {
    pub id: Uuid,
    pub lender_member_id: Uuid,
    pub borrower_member_id: Uuid,
    pub principal_yen: i64,
    pub repaid_yen: i64,
    pub balance_yen: i64,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug)]
pub enum LoanBalanceQueryError {
    Pool(String),
    Database(diesel::result::Error),
    InvalidBalance,
}

impl From<diesel::result::Error> for LoanBalanceQueryError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

pub async fn list(
    pool: &PgPool,
    household_id: Uuid,
) -> Result<Vec<LoanBalanceRow>, LoanBalanceQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| LoanBalanceQueryError::Pool(error.to_string()))?;
    conn.build_transaction()
        .repeatable_read()
        .read_only()
        .run(async |conn| {
            use public::{ledger_entries, transfers};

            let loans = transfers::table
                .inner_join(
                    ledger_entries::table.on(ledger_entries::source_id
                        .eq(transfers::id)
                        .and(ledger_entries::source_kind.eq("transfer"))),
                )
                .filter(transfers::household_id.eq(household_id))
                .filter(transfers::kind.eq("LOAN"))
                .filter(ledger_entries::household_id.eq(household_id))
                .order((ledger_entries::created_at.desc(), transfers::id.asc()))
                .select((
                    transfers::id,
                    transfers::from_member_id,
                    transfers::to_member_id,
                    transfers::amount_yen,
                    ledger_entries::created_at,
                ))
                .load::<(Uuid, Uuid, Uuid, i64, DateTime<Utc>)>(&mut *conn)
                .await?;
            let ids = loans.iter().map(|loan| loan.0).collect::<Vec<_>>();
            let repayments = if ids.is_empty() {
                Vec::new()
            } else {
                let loan_ids = ids.iter().copied().map(Some).collect::<Vec<_>>();
                transfers::table
                    .filter(transfers::household_id.eq(household_id))
                    .filter(transfers::kind.eq("REPAYMENT"))
                    .filter(transfers::loan_id.eq_any(&loan_ids))
                    .select((transfers::loan_id, transfers::amount_yen))
                    .load::<(Option<Uuid>, i64)>(&mut *conn)
                    .await?
            };
            let mut repaid_by_loan = std::collections::BTreeMap::<Uuid, i64>::new();
            for (loan_id, amount) in repayments {
                let Some(loan_id) = loan_id else {
                    return Err(LoanBalanceQueryError::InvalidBalance);
                };
                let total = repaid_by_loan.entry(loan_id).or_default();
                *total = total
                    .checked_add(amount)
                    .ok_or(LoanBalanceQueryError::InvalidBalance)?;
            }
            loans
                .into_iter()
                .map(
                    |(id, lender_member_id, borrower_member_id, principal_yen, created_at)| {
                        let repaid_yen = repaid_by_loan.get(&id).copied().unwrap_or_default();
                        let balance_yen = principal_yen
                            .checked_sub(repaid_yen)
                            .filter(|balance| {
                                principal_yen >= 0 && repaid_yen >= 0 && *balance >= 0
                            })
                            .ok_or(LoanBalanceQueryError::InvalidBalance)?;
                        Ok(LoanBalanceRow {
                            id,
                            lender_member_id,
                            borrower_member_id,
                            principal_yen,
                            repaid_yen,
                            balance_yen,
                            created_at,
                        })
                    },
                )
                .collect()
        })
        .await
}
