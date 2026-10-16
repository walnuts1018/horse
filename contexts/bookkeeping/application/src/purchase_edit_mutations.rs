use std::{future::Future, pin::Pin};

use bookkeeping_domain::purchase_commands::{
    BulkDeleteItemTarget, BulkUpdateItemPatch, BulkUpdateItemTarget, UpdatePurchaseCommand,
    UpdatePurchaseItemCommand,
};
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PurchaseEditFailure {
    InvalidInput,
    NotFound,
    Conflict,
    Infrastructure(String),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseEditRequest<T> {
    pub actor_id: Uuid,
    pub household_id: Option<Uuid>,
    pub value: T,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdatedPurchase {
    pub household_id: Uuid,
    pub purchase_id: Uuid,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdatedPurchaseItem {
    pub household_id: Uuid,
    pub item_id: Uuid,
    pub purchase_id: Uuid,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdatedPurchaseItems {
    pub items: Vec<UpdatedPurchaseItem>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DeletedPurchaseItems {
    pub household_id: Uuid,
    pub item_ids: Vec<Uuid>,
}

pub trait PurchaseEditMutationHandler: Send + Sync {
    fn update_purchase<'a>(
        &'a self,
        request: PurchaseEditRequest<UpdatePurchaseCommand>,
    ) -> Pin<Box<dyn Future<Output = Result<UpdatedPurchase, PurchaseEditFailure>> + Send + 'a>>;

    fn update_purchase_item<'a>(
        &'a self,
        request: PurchaseEditRequest<UpdatePurchaseItemCommand>,
    ) -> Pin<Box<dyn Future<Output = Result<UpdatedPurchaseItem, PurchaseEditFailure>> + Send + 'a>>;

    fn bulk_update_purchase_items<'a>(
        &'a self,
        request: PurchaseEditRequest<(Vec<BulkUpdateItemTarget>, BulkUpdateItemPatch)>,
    ) -> Pin<Box<dyn Future<Output = Result<UpdatedPurchaseItems, PurchaseEditFailure>> + Send + 'a>>;

    fn bulk_delete_purchase_items<'a>(
        &'a self,
        request: PurchaseEditRequest<Vec<BulkDeleteItemTarget>>,
    ) -> Pin<Box<dyn Future<Output = Result<DeletedPurchaseItems, PurchaseEditFailure>> + Send + 'a>>;
}
