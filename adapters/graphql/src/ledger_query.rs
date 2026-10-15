use async_graphql::{Context, Error, ID, Result, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use cross_context_postgres::ledger_query::{
    self as postgres_query, LedgerCursor, LedgerQueryError,
};
use horse_postgres_common::PgPool;
use uuid::Uuid;

use crate::{ledger_entry, pagination::PageInfo};

#[derive(Clone, Debug, SimpleObject)]
pub struct LedgerEdge {
    cursor: String,
    node: ledger_entry::LedgerEntry,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LedgerConnection {
    edges: Vec<LedgerEdge>,
    page_info: PageInfo,
    total_count: i32,
}

pub async fn graphql_ledger(
    ctx: &Context<'_>,
    household_id: ID,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<LedgerConnection> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let after_cursor = decode_cursor(after.as_deref())?;
    let before_cursor = decode_cursor(before.as_deref())?;
    let page = postgres_query::ledger(
        ctx.data::<PgPool>()?,
        postgres_query::LedgerQuery {
            actor_id: actor.0,
            household_id,
            after: after_cursor,
            before: before_cursor,
            first,
            last,
        },
    )
    .await
    .map_err(map_query_error)?;
    let mut edges = Vec::with_capacity(page.rows.len());
    for row in page.rows {
        let id = row.id;
        let node = match row.source_kind.as_str() {
            "purchase" | "PURCHASE" => {
                let purchase_id = row.source_id.to_string();
                let purchase = crate::purchase_mutations::purchase_result(
                    ctx.data::<PgPool>()?,
                    household_id,
                    &purchase_id,
                )
                .await?;
                ledger_entry::purchase_ledger_entry(
                    id,
                    row.occurred_at,
                    row.description,
                    row.amount_yen,
                    "JPY".to_owned(),
                    row.created_at,
                    purchase,
                )
            }
            "correction" | "CORRECTION" => {
                let purchase_id = row
                    .source_purchase_id
                    .ok_or_else(|| {
                        crate::internal_error("correction ledger entry has no source purchase")
                    })?
                    .to_string();
                let purchase = crate::purchase_mutations::purchase_result(
                    ctx.data::<PgPool>()?,
                    household_id,
                    &purchase_id,
                )
                .await?;
                ledger_entry::correction_ledger_entry(
                    id,
                    row.occurred_at,
                    row.description,
                    row.amount_yen,
                    "JPY".to_owned(),
                    row.created_at,
                    purchase,
                )
            }
            "funding" | "FUNDING" => {
                let projection =
                    bookkeeping_postgres::funding_command_result::load_funding_ledger_entry_result(
                        ctx.data::<PgPool>()?,
                        household_id,
                        id,
                    )
                    .await
                    .map_err(crate::internal_error)?;
                ledger_entry::LedgerEntry::FundingLedgerEntry(
                    ledger_entry::FundingLedgerEntry::try_from(projection)
                        .map_err(crate::internal_error)?,
                )
            }
            "transfer" | "TRANSFER" => {
                let projection =
                    bookkeeping_postgres::financial_command_result::load_transfer_ledger_entry_result(
                        ctx.data::<PgPool>()?,
                        household_id,
                        id,
                    )
                    .await
                    .map_err(crate::internal_error)?;
                ledger_entry::LedgerEntry::try_from(projection).map_err(crate::internal_error)?
            }
            kind => {
                return Err(crate::internal_error(format!(
                    "unknown ledger entry kind {kind}"
                )));
            }
        };
        edges.push(LedgerEdge {
            cursor: encode_cursor(id),
            node,
        });
    }
    let total_count = i32::try_from(page.total_count)
        .map_err(|_| crate::internal_error("ledger count exceeds GraphQL Int"))?;
    let has_next_page = if page.backward {
        before.as_deref().is_some_and(|value| !value.is_empty()) && page.before_has_next_page
    } else {
        page.has_extra
    };
    let has_previous_page = if page.backward {
        page.has_extra
    } else if after.as_deref().is_some_and(|value| !value.is_empty()) {
        page.after_has_previous_page
    } else {
        false
    };
    let page_info = PageInfo {
        has_next_page,
        has_previous_page,
        start_cursor: edges.first().map(|edge| edge.cursor.clone()),
        end_cursor: edges.last().map(|edge| edge.cursor.clone()),
    };
    Ok(LedgerConnection {
        edges,
        page_info,
        total_count,
    })
}

fn decode_cursor(cursor: Option<&str>) -> Result<Option<LedgerCursor>> {
    let Some(cursor) = cursor.filter(|cursor| !cursor.is_empty()) else {
        return Ok(None);
    };
    let decoded = URL_SAFE_NO_PAD
        .decode(cursor)
        .map_err(|_| Error::new("invalid cursor"))?;
    let decoded = std::str::from_utf8(&decoded).map_err(|_| Error::new("invalid cursor"))?;
    let id = decoded
        .strip_prefix("LedgerEntry:")
        .ok_or_else(|| Error::new("expected LedgerEntry cursor"))?;
    let id = Uuid::parse_str(id).map_err(|_| Error::new("invalid cursor"))?;
    if id.is_nil() {
        return Err(Error::new("invalid cursor"));
    }
    // The cursor timestamp is resolved from the household-scoped row below.
    Ok(Some(LedgerCursor { id }))
}

fn encode_cursor(id: Uuid) -> String {
    URL_SAFE_NO_PAD.encode(format!("LedgerEntry:{id}"))
}

fn map_query_error(error: LedgerQueryError) -> Error {
    match error {
        LedgerQueryError::HouseholdNotFound | LedgerQueryError::MissingCursor => {
            Error::new("not found")
        }
        LedgerQueryError::InvalidPagination => Error::new("invalid pagination"),
        LedgerQueryError::Pool(_) | LedgerQueryError::Database(_) => crate::internal_error(error),
    }
}
