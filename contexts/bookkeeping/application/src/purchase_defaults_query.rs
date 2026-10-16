use std::{future::Future, pin::Pin};

use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PurchaseDefaultProjection {
    pub merchant: String,
    pub payment_account_id: Uuid,
    pub purchase_count: i32,
    pub last_used_at: DateTime<Utc>,
}

#[derive(Debug)]
pub enum PurchaseDefaultsQueryError {
    HouseholdNotFound,
    CountOutOfRange,
    Infrastructure(String),
}

pub trait PurchaseDefaultsQuery: Send + Sync {
    fn query<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
    ) -> Pin<
        Box<
            dyn Future<Output = Result<Vec<PurchaseDefaultProjection>, PurchaseDefaultsQueryError>>
                + Send
                + 'a,
        >,
    >;
}
