use chrono::Utc;
use diesel::{
    BoolExpressionMethods, ExpressionMethods, OptionalExtension, QueryDsl, dsl::now,
    result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_household_change::UiChangeDescriptor;
use household_application::membership::{
    LeaveHouseholdCommand, LeaveHouseholdOperations, MembershipChange, MembershipChangeDecision,
    MembershipError, MembershipFacts, RemoveHouseholdMemberCommand,
    RemoveHouseholdMemberOperations, TransferHouseholdOwnerOperations, TransferOwnerCommand,
};
use household_application::workflow_failure::WorkflowFailure;
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

impl From<MembershipError> for HouseholdMembershipError {
    fn from(error: MembershipError) -> Self {
        map_workflow_error(error)
    }
}

struct PostgresMembershipOperations<'a> {
    conn: &'a mut crate::AsyncPgConnection,
}

impl<'a> PostgresMembershipOperations<'a> {
    fn new(conn: &'a mut crate::AsyncPgConnection) -> Self {
        Self { conn }
    }

    async fn load_facts(
        &mut self,
        household_id: &str,
        actor_id: &str,
        target_id: &str,
    ) -> Result<MembershipFacts, HouseholdMembershipError> {
        use public::{household_members as members, households};
        let household_id = parse_uuid(household_id, "household_id")?;
        let actor_id = parse_uuid(actor_id, "actor_id")?;
        let target_id = parse_uuid(target_id, "member_id")?;
        households::table
            .filter(households::id.eq(household_id))
            .for_update()
            .select(households::id)
            .first::<Uuid>(&mut *self.conn)
            .await
            .optional()?
            .ok_or(HouseholdMembershipError::HouseholdNotFound)?;
        let actor_role = members::table
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(actor_id))
            .filter(members::left_at.is_null())
            .select(members::role)
            .first::<String>(&mut *self.conn)
            .await
            .optional()?;
        let target_role = members::table
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(target_id))
            .filter(members::left_at.is_null())
            .for_update()
            .select(members::role)
            .first::<String>(&mut *self.conn)
            .await
            .optional()?;
        let owner_count = members::table
            .filter(members::household_id.eq(household_id))
            .filter(members::left_at.is_null())
            .filter(members::role.eq("OWNER"))
            .count()
            .get_result::<i64>(&mut *self.conn)
            .await?;
        let has_active_funding_reference =
            has_active_funding_reference(self.conn, household_id, target_id).await?;
        Ok(MembershipFacts {
            actor_role,
            target_role,
            owner_count: usize::try_from(owner_count).unwrap_or(0),
            has_active_funding_reference,
        })
    }

    async fn persist(
        &mut self,
        decision: MembershipChangeDecision,
    ) -> Result<String, HouseholdMembershipError> {
        use public::{household_members as members, households};
        let household_id = decision.household_id.value();
        let actor_id = decision.actor_id.value();
        let member_id = decision.member_id.value();
        households::table
            .filter(households::id.eq(household_id))
            .select(households::id)
            .first::<Uuid>(&mut *self.conn)
            .await
            .optional()?
            .ok_or(HouseholdMembershipError::HouseholdNotFound)?;
        let change_descriptor = decision.change.ui_change();
        match decision.change {
            MembershipChange::Leave => {
                let affected = diesel::update(
                    members::table
                        .filter(members::household_id.eq(household_id))
                        .filter(members::member_id.eq(member_id))
                        .filter(members::left_at.is_null()),
                )
                .set(members::left_at.eq(now))
                .execute(&mut *self.conn)
                .await?;
                if affected != 1 {
                    return Err(HouseholdMembershipError::MemberNotFound);
                }
            }
            MembershipChange::Remove => {
                let affected = diesel::update(
                    members::table
                        .filter(members::household_id.eq(household_id))
                        .filter(members::member_id.eq(member_id))
                        .filter(members::left_at.is_null()),
                )
                .set(members::left_at.eq(now))
                .execute(&mut *self.conn)
                .await?;
                if affected != 1 {
                    return Err(HouseholdMembershipError::MemberNotFound);
                }
            }
            MembershipChange::TransferOwner {
                new_owner_was_owner,
            } => {
                diesel::update(
                    members::table
                        .filter(members::household_id.eq(household_id))
                        .filter(members::member_id.eq(actor_id))
                        .filter(members::left_at.is_null()),
                )
                .set(members::role.eq("MEMBER"))
                .execute(&mut *self.conn)
                .await?;
                if !new_owner_was_owner {
                    diesel::update(
                        members::table
                            .filter(members::household_id.eq(household_id))
                            .filter(members::member_id.eq(member_id))
                            .filter(members::left_at.is_null()),
                    )
                    .set(members::role.eq("OWNER"))
                    .execute(&mut *self.conn)
                    .await?;
                }
            }
        };
        record_change(
            self.conn,
            household_id,
            member_id,
            actor_id,
            change_descriptor,
        )
        .await?;
        Ok(household_id.to_string())
    }
}

impl LeaveHouseholdOperations for PostgresMembershipOperations<'_> {
    type Error = HouseholdMembershipError;

    async fn load_facts(
        &mut self,
        household_id: &str,
        actor_id: &str,
        target_id: &str,
    ) -> Result<MembershipFacts, Self::Error> {
        PostgresMembershipOperations::load_facts(self, household_id, actor_id, target_id).await
    }

    async fn persist(&mut self, decision: MembershipChangeDecision) -> Result<String, Self::Error> {
        PostgresMembershipOperations::persist(self, decision).await
    }
}

