use std::{future::Future, pin::Pin};

use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseSearchFilter {
    pub household_id: Uuid,
    pub from: Option<DateTime<Utc>>,
    pub to: Option<DateTime<Utc>>,
    pub origin: Option<String>,
    pub has_evidence: Option<bool>,
    pub search: Option<String>,
    pub category_id: Option<Uuid>,
    pub uncategorized: bool,
    pub tag_id: Option<Uuid>,
    pub payment_account_id: Option<Uuid>,
    pub pool_id: Option<Uuid>,
    pub minimum_amount: Option<i64>,
    pub maximum_amount: Option<i64>,
    pub amount_target: PurchaseAmountTarget,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PurchaseAmountTarget {
    Purchase,
    Item,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseSearchCursor {
    pub id: Uuid,
    pub occurred_at: Option<DateTime<Utc>>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseSearchRow {
    pub id: Uuid,
    pub occurred_at: DateTime<Utc>,
    pub purchase: PurchaseProjection,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseProjection {
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
    pub items: Vec<PurchaseItemProjection>,
    pub payments: Vec<PurchasePaymentProjection>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseItemProjection {
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
    pub allocations: Vec<PurchaseItemAllocationProjection>,
    pub tags: Vec<(Uuid, String, bool, i64)>,
    pub remaining_refundable_amount_yen: i64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseItemAllocationProjection {
    pub pool_id: Uuid,
    pub pool_name: String,
    pub weight: i64,
    pub amount_yen: i64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchasePaymentProjection {
    pub account_id: Uuid,
    pub account_name: String,
    pub account_kind: String,
    pub account_card_last4: Option<String>,
    pub account_pool_id: Option<Uuid>,
    pub amount_yen: i64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseSearchPage {
    pub rows: Vec<PurchaseSearchRow>,
    pub total_count: i64,
    pub sync_cursor: i64,
    pub has_extra: bool,
    pub backward: bool,
}

#[derive(Debug)]
pub enum PurchaseSearchQueryError {
    HouseholdNotFound,
    InvalidPagination,
    InvalidCursor,
    CursorNotFound,
    PurchaseNotFound,
    InvalidFilter,
    Infrastructure(String),
}

pub trait PurchaseSearchQuery: Send + Sync {
    fn query<'a>(
        &'a self,
        actor_id: Uuid,
        filter: PurchaseSearchFilter,
        after: Option<PurchaseSearchCursor>,
        before: Option<PurchaseSearchCursor>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Pin<
        Box<dyn Future<Output = Result<PurchaseSearchPage, PurchaseSearchQueryError>> + Send + 'a>,
    >;

    fn purchase<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
        purchase_id: Uuid,
    ) -> Pin<
        Box<dyn Future<Output = Result<PurchaseProjection, PurchaseSearchQueryError>> + Send + 'a>,
    >;
}
