use async_graphql::{Context, Enum, Error, ID, Interface, Result, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use cross_context_application::unmatched_observations_query::{
    ObservationCursor, ObservationProjection, UnmatchedObservationsInput,
    UnmatchedObservationsQuery, UnmatchedObservationsQueryError,
};
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
    pub(crate) document_id: ID,
    pub(crate) source_reference: String,
    pub(crate) source_kind: SourceKind,
    pub(crate) merchant: String,
    pub(crate) occurred_at: Option<DateTime>,
    pub(crate) amount_yen: Long,
    pub(crate) order_id: String,
    pub(crate) transaction_id: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ConfirmedObservation {
    pub(crate) id: ID,
    pub(crate) document_id: ID,
    pub(crate) source_reference: String,
    pub(crate) source_kind: SourceKind,
    pub(crate) merchant: String,
    pub(crate) occurred_at: Option<DateTime>,
    pub(crate) amount_yen: Long,
    pub(crate) order_id: String,
    pub(crate) transaction_id: String,
    pub(crate) purchase: PurchaseObject,
}

#[allow(clippy::duplicated_attributes)]
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
    UnmatchedObservation(Box<UnmatchedObservation>),
    ConfirmedObservation(Box<ConfirmedObservation>),
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

pub(crate) struct UnmatchedObservationsParams {
    pub(crate) household_id: ID,
    pub(crate) from: Option<YearMonth>,
    pub(crate) to: Option<YearMonth>,
    pub(crate) source_kind: Option<SourceKind>,
    pub(crate) state: Option<UnmatchedObservationState>,
    pub(crate) after: Option<String>,
    pub(crate) before: Option<String>,
    pub(crate) first: Option<i32>,
    pub(crate) last: Option<i32>,
}

pub(crate) async fn graphql_unmatched_observations(
    ctx: &Context<'_>,
    params: UnmatchedObservationsParams,
) -> Result<UnmatchedObservationConnection> {
    let UnmatchedObservationsParams {
        household_id,
        from,
        to,
        source_kind,
        state,
        after,
        before,
        first,
        last,
    } = params;
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let after = decode_cursor(after.as_deref())?;
    let before = decode_cursor(before.as_deref())?;
    let source_kind = source_kind.map(|value| format!("{value:?}").to_ascii_lowercase());
    let state = state.map(|value| match value {
        UnmatchedObservationState::Confirmed => "CONFIRMED",
        UnmatchedObservationState::Unmatched => "UNMATCHED",
    });
    let page = ctx
        .data::<std::sync::Arc<dyn UnmatchedObservationsQuery>>()?
        .query(UnmatchedObservationsInput {
            actor_id: actor.0,
            household_id,
            from: from.as_ref().map(|period| period.as_str().to_owned()),
            to: to.as_ref().map(|period| period.as_str().to_owned()),
            source_kind,
            state: state.map(str::to_owned),
            after,
            before,
            first,
            last,
        })
        .await
        .map_err(map_error)?;
    let mut edges = Vec::with_capacity(page.rows.len());
    for row in page.rows {
        let cursor = encode_cursor(row.document_id, &row.source_reference);
        let node = observation_from_projection(ctx, household_id, row).await?;
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
    let Some(row) = ctx
        .data::<std::sync::Arc<dyn UnmatchedObservationsQuery>>()?
        .resolve(
            actor.0,
            household_id,
            document_id,
            source_reference.to_owned(),
        )
        .await
        .map_err(map_error)?
    else {
        return Ok(None);
    };
    Ok(Some(
        observation_from_projection(ctx, household_id, row).await?,
    ))
}

async fn observation_from_projection(
    ctx: &Context<'_>,
    household_id: Uuid,
    row: ObservationProjection,
) -> Result<Observation> {
    let id = encode_node_id(row.document_id, &row.source_reference)?;
    let source_kind = parse_source_kind(&row.source_kind)?;
    let occurred_at = row.occurred_at.map(DateTime::from);
    let amount_yen = Long(row.amount_yen.unwrap_or_default());
    if let Some(purchase_id) = row.confirmed_purchase_id {
        let purchase_nodes = crate::node_purchase_batch::load(
            ctx,
            &[household_id],
            &[("Purchase".to_owned(), purchase_id)],
        )
        .await?;
        let Some(crate::ledger_entry::Node::Purchase(purchase)) =
            purchase_nodes.get(&("Purchase".to_owned(), purchase_id))
        else {
            return Err(crate::internal_error(
                "confirmed observation purchase not found",
            ));
        };
        Ok(Observation::ConfirmedObservation(Box::new(
            ConfirmedObservation {
                id,
                document_id: ID(row.document_id.to_string()),
                source_reference: row.source_reference,
                source_kind,
                merchant: row.merchant,
                occurred_at,
                amount_yen,
                order_id: row.order_id,
                transaction_id: row.transaction_id,
                purchase: purchase.clone(),
            },
        )))
    } else {
        Ok(Observation::UnmatchedObservation(Box::new(
            UnmatchedObservation {
                id,
                document_id: ID(row.document_id.to_string()),
                source_reference: row.source_reference,
                source_kind,
                merchant: row.merchant,
                occurred_at,
                amount_yen,
                order_id: row.order_id,
                transaction_id: row.transaction_id,
            },
        )))
    }
}

fn decode_cursor(value: Option<&str>) -> Result<Option<ObservationCursor>> {
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
    Ok(Some(ObservationCursor {
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
pub(crate) fn encode_node_id(document_id: Uuid, reference: &str) -> Result<ID> {
    let mut bytes = b"UnmatchedObservation:".to_vec();
    bytes.extend_from_slice(document_id.as_bytes());
    bytes.push(0);
    bytes.extend_from_slice(&(reference.len() as u64).to_be_bytes());
    bytes.extend_from_slice(reference.as_bytes());
    Ok(ID(URL_SAFE_NO_PAD.encode(bytes)))
}
pub(crate) fn parse_source_kind(value: &str) -> Result<SourceKind> {
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
fn map_error(error: UnmatchedObservationsQueryError) -> Error {
    use UnmatchedObservationsQueryError as E;
    match error {
        E::HouseholdNotFound | E::MissingCursor => Error::new("household or observation not found"),
        E::InvalidPagination | E::InvalidPeriod | E::InvalidRange => {
            Error::new("invalid observation query")
        }
        E::Infrastructure(message) => crate::internal_error(message),
    }
}
