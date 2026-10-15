use async_graphql::{Context, ID, Result};
use diesel::{ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use horse_postgres_common::{PgPool, schema::public};
use std::collections::{HashMap, HashSet};
use uuid::Uuid;

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
    let document_ids = requested
        .iter()
        .map(|(document_id, _)| *document_id)
        .collect::<HashSet<_>>()
        .into_iter()
        .collect::<Vec<_>>();
    if document_ids.is_empty() {
        return Ok(HashMap::new());
    }

    use public::{source_confirmations as confirmations, source_observations as observations};
    let pool = ctx.data::<PgPool>()?;
    let mut conn = pool.get().await.map_err(crate::internal_error)?;
    let observation_rows = observations::table
        .filter(observations::household_id.eq_any(household_ids))
        .filter(observations::source_document_id.eq_any(&document_ids))
        .select((
            observations::household_id,
            observations::source_document_id,
            observations::source_reference,
            observations::source_kind,
            observations::merchant,
            observations::occurred_at,
            observations::amount_yen,
            observations::external_order_id,
            observations::external_transaction_id,
        ))
        .load::<(
            Uuid,
            Uuid,
            String,
            String,
            String,
            Option<chrono::DateTime<chrono::Utc>>,
            Option<i64>,
            String,
            String,
        )>(&mut conn)
        .await
        .map_err(crate::internal_error)?;

    let mut observation_by_key = HashMap::new();
    for row in observation_rows {
        let key = (row.1, row.2.clone());
        if requested_keys.contains(&key) {
            observation_by_key.insert(key, row);
        }
    }
    if observation_by_key.is_empty() {
        return Ok(HashMap::new());
    }

    let observed_document_ids = observation_by_key
        .keys()
        .map(|(document_id, _)| *document_id)
        .collect::<HashSet<_>>()
        .into_iter()
        .collect::<Vec<_>>();
    let confirmation_rows = confirmations::table
        .filter(confirmations::household_id.eq_any(household_ids))
        .filter(confirmations::source_document_id.eq_any(&observed_document_ids))
        .select((
            confirmations::household_id,
            confirmations::source_document_id,
            confirmations::source_reference,
            confirmations::purchase_id,
            confirmations::correction_id,
        ))
        .load::<(Uuid, Uuid, String, Option<Uuid>, Option<Uuid>)>(&mut conn)
        .await
        .map_err(crate::internal_error)?;

    let mut confirmation_by_key = HashMap::new();
    let mut correction_ids = HashSet::new();
    for (household_id, document_id, source_reference, purchase_id, correction_id) in
        confirmation_rows
    {
        let key = (document_id, source_reference);
        if !observation_by_key.contains_key(&key) {
            continue;
        }
        if let Some(correction_id) = correction_id {
            correction_ids.insert(correction_id);
        }
        confirmation_by_key.insert(key, (household_id, purchase_id, correction_id));
    }

    let mut correction_purchase_by_id = HashMap::new();
    if !correction_ids.is_empty() {
        use public::purchase_corrections::dsl as corrections;
        let correction_ids = correction_ids.into_iter().collect::<Vec<_>>();
        let correction_rows = corrections::purchase_corrections
            .filter(corrections::household_id.eq_any(household_ids))
            .filter(corrections::id.eq_any(&correction_ids))
            .select((
                corrections::id,
                corrections::household_id,
                corrections::purchase_id,
            ))
            .load::<(Uuid, Uuid, Uuid)>(&mut conn)
            .await
            .map_err(crate::internal_error)?;
        for (correction_id, household_id, purchase_id) in correction_rows {
            correction_purchase_by_id.insert(correction_id, (household_id, purchase_id));
        }
    }

    drop(conn);
    let mut purchase_ids = HashSet::new();
    for (household_id, purchase_id, correction_id) in confirmation_by_key.values() {
        let purchase_id = match (purchase_id, correction_id) {
            (Some(purchase_id), _) => Some(*purchase_id),
            (None, Some(correction_id)) => correction_purchase_by_id
                .get(correction_id)
                .filter(|(correction_household_id, _)| correction_household_id == household_id)
                .map(|(_, purchase_id)| *purchase_id),
            _ => None,
        };
        if let Some(purchase_id) = purchase_id {
            purchase_ids.insert(purchase_id);
        }
    }
    let purchase_requests = purchase_ids
        .into_iter()
        .map(|id| ("Purchase".to_owned(), id))
        .collect::<Vec<_>>();
    let purchase_nodes =
        crate::node_purchase_batch::load(ctx, household_ids, &purchase_requests).await?;

    let mut result = HashMap::with_capacity(observation_by_key.len());
    for (key, row) in observation_by_key {
        let (
            household_id,
            document_id,
            source_reference,
            source_kind,
            merchant,
            occurred_at,
            amount_yen,
            order_id,
            transaction_id,
        ) = row;
        let id = unmatched_observations_query::encode_node_id(document_id, &source_reference)?;
        let document_id_value = ID(document_id.to_string());
        let source_kind = unmatched_observations_query::parse_source_kind(&source_kind)?;
        let occurred_at = occurred_at.map(Into::into);
        let amount_yen = crate::Long(amount_yen.unwrap_or_default());
        let confirmation = confirmation_by_key.get(&key);
        let purchase_id = match confirmation {
            Some((_, Some(purchase_id), _)) => Some(*purchase_id),
            Some((_, None, Some(correction_id))) => {
                let Some((correction_household_id, purchase_id)) =
                    correction_purchase_by_id.get(correction_id)
                else {
                    continue;
                };
                if *correction_household_id != household_id {
                    continue;
                }
                Some(*purchase_id)
            }
            _ => None,
        };
        let node = if let Some(purchase_id) = purchase_id {
            let Some(crate::ledger_entry::Node::Purchase(purchase)) =
                purchase_nodes.get(&("Purchase".to_owned(), purchase_id))
            else {
                continue;
            };
            let purchase = purchase.clone();
            Observation::ConfirmedObservation(ConfirmedObservation {
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
            })
        } else {
            Observation::UnmatchedObservation(UnmatchedObservation {
                id,
                document_id: document_id_value,
                source_reference,
                source_kind,
                merchant,
                occurred_at,
                amount_yen,
                order_id,
                transaction_id,
            })
        };
        result.insert(key, Node::Observation(node));
    }
    Ok(result)
}
