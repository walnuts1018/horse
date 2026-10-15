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
    Transfer(financial_command_result::FinancialCommandResultError),
}

impl From<diesel::result::Error> for NodeQueryError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}
