use async_graphql::{Context, Enum, Error, ID, Interface, Result, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use horse_postgres::{PgPool, unmatched_observations_query as postgres_query};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::{
    DateTime, Long,
    pagination::PageInfo,
    policy_mutations::YearMonth,
    purchase_mutations::{PurchaseObject, SourceKind},
};

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum UnmatchedObservationState {
    Unmatched,
    Confirmed,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct UnmatchedObservation {
    pub(crate) id: ID,
    document_id: ID,
    source_reference: String,
    source_kind: SourceKind,
    merchant: String,
    occurred_at: Option<DateTime>,
    amount_yen: Long,
    order_id: String,
    transaction_id: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ConfirmedObservation {
    pub(crate) id: ID,
    document_id: ID,
    source_reference: String,
    source_kind: SourceKind,
    merchant: String,
    occurred_at: Option<DateTime>,
    amount_yen: Long,
    order_id: String,
    transaction_id: String,
    purchase: PurchaseObject,
}

#[derive(Clone, Debug, Interface)]
#[graphql(
    field(name = "id", ty = "&ID"),
    field(name = "document_id", ty = "&ID"),
    field(name = "source_reference", ty = "&String"),
    field(name = "source_kind", ty = "&SourceKind"),
    field(name = "merchant", ty = "&String"),
    field(name = "occurred_at", ty = "&Option<DateTime>"),
    field(name = "amount_yen", ty = "&Long"),
    field(name = "order_id", ty = "&String"),
    field(name = "transaction_id", ty = "&String")
)]
pub enum Observation {
    UnmatchedObservation(UnmatchedObservation),
    ConfirmedObservation(ConfirmedObservation),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct UnmatchedObservationEdge {
    cursor: String,
    node: Observation,
}
#[derive(Clone, Debug, SimpleObject)]
pub struct UnmatchedObservationConnection {
    edges: Vec<UnmatchedObservationEdge>,
    page_info: PageInfo,
    total_count: i32,
}

#[derive(Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct CursorPayload {
    document_id: String,
    source_reference: String,
}

pub async fn graphql_unmatched_observations(
    ctx: &Context<'_>,
    household_id: ID,
    from: Option<YearMonth>,
    to: Option<YearMonth>,
    source_kind: Option<SourceKind>,
    state: Option<UnmatchedObservationState>,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<UnmatchedObservationConnection> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let after = decode_cursor(after.as_deref())?;
    let before = decode_cursor(before.as_deref())?;
    let page = postgres_query::unmatched_observations(
        ctx.data::<PgPool>()?,
        actor.0,
        household_id,
        from.as_ref().map(YearMonth::as_str),
        to.as_ref().map(YearMonth::as_str),
        source_kind
            .map(|v| format!("{v:?}").to_ascii_lowercase())
            .as_deref(),
        state.map(|v| match v {
            UnmatchedObservationState::Confirmed => "CONFIRMED",
            UnmatchedObservationState::Unmatched => "UNMATCHED",
        }),
        after,
        before,
        first,
        last,
    )
    .await
    .map_err(map_error)?;
    let mut edges = Vec::with_capacity(page.rows.len());
    for row in page.rows {
        let cursor = encode_cursor(row.document_id, &row.source_reference);
        let id = encode_node_id(row.document_id, &row.source_reference)?;
        let source_kind = parse_source_kind(&row.source_kind)?;
        let occurred_at = row.occurred_at.map(DateTime::from);
        let amount_yen = Long(row.amount_yen.unwrap_or_default());
        let node = if let Some(purchase_id) = row.confirmed_purchase_id {
            let purchase = crate::purchase_mutations::purchase_result(
                ctx.data::<PgPool>()?,
                household_id,
                &purchase_id.to_string(),
            )
            .await?;
            Observation::ConfirmedObservation(ConfirmedObservation {
                id,
                document_id: ID(row.document_id.to_string()),
                source_reference: row.source_reference,
                source_kind,
                merchant: row.merchant,
                occurred_at,
                amount_yen,
                order_id: row.order_id,
                transaction_id: row.transaction_id,
                purchase,
            })
        } else {
            Observation::UnmatchedObservation(UnmatchedObservation {
                id,
                document_id: ID(row.document_id.to_string()),
                source_reference: row.source_reference,
                source_kind,
                merchant: row.merchant,
                occurred_at,
                amount_yen,
                order_id: row.order_id,
                transaction_id: row.transaction_id,
            })
        };
        edges.push(UnmatchedObservationEdge { cursor, node });
    }
    let total_count = i32::try_from(page.total_count)
        .map_err(|_| crate::internal_error("observation count exceeds GraphQL Int"))?;
    let page_info = PageInfo {
        has_next_page: page.has_next_page,
        has_previous_page: page.has_previous_page,
        start_cursor: edges.first().map(|edge| edge.cursor.clone()),
        end_cursor: edges.last().map(|edge| edge.cursor.clone()),
    };
    Ok(UnmatchedObservationConnection {
        edges,
        page_info,
        total_count,
    })
}

pub(crate) async fn resolve_unmatched_observation(
    ctx: &Context<'_>,
    household_id: Uuid,
    document_id: Uuid,
    source_reference: &str,
) -> Result<Option<Observation>> {
    let actor = crate::actor(ctx)?;
    let pool = ctx.data::<PgPool>()?;
    if !horse_postgres::household_access::is_active_member(pool, household_id, actor.0)
        .await
        .map_err(crate::internal_error)?
    {
        return Ok(None);
    }
    let Some(row) = horse_postgres::node_query::load_observation_node(
        pool,
        household_id,
        document_id,
        source_reference,
    )
    .await
    .map_err(crate::internal_error)?
    else {
        return Ok(None);
    };
    let id = encode_node_id(document_id, source_reference)?;
    let source_kind = parse_source_kind(&row.source_kind)?;
    let occurred_at = row.occurred_at.map(DateTime::from);
    let amount_yen = Long(row.amount_yen.unwrap_or_default());
    let node = if let Some(purchase_id) = row.confirmed_purchase_id {
        let purchase = crate::purchase_mutations::purchase_result(
            pool,
            household_id,
            &purchase_id.to_string(),
        )
        .await?;
        Observation::ConfirmedObservation(ConfirmedObservation {
            id,
            document_id: ID(document_id.to_string()),
            source_reference: source_reference.to_owned(),
            source_kind,
            merchant: row.merchant,
            occurred_at,
            amount_yen,
            order_id: row.order_id,
            transaction_id: row.transaction_id,
            purchase,
        })
    } else {
        Observation::UnmatchedObservation(UnmatchedObservation {
            id,
            document_id: ID(document_id.to_string()),
            source_reference: source_reference.to_owned(),
            source_kind,
            merchant: row.merchant,
            occurred_at,
            amount_yen,
            order_id: row.order_id,
            transaction_id: row.transaction_id,
        })
    };
    Ok(Some(node))
}

fn decode_cursor(value: Option<&str>) -> Result<Option<postgres_query::ObservationCursor>> {
    let Some(value) = value.filter(|value| !value.is_empty()) else {
        return Ok(None);
    };
    let bytes = URL_SAFE_NO_PAD
        .decode(value)
        .map_err(|_| Error::new("invalid observation cursor"))?;
    let payload: CursorPayload =
        serde_json::from_slice(&bytes).map_err(|_| Error::new("invalid observation cursor"))?;
    let document_id = Uuid::parse_str(&payload.document_id)
        .map_err(|_| Error::new("invalid observation cursor"))?;
    Ok(Some(postgres_query::ObservationCursor {
        document_id,
        source_reference: payload.source_reference,
    }))
}
fn encode_cursor(document_id: Uuid, source_reference: &str) -> String {
    let value = CursorPayload {
        document_id: document_id.to_string(),
        source_reference: source_reference.to_owned(),
    };
    URL_SAFE_NO_PAD.encode(serde_json::to_vec(&value).expect("cursor payload serializes"))
}
fn encode_node_id(document_id: Uuid, reference: &str) -> Result<ID> {
    let mut bytes = b"UnmatchedObservation:".to_vec();
    bytes.extend_from_slice(document_id.as_bytes());
    bytes.push(0);
    bytes.extend_from_slice(&(reference.len() as u64).to_be_bytes());
    bytes.extend_from_slice(reference.as_bytes());
    Ok(ID(URL_SAFE_NO_PAD.encode(bytes)))
}
fn parse_source_kind(value: &str) -> Result<SourceKind> {
    match value.to_ascii_uppercase().as_str() {
        "RECEIPT" => Ok(SourceKind::Receipt),
        "ORDER" => Ok(SourceKind::Order),
        "EMAIL" => Ok(SourceKind::Email),
        "CARD" => Ok(SourceKind::Card),
        "BANK" => Ok(SourceKind::Bank),
        other => Err(crate::internal_error(format!(
            "unknown observation source kind {other}"
        ))),
    }
}
fn map_error(error: postgres_query::UnmatchedObservationsError) -> Error {
    use postgres_query::UnmatchedObservationsError as E;
    match error {
        E::HouseholdNotFound | E::MissingCursor => Error::new("household or observation not found"),
        E::InvalidPagination | E::InvalidPeriod | E::InvalidRange => {
            Error::new("invalid observation query")
        }
        E::Pool(message) => crate::internal_error(message),
        E::Database(error) => crate::internal_error(error),
    }
}
