use std::{future::Future, pin::Pin};

use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdMemberReadModel {
    pub id: Uuid,
    pub display_name: String,
    pub role: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdAccountReadModel {
    pub id: Uuid,
    pub name: String,
    pub kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<Uuid>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdCategoryReadModel {
    pub id: Uuid,
    pub parent_id: Option<Uuid>,
    pub name: String,
    pub archived: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdFundingShareReadModel {
    pub member_id: Uuid,
    pub member_name: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdPoolReadModel {
    pub id: Uuid,
    pub name: String,
    pub kind: String,
    pub default_share_weight: i64,
    pub funding_shares: Vec<HouseholdFundingShareReadModel>,
    pub funding_rule_version: i32,
    pub owner_member_id: Option<Uuid>,
    pub settlement_policy: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdAllocationReadModel {
    pub pool_id: Uuid,
    pub pool_name: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdReadModel {
    pub id: Uuid,
    pub name: String,
    pub revision: i64,
    pub members: Vec<HouseholdMemberReadModel>,
    pub pools: Vec<HouseholdPoolReadModel>,
    pub accounts: Vec<HouseholdAccountReadModel>,
    pub categories: Vec<HouseholdCategoryReadModel>,
    pub default_allocation: Vec<HouseholdAllocationReadModel>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug)]
pub enum HouseholdQueryError {
    HouseholdNotFound,
    Infrastructure(String),
}

pub trait HouseholdQuery: Send + Sync {
    fn list_household_ids<'a>(
        &'a self,
        actor_id: Uuid,
    ) -> Pin<Box<dyn Future<Output = Result<Vec<Uuid>, HouseholdQueryError>> + Send + 'a>>;

    fn load_household<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
    ) -> Pin<Box<dyn Future<Output = Result<HouseholdReadModel, HouseholdQueryError>> + Send + 'a>>;
}
