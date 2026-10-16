use async_graphql::{Context, Error, ID, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_application::account_balance_analysis_query::{
    AccountBalanceAnalysisQuery, AccountBalanceAnalysisQueryError,
};
use chrono::NaiveDate;
use horse_postgres_common::PgPool;
use uuid::Uuid;

use crate::{DateTime, Long, pagination::PageInfo, policy_mutations::YearMonth};

#[derive(Clone, Debug, SimpleObject)]
pub struct AccountBalanceAnalysis {
    account_id: ID,
    currency: String,
    from: YearMonth,
    to: YearMonth,
    opening_balance_yen: Long,
    closing_balance_yen: Long,
    changes: AccountBalanceChangeConnection,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct AccountBalanceChangeConnection {
    edges: Vec<AccountBalanceChangeEdge>,
    page_info: PageInfo,
    total_count: i32,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct AccountBalanceChangeEdge {
    cursor: String,
    node: AccountBalanceChange,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct AccountBalanceChange {
    occurred_at: DateTime,
    source: AccountBalanceSource,
    description: String,
    change_yen: Long,
    balance_yen: Long,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct AccountOpeningBalance {
    pub(crate) id: ID,
    pub(crate) account: crate::account_mutations::AccountObject,
}

#[derive(Clone, Debug, Union)]
pub enum AccountBalanceSource {
    AccountOpeningBalance(AccountOpeningBalance),
    Purchase(crate::purchase_mutations::PurchaseObject),
    PurchaseWideCorrection(crate::purchase_mutations::PurchaseWideCorrection),
    PurchaseItemCorrection(crate::purchase_mutations::PurchaseItemCorrection),
    CorrectionLedgerEntry(crate::ledger_entry::CorrectionLedgerEntry),
    FundingLedgerEntry(crate::ledger_entry::FundingLedgerEntry),
    PurchaseLedgerEntry(crate::ledger_entry::PurchaseLedgerEntry),
    AdvanceTransferLedgerEntry(crate::ledger_entry::AdvanceTransferLedgerEntry),
    LoanTransferLedgerEntry(crate::ledger_entry::LoanTransferLedgerEntry),
    RepaymentTransferLedgerEntry(crate::ledger_entry::RepaymentTransferLedgerEntry),
    GiftTransferLedgerEntry(crate::ledger_entry::GiftTransferLedgerEntry),
    AccountMovement(crate::account_mutations::AccountMovementObject),
    AccountIncome(crate::account_mutations::AccountIncomeObject),
}

pub async fn resolve_account_balance_analysis(
    ctx: &Context<'_>,
    household_node_id: ID,
    account_node_id: ID,
    from: YearMonth,
    to: YearMonth,
    first: Option<i32>,
    after: Option<String>,
) -> Result<AccountBalanceAnalysis> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_node_id)?;
    let account_id = crate::node_query::decode_global_node_id(&account_node_id, "Account")?;
    let from_date = parse_month(from.as_str())?;
    let to_date = parse_month(to.as_str())?;
    if to_date < from_date || to_date > from_date + chrono::Months::new(120) {
        return Err(Error::new("to must be within ten years after from"));
    }
    let first = first.unwrap_or(20);
    if first <= 0 {
        return Err(Error::new("first must be greater than zero"));
    }
    let start = from_date.and_hms_opt(0, 0, 0).unwrap().and_utc();
    let end = (to_date + chrono::Months::new(1))
        .and_hms_opt(0, 0, 0)
        .unwrap()
        .and_utc();
    let after_id = after
        .as_deref()
        .map(|cursor| {
            decode_cursor(cursor).map_err(|_| Error::new("invalid account balance change cursor"))
        })
        .transpose()?;

    let pool = ctx.data::<PgPool>()?;
    if !household_postgres::household_access::is_active_member(pool, household_id, actor.0)
        .await
        .map_err(crate::internal_error)?
    {
        return Err(Error::new("not found"));
    }
    let analysis = ctx
        .data::<std::sync::Arc<dyn AccountBalanceAnalysisQuery>>()?
        .query(
            household_id,
            account_id,
            start,
            end,
            after_id,
            usize::try_from(first).map_err(crate::internal_error)?,
        )
        .await
        .map_err(map_query_error)?;

    let mut edges = Vec::with_capacity(analysis.changes.len());
    for change in analysis.changes {
        let source = resolve_source(
            ctx,
            household_id,
            account_id,
            change.source_kind.as_str(),
            change.source_id,
            &analysis.account_name,
            &analysis.account_kind,
        )
        .await?;
        edges.push(AccountBalanceChangeEdge {
            cursor: encode_cursor(change.journal_id),
            node: AccountBalanceChange {
                occurred_at: change.occurred_at.into(),
                source,
                description: change.description,
                change_yen: Long(change.amount_yen),
                balance_yen: Long(change.balance_yen),
            },
        });
    }
    let total_count = i32::try_from(analysis.total_count)
        .map_err(|_| crate::internal_error("account balance change count exceeds GraphQL Int"))?;
    let page_info = PageInfo {
        has_next_page: analysis.has_next_page,
        has_previous_page: analysis.has_previous_page,
        start_cursor: edges.first().map(|edge| edge.cursor.clone()),
        end_cursor: edges.last().map(|edge| edge.cursor.clone()),
    };
    Ok(AccountBalanceAnalysis {
        account_id: crate::node_query::encode_global_node_id("Account", account_id),
        currency: "JPY".to_owned(),
        from,
        to,
        opening_balance_yen: Long(analysis.opening_balance_yen),
        closing_balance_yen: Long(analysis.closing_balance_yen),
        changes: AccountBalanceChangeConnection {
            edges,
            page_info,
            total_count,
        },
    })
}

fn parse_month(value: &str) -> Result<NaiveDate> {
    NaiveDate::parse_from_str(&format!("{value}-01"), "%Y-%m-%d")
        .map_err(|_| Error::new("invalid YearMonth"))
}

async fn resolve_source(
    ctx: &Context<'_>,
    household_id: Uuid,
    account_id: Uuid,
    source_kind: &str,
    source_id: Uuid,
    account_name: &str,
    account_kind_value: &str,
) -> Result<AccountBalanceSource> {
    use crate::ledger_entry::{LedgerEntry, Node};

    if source_kind == "ACCOUNT_OPENING_BALANCE" {
        return Ok(AccountBalanceSource::AccountOpeningBalance(
            AccountOpeningBalance {
                id: crate::node_query::encode_global_node_id("AccountOpeningBalance", account_id),
                account: crate::account_mutations::AccountObject {
                    id: crate::node_query::encode_global_node_id("Account", account_id),
                    name: account_name.to_owned(),
                    kind: account_kind(account_kind_value)?,
                    card_last4: None,
                    pool_id: None,
                },
            },
        ));
    }

    let node = if source_kind == "TRANSFER" {
        crate::node_query::resolve_transfer_id_as_ledger_entry(ctx, household_id, source_id).await?
    } else {
        let type_name = match source_kind {
            "PURCHASE" => "Purchase",
            "CORRECTION" => {
                return crate::purchase_mutations::resolve_purchase_correction_node(
                    ctx,
                    household_id,
                    source_id,
                )
                .await
                .and_then(|node| match node {
                    Node::PurchaseWideCorrection(value) => {
                        Ok(AccountBalanceSource::PurchaseWideCorrection(value))
                    }
                    Node::PurchaseItemCorrection(value) => {
                        Ok(AccountBalanceSource::PurchaseItemCorrection(value))
                    }
                    _ => Err(crate::internal_error(
                        "purchase correction resolved to an unexpected node type",
                    )),
                });
            }
            "FUNDING" => "FundingLedgerEntry",
            "ACCOUNT_MOVEMENT" => "AccountMovement",
            "ACCOUNT_INCOME" => "AccountIncome",
            "PURCHASE_LEDGER_ENTRY" => "PurchaseLedgerEntry",
            "CORRECTION_LEDGER_ENTRY" => "CorrectionLedgerEntry",
            other => {
                return Err(crate::internal_error(format!(
                    "unsupported account balance source kind {other}"
                )));
            }
        };
        crate::node_query::resolve_graphql_node(ctx, household_id, type_name, source_id).await?
    };
    match node {
        Node::Purchase(value) if source_kind == "PURCHASE" => {
            Ok(AccountBalanceSource::Purchase(value))
        }
        Node::AccountMovement(value) if source_kind == "ACCOUNT_MOVEMENT" => {
            Ok(AccountBalanceSource::AccountMovement(value))
        }
        Node::AccountIncome(value) if source_kind == "ACCOUNT_INCOME" => {
            Ok(AccountBalanceSource::AccountIncome(value))
        }
        Node::LedgerEntry(LedgerEntry::FundingLedgerEntry(value)) if source_kind == "FUNDING" => {
            Ok(AccountBalanceSource::FundingLedgerEntry(value))
        }
        Node::LedgerEntry(LedgerEntry::PurchaseLedgerEntry(value))
            if source_kind == "PURCHASE_LEDGER_ENTRY" =>
        {
            Ok(AccountBalanceSource::PurchaseLedgerEntry(value))
        }
        Node::LedgerEntry(LedgerEntry::CorrectionLedgerEntry(value))
            if source_kind == "CORRECTION_LEDGER_ENTRY" =>
        {
            Ok(AccountBalanceSource::CorrectionLedgerEntry(value))
        }
        Node::LedgerEntry(LedgerEntry::TransferLedgerEntry(
            crate::ledger_entry::TransferLedgerEntry::AdvanceTransferLedgerEntry(value),
        )) if source_kind == "TRANSFER" => {
            Ok(AccountBalanceSource::AdvanceTransferLedgerEntry(value))
        }
        Node::LedgerEntry(LedgerEntry::TransferLedgerEntry(
            crate::ledger_entry::TransferLedgerEntry::LoanTransferLedgerEntry(value),
        )) if source_kind == "TRANSFER" => Ok(AccountBalanceSource::LoanTransferLedgerEntry(value)),
        Node::LedgerEntry(LedgerEntry::TransferLedgerEntry(
            crate::ledger_entry::TransferLedgerEntry::RepaymentTransferLedgerEntry(value),
        )) if source_kind == "TRANSFER" => {
            Ok(AccountBalanceSource::RepaymentTransferLedgerEntry(value))
        }
        Node::LedgerEntry(LedgerEntry::TransferLedgerEntry(
            crate::ledger_entry::TransferLedgerEntry::GiftTransferLedgerEntry(value),
        )) if source_kind == "TRANSFER" => Ok(AccountBalanceSource::GiftTransferLedgerEntry(value)),
        _ => Err(crate::internal_error(
            "account balance source resolved to an unexpected node type",
        )),
    }
}

fn encode_cursor(id: Uuid) -> String {
    let mut payload = Vec::with_capacity(b"AccountBalanceChange:".len() + 16);
    payload.extend_from_slice(b"AccountBalanceChange:");
    payload.extend_from_slice(id.as_bytes());
    URL_SAFE_NO_PAD.encode(payload)
}

fn decode_cursor(value: &str) -> std::result::Result<Uuid, ()> {
    let bytes = URL_SAFE_NO_PAD.decode(value).map_err(|_| ())?;
    let separator = bytes.iter().position(|byte| *byte == b':').ok_or(())?;
    let (kind, raw_id) = bytes.split_at(separator);
    let raw_id = &raw_id[1..];
    if kind != b"AccountBalanceChange" || raw_id.len() != 16 {
        return Err(());
    }
    Uuid::from_slice(raw_id).map_err(|_| ())
}

fn account_kind(value: &str) -> Result<crate::account_mutations::AccountKind> {
    use crate::account_mutations::AccountKind;
    match value {
        "BANK" => Ok(AccountKind::Bank),
        "CASH" => Ok(AccountKind::Cash),
        "CARD" => Ok(AccountKind::Card),
        "OTHER" => Ok(AccountKind::Other),
        _ => Err(crate::internal_error("invalid persisted account kind")),
    }
}

fn map_query_error(error: AccountBalanceAnalysisQueryError) -> Error {
    use AccountBalanceAnalysisQueryError as E;
    match error {
        E::AccountNotFound => Error::new("not found"),
        E::InvalidInput => Error::new("bad input"),
        E::BalanceOverflow => Error::new("account balance exceeds int64"),
        E::Infrastructure(error) => crate::internal_error(error),
    }
}
