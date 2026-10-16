use async_trait::async_trait;
use chrono::{DateTime, NaiveDate, Utc};
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseCorrectionPayment {
    pub account_id: Uuid,
    pub account_name: String,
    pub account_kind: String,
    pub account_card_last4: Option<String>,
    pub account_pool_id: Option<Uuid>,
    pub amount_yen: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseCorrection {
    pub id: Uuid,
    pub item_id: Option<Uuid>,
    pub amount_delta_yen: i64,
    pub reason: String,
    pub occurred_at: DateTime<Utc>,
    pub settlement_month: NaiveDate,
    pub payment_allocations: Vec<PurchaseCorrectionPayment>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseCorrectionEdge {
    pub cursor: String,
    pub node: PurchaseCorrection,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseCorrectionConnection {
    pub edges: Vec<PurchaseCorrectionEdge>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug)]
pub enum PurchaseCorrectionQueryError {
    PurchaseNotFound,
    ResourceUnavailable,
    CursorNotFound,
    InvalidPageSize,
    InvalidCursor,
    Infrastructure(String),
}

#[async_trait]
pub trait PurchaseCorrectionQuery: Send + Sync {
    async fn load(
        &self,
        actor_id: Uuid,
        purchase_id: Uuid,
        first: Option<i32>,
        after: Option<String>,
    ) -> Result<PurchaseCorrectionConnection, PurchaseCorrectionQueryError>;
}
