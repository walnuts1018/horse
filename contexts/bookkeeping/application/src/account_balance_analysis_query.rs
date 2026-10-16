use std::{future::Future, pin::Pin};

use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountBalanceChange {
    pub journal_id: Uuid,
    pub source_kind: String,
    pub source_id: Uuid,
    pub occurred_at: DateTime<Utc>,
    pub amount_yen: i64,
    pub balance_yen: i64,
    pub description: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountBalanceAnalysis {
    pub account_id: Uuid,
    pub account_name: String,
    pub account_kind: String,
    pub opening_balance_yen: i64,
    pub closing_balance_yen: i64,
    pub changes: Vec<AccountBalanceChange>,
    pub total_count: usize,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug)]
pub enum AccountBalanceAnalysisQueryError {
    Infrastructure(String),
    InvalidInput,
    AccountNotFound,
    BalanceOverflow,
}

pub trait AccountBalanceAnalysisQuery: Send + Sync {
    fn query<'a>(
        &'a self,
        household_id: Uuid,
        account_id: Uuid,
        start: DateTime<Utc>,
        end: DateTime<Utc>,
        after: Option<Uuid>,
        page_size: usize,
    ) -> Pin<
        Box<
            dyn Future<Output = Result<AccountBalanceAnalysis, AccountBalanceAnalysisQueryError>>
                + Send
                + 'a,
        >,
    >;
}
