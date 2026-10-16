use std::{future::Future, pin::Pin};
use uuid::Uuid;

#[derive(Debug)]
pub enum MonthlySpendingQueryError {
    HouseholdNotFound,
    InvalidPeriod,
    AmountOutOfRange,
    Infrastructure(String),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MonthlyCategorySpendingProjection {
    pub period: String,
    pub category_id: Option<Uuid>,
    pub category_name: Option<String>,
    pub amount_yen: i64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MonthlyMemberSpendingProjection {
    pub period: String,
    pub member_id: Uuid,
    pub member_name: String,
    pub amount_yen: i64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MonthlyMemberSpendingReportProjection {
    pub items: Vec<MonthlyMemberSpendingProjection>,
    pub has_unattributed_purchases: bool,
}

pub trait MonthlySpendingQuery: Send + Sync {
    fn monthly_category_spending<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
        from: &'a str,
        to: &'a str,
    ) -> Pin<
        Box<
            dyn Future<
                    Output = Result<
                        Vec<MonthlyCategorySpendingProjection>,
                        MonthlySpendingQueryError,
                    >,
                > + Send
                + 'a,
        >,
    >;

    fn monthly_member_spending<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
        from: &'a str,
        to: &'a str,
    ) -> Pin<
        Box<
            dyn Future<
                    Output = Result<
                        MonthlyMemberSpendingReportProjection,
                        MonthlySpendingQueryError,
                    >,
                > + Send
                + 'a,
        >,
    >;
}
