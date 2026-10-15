use bookkeeping_workflows::create_household::{
    CreateHouseholdCommand, CreateHouseholdError, decide_create_household,
};
use diesel::{ExpressionMethods, dsl::now, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedHousehold {
    pub household_id: String,
}

#[derive(Debug)]
pub enum CreateHouseholdPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid,
    InvalidWorkflow(CreateHouseholdError),
}

impl From<DieselError> for CreateHouseholdPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_create_household(
    pool: &PgPool,
    command: CreateHouseholdCommand,
) -> Result<PersistedHousehold, CreateHouseholdPersistenceError> {
    let decision = decide_create_household(command)
        .map_err(CreateHouseholdPersistenceError::InvalidWorkflow)?;
    let actor_id = Uuid::parse_str(&decision.actor_id)
        .map_err(|_| CreateHouseholdPersistenceError::InvalidUuid)?;
    let household_id = Uuid::new_v4();

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CreateHouseholdPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedHousehold, CreateHouseholdPersistenceError, _>(async |conn| {
            use public::household_events::dsl as events;
            use public::household_members::dsl as members;
            use public::households::dsl as households;
            use public::members::dsl as people;

            diesel::insert_into(households::households)
                .values((
                    households::id.eq(household_id),
                    households::name.eq(&decision.name),
                    households::revision.eq(1_i64),
                    households::default_allocation_version.eq(1_i64),
                    households::created_at.eq(now),
                    households::updated_at.eq(now),
                ))
                .execute(&mut *conn)
                .await?;

            diesel::insert_into(members::household_members)
                .values((
                    members::household_id.eq(household_id),
                    members::member_id.eq(actor_id),
                    members::role.eq("OWNER"),
                ))
                .execute(&mut *conn)
                .await?;

            for member_name in &decision.member_names {
                let member_id = Uuid::new_v4();
                let subject = format!("invite:{member_id}");
                diesel::insert_into(people::members)
                    .values((
                        people::id.eq(member_id),
                        people::oidc_subject.eq(subject),
                        people::display_name.eq(member_name),
                        people::created_at.eq(now),
                    ))
                    .execute(&mut *conn)
                    .await?;
                diesel::insert_into(members::household_members)
                    .values((
                        members::household_id.eq(household_id),
                        members::member_id.eq(member_id),
                    ))
                    .execute(&mut *conn)
                    .await?;
            }

            diesel::insert_into(events::household_events)
                .values((
                    events::household_id.eq(household_id),
                    events::event_type.eq("household.created"),
                    events::entity_kind.eq("household"),
                    events::entity_id.eq(household_id),
                    events::revision.eq(1_i64),
                    events::changed_fields.eq(Vec::<Option<String>>::new()),
                    events::actor_id.eq(actor_id),
                    events::created_at.eq(now),
                ))
                .execute(&mut *conn)
                .await?;

            Ok(PersistedHousehold {
                household_id: household_id.to_string(),
            })
        })
        .await
}
