use std::{future::Future, pin::Pin};
use uuid::Uuid;

use crate::account_balances::AccountBalance;

#[derive(Debug)]
pub enum AccountBalancesQueryError {
    Infrastructure(String),
}

pub trait AccountBalancesQuery: Send + Sync {
    fn query<'a>(
        &'a self,
        household_id: Uuid,
    ) -> Pin<
        Box<
            dyn Future<Output = Result<Vec<AccountBalance>, AccountBalancesQueryError>> + Send + 'a,
        >,
    >;
}
