use async_graphql::{Context, Error, ID, Result, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use horse_postgres::{PgPool, allocation_rule_query as postgres_query};
use uuid::Uuid;

use crate::{pagination::PageInfo, pool_mutations::AllocationRuleObject};

#[derive(Clone, Debug, SimpleObject)]
pub struct AllocationRuleEdge {
    cursor: String,
    node: AllocationRuleObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct AllocationRuleConnection {
    edges: Vec<AllocationRuleEdge>,
    page_info: PageInfo,
    total_count: i32,
}

pub async fn graphql_allocation_rules(
    ctx: &Context<'_>,
    household_id: ID,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<AllocationRuleConnection> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let after_id = decode_cursor(after.as_deref())?;
    let before_id = decode_cursor(before.as_deref())?;
    if first.is_some() && last.is_some() {
        return Err(Error::new("first and last cannot be used together"));
    }
    let backward = last.is_some();
    let page_size = last.or(first).unwrap_or(50);
    if !(1..=100).contains(&page_size) {
        return Err(Error::new("page size must be between 1 and 100"));
    }
    let page = postgres_query::allocation_rules(
        ctx.data::<PgPool>()?,
        actor.0,
        household_id,
        after_id,
        before_id,
        page_size as usize,
        backward,
    )
    .await
    .map_err(map_query_error)?;
    let edges = page
        .rows
        .into_iter()
        .map(|projection| {
            let cursor = encode_cursor(projection.id);
            let node = crate::pool_mutations::map_allocation_rule_projection(projection)?;
            Ok(AllocationRuleEdge { cursor, node })
        })
        .collect::<Result<Vec<_>>>()?;
    let total_count =
        i32::try_from(page.total_count).map_err(|error| crate::internal_error(error))?;
    Ok(AllocationRuleConnection {
        page_info: PageInfo {
            has_next_page: page.has_next_page,
            has_previous_page: page.has_previous_page,
            start_cursor: edges.first().map(|edge| edge.cursor.clone()),
            end_cursor: edges.last().map(|edge| edge.cursor.clone()),
        },
        edges,
        total_count,
    })
}

fn decode_cursor(cursor: Option<&str>) -> Result<Option<Uuid>> {
    let Some(cursor) = cursor.filter(|cursor| !cursor.is_empty()) else {
        return Ok(None);
    };
    let decoded = URL_SAFE_NO_PAD
        .decode(cursor)
        .map_err(|_| Error::new("invalid cursor"))?;
    let value = std::str::from_utf8(&decoded).map_err(|_| Error::new("invalid cursor"))?;
    let id = value
        .strip_prefix("AllocationRule:")
        .ok_or_else(|| Error::new("invalid cursor"))?;
    Uuid::parse_str(id)
        .map(Some)
        .map_err(|_| Error::new("invalid cursor"))
}

fn encode_cursor(id: Uuid) -> String {
    URL_SAFE_NO_PAD.encode(format!("AllocationRule:{id}"))
}

fn map_query_error(error: postgres_query::AllocationRulesQueryError) -> Error {
    use postgres_query::AllocationRulesQueryError as E;
    match error {
        E::HouseholdNotFound | E::CursorNotFound => Error::new("not found"),
        E::MemberNotFound => Error::new("household access denied"),
        error @ (E::Pool(_) | E::Database(_) | E::Projection(_)) => crate::internal_error(error),
    }
}
