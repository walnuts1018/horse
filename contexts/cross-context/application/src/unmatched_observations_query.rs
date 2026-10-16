use async_trait::async_trait;
use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ObservationCursor {
    pub document_id: Uuid,
    pub source_reference: String,
}

#[derive(Clone, Debug)]
pub struct UnmatchedObservationsInput {
    pub actor_id: Uuid,
    pub household_id: Uuid,
    pub from: Option<String>,
    pub to: Option<String>,
    pub source_kind: Option<String>,
    pub state: Option<String>,
    pub after: Option<ObservationCursor>,
    pub before: Option<ObservationCursor>,
    pub first: Option<i32>,
    pub last: Option<i32>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ObservationProjection {
    pub document_id: Uuid,
    pub source_reference: String,
    pub source_kind: String,
    pub merchant: String,
    pub occurred_at: Option<DateTime<Utc>>,
    pub amount_yen: Option<i64>,
    pub order_id: String,
    pub transaction_id: String,
    pub confirmed_purchase_id: Option<Uuid>,
}

#[derive(Clone, Debug)]
pub struct ObservationPage {
    pub rows: Vec<ObservationProjection>,
    pub total_count: i64,
    pub has_previous_page: bool,
    pub has_next_page: bool,
}

#[derive(Debug)]
pub enum UnmatchedObservationsQueryError {
    HouseholdNotFound,
    InvalidPagination,
    InvalidPeriod,
    InvalidRange,
    MissingCursor,
    Infrastructure(String),
}

#[async_trait]
pub trait UnmatchedObservationsQuery: Send + Sync {
    async fn query(
        &self,
        input: UnmatchedObservationsInput,
    ) -> Result<ObservationPage, UnmatchedObservationsQueryError>;

    async fn resolve(
        &self,
        actor_id: Uuid,
        household_id: Uuid,
        document_id: Uuid,
        source_reference: String,
    ) -> Result<Option<ObservationProjection>, UnmatchedObservationsQueryError>;
}
