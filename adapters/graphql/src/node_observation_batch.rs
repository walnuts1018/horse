use async_graphql::{Context, ID, Result};
use std::collections::{HashMap, HashSet};
use uuid::Uuid;

use evidence_application::observation_node_batch_query::{
    ObservationKey as ApplicationObservationKey, ObservationNodeBatchInput,
    ObservationNodeBatchQuery, ObservationNodeBatchQueryError,
};

use crate::{
    ledger_entry::Node,
    unmatched_observations_query::{self, ConfirmedObservation, Observation, UnmatchedObservation},
};

type ObservationKey = (Uuid, String);

/// Loads observation nodes for the requested document and reference pairs.
pub(crate) async fn load(
    ctx: &Context<'_>,
    household_ids: &[Uuid],
    requested: &[ObservationKey],
) -> Result<HashMap<ObservationKey, Node>> {
    if household_ids.is_empty() || requested.is_empty() {
        return Ok(HashMap::new());
    }

    let requested_keys = requested.iter().cloned().collect::<HashSet<_>>();
    let requested = requested_keys
        .iter()
        .map(
            |(document_id, source_reference)| ApplicationObservationKey {
                document_id: *document_id,
                source_reference: source_reference.clone(),
            },
        )
        .collect();
    let observation_rows = ctx
        .data::<std::sync::Arc<dyn ObservationNodeBatchQuery>>()?
        .query(ObservationNodeBatchInput {
            household_ids: household_ids.to_vec(),
            requested,
        })
        .await
        .map_err(map_query_error)?;

    let mut observation_by_key = HashMap::new();
    for row in observation_rows {
        let key = (row.document_id, row.source_reference.clone());
        observation_by_key.insert(key, row);
    }
    if observation_by_key.is_empty() {
        return Ok(HashMap::new());
    }

    let purchase_ids = observation_by_key
        .values()
        .filter_map(|row| row.confirmed_purchase_id)
        .collect::<HashSet<_>>();
    let purchase_requests = purchase_ids
        .into_iter()
        .map(|id| ("Purchase".to_owned(), id))
        .collect::<Vec<_>>();
    let purchase_nodes =
        crate::node_purchase_batch::load(ctx, household_ids, &purchase_requests).await?;

    let mut result = HashMap::with_capacity(observation_by_key.len());
    for (key, row) in observation_by_key {
        if row.invalid_confirmed_correction {
            continue;
        }
        let purchase_id = row.confirmed_purchase_id;
        let (
            document_id,
            source_reference,
            source_kind,
            merchant,
            occurred_at,
            amount_yen,
            order_id,
            transaction_id,
        ) = (
            row.document_id,
            row.source_reference,
            row.source_kind,
            row.merchant,
            row.occurred_at,
            row.amount_yen,
            row.external_order_id,
            row.external_transaction_id,
        );
        let id = unmatched_observations_query::encode_node_id(document_id, &source_reference)?;
        let document_id_value = ID(document_id.to_string());
        let source_kind = unmatched_observations_query::parse_source_kind(&source_kind)?;
        let occurred_at = occurred_at.map(Into::into);
        let amount_yen = crate::Long(amount_yen.unwrap_or_default());
        let node = if let Some(purchase_id) = purchase_id {
            let Some(crate::ledger_entry::Node::Purchase(purchase)) =
                purchase_nodes.get(&("Purchase".to_owned(), purchase_id))
            else {
                continue;
            };
            let purchase = purchase.clone();
            Observation::ConfirmedObservation(Box::new(ConfirmedObservation {
                id,
                document_id: document_id_value,
                source_reference,
                source_kind,
                merchant,
                occurred_at,
                amount_yen,
                order_id,
                transaction_id,
                purchase,
            }))
        } else {
            Observation::UnmatchedObservation(Box::new(UnmatchedObservation {
                id,
                document_id: document_id_value,
                source_reference,
                source_kind,
                merchant,
                occurred_at,
                amount_yen,
                order_id,
                transaction_id,
            }))
        };
        result.insert(key, Node::Observation(node));
    }
    Ok(result)
}

fn map_query_error(error: ObservationNodeBatchQueryError) -> async_graphql::Error {
    crate::internal_error(error)
}
