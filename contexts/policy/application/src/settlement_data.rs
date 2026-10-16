use chrono::{DateTime, NaiveDate, Utc};
use uuid::Uuid;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct FundingSchedule {
    pub household_id: Uuid,
    pub id: Uuid,
    pub member_id: Uuid,
    pub pool_id: Uuid,
    pub amount_yen: i64,
    pub cadence: String,
    pub starts_on: NaiveDate,
    pub ends_on: Option<NaiveDate>,
    pub active: bool,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct FundingRule {
    pub pool_id: Uuid,
    pub version: i32,
    pub owner_member_id: Option<Uuid>,
    pub settlement_policy: Option<String>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct FundingShare {
    pub pool_id: Uuid,
    pub version: i32,
    pub member_id: Uuid,
    pub weight: i64,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct FundingRulesAndShares {
    pub rules: Vec<FundingRule>,
    pub shares: Vec<FundingShare>,
}

#[allow(async_fn_in_trait)]
pub trait SettlementDataRead: Send + Sync {
    async fn schedules_for_period(
        &self,
        household_id: Uuid,
        start: NaiveDate,
        end: NaiveDate,
    ) -> Result<Vec<FundingSchedule>, String>;

    async fn schedules_overlapping(
        &self,
        household_ids: &[Uuid],
        start: NaiveDate,
        end: NaiveDate,
    ) -> Result<Vec<FundingSchedule>, String>;

    async fn earliest_schedule_start(
        &self,
        household_id: Uuid,
    ) -> Result<Option<NaiveDate>, String>;

    async fn rules_and_shares(&self, household_id: Uuid) -> Result<FundingRulesAndShares, String>;

    async fn historical_rules(
        &self,
        household_id: Uuid,
        pool_ids: &[Uuid],
        versions: &[i32],
    ) -> Result<Vec<FundingRule>, String>;

    async fn pool_funding_owner(
        &self,
        household_id: Uuid,
        pool_id: Uuid,
        version: i32,
    ) -> Result<Option<Uuid>, String>;

    async fn active_funding_schedule(
        &self,
        household_id: Uuid,
        member_id: Uuid,
        pool_id: Uuid,
    ) -> Result<Option<Uuid>, String>;

    async fn rule_owner(
        &self,
        household_id: Uuid,
        pool_id: Uuid,
        version: i32,
    ) -> Result<Option<Uuid>, String>;
}
