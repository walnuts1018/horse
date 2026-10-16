use async_graphql::{Context, Error, ID, Result, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_application::account_history_query::{
    AccountHistoryQuery, AccountHistoryQueryError as QueryError, AccountIncomeProjection,
    AccountMovementProjection, AccountProjection,
};
use uuid::Uuid;

use crate::{Actor, account_mutations, pagination::PageInfo};

#[derive(Clone, Debug, SimpleObject)]
pub struct AccountMovementEdge {
    cursor: String,
    node: account_mutations::AccountMovementObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct AccountMovementConnection {
    edges: Vec<AccountMovementEdge>,
    page_info: PageInfo,
    total_count: i32,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct AccountIncomeEdge {
    cursor: String,
    node: account_mutations::AccountIncomeObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct AccountIncomeConnection {
    edges: Vec<AccountIncomeEdge>,
    page_info: PageInfo,
    total_count: i32,
}

pub async fn graphql_account_movements(
    ctx: &Context<'_>,
    household_id: ID,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<AccountMovementConnection> {
    let actor = actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let after_id = decode_cursor(after.as_deref(), "AccountMovement")?;
    let before_id = decode_cursor(before.as_deref(), "AccountMovement")?;
    let page = ctx
        .data::<std::sync::Arc<dyn AccountHistoryQuery>>()?
        .account_movements(actor.0, household_id, after_id, before_id, first, last)
        .await
        .map_err(map_query_error)?;
    let rows = page
        .rows
        .into_iter()
        .map(map_movement)
        .collect::<Result<Vec<_>>>()?;
    let edges = rows
        .into_iter()
        .map(|node| {
            let id = decode_node_id(node.id.as_str(), "AccountMovement")
                .expect("movement node IDs were encoded locally");
            AccountMovementEdge {
                cursor: encode_cursor("AccountMovement", id),
                node,
            }
        })
        .collect::<Vec<_>>();
    let total_count = i32::try_from(page.total_count)
        .map_err(|_| crate::internal_error("account movement count exceeds GraphQL Int"))?;
    Ok(AccountMovementConnection {
        page_info: page_info(&edges, page.has_next_page, page.has_previous_page),
        edges,
        total_count,
    })
}

pub async fn graphql_account_incomes(
    ctx: &Context<'_>,
    household_id: ID,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<AccountIncomeConnection> {
    let actor = actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let after_id = decode_cursor(after.as_deref(), "AccountIncome")?;
    let before_id = decode_cursor(before.as_deref(), "AccountIncome")?;
    let page = ctx
        .data::<std::sync::Arc<dyn AccountHistoryQuery>>()?
        .account_incomes(actor.0, household_id, after_id, before_id, first, last)
        .await
        .map_err(map_query_error)?;
    let rows = page
        .rows
        .into_iter()
        .map(map_income)
        .collect::<Result<Vec<_>>>()?;
    let edges = rows
        .into_iter()
        .map(|node| {
            let id = decode_node_id(node.id.as_str(), "AccountIncome")
                .expect("income node IDs were encoded locally");
            AccountIncomeEdge {
                cursor: encode_cursor("AccountIncome", id),
                node,
            }
        })
        .collect::<Vec<_>>();
    let total_count = i32::try_from(page.total_count)
        .map_err(|_| crate::internal_error("account income count exceeds GraphQL Int"))?;
    Ok(AccountIncomeConnection {
        page_info: page_info(&edges, page.has_next_page, page.has_previous_page),
        edges,
        total_count,
    })
}

fn actor(ctx: &Context<'_>) -> Result<Actor> {
    ctx.data::<Actor>()
        .copied()
        .map_err(|_| Error::new("authentication required"))
}

fn map_movement(
    row: AccountMovementProjection,
) -> Result<account_mutations::AccountMovementObject> {
    use account_mutations::AccountMovementKind;

    let kind = match row.kind.as_str() {
        "TRANSFER" => AccountMovementKind::Transfer,
        "CARD_BILL_PAYMENT" => AccountMovementKind::CardBillPayment,
        _ => {
            return Err(crate::internal_error(
                "invalid persisted account movement kind",
            ));
        }
    };
    Ok(account_mutations::AccountMovementObject {
        id: encode_node_id("AccountMovement", row.id),
        household_id: encode_node_id("Household", row.household_id),
        kind,
        occurred_at: row.occurred_at.into(),
        from_account_id: encode_node_id("Account", row.from_account_id),
        from_account: map_account(row.from_account)?,
        to_account_id: encode_node_id("Account", row.to_account_id),
        to_account: map_account(row.to_account)?,
        amount_yen: crate::Long(row.amount_yen),
        currency: "JPY".to_owned(),
        description: row.description,
        created_at: row.created_at.into(),
    })
}

fn map_income(row: AccountIncomeProjection) -> Result<account_mutations::AccountIncomeObject> {
    Ok(account_mutations::AccountIncomeObject {
        id: encode_node_id("AccountIncome", row.id),
        household_id: encode_node_id("Household", row.household_id),
        account_id: encode_node_id("Account", row.account_id),
        amount_yen: crate::Long(row.amount_yen),
        currency: "JPY".to_owned(),
        occurred_at: row.occurred_at.into(),
        description: row.description,
        created_at: row.created_at.into(),
    })
}

fn map_account(row: AccountProjection) -> Result<account_mutations::AccountObject> {
    Ok(row.into())
}

impl From<AccountProjection> for account_mutations::AccountObject {
    fn from(row: AccountProjection) -> Self {
        let kind = match row.kind.as_str() {
            "BANK" => account_mutations::AccountKind::Bank,
            "CASH" => account_mutations::AccountKind::Cash,
            "CARD" => account_mutations::AccountKind::Card,
            _ => account_mutations::AccountKind::Other,
        };
        Self {
            id: encode_node_id("Account", row.id),
            name: row.name,
            kind,
            card_last4: row.card_last4,
            pool_id: row.pool_id.map(|id| encode_node_id("Pool", id)),
        }
    }
}

fn page_info<E>(edges: &[E], has_next_page: bool, has_previous_page: bool) -> PageInfo
where
    E: EdgeCursor,
{
    PageInfo {
        has_next_page,
        has_previous_page,
        start_cursor: edges.first().map(EdgeCursor::cursor),
        end_cursor: edges.last().map(EdgeCursor::cursor),
    }
}

trait EdgeCursor {
    fn cursor(&self) -> String;
}

impl EdgeCursor for AccountMovementEdge {
    fn cursor(&self) -> String {
        self.cursor.clone()
    }
}

impl EdgeCursor for AccountIncomeEdge {
    fn cursor(&self) -> String {
        self.cursor.clone()
    }
}

fn decode_cursor(cursor: Option<&str>, kind: &str) -> Result<Option<Uuid>> {
    let Some(cursor) = cursor.filter(|cursor| !cursor.is_empty()) else {
        return Ok(None);
    };
    let bytes = URL_SAFE_NO_PAD
        .decode(cursor)
        .map_err(|_| Error::new("invalid cursor"))?;
    let value = std::str::from_utf8(&bytes).map_err(|_| Error::new("invalid cursor"))?;
    let raw_id = value
        .strip_prefix(&format!("{kind}:"))
        .ok_or_else(|| Error::new("invalid cursor"))?;
    Uuid::parse_str(raw_id)
        .map(Some)
        .map_err(|_| Error::new("invalid cursor"))
}

fn encode_cursor(kind: &str, id: Uuid) -> String {
    URL_SAFE_NO_PAD.encode(format!("{kind}:{id}"))
}

fn encode_node_id(kind: &str, id: Uuid) -> ID {
    let mut bytes = Vec::with_capacity(kind.len() + 17);
    bytes.extend_from_slice(kind.as_bytes());
    bytes.push(b':');
    bytes.extend_from_slice(id.as_bytes());
    ID(URL_SAFE_NO_PAD.encode(bytes))
}

fn decode_node_id(value: &str, expected_kind: &str) -> std::result::Result<Uuid, ()> {
    let bytes = URL_SAFE_NO_PAD.decode(value).map_err(|_| ())?;
    let separator = bytes.iter().position(|byte| *byte == b':').ok_or(())?;
    let (kind, payload_with_separator) = bytes.split_at(separator);
    let payload = &payload_with_separator[1..];
    if kind != expected_kind.as_bytes() || payload.len() != 16 {
        return Err(());
    }
    Uuid::from_slice(payload).map_err(|_| ())
}

fn map_query_error(error: QueryError) -> Error {
    match error {
        QueryError::HouseholdNotFound => Error::new("household not found"),
        QueryError::InvalidPagination => Error::new("invalid pagination"),
        QueryError::InvalidCursor => Error::new("invalid cursor"),
        QueryError::Infrastructure(error) => crate::internal_error(error),
    }
}
