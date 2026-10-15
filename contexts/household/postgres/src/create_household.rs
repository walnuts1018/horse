use chrono::{DateTime, Utc};
use diesel::{
    ExpressionMethods, JoinOnDsl, OptionalExtension, QueryDsl, dsl::now,
    result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use household_application::create_household::{
    CreateHouseholdCommand, CreateHouseholdDecision, CreateHouseholdError,
    CreateHouseholdOperations,
};
use household_application::ports::{CreateHouseholdPortError, CreatedHousehold, HouseholdStore};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedHousehold {
    pub household_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdMemberReadModel {
    pub id: Uuid,
    pub display_name: String,
    pub role: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdAccountReadModel {
    pub id: Uuid,
    pub name: String,
    pub kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<Uuid>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdCategoryReadModel {
    pub id: Uuid,
    pub parent_id: Option<Uuid>,
    pub name: String,
    pub archived: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdFundingShareReadModel {
    pub member_id: Uuid,
    pub member_name: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdPoolReadModel {
    pub id: Uuid,
    pub name: String,
    pub kind: String,
    pub default_share_weight: i64,
    pub funding_shares: Vec<HouseholdFundingShareReadModel>,
    pub funding_rule_version: i32,
    pub owner_member_id: Option<Uuid>,
    pub settlement_policy: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdAllocationReadModel {
    pub pool_id: Uuid,
    pub pool_name: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HouseholdReadModel {
    pub id: Uuid,
    pub name: String,
    pub revision: i64,
    pub members: Vec<HouseholdMemberReadModel>,
    pub pools: Vec<HouseholdPoolReadModel>,
    pub accounts: Vec<HouseholdAccountReadModel>,
    pub categories: Vec<HouseholdCategoryReadModel>,
    pub default_allocation: Vec<HouseholdAllocationReadModel>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug)]
pub enum CreateHouseholdPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid,
    InvalidWorkflow(CreateHouseholdError),
    HouseholdNotFound,
    MemberNotFound,
    InvalidPersistedData,
}

impl From<DieselError> for CreateHouseholdPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

impl From<CreateHouseholdError> for CreateHouseholdPersistenceError {
    fn from(error: CreateHouseholdError) -> Self {
        Self::InvalidWorkflow(error)
    }
}

pub async fn load_household(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
) -> Result<HouseholdReadModel, CreateHouseholdPersistenceError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| CreateHouseholdPersistenceError::Pool(error.to_string()))?;
    use public::{
        accounts, allocation_defaults, categories, household_members, households, members, pools,
    };

    let household = households::table
        .filter(households::id.eq(household_id))
        .select((
            households::id,
            households::name,
            households::revision,
            households::default_allocation_version,
            households::created_at,
            households::updated_at,
        ))
        .first::<(Uuid, String, i64, i64, DateTime<Utc>, DateTime<Utc>)>(&mut *conn)
        .await
        .optional()?
        .ok_or(CreateHouseholdPersistenceError::HouseholdNotFound)?;
    let active_member = diesel::select(diesel::dsl::exists(
        household_members::table
            .filter(household_members::household_id.eq(household_id))
            .filter(household_members::member_id.eq(actor_id))
            .filter(household_members::left_at.is_null()),
    ))
    .get_result::<bool>(&mut *conn)
    .await?;
    if !active_member {
        return Err(CreateHouseholdPersistenceError::MemberNotFound);
    }

    let member_rows = household_members::table
        .inner_join(members::table.on(household_members::member_id.eq(members::id)))
        .filter(household_members::household_id.eq(household_id))
        .filter(household_members::left_at.is_null())
        .order_by(household_members::joined_at.asc())
        .select((members::id, members::display_name, household_members::role))
        .load::<(Uuid, String, String)>(&mut *conn)
        .await?
        .into_iter()
        .map(|(id, display_name, role)| HouseholdMemberReadModel {
            id,
            display_name,
            role,
        })
        .collect();

    let account_rows = accounts::table
        .filter(accounts::household_id.eq(household_id))
        .order_by(accounts::created_at.asc())
        .select((
            accounts::id,
            accounts::name,
            accounts::kind,
            accounts::card_last4,
            accounts::pool_id,
        ))
        .load::<(Uuid, String, String, Option<String>, Option<Uuid>)>(&mut *conn)
        .await?
        .into_iter()
        .map(
            |(id, name, kind, card_last4, pool_id)| HouseholdAccountReadModel {
                id,
                name,
                kind,
                card_last4,
                pool_id,
            },
        )
        .collect();

    let category_rows = categories::table
        .filter(categories::household_id.eq(household_id))
        .order_by(categories::name.asc())
        .then_order_by(categories::id.asc())
        .select((
            categories::id,
            categories::parent_id,
            categories::name,
            categories::archived,
        ))
        .load::<(Uuid, Option<Uuid>, String, bool)>(&mut *conn)
        .await?
        .into_iter()
        .map(
            |(id, parent_id, name, archived)| HouseholdCategoryReadModel {
                id,
                parent_id,
                name,
                archived,
            },
        )
        .collect();

    let pool_rows = pools::table
        .filter(pools::household_id.eq(household_id))
        .order_by(pools::created_at.asc())
        .select((
            pools::id,
            pools::name,
            pools::kind,
            pools::current_funding_version,
        ))
        .load::<(Uuid, String, String, Option<i32>)>(&mut *conn)
        .await?;
    let defaults = allocation_defaults::table
        .filter(allocation_defaults::household_id.eq(household_id))
        .filter(allocation_defaults::version.eq(household.3))
        .select((allocation_defaults::pool_id, allocation_defaults::weight))
        .load::<(Uuid, i64)>(&mut *conn)
        .await?
        .into_iter()
        .collect::<std::collections::HashMap<_, _>>();
    let pool_names = pool_rows
        .iter()
        .map(|(id, name, _, _)| (*id, name.clone()))
        .collect::<std::collections::HashMap<_, _>>();
    let default_allocation = pool_rows
        .iter()
        .filter_map(|(id, _, _, _)| {
            defaults.get(id).map(|weight| HouseholdAllocationReadModel {
                pool_id: *id,
                pool_name: pool_names[id].clone(),
                weight: *weight,
            })
        })
        .collect();
    let mut pool_models = Vec::with_capacity(pool_rows.len());
    for (id, name, kind, funding_version) in pool_rows {
        let default_share_weight = defaults.get(&id).copied().unwrap_or_default();
        let mut owner_member_id = None;
        let mut settlement_policy = None;
        let mut funding_shares = Vec::new();
        let funding_rule_version = funding_version.unwrap_or_default();
        if let Some(version) = funding_version {
            use public::{pool_funding_rules, pool_funding_shares};
            let rule = pool_funding_rules::table
                .filter(pool_funding_rules::household_id.eq(household_id))
                .filter(pool_funding_rules::pool_id.eq(id))
                .filter(pool_funding_rules::version.eq(version))
                .select((
                    pool_funding_rules::owner_member_id,
                    pool_funding_rules::settlement_policy,
                ))
                .first::<(Option<Uuid>, Option<String>)>(&mut *conn)
                .await
                .optional()?;
            if let Some((owner_id, policy)) = rule {
                owner_member_id = owner_id;
                settlement_policy = policy;
                funding_shares = pool_funding_shares::table
                    .inner_join(members::table.on(pool_funding_shares::member_id.eq(members::id)))
                    .filter(pool_funding_shares::household_id.eq(household_id))
                    .filter(pool_funding_shares::pool_id.eq(id))
                    .filter(pool_funding_shares::version.eq(version))
                    .order_by(pool_funding_shares::member_id.asc())
                    .select((
                        pool_funding_shares::member_id,
                        members::display_name,
                        pool_funding_shares::weight,
                    ))
                    .load::<(Uuid, String, i64)>(&mut *conn)
                    .await?
                    .into_iter()
                    .map(
                        |(member_id, member_name, weight)| HouseholdFundingShareReadModel {
                            member_id,
                            member_name,
                            weight,
                        },
                    )
                    .collect();
            }
        }
        if !matches!(kind.as_str(), "PERSONAL" | "SHARED")
            || settlement_policy
                .as_deref()
                .is_some_and(|value| !matches!(value, "BALANCE_TO_STANDARD" | "REIMBURSE_PERSONAL"))
        {
            return Err(CreateHouseholdPersistenceError::InvalidPersistedData);
        }
        pool_models.push(HouseholdPoolReadModel {
            id,
            name,
            kind,
            default_share_weight,
            funding_shares,
            funding_rule_version,
            owner_member_id,
            settlement_policy,
        });
    }

    Ok(HouseholdReadModel {
        id: household.0,
        name: household.1,
        revision: household.2,
        members: member_rows,
        pools: pool_models,
        accounts: account_rows,
        categories: category_rows,
        default_allocation,
        created_at: household.4,
        updated_at: household.5,
    })
}

pub async fn persist_create_household(
    pool: &PgPool,
    command: CreateHouseholdCommand,
) -> Result<PersistedHousehold, CreateHouseholdPersistenceError> {
    let mut operations = PostgresCreateHouseholdOperations { pool };
    let household_id =
        household_application::create_household::create_household(&mut operations, command).await?;
    Ok(PersistedHousehold { household_id })
}

async fn persist_create_household_decision(
    pool: &PgPool,
    decision: CreateHouseholdDecision,
) -> Result<String, CreateHouseholdPersistenceError> {
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

            Ok(household_id.to_string())
        })
        .await
}

struct PostgresCreateHouseholdOperations<'a> {
    pool: &'a PgPool,
}

impl CreateHouseholdOperations for PostgresCreateHouseholdOperations<'_> {
    type Error = CreateHouseholdPersistenceError;

    async fn persist(&mut self, decision: CreateHouseholdDecision) -> Result<String, Self::Error> {
        persist_create_household_decision(self.pool, decision).await
    }
}

