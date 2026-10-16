use chrono::{DateTime, Utc};
use diesel::{ExpressionMethods, insert_into};
use diesel_async::RunQueryDsl;
use horse_household_change::UiChangeDescriptor;
use uuid::Uuid;

use crate::{AsyncPgConnection, schema::public::household_events};

pub async fn insert_ui_change(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
    entity_id: Uuid,
    revision: i64,
    actor_id: Uuid,
    created_at: DateTime<Utc>,
    change: UiChangeDescriptor,
) -> Result<usize, diesel::result::Error> {
    insert_into(household_events::table)
        .values((
            household_events::household_id.eq(household_id),
            household_events::event_type.eq(change.event_type),
            household_events::entity_kind.eq(change.entity_kind),
            household_events::entity_id.eq(entity_id),
            household_events::revision.eq(revision),
            household_events::changed_fields.eq(change
                .changed_fields
                .into_iter()
                .map(Some)
                .collect::<Vec<_>>()),
            household_events::payload.eq(change.payload),
            household_events::actor_id.eq(actor_id),
            household_events::created_at.eq(created_at),
        ))
        .execute(conn)
        .await
}
