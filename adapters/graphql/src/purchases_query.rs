use async_graphql::{Context, Enum, Error, ID, InputObject, Result, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_postgres::{
    purchase_command_result as postgres_purchase_result, purchase_search_query as postgres_query,
};
use chrono::{DateTime as ChronoDateTime, SecondsFormat, Utc};
use horse_postgres_common::PgPool;
use uuid::Uuid;

use crate::{Long, pagination::PageInfo, purchase_mutations};

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum PurchaseAmountTarget {
    Purchase,
    Item,
}

#[derive(Clone, Debug, InputObject)]
pub struct PurchaseFilter {
    household_id: ID,
    from: Option<crate::DateTime>,
    to: Option<crate::DateTime>,
    origin: Option<purchase_mutations::PurchaseOrigin>,
    has_evidence: Option<bool>,
    search: Option<String>,
    category_id: Option<ID>,
    uncategorized: Option<bool>,
    tag_id: Option<ID>,
    payment_account_id: Option<ID>,
    pool_id: Option<ID>,
    minimum_amount: Option<Long>,
    maximum_amount: Option<Long>,
    amount_target: Option<PurchaseAmountTarget>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct PurchaseEdge {
    cursor: String,
    node: purchase_mutations::PurchaseObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct PurchaseConnection {
    edges: Vec<PurchaseEdge>,
    page_info: PageInfo,
    total_count: i32,
    sync_cursor: String,
}

pub async fn graphql_purchases(
    ctx: &Context<'_>,
    filter: PurchaseFilter,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<PurchaseConnection> {
    let actor = crate::actor(ctx)?;
    let household_id = decode_node_id(&filter.household_id, "Household")?;
    let category_id = filter
        .category_id
        .as_ref()
        .map(|id| decode_node_id(id, "Category"))
        .transpose()?;
    let tag_id = filter
        .tag_id
        .as_ref()
        .map(|id| decode_node_id(id, "Tag"))
        .transpose()?;
    let payment_account_id = filter
        .payment_account_id
        .as_ref()
        .map(|id| decode_node_id(id, "Account"))
        .transpose()?;
    let pool_id = filter
        .pool_id
        .as_ref()
        .map(|id| decode_node_id(id, "Pool"))
        .transpose()?;
    let uncategorized = filter.uncategorized.unwrap_or(false);
    if category_id.is_some() && uncategorized {
        return Err(Error::new(
            "categoryId and uncategorized cannot both be set",
        ));
    }
    if filter
        .minimum_amount
        .zip(filter.maximum_amount)
        .is_some_and(|(minimum, maximum)| minimum.0 > maximum.0)
    {
        return Err(Error::new("minimumAmount cannot exceed maximumAmount"));
    }
    let from = filter
        .from
        .as_ref()
        .map(crate::DateTime::parse_utc)
        .transpose()
        .map_err(|_| Error::new("invalid from date"))?;
    let to = filter
        .to
        .as_ref()
        .map(crate::DateTime::parse_utc)
        .transpose()
        .map_err(|_| Error::new("invalid to date"))?;
    let amount_target = match filter
        .amount_target
        .unwrap_or(PurchaseAmountTarget::Purchase)
    {
        PurchaseAmountTarget::Purchase => postgres_query::PurchaseAmountTarget::Purchase,
        PurchaseAmountTarget::Item => postgres_query::PurchaseAmountTarget::Item,
    };
    let after_cursor = after
        .as_deref()
        .filter(|value| !value.is_empty())
        .map(decode_purchase_cursor)
        .transpose()?;
    let before_cursor = before
        .as_deref()
        .filter(|value| !value.is_empty())
        .map(decode_purchase_cursor)
        .transpose()?;
    let page = postgres_query::purchases(
        ctx.data::<PgPool>()?,
        actor.0,
        postgres_query::PurchaseSearchFilter {
            household_id,
            from,
            to,
            origin: filter.origin.map(origin_name),
            has_evidence: filter.has_evidence,
            search: filter.search.clone(),
            category_id,
            uncategorized,
            tag_id,
            payment_account_id,
            pool_id,
            minimum_amount: filter.minimum_amount.map(|value| value.0),
            maximum_amount: filter.maximum_amount.map(|value| value.0),
            amount_target,
        },
        after_cursor,
        before_cursor,
        first,
        last,
    )
    .await
    .map_err(map_query_error)?;
    let pool = ctx.data::<PgPool>()?;
    let mut edges = Vec::with_capacity(page.rows.len());
    for row in &page.rows {
        let projection =
            postgres_purchase_result::load_purchase_command_result(pool, household_id, row.id)
                .await
                .map_err(crate::internal_error)?;
        let mut node = purchase_mutations::PurchaseObject::try_from(projection)
            .map_err(crate::internal_error)?;
        if amount_target == postgres_query::PurchaseAmountTarget::Item
            && has_item_filters(&filter, category_id, uncategorized)
        {
            filter_purchase_items(
                &mut node,
                &filter,
                category_id,
                tag_id,
                pool_id,
                uncategorized,
            );
        }
        edges.push(PurchaseEdge {
            cursor: encode_purchase_cursor(row.occurred_at, row.id),
            node,
        });
    }
    let total_count = i32::try_from(page.total_count)
        .map_err(|_| crate::internal_error("purchase count exceeds GraphQL Int"))?;
    let mut page_info = PageInfo {
        has_next_page: !page.backward && page.has_extra,
        has_previous_page: page.backward && page.has_extra,
        start_cursor: edges.first().map(|edge| edge.cursor.clone()),
        end_cursor: edges.last().map(|edge| edge.cursor.clone()),
    };
    if !page.backward && after.as_deref().is_some_and(|value| !value.is_empty()) {
        page_info.has_previous_page = true;
    }
    if page.backward && before.as_deref().is_some_and(|value| !value.is_empty()) {
        page_info.has_next_page = true;
    }
    Ok(PurchaseConnection {
        edges,
        page_info,
        total_count,
        sync_cursor: page.sync_cursor.to_string(),
    })
}

fn origin_name(value: purchase_mutations::PurchaseOrigin) -> String {
    match value {
        purchase_mutations::PurchaseOrigin::Ai => "AI",
        purchase_mutations::PurchaseOrigin::Manual => "MANUAL",
        purchase_mutations::PurchaseOrigin::Unknown => "UNKNOWN",
    }
    .to_owned()
}

fn decode_node_id(value: &ID, expected_kind: &str) -> Result<Uuid> {
    let bytes = URL_SAFE_NO_PAD
        .decode(value.as_str())
        .map_err(|_| Error::new("invalid node ID"))?;
    let separator = bytes
        .iter()
        .position(|byte| *byte == b':')
        .ok_or_else(|| Error::new("invalid node ID"))?;
    let (kind, payload_with_separator) = bytes.split_at(separator);
    let payload = &payload_with_separator[1..];
    if kind != expected_kind.as_bytes() || payload.len() != 16 {
        return Err(Error::new("invalid node ID"));
    }
    let id = Uuid::from_slice(payload).map_err(|_| Error::new("invalid node ID"))?;
    if id.is_nil() {
        return Err(Error::new("invalid node ID"));
    }
    Ok(id)
}

fn decode_purchase_cursor(value: &str) -> Result<postgres_query::PurchaseSearchCursor> {
    let decoded = URL_SAFE_NO_PAD
        .decode(value)
        .map_err(|_| Error::new("invalid cursor"))?;
    let decoded = std::str::from_utf8(&decoded).map_err(|_| Error::new("invalid cursor"))?;
    let payload = decoded
        .strip_prefix("Purchase:")
        .ok_or_else(|| Error::new("invalid cursor"))?;
    let (position, id) = match payload.split_once('|') {
        Some((position, id)) => {
            let occurred_at = ChronoDateTime::parse_from_rfc3339(position)
                .map_err(|_| Error::new("invalid cursor"))?
                .with_timezone(&Utc);
            (Some(occurred_at), id)
        }
        None => (None, payload),
    };
    let id = Uuid::parse_str(id).map_err(|_| Error::new("invalid cursor"))?;
    if id.is_nil() {
        return Err(Error::new("invalid cursor"));
    }
    Ok(postgres_query::PurchaseSearchCursor {
        id,
        occurred_at: position,
    })
}

fn encode_purchase_cursor(occurred_at: ChronoDateTime<Utc>, id: Uuid) -> String {
    let position = occurred_at.to_rfc3339_opts(SecondsFormat::AutoSi, true);
    URL_SAFE_NO_PAD.encode(format!("Purchase:{position}|{id}"))
}

fn has_item_filters(
    filter: &PurchaseFilter,
    category_id: Option<Uuid>,
    uncategorized: bool,
) -> bool {
    filter.minimum_amount.is_some()
        || filter.maximum_amount.is_some()
        || category_id.is_some()
        || uncategorized
        || filter.tag_id.is_some()
        || filter.pool_id.is_some()
        || filter
            .search
            .as_deref()
            .is_some_and(|value| !value.trim().is_empty())
}

fn filter_purchase_items(
    purchase: &mut purchase_mutations::PurchaseObject,
    filter: &PurchaseFilter,
    category_id: Option<Uuid>,
    tag_id: Option<Uuid>,
    pool_id: Option<Uuid>,
    uncategorized: bool,
) {
    let merchant_matches = filter.search.as_deref().is_some_and(|search| {
        let search = search.trim().to_lowercase();
        !search.is_empty() && purchase.merchant.to_lowercase().contains(&search)
    });
    let search = filter
        .search
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(str::to_lowercase);
    purchase.items.retain(|item| {
        if filter
            .minimum_amount
            .is_some_and(|minimum| item.amount_yen.0 < minimum.0)
            || filter
                .maximum_amount
                .is_some_and(|maximum| item.amount_yen.0 > maximum.0)
        {
            return false;
        }
        if let Some(category_id) = category_id {
            let expected = encode_node_id("Category", category_id);
            if item.category_id.as_ref() != Some(&expected) {
                return false;
            }
        } else if uncategorized && item.category_id.is_some() {
            return false;
        }
        if let Some(tag_id) = tag_id {
            let expected = encode_node_id("Tag", tag_id);
            if !item.tags.iter().any(|tag| tag.id == expected) {
                return false;
            }
        }
        if let Some(pool_id) = pool_id {
            let expected = encode_node_id("Pool", pool_id);
            if !item
                .allocations
                .iter()
                .any(|allocation| allocation.pool_id == expected)
            {
                return false;
            }
        }
        if let Some(search) = &search {
            if !merchant_matches && !item.name.to_lowercase().contains(search) {
                return false;
            }
        }
        true
    });
}

fn encode_node_id(kind: &str, id: Uuid) -> ID {
    let mut payload = Vec::with_capacity(kind.len() + 17);
    payload.extend_from_slice(kind.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    ID(URL_SAFE_NO_PAD.encode(payload))
}

fn map_query_error(error: postgres_query::PurchaseSearchQueryError) -> Error {
    use postgres_query::PurchaseSearchQueryError as E;
    match error {
        E::HouseholdNotFound => Error::new("household not found"),
        E::InvalidPagination => Error::new("invalid pagination"),
        E::InvalidCursor => Error::new("invalid cursor"),
        E::CursorNotFound => Error::new("cursor not found"),
        E::InvalidFilter => Error::new("invalid purchase filter"),
        error @ (E::Pool(_) | E::Database(_)) => crate::internal_error(error),
    }
}
