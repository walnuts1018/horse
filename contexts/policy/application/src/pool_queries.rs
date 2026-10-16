use chrono::{DateTime, Utc};
use std::future::Future;
use std::pin::Pin;
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PolicyPage<T> {
    pub rows: Vec<T>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocationRule {
    pub id: Uuid,
    pub household_id: Uuid,
    pub revision: i64,
    pub target: AllocationRuleTarget,
    pub allocations: Vec<Allocation>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Allocation {
    pub pool_id: Uuid,
    pub pool_name: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AllocationRuleTarget {
    Category {
        id: Uuid,
        household_id: Uuid,
        parent_id: Option<Uuid>,
        name: String,
        archived: bool,
    },
    Merchant {
        name: String,
    },
    ProductConcept(ProductConcept),
    ProductVariant(ProductVariant),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductConcept {
    pub id: Uuid,
    pub household_id: Uuid,
    pub name: String,
    pub normalized_name: String,
    pub category_id: Option<Uuid>,
    pub archived: bool,
    pub revision: i64,
    pub variants: Vec<ProductVariant>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductVariant {
    pub id: Uuid,
    pub household_id: Uuid,
    pub concept_id: Uuid,
    pub name: String,
    pub normalized_name: String,
    pub brand: String,
    pub package_size: String,
    pub barcode: Option<String>,
    pub archived: bool,
    pub revision: i64,
    pub aliases: Vec<ProductAlias>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductAlias {
    pub id: Uuid,
    pub household_id: Uuid,
    pub variant_id: Uuid,
    pub merchant_key: String,
    pub alias: String,
    pub normalized_alias: String,
    pub revision: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingSchedule {
    pub id: String,
    pub household_id: String,
    pub member_id: String,
    pub pool_id: String,
    pub amount_yen: i64,
    pub cadence: String,
    pub starts_on: String,
    pub ends_on: Option<String>,
    pub active: bool,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PoolRulesQuery {
    pub actor_id: Uuid,
    pub household_id: Uuid,
    pub active: Option<bool>,
    pub after_id: Option<Uuid>,
    pub before_id: Option<Uuid>,
    pub page_size: usize,
    pub backward: bool,
}

#[derive(Debug)]
pub enum PoolRulesQueryFailure {
    NotFound,
    AccessDenied,
    Infrastructure(String),
}

pub trait AllocationRulesQuery: Send + Sync {
    fn query<'a>(
        &'a self,
        query: PoolRulesQuery,
    ) -> Pin<
        Box<
            dyn Future<Output = Result<PolicyPage<AllocationRule>, PoolRulesQueryFailure>>
                + Send
                + 'a,
        >,
    >;
}

pub trait FundingSchedulesQuery: Send + Sync {
    fn query<'a>(
        &'a self,
        query: PoolRulesQuery,
    ) -> Pin<
        Box<
            dyn Future<Output = Result<PolicyPage<FundingSchedule>, PoolRulesQueryFailure>>
                + Send
                + 'a,
        >,
    >;
}
