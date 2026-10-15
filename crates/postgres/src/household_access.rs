use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum HouseholdAccessError {
    Pool(String),
    Database(DieselError),
}

impl From<DieselError> for HouseholdAccessError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn is_active_member(
    pool: &PgPool,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<bool, HouseholdAccessError> {
    use public::household_members::dsl as members;

    let mut conn = pool
        .get()
        .await
        .map_err(|error| HouseholdAccessError::Pool(error.to_string()))?;
    members::household_members
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .filter(members::left_at.is_null())
        .select(members::member_id)
        .first::<Uuid>(&mut conn)
        .await
        .optional()
        .map(|member| member.is_some())
        .map_err(Into::into)
}
