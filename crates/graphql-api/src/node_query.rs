use async_graphql::{Context, Error, ID, Result};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use horse_postgres::{PgPool, financial_command_result, node_query as postgres_query};
use uuid::Uuid;

use crate::ledger_entry::{self, LedgerEntry};

pub(crate) async fn resolve_graphql_node(
    ctx: &Context<'_>,
    household_id: Uuid,
    type_name: &str,
    id: Uuid,
) -> Result<ledger_entry::Node> {
    let actor = crate::actor(ctx)?;
    let pool = ctx.data::<PgPool>()?;
    if !horse_postgres::household_access::is_active_member(pool, household_id, actor.0)
        .await
        .map_err(crate::internal_error)?
    {
        return Err(Error::new("not found"));
    }

    match type_name {
        "AccountOpeningBalance" => {
            let account = postgres_query::load_account_opening_balance_node(pool, household_id, id)
                .await
                .map_err(crate::internal_error)?
                .ok_or_else(|| Error::new("not found"))?;
            let kind = match account.account_kind.as_str() {
                "BANK" => crate::account_mutations::AccountKind::Bank,
                "CASH" => crate::account_mutations::AccountKind::Cash,
                "CARD" => crate::account_mutations::AccountKind::Card,
                "OTHER" => crate::account_mutations::AccountKind::Other,
                value => {
                    return Err(crate::internal_error(format!(
                        "invalid account kind {value}"
                    )));
                }
            };
            Ok(ledger_entry::Node::AccountOpeningBalance(
                crate::account_balance_analysis::AccountOpeningBalance {
                    id: encode_global_node_id("AccountOpeningBalance", account.account_id),
                    account: crate::account_mutations::AccountObject {
                        id: encode_global_node_id("Account", account.account_id),
                        name: account.account_name,
                        kind,
                        card_last4: None,
                        pool_id: None,
                    },
                },
            ))
        }
        "LedgerEntry"
        | "AdvanceTransferLedgerEntry"
        | "LoanTransferLedgerEntry"
        | "RepaymentTransferLedgerEntry"
        | "GiftTransferLedgerEntry" => {
            let value =
                financial_command_result::load_transfer_ledger_entry_result(pool, household_id, id)
                    .await
                    .map_err(crate::internal_error)?;
            let entry = LedgerEntry::try_from(value).map_err(crate::internal_error)?;
            Ok(ledger_entry::Node::LedgerEntry(entry))
        }
        _ => Err(Error::new(format!("unsupported node type {type_name}"))),
    }
}

pub(crate) async fn resolve_transfer_id_as_ledger_entry(
    ctx: &Context<'_>,
    household_id: Uuid,
    transfer_id: Uuid,
) -> Result<ledger_entry::Node> {
    let actor = crate::actor(ctx)?;
    let pool = ctx.data::<PgPool>()?;
    if !horse_postgres::household_access::is_active_member(pool, household_id, actor.0)
        .await
        .map_err(crate::internal_error)?
    {
        return Err(Error::new("not found"));
    }
    let value = postgres_query::load_transfer_ledger_entry_result_by_transfer_id(
        pool,
        household_id,
        transfer_id,
    )
    .await
    .map_err(crate::internal_error)?;
    LedgerEntry::try_from(value)
        .map(ledger_entry::Node::LedgerEntry)
        .map_err(crate::internal_error)
}

pub(crate) fn decode_global_node_id(value: &ID, expected_type: &str) -> Result<Uuid> {
    let bytes = URL_SAFE_NO_PAD
        .decode(value.as_str())
        .map_err(|_| Error::new("invalid node ID"))?;
    let (type_name, raw_id) = bytes
        .split_once(|byte| *byte == b':')
        .ok_or_else(|| Error::new("invalid node ID"))?;
    if type_name != expected_type.as_bytes() || raw_id.len() != 16 {
        return Err(Error::new("invalid node ID"));
    }
    let id = Uuid::from_slice(raw_id).map_err(|_| Error::new("invalid node ID"))?;
    if id.is_nil() {
        return Err(Error::new("invalid node ID"));
    }
    Ok(id)
}

pub(crate) fn encode_global_node_id(type_name: &str, id: Uuid) -> ID {
    let mut payload = Vec::with_capacity(type_name.len() + 17);
    payload.extend_from_slice(type_name.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    ID(URL_SAFE_NO_PAD.encode(payload))
}

pub(crate) fn encode_ledger_entry_id(id: Uuid) -> ID {
    encode_global_node_id("LedgerEntry", id)
}

pub(crate) fn transfer_node_from_result(
    value: financial_command_result::TransferLedgerEntryResult,
) -> Result<ledger_entry::Node> {
    LedgerEntry::try_from(value)
        .map(ledger_entry::Node::LedgerEntry)
        .map_err(crate::internal_error)
}

pub(crate) fn ledger_node_type(entry: &LedgerEntry) -> &'static str {
    match entry {
        LedgerEntry::FundingLedgerEntry(_) => "FundingLedgerEntry",
        LedgerEntry::PurchaseLedgerEntry(_) => "PurchaseLedgerEntry",
        LedgerEntry::CorrectionLedgerEntry(_) => "CorrectionLedgerEntry",
        LedgerEntry::TransferLedgerEntry(
            ledger_entry::TransferLedgerEntry::AdvanceTransferLedgerEntry(_),
        ) => "AdvanceTransferLedgerEntry",
        LedgerEntry::TransferLedgerEntry(
            ledger_entry::TransferLedgerEntry::LoanTransferLedgerEntry(_),
        ) => "LoanTransferLedgerEntry",
        LedgerEntry::TransferLedgerEntry(
            ledger_entry::TransferLedgerEntry::RepaymentTransferLedgerEntry(_),
        ) => "RepaymentTransferLedgerEntry",
        LedgerEntry::TransferLedgerEntry(
            ledger_entry::TransferLedgerEntry::GiftTransferLedgerEntry(_),
        ) => "GiftTransferLedgerEntry",
    }
}
