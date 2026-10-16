use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{AsyncPgConnection, schema::public};

#[derive(Debug)]
pub enum AddPoolToDefaultAllocationError {
    Database(DieselError),
    NumericOutOfRange,
    HouseholdNotFound,
}

impl From<DieselError> for AddPoolToDefaultAllocationError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn add_pool_to_default_allocation(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
    pool_id: Uuid,
) -> Result<(i64, i64), AddPoolToDefaultAllocationError> {
    use public::allocation_defaults::dsl as defaults;
    use public::households::dsl as households;

    let (revision, current_version) = households::households
        .filter(households::id.eq(household_id))
        .for_update()
        .select((households::revision, households::default_allocation_version))
        .first::<(i64, i64)>(conn)
        .await
        .optional()?
        .ok_or(AddPoolToDefaultAllocationError::HouseholdNotFound)?;
    let version = current_version
        .checked_add(1)
        .ok_or(AddPoolToDefaultAllocationError::NumericOutOfRange)?;
    let prior_defaults = defaults::allocation_defaults
        .filter(defaults::household_id.eq(household_id))
        .filter(defaults::version.eq(current_version))
        .select((defaults::pool_id, defaults::weight))
        .load::<(Uuid, i64)>(conn)
        .await?;
    let rows = prior_defaults
        .into_iter()
        .map(|(prior_pool_id, weight)| {
            (
                defaults::household_id.eq(household_id),
                defaults::version.eq(version),
                defaults::pool_id.eq(prior_pool_id),
                defaults::weight.eq(weight),
            )
        })
        .collect::<Vec<_>>();
    if !rows.is_empty() {
        diesel::insert_into(defaults::allocation_defaults)
            .values(rows)
            .execute(&mut *conn)
            .await?;
    }
    diesel::insert_into(defaults::allocation_defaults)
        .values((
            defaults::household_id.eq(household_id),
            defaults::version.eq(version),
            defaults::pool_id.eq(pool_id),
            defaults::weight.eq(1_i64),
        ))
        .execute(&mut *conn)
        .await?;
    let revision = revision
        .checked_add(1)
        .ok_or(AddPoolToDefaultAllocationError::NumericOutOfRange)?;
    diesel::update(households::households.filter(households::id.eq(household_id)))
        .set((
            households::default_allocation_version.eq(version),
            households::revision.eq(revision),
            households::updated_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;
    Ok((revision, version))
}
