use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::RunQueryDsl;
use std::collections::{HashMap, HashSet};
use uuid::Uuid;

use crate::{AsyncPgConnection, PgPool, schema::public};

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

pub async fn household_exists(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
) -> Result<bool, DieselError> {
    use public::households::dsl as households;

    households::households
        .filter(households::id.eq(household_id))
        .select(households::id)
        .first::<Uuid>(conn)
        .await
        .optional()
        .map(|household| household.is_some())
}

pub async fn lock_household_for_update(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
) -> Result<bool, DieselError> {
    use public::households::dsl as households;

    households::households
        .filter(households::id.eq(household_id))
        .for_update()
        .select(households::id)
        .first::<Uuid>(conn)
        .await
        .optional()
        .map(|household| household.is_some())
}

pub async fn lock_household_for_share(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
) -> Result<bool, DieselError> {
    use public::households::dsl as households;

    households::households
        .filter(households::id.eq(household_id))
        .for_share()
        .select(households::id)
        .first::<Uuid>(conn)
        .await
        .optional()
        .map(|household| household.is_some())
}

pub async fn default_allocation_version_for_share(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
) -> Result<Option<i64>, DieselError> {
    use public::households::dsl as households;

    households::households
        .filter(households::id.eq(household_id))
        .for_share()
        .select(households::default_allocation_version)
        .first::<i64>(conn)
        .await
        .optional()
}

pub async fn active_member_role(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<Option<String>, DieselError> {
    use public::household_members::dsl as members;

    members::household_members
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .filter(members::left_at.is_null())
        .for_share()
        .select(members::role)
        .first::<String>(conn)
        .await
        .optional()
}

pub async fn member_ids_for_household(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
    member_ids: &[Uuid],
    active_only: bool,
    lock_for_update: bool,
) -> Result<HashSet<Uuid>, DieselError> {
    use public::household_members::dsl as members;

    if member_ids.is_empty() {
        return Ok(HashSet::new());
    }
    let rows = match (active_only, lock_for_update) {
        (true, true) => {
            members::household_members
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq_any(member_ids))
                .filter(members::left_at.is_null())
                .for_update()
                .select(members::member_id)
                .load::<Uuid>(conn)
                .await?
        }
        (true, false) => {
            members::household_members
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq_any(member_ids))
                .filter(members::left_at.is_null())
                .select(members::member_id)
                .load::<Uuid>(conn)
                .await?
        }
        (false, true) => {
            members::household_members
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq_any(member_ids))
                .for_update()
                .select(members::member_id)
                .load::<Uuid>(conn)
                .await?
        }
        (false, false) => {
            members::household_members
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq_any(member_ids))
                .select(members::member_id)
                .load::<Uuid>(conn)
                .await?
        }
    };
    Ok(rows.into_iter().collect())
}

pub async fn member_roles_for_households(
    conn: &mut AsyncPgConnection,
    household_ids: &[Uuid],
    member_ids: &[Uuid],
) -> Result<HashMap<(Uuid, Uuid), String>, DieselError> {
    use public::household_members::dsl as members;

    if household_ids.is_empty() || member_ids.is_empty() {
        return Ok(HashMap::new());
    }
    members::household_members
        .filter(members::household_id.eq_any(household_ids))
        .filter(members::member_id.eq_any(member_ids))
        .select((members::household_id, members::member_id, members::role))
        .load::<(Uuid, Uuid, String)>(conn)
        .await
        .map(|rows| {
            rows.into_iter()
                .map(|(household_id, member_id, role)| ((household_id, member_id), role))
                .collect()
        })
}
