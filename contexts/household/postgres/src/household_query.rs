use diesel::{ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum HouseholdQueryError {
    Pool(String),
    Database(diesel::result::Error),
}

impl From<diesel::result::Error> for HouseholdQueryError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}

pub async fn list_household_ids(
    pool: &PgPool,
    actor_id: Uuid,
) -> Result<Vec<Uuid>, HouseholdQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| HouseholdQueryError::Pool(error.to_string()))?;
    use public::household_members;

    household_members::table
        .filter(household_members::member_id.eq(actor_id))
        .filter(household_members::left_at.is_null())
        .order_by(household_members::joined_at.asc())
        .select(household_members::household_id)
        .load::<Uuid>(&mut *conn)
        .await
        .map_err(Into::into)
}
