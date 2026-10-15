use chrono::Utc;
use diesel::{
    BoolExpressionMethods, ExpressionMethods, OptionalExtension, QueryDsl, dsl::now,
    result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use household_application::membership::{
    LeaveHouseholdCommand, LeaveHouseholdOperations, MembershipChange, MembershipChangeDecision,
    MembershipError, MembershipFacts, RemoveHouseholdMemberCommand,
    RemoveHouseholdMemberOperations, TransferHouseholdOwnerOperations, TransferOwnerCommand,
};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedMembershipChange {
    pub household_id: String,
}

#[derive(Debug)]
pub enum HouseholdMembershipError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    Workflow(MembershipError),
    HouseholdNotFound,
    MemberNotFound,
    Conflict,
}

impl From<DieselError> for HouseholdMembershipError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn leave_household(
    pool: &PgPool,
    command: LeaveHouseholdCommand,
) -> Result<PersistedMembershipChange, HouseholdMembershipError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdMembershipError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedMembershipChange, HouseholdMembershipError, _>(async |conn| {
            let mut operations = PostgresMembershipOperations::new(conn);
            let household_id = household_application::membership::leave_household(
                &mut operations,
                command,
            )
            .await?;
            Ok(PersistedMembershipChange {
                household_id,
            })
        })
        .await
}

pub async fn remove_household_member(
    pool: &PgPool,
    command: RemoveHouseholdMemberCommand,
) -> Result<PersistedMembershipChange, HouseholdMembershipError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdMembershipError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedMembershipChange, HouseholdMembershipError, _>(async |conn| {
            let mut operations = PostgresMembershipOperations::new(conn);
            let household_id = household_application::membership::remove_household_member(
                &mut operations,
                command,
            )
            .await?;
            Ok(PersistedMembershipChange {
                household_id,
            })
        })
        .await
}

pub async fn transfer_owner(
    pool: &PgPool,
    command: TransferOwnerCommand,
) -> Result<PersistedMembershipChange, HouseholdMembershipError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdMembershipError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedMembershipChange, HouseholdMembershipError, _>(async |conn| {
            let mut operations = PostgresMembershipOperations::new(conn);
            let household_id = household_application::membership::transfer_owner(
                &mut operations,
                command,
            )
            .await?;
            Ok(PersistedMembershipChange {
                household_id,
            })
        })
        .await
}

async fn has_active_funding_reference(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    member_id: Uuid,
) -> Result<bool, HouseholdMembershipError> {
    use public::{
        funding_schedules as schedules, pool_funding_rules as rules, pool_funding_shares as shares,
        pools,
    };
    let pool_versions = pools::table
        .filter(pools::household_id.eq(household_id))
        .filter(pools::current_funding_version.is_not_null())
        .select((pools::id, pools::current_funding_version))
        .load::<(Uuid, Option<i32>)>(&mut *conn)
        .await?;
    for (pool_id, version) in pool_versions {
        let Some(version) = version else { continue };
        let rule = rules::table
            .filter(rules::household_id.eq(household_id))
            .filter(rules::pool_id.eq(pool_id))
            .filter(rules::version.eq(version))
            .select(rules::owner_member_id)
            .first::<Option<Uuid>>(&mut *conn)
            .await
            .optional()?;
        let Some(owner) = rule else { continue };
        if owner == Some(member_id) {
            return Ok(true);
        }
        let has_share = shares::table
            .filter(shares::household_id.eq(household_id))
            .filter(shares::pool_id.eq(pool_id))
            .filter(shares::version.eq(version))
            .filter(shares::member_id.eq(member_id))
            .select(shares::member_id)
            .first::<Uuid>(&mut *conn)
            .await
            .optional()?
            .is_some();
        if has_share {
            return Ok(true);
        }
    }
    let today = Utc::now().date_naive();
    Ok(schedules::table
        .filter(schedules::household_id.eq(household_id))
        .filter(schedules::member_id.eq(member_id))
        .filter(schedules::active.eq(true))
        .filter(
            schedules::ends_on
                .is_null()
                .or(schedules::ends_on.ge(today)),
        )
        .select(schedules::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .is_some())
}

async fn record_change(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    entity_id: Uuid,
    actor_id: Uuid,
    event_type: &str,
    changed_fields: Vec<Option<String>>,
) -> Result<(), HouseholdMembershipError> {
    use public::{household_events as events, households};
    let revision = diesel::update(households::table.filter(households::id.eq(household_id)))
        .set((
            households::revision.eq(households::revision + 1),
            households::updated_at.eq(now),
        ))
        .returning(households::revision)
        .get_result::<i64>(&mut *conn)
        .await?;
    diesel::insert_into(events::table)
        .values((
            events::household_id.eq(household_id),
            events::event_type.eq(event_type),
            events::entity_kind.eq("member"),
            events::entity_id.eq(entity_id),
            events::revision.eq(revision),
            events::changed_fields.eq(changed_fields),
            events::actor_id.eq(actor_id),
            events::created_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;
    Ok(())
}

fn map_workflow_error(error: MembershipError) -> HouseholdMembershipError {
    match error {
        MembershipError::Forbidden => HouseholdMembershipError::MemberNotFound,
        MembershipError::LastOwner
        | MembershipError::SelfTransfer
        | MembershipError::ReferencedMember => HouseholdMembershipError::Conflict,
        other => HouseholdMembershipError::Workflow(other),
    }
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, HouseholdMembershipError> {
    Uuid::parse_str(value).map_err(|_| HouseholdMembershipError::InvalidUuid(field))
}
