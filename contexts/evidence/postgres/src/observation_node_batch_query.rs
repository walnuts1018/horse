use std::{collections::HashMap, future::Future, pin::Pin};

use diesel::{ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use evidence_application::observation_node_batch_query::{
    ObservationKey, ObservationNodeBatchInput, ObservationNodeBatchQuery,
    ObservationNodeBatchQueryError, ObservationNodeProjection,
};
use uuid::Uuid;

use crate::{PgPool, schema::public};

pub struct PostgresObservationNodeBatchQuery {
    pool: PgPool,
}

impl PostgresObservationNodeBatchQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl ObservationNodeBatchQuery for PostgresObservationNodeBatchQuery {
    fn query<'a>(
        &'a self,
        input: ObservationNodeBatchInput,
    ) -> Pin<
        Box<
            dyn Future<
                    Output = Result<Vec<ObservationNodeProjection>, ObservationNodeBatchQueryError>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move { query(&self.pool, input).await })
    }
}

type ObservationRow = (
    Uuid,
    Uuid,
    String,
    String,
    String,
    Option<chrono::DateTime<chrono::Utc>>,
    Option<i64>,
    String,
    String,
);

async fn query(
    pool: &PgPool,
    input: ObservationNodeBatchInput,
) -> Result<Vec<ObservationNodeProjection>, ObservationNodeBatchQueryError> {
    if input.household_ids.is_empty() || input.requested.is_empty() {
        return Ok(Vec::new());
    }

    let document_ids = input
        .requested
        .iter()
        .map(|key| key.document_id)
        .collect::<std::collections::HashSet<_>>()
        .into_iter()
        .collect::<Vec<_>>();
    if document_ids.is_empty() {
        return Ok(Vec::new());
    }

    use public::{source_confirmations as confirmations, source_observations as observations};
    let mut conn = pool
        .get()
        .await
        .map_err(|error| ObservationNodeBatchQueryError::Infrastructure(error.to_string()))?;
    let observation_rows = observations::table
        .filter(observations::household_id.eq_any(&input.household_ids))
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
        .load::<ObservationRow>(&mut conn)
        .await
        .map_err(infrastructure)?;

    let mut observation_by_key = HashMap::new();
    for row in observation_rows {
        let key = ObservationKey {
            document_id: row.1,
            source_reference: row.2.clone(),
        };
        if input.requested.contains(&key) {
            observation_by_key.insert(key, row);
        }
    }
    if observation_by_key.is_empty() {
        return Ok(Vec::new());
    }

    let observed_document_ids = observation_by_key
        .keys()
        .map(|key| key.document_id)
        .collect::<std::collections::HashSet<_>>()
        .into_iter()
        .collect::<Vec<_>>();
    let confirmation_rows = confirmations::table
        .filter(confirmations::household_id.eq_any(&input.household_ids))
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
        .map_err(infrastructure)?;

    let mut confirmation_by_key = HashMap::new();
    let mut correction_ids = std::collections::HashSet::new();
    for (household_id, document_id, source_reference, purchase_id, correction_id) in
        confirmation_rows
    {
        let key = ObservationKey {
            document_id,
            source_reference,
        };
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
            .filter(corrections::household_id.eq_any(&input.household_ids))
            .filter(corrections::id.eq_any(&correction_ids))
            .select((
                corrections::id,
                corrections::household_id,
                corrections::purchase_id,
            ))
            .load::<(Uuid, Uuid, Uuid)>(&mut conn)
            .await
            .map_err(infrastructure)?;
        for (correction_id, household_id, purchase_id) in correction_rows {
            correction_purchase_by_id.insert(correction_id, (household_id, purchase_id));
        }
    }

    Ok(observation_by_key
        .into_iter()
        .map(|(key, row)| {
            let (
                household_id,
                document_id,
                source_reference,
                source_kind,
                merchant,
                occurred_at,
                amount_yen,
                external_order_id,
                external_transaction_id,
            ) = row;
            let confirmation = confirmation_by_key.get(&key);
            let confirmed_purchase_id = confirmation.and_then(
                |(_, purchase_id, correction_id)| match (purchase_id, correction_id) {
                    (Some(purchase_id), _) => Some(*purchase_id),
                    (None, Some(correction_id)) => correction_purchase_by_id
                        .get(correction_id)
                        .filter(|(correction_household_id, _)| {
                            *correction_household_id == household_id
                        })
                        .map(|(_, purchase_id)| *purchase_id),
                    _ => None,
                },
            );
            let invalid_confirmed_correction = matches!(
                confirmation,
                Some((_, None, Some(correction_id)))
                    if correction_purchase_by_id
                        .get(correction_id)
                        .is_none_or(|(correction_household_id, _)| *correction_household_id != household_id)
            );
            ObservationNodeProjection {
                household_id,
                document_id,
                source_reference,
                source_kind,
                merchant,
                occurred_at,
                amount_yen,
                external_order_id,
                external_transaction_id,
                confirmed_purchase_id,
                invalid_confirmed_correction,
            }
        })
        .collect::<Vec<_>>())
}

fn infrastructure(error: impl std::fmt::Display) -> ObservationNodeBatchQueryError {
    ObservationNodeBatchQueryError::Infrastructure(error.to_string())
}
