use async_trait::async_trait;
use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Clone, Debug)]
pub struct PurchaseEvent {
    pub cursor: i64,
    pub household_id: Uuid,
    pub event_type: String,
    pub entity_kind: String,
    pub entity_id: Uuid,
    pub revision: i64,
    pub changed_fields: Vec<Option<String>>,
    pub created_at: DateTime<Utc>,
    pub payload: serde_json::Value,
}

#[derive(Clone, Debug)]
pub struct PurchaseSnapshot {
    pub id: Uuid,
    pub household_id: Uuid,
    pub occurred_at: DateTime<Utc>,
    pub date_precision: String,
    pub merchant: String,
    pub origin: String,
    pub status: String,
    pub revision: i64,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
    pub has_evidence: bool,
    pub source_kinds: Vec<String>,
    pub total_yen: i64,
    pub correction_amount_yen: i64,
    pub correction_count: i64,
    pub remaining_refundable_amount_yen: i64,
    pub items: Vec<PurchaseItemSnapshot>,
    pub payments: Vec<PurchasePaymentSnapshot>,
}

#[derive(Clone, Debug)]
pub struct PurchaseItemSnapshot {
    pub id: Uuid,
    pub name: String,
    pub amount_yen: i64,
    pub category_id: Option<Uuid>,
    pub product_variant_id: Option<Uuid>,
    pub revision: i64,
    pub quantity_numerator: Option<i64>,
    pub quantity_denominator: Option<i64>,
    pub quantity_unit: Option<String>,
    pub printed_unit_price_yen: Option<i64>,
    pub printed_unit_price_quantity_numerator: Option<i64>,
    pub printed_unit_price_quantity_denominator: Option<i64>,
    pub printed_unit_price_unit: Option<String>,
    pub allocations: Vec<PurchaseItemAllocationSnapshot>,
    pub tags: Vec<(Uuid, String, bool, i64)>,
    pub remaining_refundable_amount_yen: i64,
}

#[derive(Clone, Debug)]
pub struct PurchaseItemAllocationSnapshot {
    pub pool_id: Uuid,
    pub pool_name: String,
    pub weight: i64,
    pub amount_yen: i64,
}

#[derive(Clone, Debug)]
pub struct PurchasePaymentSnapshot {
    pub account_id: Uuid,
    pub account_name: String,
    pub account_kind: String,
    pub account_card_last4: Option<String>,
    pub account_pool_id: Option<Uuid>,
    pub amount_yen: i64,
}

#[async_trait]
pub trait SubscriptionQuery: Send + Sync {
    async fn session_is_valid(&self, actor_id: Uuid, token_hash: [u8; 32]) -> Result<bool, String>;
    async fn household_access(&self, actor_id: Uuid, household_id: Uuid) -> Result<bool, String>;
    async fn latest_purchase_cursor(&self, household_id: Uuid) -> Result<i64, String>;
    async fn purchase_events(
        &self,
        cursors: Vec<(Uuid, i64)>,
    ) -> Result<Vec<PurchaseEvent>, String>;
    async fn purchase_id_for_item(&self, item_id: Uuid) -> Result<Option<Uuid>, String>;
    async fn purchase_snapshot(
        &self,
        household_id: Uuid,
        purchase_id: Uuid,
    ) -> Result<Option<PurchaseSnapshot>, String>;
    async fn shared_draft_revision(
        &self,
        draft_id: Uuid,
        after: i64,
    ) -> Result<Option<i64>, String>;
    async fn shared_draft_is_accessible(
        &self,
        actor_id: Uuid,
        draft_id: Uuid,
    ) -> Result<bool, String>;
}
