use async_graphql::{Context, Error, ID, Result, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use policy_application::pool_queries::{
    FundingSchedulesQuery, PoolRulesQuery, PoolRulesQueryFailure,
};
use std::sync::Arc;
use uuid::Uuid;

use crate::{pagination::PageInfo, pool_mutations::FundingScheduleObject};

#[derive(Clone, Debug, SimpleObject)]
pub struct FundingScheduleEdge {
    cursor: String,
    node: FundingScheduleObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct FundingScheduleConnection {
    edges: Vec<FundingScheduleEdge>,
    page_info: PageInfo,
    total_count: i32,
}

pub async fn graphql_funding_schedules(
    ctx: &Context<'_>,
    household_id: ID,
    active: Option<bool>,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<FundingScheduleConnection> {
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
    let page = ctx
        .data::<Arc<dyn FundingSchedulesQuery>>()?
        .query(PoolRulesQuery {
            actor_id: actor.0,
            household_id,
            active,
            after_id,
            before_id,
            page_size: page_size as usize,
            backward,
        })
        .await
        .map_err(map_query_error)?;
    let edges = page
        .rows
        .into_iter()
        .map(|projection| {
            let cursor = encode_cursor(&projection.id);
            let node = crate::pool_mutations::map_funding_schedule_projection(
                policy_application::set_funding_schedule::PersistedFundingSchedule {
                    id: projection.id,
                    household_id: projection.household_id,
                    member_id: projection.member_id,
                    pool_id: projection.pool_id,
                    amount_yen: projection.amount_yen,
                    cadence: projection.cadence,
                    starts_on: projection.starts_on,
                    ends_on: projection.ends_on,
                    active: projection.active,
                    created_at: projection.created_at,
                },
            )?;
            Ok(FundingScheduleEdge { cursor, node })
        })
        .collect::<Result<Vec<_>>>()?;
    let total_count = i32::try_from(page.total_count).map_err(crate::internal_error)?;
    Ok(FundingScheduleConnection {
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
        .strip_prefix("FundingSchedule:")
        .ok_or_else(|| Error::new("invalid cursor"))?;
    Uuid::parse_str(id)
        .map(Some)
        .map_err(|_| Error::new("invalid cursor"))
}

fn encode_cursor(id: &str) -> String {
    URL_SAFE_NO_PAD.encode(format!("FundingSchedule:{id}"))
}

fn map_query_error(error: PoolRulesQueryFailure) -> Error {
    match error {
        PoolRulesQueryFailure::NotFound => Error::new("not found"),
        PoolRulesQueryFailure::AccessDenied => Error::new("household access denied"),
        PoolRulesQueryFailure::Infrastructure(error) => crate::internal_error(error),
    }
}
