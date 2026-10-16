use async_trait::async_trait;
use bookkeeping_domain::purchase_commands::{UpdatePurchaseCommand, UpdatePurchaseItemCommand};
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PurchaseBulkMutationFailure {
    InvalidInput,
    NotFound,
    Conflict,
    Infrastructure(String),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkUpdatePurchasesRequest {
    pub actor_id: Uuid,
    pub commands: Vec<UpdatePurchaseCommand>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkUpdatePurchasesResult {
    pub household_id: Uuid,
    pub purchase_ids: Vec<Uuid>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SavePurchaseItemEditsRequest {
    pub actor_id: Uuid,
    pub household_id: Uuid,
    pub purchase_id: Uuid,
    pub items: Vec<UpdatePurchaseItemCommand>,
    pub purchase_patch: Option<UpdatePurchaseCommand>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SavePurchaseItemEditsResult {
    pub household_id: Uuid,
    pub item_ids: Vec<Uuid>,
}

#[async_trait]
pub trait PurchaseBulkMutationHandler: Send + Sync {
    async fn authorize_purchase(
        &self,
        actor_id: Uuid,
        purchase_id: Uuid,
    ) -> Result<Uuid, PurchaseBulkMutationFailure>;

    async fn bulk_update_purchases(
        &self,
        request: BulkUpdatePurchasesRequest,
    ) -> Result<BulkUpdatePurchasesResult, PurchaseBulkMutationFailure>;

    async fn save_purchase_item_edits(
        &self,
        request: SavePurchaseItemEditsRequest,
    ) -> Result<SavePurchaseItemEditsResult, PurchaseBulkMutationFailure>;
}
