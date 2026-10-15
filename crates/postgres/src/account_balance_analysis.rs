use chrono::{DateTime, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::RunQueryDsl;
use std::collections::BTreeMap;
use uuid::Uuid;

use crate::{PgPool, schema::public};

const OPENING_BALANCE_SOURCE_KIND: &str = "ACCOUNT_OPENING_BALANCE";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountBalanceChange {
    pub journal_id: Uuid,
    pub source_kind: String,
    pub source_id: Uuid,
    pub occurred_at: DateTime<Utc>,
    pub amount_yen: i64,
    pub balance_yen: i64,
    pub description: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountBalanceAnalysis {
    pub account_id: Uuid,
    pub account_name: String,
    pub account_kind: String,
    pub opening_balance_yen: i64,
    pub closing_balance_yen: i64,
    pub changes: Vec<AccountBalanceChange>,
    pub total_count: usize,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug)]
pub enum AccountBalanceAnalysisError {
    Pool(String),
    Database(DieselError),
    InvalidPeriod,
    InvalidPageSize,
    AccountNotFound,
    InvalidAccountKind(String),
    CursorNotFound,
    BalanceOverflow,
}

impl From<DieselError> for AccountBalanceAnalysisError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

#[derive(Debug)]
struct JournalChangeRow {
    journal_id: Uuid,
    source_kind: String,
    source_id: Uuid,
    occurred_at: DateTime<Utc>,
    amount_yen: i64,
}

pub async fn account_balance_analysis_for_period(
    pool: &PgPool,
    household_id: Uuid,
    account_id: Uuid,
    start: DateTime<Utc>,
    end: DateTime<Utc>,
    after: Option<Uuid>,
    page_size: usize,
) -> Result<AccountBalanceAnalysis, AccountBalanceAnalysisError> {
    if start >= end {
        return Err(AccountBalanceAnalysisError::InvalidPeriod);
    }
    if page_size == 0 {
        return Err(AccountBalanceAnalysisError::InvalidPageSize);
    }

    use public::accounts::dsl as accounts;
    use public::journal_entries::dsl as entries;
    use public::journals::dsl as journals;

    let mut conn = pool
        .get()
        .await
        .map_err(|error| AccountBalanceAnalysisError::Pool(error.to_string()))?;
    let account = accounts::accounts
        .filter(accounts::household_id.eq(household_id))
        .filter(accounts::id.eq(account_id))
        .select((accounts::name, accounts::kind))
        .first::<(String, String)>(&mut *conn)
        .await
        .optional()?
        .ok_or(AccountBalanceAnalysisError::AccountNotFound)?;
    if !matches!(account.1.as_str(), "BANK" | "CASH" | "CARD" | "OTHER") {
        return Err(AccountBalanceAnalysisError::InvalidAccountKind(
            account.1.clone(),
        ));
    }

    let raw_changes = journals::journals
        .inner_join(entries::journal_entries.on(entries::journal_id.eq(journals::id)))
        .filter(journals::household_id.eq(household_id))
        .filter(entries::account_id.eq(Some(account_id)))
        .filter(journals::occurred_at.lt(end))
        .select((
            journals::id,
            journals::source_kind,
            journals::source_id,
            journals::occurred_at,
            entries::debit_yen,
            entries::credit_yen,
        ))
        .load::<(Uuid, String, Uuid, DateTime<Utc>, i64, i64)>(&mut *conn)
        .await?
        .into_iter()
        .collect::<Vec<_>>();

    let mut grouped_changes = BTreeMap::<Uuid, JournalChangeRow>::new();
    for (journal_id, source_kind, source_id, occurred_at, debit_yen, credit_yen) in raw_changes {
        let amount_yen = signed_amount(&account.1, debit_yen, credit_yen)?;
        match grouped_changes.get_mut(&journal_id) {
            Some(change) => {
                change.amount_yen = checked_balance_add(change.amount_yen, amount_yen)?;
            }
            None => {
                grouped_changes.insert(
                    journal_id,
                    JournalChangeRow {
                        journal_id,
                        source_kind,
                        source_id,
                        occurred_at,
                        amount_yen,
                    },
                );
            }
        }
    }

    let opening_at = grouped_changes
        .iter()
        .map(|(_, row)| row)
        .filter(|row| row.source_kind == OPENING_BALANCE_SOURCE_KIND && row.source_id == account_id)
        .map(|row| row.occurred_at)
        .min();
    let mut changes = grouped_changes
        .into_iter()
        .map(|(_, row)| row)
        .filter(|row| opening_at.is_none_or(|opened_at| row.occurred_at >= opened_at))
        .map(|row| {
            let amount_yen = row.amount_yen;
            (row, amount_yen)
        })
        .collect::<Vec<_>>();
    changes.sort_by_key(|(row, _)| {
        (
            row.occurred_at,
            i32::from(row.source_kind != OPENING_BALANCE_SOURCE_KIND),
            row.journal_id,
        )
    });

    let mut balance = 0_i64;
    let mut opening_balance_yen = 0_i64;
    let mut period_changes = Vec::new();
    for (row, amount_yen) in changes {
        balance = checked_balance_add(balance, amount_yen)?;
        if row.occurred_at < start {
            opening_balance_yen = balance;
        } else {
            period_changes.push(AccountBalanceChange {
                journal_id: row.journal_id,
                source_kind: row.source_kind.clone(),
                source_id: row.source_id,
                occurred_at: row.occurred_at,
                amount_yen,
                balance_yen: balance,
                description: if row.source_kind == OPENING_BALANCE_SOURCE_KIND {
                    format!("期首残高: {}", account.0)
                } else {
                    source_description(&row.source_kind)
                },
            });
        }
    }

    let closing_balance_yen = balance;
    let total_count = period_changes.len();
    if let Some(cursor_id) = after {
        let cursor = period_changes
            .iter()
            .find(|change| change.journal_id == cursor_id)
            .ok_or(AccountBalanceAnalysisError::CursorNotFound)?;
        let cursor_key = (
            cursor.occurred_at,
            cursor.source_kind == OPENING_BALANCE_SOURCE_KIND,
            cursor.journal_id,
        );
        period_changes.retain(|change| {
            (
                change.occurred_at,
                change.source_kind == OPENING_BALANCE_SOURCE_KIND,
                change.journal_id,
            ) < cursor_key
        });
    }
    period_changes.reverse();
    let has_next_page = period_changes.len() > page_size;
    period_changes.truncate(page_size);
    load_descriptions(&mut conn, household_id, &mut period_changes).await?;

    Ok(AccountBalanceAnalysis {
        account_id,
        account_name: account.0,
        account_kind: account.1,
        opening_balance_yen,
        closing_balance_yen,
        changes: period_changes,
        total_count,
        has_next_page,
        has_previous_page: after.is_some(),
    })
}

fn signed_amount(
    account_kind: &str,
    debit_yen: i64,
    credit_yen: i64,
) -> Result<i64, AccountBalanceAnalysisError> {
    match account_kind {
        "CARD" => credit_yen.checked_sub(debit_yen),
        "CASH" | "BANK" | "OTHER" => debit_yen.checked_sub(credit_yen),
        _ => return Err(AccountBalanceAnalysisError::AccountNotFound),
    }
    .ok_or(AccountBalanceAnalysisError::BalanceOverflow)
}

fn checked_balance_add(balance: i64, amount: i64) -> Result<i64, AccountBalanceAnalysisError> {
    balance
        .checked_add(amount)
        .ok_or(AccountBalanceAnalysisError::BalanceOverflow)
}

fn source_description(source_kind: &str) -> String {
    match source_kind {
        OPENING_BALANCE_SOURCE_KIND => "初期残高",
        "PURCHASE" => "購入",
        "CORRECTION" => "訂正",
        "FUNDING" => "資金拠出",
        "TRANSFER" => "送金",
        "ACCOUNT_MOVEMENT" => "口座振替",
        "ACCOUNT_INCOME" => "外部入金",
        _ => "仕訳",
    }
    .to_owned()
}

async fn load_descriptions(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    changes: &mut [AccountBalanceChange],
) -> Result<(), AccountBalanceAnalysisError> {
    use public::ledger_entries::dsl as ledger_entries;
    use public::purchase_corrections::dsl as corrections;
    use public::purchases::dsl as purchases;
    use public::transfers::dsl as transfers;

    let ids_for = |kind: &str| {
        changes
            .iter()
            .filter(|change| change.source_kind == kind)
            .map(|change| change.source_id)
            .collect::<Vec<_>>()
    };
    let purchase_ids = ids_for("PURCHASE");
    if !purchase_ids.is_empty() {
        let rows = purchases::purchases
            .filter(purchases::household_id.eq(household_id))
            .filter(purchases::id.eq_any(&purchase_ids))
            .select((purchases::id, purchases::merchant))
            .load::<(Uuid, String)>(conn)
            .await?;
        apply_descriptions(changes, "PURCHASE", rows);
    }

    let correction_ids = ids_for("CORRECTION");
    if !correction_ids.is_empty() {
        let rows = corrections::purchase_corrections
            .filter(corrections::household_id.eq(household_id))
            .filter(corrections::id.eq_any(&correction_ids))
            .select((corrections::id, corrections::reason))
            .load::<(Uuid, String)>(conn)
            .await?;
        apply_descriptions(changes, "CORRECTION", rows);
    }

    let funding_ids = ids_for("FUNDING");
    if !funding_ids.is_empty() {
        let rows = ledger_entries::ledger_entries
            .filter(ledger_entries::household_id.eq(household_id))
            .filter(ledger_entries::id.eq_any(&funding_ids))
            .select((ledger_entries::id, ledger_entries::description))
            .load::<(Uuid, String)>(conn)
            .await?;
        apply_descriptions(changes, "FUNDING", rows);
    }

    let transfer_ids = ids_for("TRANSFER");
    if !transfer_ids.is_empty() {
        let rows = transfers::transfers
            .filter(transfers::household_id.eq(household_id))
            .filter(transfers::id.eq_any(&transfer_ids))
            .select((transfers::id, transfers::description))
            .load::<(Uuid, String)>(conn)
            .await?;
        apply_descriptions(changes, "TRANSFER", rows);
    }
    Ok(())
}

fn apply_descriptions(
    changes: &mut [AccountBalanceChange],
    source_kind: &str,
    descriptions: Vec<(Uuid, String)>,
) {
    for (id, description) in descriptions {
        if let Some(change) = changes
            .iter_mut()
            .find(|change| change.source_kind == source_kind && change.source_id == id)
        {
            change.description = description;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{AccountBalanceAnalysisError, signed_amount};

    #[test]
    fn account_balance_sign_follows_account_kind_and_rejects_overflow() {
        assert_eq!(signed_amount("BANK", 120, 30).unwrap(), 90);
        assert_eq!(signed_amount("CARD", 30, 120).unwrap(), 90);
        assert!(matches!(
            super::checked_balance_add(i64::MAX, 1),
            Err(AccountBalanceAnalysisError::BalanceOverflow)
        ));
    }
}