// §4の依存方向に従うAdapter。Application層のHouseholdStore契約をPostgreSQLで実装する。
// 既存のpersist_create_householdは内部実装として残し、公開APIの互換性を維持する。
#[derive(Debug, Clone)]
pub struct PostgresHouseholds {
    pool: PgPool,
}

impl PostgresHouseholds {
    pub const fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl HouseholdStore for PostgresHouseholds {
    async fn create_household(
        &self,
        command: CreateHouseholdCommand,
    ) -> Result<CreatedHousehold, CreateHouseholdPortError> {
        persist_create_household(&self.pool, command)
            .await
            .map(|persisted| CreatedHousehold {
                household_id: persisted.household_id,
            })
            .map_err(|error| match error {
                CreateHouseholdPersistenceError::InvalidWorkflow(workflow) => {
                    CreateHouseholdPortError::from_workflow(workflow)
                }
                CreateHouseholdPersistenceError::InvalidUuid => {
                    CreateHouseholdPortError::InvalidInput
                }
                CreateHouseholdPersistenceError::HouseholdNotFound
                | CreateHouseholdPersistenceError::MemberNotFound => {
                    CreateHouseholdPortError::NotFound
                }
                CreateHouseholdPersistenceError::Pool(_)
                | CreateHouseholdPersistenceError::Database(_)
                | CreateHouseholdPersistenceError::InvalidPersistedData => {
                    CreateHouseholdPortError::Infra
                }
            })
    }
}
