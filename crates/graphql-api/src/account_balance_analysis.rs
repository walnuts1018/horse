use async_graphql::{Context, Error, ID, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use chrono::NaiveDate;
use horse_postgres::{PgPool, account_balance_analysis as postgres_query};
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
    id: ID,
    account: crate::account_mutations::AccountObject,
}

#[derive(Clone, Debug, Union)]
pub enum AccountBalanceSource {
    AccountOpeningBalance(AccountOpeningBalance),
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
    let account_id = decode_node_id(&account_node_id, "Account")?;
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
    if !horse_postgres::household_access::is_active_member(pool, household_id, actor.0)
        .await
        .map_err(crate::internal_error)?
    {
        return Err(Error::new("not found"));
    }
    let analysis = postgres_query::account_balance_analysis_for_period(
        pool,
        household_id,
        account_id,
        start,
        end,
        after_id,
        usize::try_from(first).map_err(crate::internal_error)?,
    )
    .await
    .map_err(map_query_error)?;

    let edges = analysis
        .changes
        .into_iter()
        .map(|change| {
            let source = match change.source_kind.as_str() {
                "ACCOUNT_OPENING_BALANCE" => {
                    AccountBalanceSource::AccountOpeningBalance(AccountOpeningBalance {
                        id: encode_node_id("AccountOpeningBalance", account_id),
                        account: crate::account_mutations::AccountObject {
                            id: encode_node_id("Account", account_id),
                            name: analysis.account_name.clone(),
                            kind: account_kind(&analysis.account_kind)?,
                            card_last4: None,
                            pool_id: None,
                        },
                    })
                }
                _ => return Err(Error::new("unsupported account balance source")),
            };
            Ok(AccountBalanceChangeEdge {
                cursor: encode_cursor(change.journal_id),
                node: AccountBalanceChange {
                    occurred_at: change.occurred_at.into(),
                    source,
                    description: change.description,
                    change_yen: Long(change.amount_yen),
                    balance_yen: Long(change.balance_yen),
                },
            })
        })
        .collect::<Result<Vec<_>>>()?;
    let total_count = i32::try_from(analysis.total_count)
        .map_err(|_| crate::internal_error("account balance change count exceeds GraphQL Int"))?;
    let page_info = PageInfo {
        has_next_page: analysis.has_next_page,
        has_previous_page: analysis.has_previous_page,
        start_cursor: edges.first().map(|edge| edge.cursor.clone()),
        end_cursor: edges.last().map(|edge| edge.cursor.clone()),
    };
    Ok(AccountBalanceAnalysis {
        account_id: encode_node_id("Account", account_id),
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

fn decode_node_id(value: &ID, kind: &str) -> Result<Uuid> {
    let bytes = URL_SAFE_NO_PAD
        .decode(value.as_str())
        .map_err(|_| Error::new("invalid Account node ID"))?;
    let (prefix, raw_id) = bytes
        .split_once(|byte| *byte == b':')
        .ok_or_else(|| Error::new("invalid Account node ID"))?;
    if prefix != kind.as_bytes() || raw_id.len() != 16 {
        return Err(Error::new("invalid Account node ID"));
    }
    Uuid::from_slice(raw_id).map_err(|_| Error::new("invalid Account node ID"))
}

fn encode_node_id(kind: &str, id: Uuid) -> ID {
    let mut payload = Vec::with_capacity(kind.len() + 17);
    payload.extend_from_slice(kind.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    ID(URL_SAFE_NO_PAD.encode(payload))
}

fn encode_cursor(id: Uuid) -> String {
    let mut payload = Vec::with_capacity(b"AccountBalanceChange:".len() + 16);
    payload.extend_from_slice(b"AccountBalanceChange:");
    payload.extend_from_slice(id.as_bytes());
    URL_SAFE_NO_PAD.encode(payload)
}

fn decode_cursor(value: &str) -> std::result::Result<Uuid, ()> {
    let bytes = URL_SAFE_NO_PAD.decode(value).map_err(|_| ())?;
    let (kind, raw_id) = bytes.split_once(|byte| *byte == b':').ok_or(())?;
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

fn map_query_error(error: postgres_query::AccountBalanceAnalysisError) -> Error {
    use postgres_query::AccountBalanceAnalysisError as E;
    match error {
        E::AccountNotFound => Error::new("not found"),
        E::InvalidPeriod | E::InvalidPageSize | E::CursorNotFound => Error::new("bad input"),
        E::BalanceOverflow => Error::new("account balance exceeds int64"),
        other => crate::internal_error(other),
    }
}
