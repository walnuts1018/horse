use async_trait::async_trait;
use chrono::NaiveDate;
use uuid::Uuid;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HomeDailySpendingProjection {
    pub date: NaiveDate,
    pub amount_yen: i64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HomeSummaryProjection {
    pub daily_spending: Vec<HomeDailySpendingProjection>,
    pub monthly_item_count: i64,
}

#[derive(Debug)]
pub enum HomeSummaryQueryError {
    HouseholdNotFound,
    AmountOutOfRange,
    Failed(String),
}

#[async_trait]
pub trait HomeSummaryQuery: Send + Sync {
    async fn load(
        &self,
        actor_id: Uuid,
        household_id: Uuid,
    ) -> Result<HomeSummaryProjection, HomeSummaryQueryError>;
}
