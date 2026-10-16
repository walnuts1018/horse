use std::{future::Future, pin::Pin};

use diesel::{ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use household_application::invitation_node_query::{
    HouseholdInvitationNodeQuery, HouseholdInvitationNodeQueryInput, HouseholdInvitationProjection,
};
use uuid::Uuid;

use crate::{PgPool, schema::public};

pub struct PostgresHouseholdInvitationNodeQuery {
    pool: PgPool,
}

impl PostgresHouseholdInvitationNodeQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl HouseholdInvitationNodeQuery for PostgresHouseholdInvitationNodeQuery {
    fn load<'a>(
        &'a self,
        input: HouseholdInvitationNodeQueryInput,
    ) -> Pin<Box<dyn Future<Output = Result<Vec<HouseholdInvitationProjection>, String>> + Send + 'a>>
    {
        Box::pin(async move {
            if input.household_ids.is_empty() || input.invitation_ids.is_empty() {
                return Ok(Vec::new());
            }
            let mut conn = self
                .pool
                .get()
                .await
                .map_err(|error| format!("{error:?}"))?;
            let owner_household_ids = public::household_members::table
                .filter(public::household_members::household_id.eq_any(&input.household_ids))
                .filter(public::household_members::member_id.eq(input.actor_id))
                .filter(public::household_members::role.eq("OWNER"))
                .filter(public::household_members::left_at.is_null())
                .select(public::household_members::household_id)
                .distinct()
                .load::<Uuid>(&mut conn)
                .await
                .map_err(|error| format!("{error:?}"))?;
            if owner_household_ids.is_empty() {
                return Ok(Vec::new());
            }
            let rows = public::household_invitations::table
                .filter(public::household_invitations::household_id.eq_any(owner_household_ids))
                .filter(public::household_invitations::id.eq_any(&input.invitation_ids))
                .select((
                    public::household_invitations::id,
                    public::household_invitations::household_id,
                    public::household_invitations::created_at,
                    public::household_invitations::expires_at,
                    public::household_invitations::accepted_at,
                    public::household_invitations::revoked_at,
                ))
                .load::<(
                    Uuid,
                    Uuid,
                    chrono::DateTime<chrono::Utc>,
                    chrono::DateTime<chrono::Utc>,
                    Option<chrono::DateTime<chrono::Utc>>,
                    Option<chrono::DateTime<chrono::Utc>>,
                )>(&mut conn)
                .await
                .map_err(|error| format!("{error:?}"))?;
            Ok(rows
                .into_iter()
                .map(
                    |(id, household_id, created_at, expires_at, accepted_at, revoked_at)| {
                        HouseholdInvitationProjection {
                            id,
                            household_id,
                            created_at,
                            expires_at,
                            accepted_at,
                            revoked_at,
                        }
                    },
                )
                .collect())
        })
    }
}
