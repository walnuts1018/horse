use std::{future::Future, pin::Pin};

use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AccountProjection {
    pub id: Uuid,
    pub name: String,
    pub kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<Uuid>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AccountMovementProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub kind: String,
    pub occurred_at: DateTime<Utc>,
    pub from_account_id: Uuid,
    pub from_account: AccountProjection,
    pub to_account_id: Uuid,
    pub to_account: AccountProjection,
    pub amount_yen: i64,
    pub description: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AccountIncomeProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub account_id: Uuid,
    pub amount_yen: i64,
    pub occurred_at: DateTime<Utc>,
    pub description: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AccountMovementPage {
    pub rows: Vec<AccountMovementProjection>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AccountIncomePage {
    pub rows: Vec<AccountIncomeProjection>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug)]
pub enum AccountHistoryQueryError {
    HouseholdNotFound,
    InvalidPagination,
    InvalidCursor,
    Infrastructure(String),
}

pub trait AccountHistoryQuery: Send + Sync {
    fn account_movements<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
        after_id: Option<Uuid>,
        before_id: Option<Uuid>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Pin<
        Box<dyn Future<Output = Result<AccountMovementPage, AccountHistoryQueryError>> + Send + 'a>,
    >;

    fn account_incomes<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
        after_id: Option<Uuid>,
        before_id: Option<Uuid>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Pin<
        Box<dyn Future<Output = Result<AccountIncomePage, AccountHistoryQueryError>> + Send + 'a>,
    >;
}
