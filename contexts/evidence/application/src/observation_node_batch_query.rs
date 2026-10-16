use std::{collections::HashSet, future::Future, pin::Pin};

use uuid::Uuid;

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
