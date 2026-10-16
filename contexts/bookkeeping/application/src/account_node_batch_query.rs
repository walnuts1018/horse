use std::{future::Future, pin::Pin};

use super::account_history_query::{AccountIncomeProjection, AccountMovementProjection};
use uuid::Uuid;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AccountOpeningBalanceProjection {
    pub account_id: Uuid,
    pub account_name: String,
    pub account_kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<Uuid>,
}

#[derive(Clone, Debug, Default, Eq, PartialEq)]
pub struct AccountNodeBatchProjection {
    pub opening_balances: Vec<AccountOpeningBalanceProjection>,
    pub movements: Vec<AccountMovementProjection>,
    pub incomes: Vec<AccountIncomeProjection>,
}

#[derive(Debug)]
pub enum AccountNodeBatchQueryError {
    Infrastructure(String),
}

pub trait AccountNodeBatchQuery: Send + Sync {
    fn load<'a>(
        &'a self,
        household_ids: &'a [Uuid],
        opening_balance_ids: &'a [Uuid],
        movement_ids: &'a [Uuid],
        income_ids: &'a [Uuid],
    ) -> Pin<
        Box<
            dyn Future<Output = Result<AccountNodeBatchProjection, AccountNodeBatchQueryError>>
                + Send
                + 'a,
        >,
    >;
}
