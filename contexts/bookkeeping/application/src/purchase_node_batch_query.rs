use std::{future::Future, pin::Pin};

use chrono::{DateTime, NaiveDate, Utc};
use uuid::Uuid;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseItemParentProjection {
    pub household_id: Uuid,
    pub purchase_id: Uuid,
    pub item_id: Uuid,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseCorrectionProjection {
    pub household_id: Uuid,
    pub purchase_id: Uuid,
    pub id: Uuid,
    pub item_id: Option<Uuid>,
    pub amount_delta_yen: i64,
    pub reason: String,
    pub settlement_month: NaiveDate,
    pub occurred_at: DateTime<Utc>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseCorrectionPaymentProjection {
    pub correction_id: Uuid,
    pub account_id: Uuid,
    pub account_name: String,
    pub account_kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<Uuid>,
    pub amount_yen: i64,
}

#[derive(Clone, Debug, Eq, PartialEq, Default)]
pub struct PurchaseNodeBatchProjection {
    pub item_parents: Vec<PurchaseItemParentProjection>,
    pub corrections: Vec<PurchaseCorrectionProjection>,
    pub correction_payments: Vec<PurchaseCorrectionPaymentProjection>,
}

#[derive(Debug)]
pub enum PurchaseNodeBatchQueryError {
    Infrastructure(String),
}

pub trait PurchaseNodeBatchQuery: Send + Sync {
    fn query<'a>(
        &'a self,
        household_ids: &'a [Uuid],
        purchase_ids: &'a [Uuid],
        item_ids: &'a [Uuid],
        correction_ids: &'a [Uuid],
    ) -> Pin<
        Box<
            dyn Future<Output = Result<PurchaseNodeBatchProjection, PurchaseNodeBatchQueryError>>
                + Send
                + 'a,
        >,
    >;
}
