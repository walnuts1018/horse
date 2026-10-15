use chrono::Utc;
use diesel::{
    BoolExpressionMethods, ExpressionMethods, OptionalExtension, QueryDsl, dsl::now,
    result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use household_workflows::membership::{
    LeaveHouseholdCommand, MembershipChange, MembershipError, RemoveHouseholdMemberCommand,
    TransferOwnerCommand, decide_leave_household, decide_remove_household_member,
    decide_transfer_owner,
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
    let household_id = parse_uuid(&command.household_id, "household_id")?;
    let member_id = parse_uuid(&command.member_id, "member_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdMembershipError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedMembershipChange, HouseholdMembershipError, _>(async |conn| {
            use public::{household_members as members, households};
            let household = households::table
                .filter(households::id.eq(household_id))
                .for_update()
                .select(households::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .ok_or(HouseholdMembershipError::HouseholdNotFound)?;
            let role = members::table
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(member_id))
                .filter(members::left_at.is_null())
                .select(members::role)
                .first::<String>(&mut *conn)
                .await
                .optional()?
                .ok_or(HouseholdMembershipError::MemberNotFound)?;
            let owners = members::table
                .filter(members::household_id.eq(household_id))
                .filter(members::left_at.is_null())
                .filter(members::role.eq("OWNER"))
                .count()
                .get_result::<i64>(&mut *conn)
                .await?;
            let referenced = has_active_funding_reference(conn, household_id, member_id).await?;
            let _decision = decide_leave_household(
                command.clone(),
                Some(&role),
                usize::try_from(owners).unwrap_or(0),
                referenced,
            )
            .map_err(map_workflow_error)?;
            let affected = diesel::update(
                members::table
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq(member_id))
                    .filter(members::left_at.is_null()),
            )
            .set(members::left_at.eq(now))
            .execute(&mut *conn)
            .await?;
            if affected != 1 {
                return Err(HouseholdMembershipError::MemberNotFound);
            }
            record_change(
                conn,
                household_id,
                member_id,
                member_id,
                "household.member.left",
                vec![Some("members".to_owned())],
            )
            .await?;
            Ok(PersistedMembershipChange {
                household_id: household.to_string(),
            })
        })
        .await
}

pub async fn remove_household_member(
    pool: &PgPool,
    command: RemoveHouseholdMemberCommand,
) -> Result<PersistedMembershipChange, HouseholdMembershipError> {
    let household_id = parse_uuid(&command.household_id, "household_id")?;
    let actor_id = parse_uuid(&command.actor_id, "actor_id")?;
    let member_id = parse_uuid(&command.member_id, "member_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdMembershipError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedMembershipChange, HouseholdMembershipError, _>(async |conn| {
            use public::{household_members as members, households};
            let household = households::table
                .filter(households::id.eq(household_id))
                .for_update()
                .select(households::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .ok_or(HouseholdMembershipError::HouseholdNotFound)?;
            let actor_role = members::table
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(actor_id))
                .filter(members::left_at.is_null())
                .select(members::role)
                .first::<String>(&mut *conn)
                .await
                .optional()?;
            let target_role = members::table
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(member_id))
                .filter(members::left_at.is_null())
                .for_update()
                .select(members::role)
                .first::<String>(&mut *conn)
                .await
                .optional()?;
            let owners = members::table
                .filter(members::household_id.eq(household_id))
                .filter(members::left_at.is_null())
                .filter(members::role.eq("OWNER"))
                .count()
                .get_result::<i64>(&mut *conn)
                .await?;
            let referenced = has_active_funding_reference(conn, household_id, member_id).await?;
            decide_remove_household_member(
                command,
                actor_role.as_deref(),
                target_role.as_deref(),
                usize::try_from(owners).unwrap_or(0),
                referenced,
            )
            .map_err(map_workflow_error)?;
            let affected = diesel::update(
                members::table
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq(member_id))
                    .filter(members::left_at.is_null()),
            )
            .set(members::left_at.eq(now))
            .execute(&mut *conn)
            .await?;
            if affected != 1 {
                return Err(HouseholdMembershipError::MemberNotFound);
            }
            record_change(
                conn,
                household_id,
                member_id,
                actor_id,
                "household.member.removed",
                vec![Some("members".to_owned())],
            )
            .await?;
            Ok(PersistedMembershipChange {
                household_id: household.to_string(),
            })
        })
        .await
}

pub async fn transfer_owner(
    pool: &PgPool,
    command: TransferOwnerCommand,
) -> Result<PersistedMembershipChange, HouseholdMembershipError> {
    let household_id = parse_uuid(&command.household_id, "household_id")?;
    let actor_id = parse_uuid(&command.actor_id, "actor_id")?;
    let new_owner_id = parse_uuid(&command.new_owner_id, "new_owner_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdMembershipError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedMembershipChange, HouseholdMembershipError, _>(async |conn| {
            use public::{household_members as members, households};
            let household = households::table
                .filter(households::id.eq(household_id))
                .for_update()
                .select(households::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .ok_or(HouseholdMembershipError::HouseholdNotFound)?;
            let actor_role = members::table
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(actor_id))
                .filter(members::left_at.is_null())
                .select(members::role)
                .first::<String>(&mut *conn)
                .await
                .optional()?;
            let target_role = members::table
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(new_owner_id))
                .filter(members::left_at.is_null())
                .select(members::role)
                .first::<String>(&mut *conn)
                .await
                .optional()?;
            let decision =
                decide_transfer_owner(command, actor_role.as_deref(), target_role.as_deref())
                    .map_err(map_workflow_error)?;
            diesel::update(
                members::table
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq(actor_id))
                    .filter(members::left_at.is_null()),
            )
            .set(members::role.eq("MEMBER"))
            .execute(&mut *conn)
            .await?;
            if !matches!(
                decision.change,
                MembershipChange::TransferOwner {
                    new_owner_was_owner: true
                }
            ) {
                diesel::update(
                    members::table
                        .filter(members::household_id.eq(household_id))
                        .filter(members::member_id.eq(new_owner_id))
                        .filter(members::left_at.is_null()),
                )
                .set(members::role.eq("OWNER"))
                .execute(&mut *conn)
                .await?;
            }
            record_change(
                conn,
                household_id,
                new_owner_id,
                actor_id,
                "household.owner.transferred",
                vec![Some("members".to_owned()), Some("owner".to_owned())],
            )
            .await?;
            Ok(PersistedMembershipChange {
                household_id: household.to_string(),
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
