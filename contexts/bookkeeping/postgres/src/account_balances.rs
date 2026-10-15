use diesel::{ExpressionMethods, JoinOnDsl, QueryDsl, result::Error as DieselError};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use bookkeeping_application::account_balances::{
    AccountBalance, AccountBalanceInput, AccountBalancesError, AccountKind,
    JournalEntryBalanceInput, calculate_account_balances,
};

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum AccountBalancesReadError {
    Pool(String),
    Database(DieselError),
    BalanceOverflow,
    InvalidAccountKind(String),
}

impl From<DieselError> for AccountBalancesReadError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

impl From<AccountBalancesError> for AccountBalancesReadError {
    fn from(_: AccountBalancesError) -> Self {
        Self::BalanceOverflow
    }
}

pub async fn account_balances(
    pool: &PgPool,
    household_id: Uuid,
) -> Result<Vec<AccountBalance>, AccountBalancesReadError> {
    use public::accounts::dsl as accounts;
    use public::journal_entries::dsl as entries;
    use public::journals::dsl as journals;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| AccountBalancesReadError::Pool(error.to_string()))?;
    pooled
        .build_transaction()
        .repeatable_read()
        .read_only()
        .run(async |conn| {
            let account_rows = accounts::accounts
                .filter(accounts::household_id.eq(household_id))
                .order((accounts::name.asc(), accounts::id.asc()))
                .select((accounts::id, accounts::kind))
                .load::<(Uuid, String)>(conn)
                .await?;
            let account_inputs = account_rows
                .iter()
                .map(|(account_id, kind)| {
                    let kind = match kind.as_str() {
                        "CARD" => AccountKind::Card,
                        "BANK" | "CASH" | "OTHER" => AccountKind::Other,
                        _ => {
                            return Err(AccountBalancesReadError::InvalidAccountKind(kind.clone()));
                        }
                    };
                    Ok(AccountBalanceInput {
                        account_id: *account_id,
                        kind,
                    })
                })
                .collect::<Result<Vec<_>, _>>()?;

            let entry_rows = entries::journal_entries
                .inner_join(journals::journals.on(journals::id.eq(entries::journal_id)))
                .filter(entries::household_id.eq(household_id))
                .filter(entries::account_id.is_not_null())
                .select((
                    entries::account_id,
                    journals::source_kind,
                    journals::source_id,
                    journals::occurred_at,
                    entries::debit_yen,
                    entries::credit_yen,
                ))
                .load::<(
                    Option<Uuid>,
                    String,
                    Uuid,
                    chrono::DateTime<chrono::Utc>,
                    i64,
                    i64,
                )>(conn)
                .await?;
            let entry_inputs = entry_rows
                .into_iter()
                .filter_map(
                    |(account_id, source_kind, source_id, occurred_at, debit_yen, credit_yen)| {
                        account_id.map(|account_id| JournalEntryBalanceInput {
                            account_id,
                            source_kind,
                            source_id,
                            occurred_at_micros: occurred_at.timestamp_micros(),
                            debit_yen,
                            credit_yen,
                        })
                    },
                )
                .collect::<Vec<_>>();

            calculate_account_balances(&account_inputs, &entry_inputs)
                .map_err(AccountBalancesReadError::from)
        })
        .await
}
