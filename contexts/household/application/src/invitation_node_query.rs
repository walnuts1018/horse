use chrono::{DateTime, Utc};
use std::{future::Future, pin::Pin};
use uuid::Uuid;

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HouseholdInvitationNodeQueryInput {
    pub actor_id: Uuid,
    pub household_ids: Vec<Uuid>,
    pub invitation_ids: Vec<Uuid>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HouseholdInvitationProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub created_at: DateTime<Utc>,
    pub expires_at: DateTime<Utc>,
    pub accepted_at: Option<DateTime<Utc>>,
    pub revoked_at: Option<DateTime<Utc>>,
}

pub trait HouseholdInvitationNodeQuery: Send + Sync {
    fn load<'a>(
        &'a self,
        input: HouseholdInvitationNodeQueryInput,
    ) -> Pin<Box<dyn Future<Output = Result<Vec<HouseholdInvitationProjection>, String>> + Send + 'a>>;
}
