pub use bookkeeping_domain::create_account::*;

use std::{future::Future, pin::Pin};

use uuid::Uuid;

use crate::workflow_failure::WorkflowFailure;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CreateAccountRequest {
    pub actor_id: Uuid,
    pub household_id: Uuid,
    pub name: String,
    pub kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<Uuid>,
    pub opening_balance: Option<CreateAccountOpeningBalance>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CreateAccountOpeningBalance {
    pub amount_yen: i64,
    pub at: String,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CreatedAccount {
    pub id: Uuid,
    pub name: String,
    pub kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<Uuid>,
}

pub type CreateAccountFuture<'a> = Pin<
    Box<
        dyn Future<Output = Result<CreatedAccount, WorkflowFailure<CreateAccountError, String>>>
            + Send
            + 'a,
    >,
>;

pub trait CreateAccountHandler: Send + Sync {
    fn handle<'a>(&'a self, request: CreateAccountRequest) -> CreateAccountFuture<'a>;
}
