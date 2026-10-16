use std::{future::Future, pin::Pin};

use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LedgerEntryProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub source_kind: String,
    pub source_id: Uuid,
    pub correction_purchase_id: Option<Uuid>,
    pub occurred_at: DateTime<Utc>,
    pub description: String,
    pub amount_yen: i64,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug)]
pub enum LedgerEntryBatchQueryError {
    Infrastructure(String),
}

pub trait LedgerEntryBatchQuery: Send + Sync {
    fn query<'a>(
        &'a self,
        household_ids: &'a [Uuid],
        ids: &'a [Uuid],
    ) -> Pin<
        Box<
            dyn Future<Output = Result<Vec<LedgerEntryProjection>, LedgerEntryBatchQueryError>>
                + Send
                + 'a,
        >,
    >;
}
