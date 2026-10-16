use std::collections::HashMap;

use chrono::{DateTime, Utc};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, JoinOnDsl, QueryDsl, result::Error as DieselError,
};
use diesel_async::{AsyncPgConnection, RunQueryDsl};
use uuid::Uuid;

use crate::schema::public;

pub async fn members_for_settlement(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
) -> Result<Vec<(Uuid, String, Option<DateTime<Utc>>)>, DieselError> {
    use public::{household_members, members};

    household_members::table
        .inner_join(members::table.on(members::id.eq(household_members::member_id)))
        .filter(household_members::household_id.eq(household_id))
        .order((household_members::joined_at, household_members::member_id))
        .select((
            household_members::member_id,
            members::display_name,
            household_members::left_at,
        ))
        .load::<(Uuid, String, Option<DateTime<Utc>>)>(conn)
        .await
}

pub async fn members_present_after(
    conn: &mut AsyncPgConnection,
    household_id: Uuid,
    period_start_utc: DateTime<Utc>,
) -> Result<Vec<(Uuid, String)>, DieselError> {
    use public::{household_members, members};

    household_members::table
        .inner_join(members::table.on(members::id.eq(household_members::member_id)))
        .filter(household_members::household_id.eq(household_id))
        .filter(
            household_members::left_at
                .is_null()
                .or(household_members::left_at.gt(period_start_utc)),
        )
        .select((household_members::member_id, members::display_name))
        .load::<(Uuid, String)>(conn)
        .await
}

pub async fn transfer_members_for_households(
    conn: &mut AsyncPgConnection,
    household_ids: &[Uuid],
    member_ids: &[Uuid],
) -> Result<(HashMap<Uuid, String>, HashMap<(Uuid, Uuid), String>), DieselError> {
    let members = public::members::table
        .filter(public::members::id.eq_any(member_ids))
        .select((public::members::id, public::members::display_name))
        .load::<(Uuid, String)>(conn)
        .await?
        .into_iter()
        .collect();
    let roles = household_postgres::household_access::member_roles_for_households(
        conn,
        household_ids,
        member_ids,
    )
    .await?;
    Ok((members, roles))
}
