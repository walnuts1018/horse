use async_trait::async_trait;
use cross_context_application::unmatched_observations_query::{
    ObservationPage, ObservationProjection, UnmatchedObservationsInput, UnmatchedObservationsQuery,
    UnmatchedObservationsQueryError,
};
use uuid::Uuid;

use crate::{PgPool, node_query, unmatched_observations_query};

pub struct PostgresUnmatchedObservationsQuery {
    pool: PgPool,
}

impl PostgresUnmatchedObservationsQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl UnmatchedObservationsQuery for PostgresUnmatchedObservationsQuery {
    async fn query(
        &self,
        input: UnmatchedObservationsInput,
    ) -> Result<ObservationPage, UnmatchedObservationsQueryError> {
        let result = unmatched_observations_query::unmatched_observations(
            &self.pool,
            unmatched_observations_query::UnmatchedObservationsQuery {
                actor_id: input.actor_id,
                household_id: input.household_id,
                from: input.from.as_deref(),
                to: input.to.as_deref(),
                source_kind: input.source_kind.as_deref(),
                state: input.state.as_deref(),
                undated_only: input.undated_only,
                after: input
                    .after
                    .map(|cursor| unmatched_observations_query::ObservationCursor {
                        document_id: cursor.document_id,
                        source_reference: cursor.source_reference,
                    }),
                before: input.before.map(|cursor| {
                    unmatched_observations_query::ObservationCursor {
                        document_id: cursor.document_id,
                        source_reference: cursor.source_reference,
                    }
                }),
                first: input.first,
                last: input.last,
            },
        )
        .await
        .map_err(map_page_error)?;
        Ok(ObservationPage {
            rows: result
                .rows
                .into_iter()
                .map(|row| ObservationProjection {
                    document_id: row.document_id,
                    source_reference: row.source_reference,
                    source_kind: row.source_kind,
                    merchant: row.merchant,
                    occurred_at: row.occurred_at,
                    amount_yen: row.amount_yen,
                    order_id: row.order_id,
                    transaction_id: row.transaction_id,
                    confirmed_purchase_id: row.confirmed_purchase_id,
                })
                .collect(),
            total_count: result.total_count,
            has_previous_page: result.has_previous_page,
            has_next_page: result.has_next_page,
        })
    }

    async fn resolve(
        &self,
        actor_id: Uuid,
        household_id: Uuid,
        document_id: Uuid,
        source_reference: String,
    ) -> Result<Option<ObservationProjection>, UnmatchedObservationsQueryError> {
        let active = household_postgres::household_access::is_active_member(
            &self.pool,
            household_id,
            actor_id,
        )
        .await
        .map_err(|error| {
            let message = match error {
                household_postgres::household_access::HouseholdAccessError::Pool(message) => {
                    message
                }
                household_postgres::household_access::HouseholdAccessError::Database(error) => {
                    error.to_string()
                }
            };
            UnmatchedObservationsQueryError::Infrastructure(message)
        })?;
        if !active {
            return Ok(None);
        }
        let row = node_query::load_observation_node(
            &self.pool,
            household_id,
            document_id,
            &source_reference,
        )
        .await
        .map_err(|error| {
            let message = match error {
                node_query::NodeQueryError::Pool(message) => message,
                node_query::NodeQueryError::Database(error) => error.to_string(),
                node_query::NodeQueryError::MissingLedgerEntry => {
                    "observation node not found".to_owned()
                }
                node_query::NodeQueryError::InvalidPersistedData => {
                    "observation node contains invalid persisted data".to_owned()
                }
                node_query::NodeQueryError::Transfer(error) => format!("{error:?}"),
            };
            UnmatchedObservationsQueryError::Infrastructure(message)
        })?;
        Ok(row.map(|row| ObservationProjection {
            document_id,
            source_reference,
            source_kind: row.source_kind,
            merchant: row.merchant,
            occurred_at: row.occurred_at,
            amount_yen: row.amount_yen,
            order_id: row.order_id,
            transaction_id: row.transaction_id,
            confirmed_purchase_id: row.confirmed_purchase_id,
        }))
    }
}

fn map_page_error(
    error: unmatched_observations_query::UnmatchedObservationsError,
) -> UnmatchedObservationsQueryError {
    use unmatched_observations_query::UnmatchedObservationsError as Error;
    match error {
        Error::HouseholdNotFound => UnmatchedObservationsQueryError::HouseholdNotFound,
        Error::InvalidPagination => UnmatchedObservationsQueryError::InvalidPagination,
        Error::InvalidPeriod => UnmatchedObservationsQueryError::InvalidPeriod,
        Error::InvalidRange => UnmatchedObservationsQueryError::InvalidRange,
        Error::MissingCursor => UnmatchedObservationsQueryError::MissingCursor,
        Error::Pool(message) => UnmatchedObservationsQueryError::Infrastructure(message),
        Error::Database(error) => {
            UnmatchedObservationsQueryError::Infrastructure(error.to_string())
        }
    }
}
