use diesel::{ExpressionMethods, OptionalExtension, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, financial_command_result, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountOpeningBalanceNodeResult {
    pub account_id: Uuid,
    pub account_name: String,
    pub account_kind: String,
}

pub async fn load_account_movement_node(
    pool: &PgPool,
    household_id: Uuid,
    movement_id: Uuid,
) -> Result<Option<crate::account_history_query::AccountMovementProjection>, NodeQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    let Some((id, household_id, kind, occurred_at, from_account_id, to_account_id, amount_yen, description, created_at)) = public::account_movements::table
        .filter(public::account_movements::household_id.eq(household_id))
        .filter(public::account_movements::id.eq(movement_id))
        .select((
            public::account_movements::id,
            public::account_movements::household_id,
            public::account_movements::kind,
            public::account_movements::occurred_at,
            public::account_movements::from_account_id,
            public::account_movements::to_account_id,
            public::account_movements::amount_yen,
            public::account_movements::description,
            public::account_movements::created_at,
        ))
        .first::<(Uuid, Uuid, String, chrono::DateTime<chrono::Utc>, Uuid, Uuid, i64, String, chrono::DateTime<chrono::Utc>)>(&mut *conn)
        .await
        .optional()?
    else {
        return Ok(None);
    };
    let accounts = load_account_projections(&mut conn, household_id, &[from_account_id, to_account_id]).await?;
    let from_account = accounts
        .iter()
        .find(|account| account.id == from_account_id)
        .cloned()
        .ok_or(NodeQueryError::InvalidPersistedData)?;
    let to_account = accounts
        .iter()
        .find(|account| account.id == to_account_id)
        .cloned()
        .ok_or(NodeQueryError::InvalidPersistedData)?;
    Ok(Some(crate::account_history_query::AccountMovementProjection {
        id,
        household_id,
        kind,
        occurred_at,
        from_account_id,
        from_account,
        to_account_id,
        to_account,
        amount_yen,
        description,
        created_at,
    }))
}

pub async fn load_account_income_node(
    pool: &PgPool,
    household_id: Uuid,
    income_id: Uuid,
) -> Result<Option<crate::account_history_query::AccountIncomeProjection>, NodeQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    let Some((id, household_id, account_id, amount_yen, occurred_at, description, created_at)) = public::account_incomes::table
        .filter(public::account_incomes::household_id.eq(household_id))
        .filter(public::account_incomes::id.eq(income_id))
        .select((
            public::account_incomes::id,
            public::account_incomes::household_id,
            public::account_incomes::account_id,
            public::account_incomes::amount_yen,
            public::account_incomes::occurred_at,
            public::account_incomes::description,
            public::account_incomes::created_at,
        ))
        .first::<(Uuid, Uuid, Uuid, i64, chrono::DateTime<chrono::Utc>, String, chrono::DateTime<chrono::Utc>)>(&mut *conn)
        .await
        .optional()?
    else {
        return Ok(None);
    };
    let account = load_account_projections(&mut conn, household_id, &[account_id])
        .await?
        .into_iter()
        .next()
        .ok_or(NodeQueryError::InvalidPersistedData)?;
    Ok(Some(crate::account_history_query::AccountIncomeProjection {
        id,
        household_id,
        account_id,
        account,
        amount_yen,
        occurred_at,
        description,
        created_at,
    }))
}

async fn load_account_projections(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    account_ids: &[Uuid],
) -> Result<Vec<crate::account_history_query::AccountProjection>, NodeQueryError> {
    let rows = public::accounts::table
        .filter(public::accounts::household_id.eq(household_id))
        .filter(public::accounts::id.eq_any(account_ids))
        .select((
            public::accounts::id,
            public::accounts::name,
            public::accounts::kind,
            public::accounts::card_last4,
            public::accounts::pool_id,
        ))
        .load::<(Uuid, String, String, Option<String>, Option<Uuid>)>(conn)
        .await?;
    rows.into_iter()
        .map(|(id, name, kind, card_last4, pool_id)| {
            if !matches!(kind.as_str(), "BANK" | "CASH" | "CARD" | "OTHER") {
                return Err(NodeQueryError::InvalidPersistedData);
            }
            Ok(crate::account_history_query::AccountProjection {
                id,
                name,
                kind,
                card_last4,
                pool_id,
            })
        })
        .collect()
}

pub async fn load_account_opening_balance_node(
    pool: &PgPool,
    household_id: Uuid,
    account_id: Uuid,
) -> Result<Option<AccountOpeningBalanceNodeResult>, NodeQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    let result = public::accounts::table
        .inner_join(
            public::journals::table.on(public::journals::household_id
                .eq(public::accounts::household_id)
                .and(public::journals::source_id.eq(public::accounts::id))
                .and(public::journals::source_kind.eq("ACCOUNT_OPENING_BALANCE"))),
        )
        .filter(public::accounts::household_id.eq(household_id))
        .filter(public::accounts::id.eq(account_id))
        .select((
            public::accounts::id,
            public::accounts::name,
            public::accounts::kind,
        ))
        .first::<(Uuid, String, String)>(&mut *conn)
        .await
        .optional()?
        .map(
            |(account_id, account_name, account_kind)| AccountOpeningBalanceNodeResult {
                account_id,
                account_name,
                account_kind,
            },
        );
    Ok(result)
}

pub async fn load_transfer_ledger_entry_result_by_transfer_id(
    pool: &PgPool,
    household_id: Uuid,
    transfer_id: Uuid,
) -> Result<financial_command_result::TransferLedgerEntryResult, NodeQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    let ledger_entry_id = public::ledger_entries::table
        .filter(public::ledger_entries::household_id.eq(household_id))
        .filter(public::ledger_entries::source_kind.eq("transfer"))
        .filter(public::ledger_entries::source_id.eq(transfer_id))
        .select(public::ledger_entries::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .ok_or(NodeQueryError::MissingLedgerEntry)?;
    drop(conn);
    financial_command_result::load_transfer_ledger_entry_result(pool, household_id, ledger_entry_id)
        .await
        .map_err(NodeQueryError::Transfer)
}

#[derive(Debug)]
pub enum NodeQueryError {
    Pool(String),
    Database(diesel::result::Error),
    MissingLedgerEntry,
    InvalidPersistedData,
    Transfer(financial_command_result::FinancialCommandResultError),
}

impl From<diesel::result::Error> for NodeQueryError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}
