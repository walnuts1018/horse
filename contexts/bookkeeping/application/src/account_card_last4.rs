pub use bookkeeping_domain::account_card_last4::*;

use std::{future::Future, pin::Pin};

use uuid::Uuid;

use crate::workflow_failure::WorkflowFailure;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SetCardLast4Request {
    pub actor_id: Uuid,
    pub account_id: Uuid,
    pub card_last4: Option<String>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AccountCardLast4Projection {
    pub id: Uuid,
    pub name: String,
    pub kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<Uuid>,
}

pub trait SetCardLast4Handler: Send + Sync {
    fn handle<'a>(
        &'a self,
        request: SetCardLast4Request,
    ) -> Pin<
        Box<
            dyn Future<
                    Output = Result<
                        AccountCardLast4Projection,
                        WorkflowFailure<SetCardLast4Error, String>,
                    >,
                > + Send
                + 'a,
        >,
    >;
}