impl RemoveHouseholdMemberOperations for PostgresMembershipOperations<'_> {
    type Error = HouseholdMembershipError;

    async fn load_facts(
        &mut self,
        household_id: &str,
        actor_id: &str,
        target_id: &str,
    ) -> Result<MembershipFacts, Self::Error> {
        PostgresMembershipOperations::load_facts(self, household_id, actor_id, target_id).await
    }

    async fn persist(&mut self, decision: MembershipChangeDecision) -> Result<String, Self::Error> {
        PostgresMembershipOperations::persist(self, decision).await
    }
}

impl TransferHouseholdOwnerOperations for PostgresMembershipOperations<'_> {
    type Error = HouseholdMembershipError;

    async fn load_facts(
        &mut self,
        household_id: &str,
        actor_id: &str,
        target_id: &str,
    ) -> Result<MembershipFacts, Self::Error> {
        PostgresMembershipOperations::load_facts(self, household_id, actor_id, target_id).await
    }

    async fn persist(&mut self, decision: MembershipChangeDecision) -> Result<String, Self::Error> {
        PostgresMembershipOperations::persist(self, decision).await
    }
}

pub async fn leave_household(
    pool: &PgPool,
    command: LeaveHouseholdCommand,
) -> Result<PersistedMembershipChange, HouseholdMembershipError> {
    leave_household_inner(pool, command).await
}

pub async fn leave_household_workflow(
    pool: &PgPool,
    command: LeaveHouseholdCommand,
) -> Result<PersistedMembershipChange, WorkflowFailure<MembershipError, HouseholdMembershipError>> {
    leave_household_inner(pool, command)
        .await
        .map_err(map_workflow_failure)
}

async fn leave_household_inner(
    pool: &PgPool,
    command: LeaveHouseholdCommand,
) -> Result<PersistedMembershipChange, HouseholdMembershipError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdMembershipError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedMembershipChange, HouseholdMembershipError, _>(async |conn| {
            let mut operations = PostgresMembershipOperations::new(conn);
            let household_id =
                household_application::membership::leave_household(&mut operations, command)
                    .await?;
            Ok(PersistedMembershipChange { household_id })
        })
        .await
}

pub async fn remove_household_member(
    pool: &PgPool,
    command: RemoveHouseholdMemberCommand,
) -> Result<PersistedMembershipChange, HouseholdMembershipError> {
    remove_household_member_inner(pool, command).await
}

pub async fn remove_household_member_workflow(
    pool: &PgPool,
    command: RemoveHouseholdMemberCommand,
) -> Result<PersistedMembershipChange, WorkflowFailure<MembershipError, HouseholdMembershipError>> {
    remove_household_member_inner(pool, command)
        .await
        .map_err(map_workflow_failure)
}

async fn remove_household_member_inner(
    pool: &PgPool,
    command: RemoveHouseholdMemberCommand,
) -> Result<PersistedMembershipChange, HouseholdMembershipError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdMembershipError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedMembershipChange, HouseholdMembershipError, _>(async |conn| {
            let mut operations = PostgresMembershipOperations::new(conn);
            let household_id = household_application::membership::remove_household_member(
                &mut operations,
                command,
            )
            .await?;
            Ok(PersistedMembershipChange { household_id })
        })
        .await
}

pub async fn transfer_owner(
    pool: &PgPool,
    command: TransferOwnerCommand,
) -> Result<PersistedMembershipChange, HouseholdMembershipError> {
    transfer_owner_inner(pool, command).await
}

pub async fn transfer_owner_workflow(
    pool: &PgPool,
    command: TransferOwnerCommand,
) -> Result<PersistedMembershipChange, WorkflowFailure<MembershipError, HouseholdMembershipError>> {
    transfer_owner_inner(pool, command)
        .await
        .map_err(map_workflow_failure)
}

async fn transfer_owner_inner(
    pool: &PgPool,
    command: TransferOwnerCommand,
) -> Result<PersistedMembershipChange, HouseholdMembershipError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| HouseholdMembershipError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedMembershipChange, HouseholdMembershipError, _>(async |conn| {
            let mut operations = PostgresMembershipOperations::new(conn);
            let household_id =
                household_application::membership::transfer_owner(&mut operations, command).await?;
            Ok(PersistedMembershipChange { household_id })
        })
        .await
}

fn map_workflow_failure(
    error: HouseholdMembershipError,
) -> WorkflowFailure<MembershipError, HouseholdMembershipError> {
    match error {
        HouseholdMembershipError::InvalidUuid(_) => {
            WorkflowFailure::Decision(MembershipError::InvalidIdentifier)
        }
        HouseholdMembershipError::Workflow(MembershipError::InvalidIdentifier) => {
            WorkflowFailure::Decision(MembershipError::InvalidIdentifier)
        }
        HouseholdMembershipError::HouseholdNotFound | HouseholdMembershipError::MemberNotFound => {
            WorkflowFailure::NotFound
        }
        HouseholdMembershipError::Conflict => WorkflowFailure::Conflict,
        HouseholdMembershipError::Workflow(error) => WorkflowFailure::Decision(error),
        error @ (HouseholdMembershipError::Pool(_) | HouseholdMembershipError::Database(_)) => {
            WorkflowFailure::Infrastructure(error)
        }
    }
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
    change: UiChangeDescriptor,
) -> Result<(), HouseholdMembershipError> {
    use public::households;
    let revision = diesel::update(households::table.filter(households::id.eq(household_id)))
        .set((
            households::revision.eq(households::revision + 1),
            households::updated_at.eq(now),
        ))
        .returning(households::revision)
        .get_result::<i64>(&mut *conn)
        .await?;
    horse_postgres_common::insert_ui_change(
        conn,
        household_id,
        entity_id,
        revision,
        actor_id,
        Utc::now(),
        change,
    )
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
