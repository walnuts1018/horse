use std::{collections::HashSet, future::Future, pin::Pin};

use uuid::Uuid;

use evidence_domain::source_observation::{
    InvalidSourceReference, Reconciliation, SourceObservation, SourceObservationKey,
    SourceReference, reconcile_observations,
};

#[derive(Clone, Debug, Eq, Hash, PartialEq)]
pub struct ObservationKey {
    pub document_id: Uuid,
    pub source_reference: String,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ObservationNodeProjection {
    pub household_id: Uuid,
    pub document_id: Uuid,
    pub source_reference: String,
    pub source_kind: String,
    pub merchant: String,
    pub occurred_at: Option<chrono::DateTime<chrono::Utc>>,
    pub amount_yen: Option<i64>,
    pub external_order_id: String,
    pub external_transaction_id: String,
    pub confirmed_purchase_id: Option<Uuid>,
    pub invalid_confirmed_correction: bool,
}

impl ObservationNodeProjection {
    pub fn source_observation(&self) -> Result<SourceObservation, InvalidSourceReference> {
        Ok(SourceObservation {
            key: SourceObservationKey {
                document_id: self.document_id,
                source_reference: SourceReference::new(self.source_reference.clone())?,
            },
            source_kind: self.source_kind.clone(),
            merchant: non_empty(&self.merchant),
            occurred_at: self.occurred_at,
            amount_yen: self.amount_yen,
            external_order_id: non_empty(&self.external_order_id),
            external_transaction_id: non_empty(&self.external_transaction_id),
        })
    }
}

pub fn reconcile_observation_nodes(
    left: &ObservationNodeProjection,
    right: &ObservationNodeProjection,
) -> Result<Reconciliation, InvalidSourceReference> {
    Ok(reconcile_observations(
        &left.source_observation()?,
        &right.source_observation()?,
    ))
}

fn non_empty(value: &str) -> Option<String> {
    (!value.trim().is_empty()).then(|| value.to_owned())
}

#[derive(Clone, Debug)]
pub struct ObservationNodeBatchInput {
    pub household_ids: Vec<Uuid>,
    pub requested: HashSet<ObservationKey>,
}

#[derive(Debug)]
pub enum ObservationNodeBatchQueryError {
    Infrastructure(String),
}

pub trait ObservationNodeBatchQuery: Send + Sync {
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
    >;
}
