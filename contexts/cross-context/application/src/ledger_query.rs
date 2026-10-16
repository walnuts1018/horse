use async_trait::async_trait;
use bookkeeping_domain::SettlementPolicy;
use chrono::{DateTime, Utc};
use std::collections::HashMap;
use uuid::Uuid;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LedgerCursor {
    pub id: Uuid,
}

#[derive(Clone, Debug)]
pub struct LedgerQuery {
    pub actor_id: Uuid,
    pub household_id: Uuid,
    pub after: Option<LedgerCursor>,
    pub before: Option<LedgerCursor>,
    pub first: Option<i32>,
    pub last: Option<i32>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LedgerRow {
    pub id: Uuid,
    pub source_kind: String,
    pub source_id: Uuid,
    pub source_purchase_id: Option<Uuid>,
    pub occurred_at: DateTime<Utc>,
    pub description: String,
    pub amount_yen: i64,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct FundingMemberProjection {
    pub id: Uuid,
    pub display_name: String,
    pub role: String,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct FundingPoolShareProjection {
    pub member_id: Uuid,
    pub member_name: String,
    pub weight: i64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct FundingPoolProjection {
    pub id: Uuid,
    pub name: String,
    pub kind: String,
    pub default_share_weight: i64,
    pub funding_shares: Vec<FundingPoolShareProjection>,
    pub funding_rule_version: i32,
    pub owner_member_id: Option<Uuid>,
    pub settlement_policy: Option<SettlementPolicy>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct FundingLedgerEntryProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub occurred_at: DateTime<Utc>,
    pub description: String,
    pub amount_yen: i64,
    pub currency: String,
    pub created_at: DateTime<Utc>,
    pub member: FundingMemberProjection,
    pub pool: FundingPoolProjection,
}

#[derive(Clone, Debug)]
pub struct LedgerPage {
    pub rows: Vec<LedgerRow>,
    pub total_count: i64,
    pub has_extra: bool,
    pub backward: bool,
    pub after_has_previous_page: bool,
    pub before_has_next_page: bool,
    pub funding_entries: HashMap<Uuid, FundingLedgerEntryProjection>,
    pub transfer_entries:
        HashMap<Uuid, super::statement_link_mutations::TransferLedgerEntryProjection>,
}

#[derive(Debug)]
pub enum LedgerQueryError {
    HouseholdNotFound,
    InvalidPagination,
    MissingCursor,
    Infrastructure(String),
}

#[async_trait]
pub trait LedgerQueryHandler: Send + Sync {
    async fn query(&self, query: LedgerQuery) -> Result<LedgerPage, LedgerQueryError>;
}
