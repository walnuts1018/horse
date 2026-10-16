use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use household_application::record_change::RecordHouseholdChange;

use crate::{AsyncPgConnection, schema::public};

#[derive(Debug)]
pub enum RecordHouseholdChangeError {
    Database(DieselError),
    HouseholdNotFound,
    NumericOutOfRange,
}

impl From<DieselError> for RecordHouseholdChangeError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn record_household_change(
    conn: &mut AsyncPgConnection,
    change: RecordHouseholdChange,
) -> Result<i64, RecordHouseholdChangeError> {
    let current_revision = public::households::table
        .filter(public::households::id.eq(change.household_id))
        .for_update()
        .select(public::households::revision)
        .first::<i64>(conn)
        .await
        .optional()?
        .ok_or(RecordHouseholdChangeError::HouseholdNotFound)?;
    let revision = current_revision
        .checked_add(1)
        .ok_or(RecordHouseholdChangeError::NumericOutOfRange)?;
    diesel::update(
        public::households::table.filter(public::households::id.eq(change.household_id)),
    )
    .set((
        public::households::revision.eq(revision),
        public::households::updated_at.eq(now),
    ))
    .execute(&mut *conn)
    .await?;
    horse_postgres_common::insert_ui_change(
        conn,
        change.household_id,
        change.entity_id,
        revision,
        change.actor_id,
        change.occurred_at,
        change.change,
    )
    .await?;
    Ok(revision)
}
