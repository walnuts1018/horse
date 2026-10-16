use chrono::{DateTime, Utc};
use horse_household_change::UiChangeDescriptor;
use uuid::Uuid;

#[derive(Debug, Clone)]
pub struct RecordHouseholdChange {
    pub household_id: Uuid,
    pub entity_id: Uuid,
    pub actor_id: Uuid,
    pub occurred_at: DateTime<Utc>,
    pub change: UiChangeDescriptor,
}
