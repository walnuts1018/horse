use std::{collections::BTreeSet, num::NonZeroI64};

use chrono::Utc;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use household_postgres::default_allocation_snapshot::{
    AddPoolToDefaultAllocationError, add_pool_to_default_allocation,
};
use policy_application::create_pool::{CreatePoolCommand, decide_create_pool};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum CreatePoolMutationError {
    Pool(String),
    Database(DieselError),
    InvalidWorkflow(policy_application::create_pool::CreatePoolError),
    HouseholdNotFound,
    MemberNotFound,
    NumericOutOfRange(&'static str),
}

impl From<DieselError> for CreatePoolMutationError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_create_pool(
    pool: &PgPool,
    command: CreatePoolCommand,
) -> Result<Uuid, CreatePoolMutationError> {
    let decision = decide_create_pool(command).map_err(CreatePoolMutationError::InvalidWorkflow)?;
    let actor_id = decision.actor_id.value();
    let household_id = decision.household_id.value();
    let pool_id = decision.pool_id.value();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CreatePoolMutationError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<Uuid, CreatePoolMutationError, _>(async move |conn| {
            public::households::table
                .filter(public::households::id.eq(household_id))
                .select(public::households::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .ok_or(CreatePoolMutationError::HouseholdNotFound)?;
            let role = public::household_members::table
                .filter(public::household_members::household_id.eq(household_id))
                .filter(public::household_members::member_id.eq(actor_id))
                .filter(public::household_members::left_at.is_null())
                .select(public::household_members::role)
                .first::<String>(&mut *conn)
                .await
                .optional()?;
            if authorize(role.as_deref(), Operation::FinancialData).is_err() {
                return Err(CreatePoolMutationError::MemberNotFound);
            }
            diesel::insert_into(public::pools::table)
                .values((
                    public::pools::id.eq(pool_id),
                    public::pools::household_id.eq(household_id),
                    public::pools::name.eq(&decision.name),
                    public::pools::kind.eq(match decision.kind {
                        policy_application::create_pool::PoolKind::Personal => "PERSONAL",
                        policy_application::create_pool::PoolKind::Shared => "SHARED",
                    }),
                ))
                .execute(&mut *conn)
                .await?;
            diesel::insert_into(public::pool_funding_rules::table)
                .values((
                    public::pool_funding_rules::household_id.eq(household_id),
                    public::pool_funding_rules::pool_id.eq(pool_id),
                    public::pool_funding_rules::version.eq(1_i32),
                    public::pool_funding_rules::created_by.eq(actor_id),
                ))
                .execute(&mut *conn)
                .await?;
            let (_, version) = add_pool_to_default_allocation(&mut *conn, household_id, pool_id)
                .await
                .map_err(|error| match error {
                    AddPoolToDefaultAllocationError::Database(error) => {
                        CreatePoolMutationError::Database(error)
                    }
                    AddPoolToDefaultAllocationError::NumericOutOfRange => {
                        CreatePoolMutationError::NumericOutOfRange("household defaults")
                    }
                    AddPoolToDefaultAllocationError::HouseholdNotFound => {
                        CreatePoolMutationError::HouseholdNotFound
                    }
                })?;
            horse_postgres_common::insert_ui_change(
                &mut *conn,
                household_id,
                pool_id,
                version,
                actor_id,
                Utc::now(),
                policy_application::ui_changes::pool_created(),
            )
            .await?;
            Ok(pool_id)
        })
        .await
}

pub struct PostgresCreatePoolHandler {
    pool: PgPool,
}

impl PostgresCreatePoolHandler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl policy_application::create_pool::CreatePoolHandler for PostgresCreatePoolHandler {
    fn create<'a>(
        &'a self,
        command: CreatePoolCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<Uuid, policy_application::WorkflowFailure>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            persist_create_pool(&self.pool, command)
                .await
                .map_err(|error| match error {
                    CreatePoolMutationError::InvalidWorkflow(_) => {
                        policy_application::WorkflowFailure::Decision
                    }
                    CreatePoolMutationError::HouseholdNotFound
                    | CreatePoolMutationError::MemberNotFound => {
                        policy_application::WorkflowFailure::NotFound
                    }
                    CreatePoolMutationError::Pool(message) => {
                        policy_application::WorkflowFailure::Infrastructure(message)
                    }
                    CreatePoolMutationError::Database(error) => {
                        policy_application::WorkflowFailure::Infrastructure(error.to_string())
                    }
                    CreatePoolMutationError::NumericOutOfRange(_) => {
                        policy_application::WorkflowFailure::Infrastructure(
                            "numeric value is out of range".to_owned(),
                        )
                    }
                })
        })
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PoolFundingShareInput {
    pub member_id: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PoolFundingConfiguration {
    Personal {
        owner_member_id: String,
    },
    Shared {
        funding_shares: Vec<PoolFundingShareInput>,
        settlement_policy: String,
    },
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetPoolFundingRuleCommand {
    pub actor_id: String,
    pub household_id: String,
    pub pool_id: String,
    pub expected_version: i32,
    pub configuration: PoolFundingConfiguration,
}

struct ValidatedFundingConfiguration {
    owner_member_id: Option<Uuid>,
    settlement_policy: Option<String>,
    shares: Vec<(Uuid, i64)>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedPoolFundingRule {
    pub pool_id: String,
    pub version: i32,
}

#[derive(Debug)]
pub enum SetPoolFundingRulePersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidConfiguration,
    HouseholdNotFound,
    ActorNotFound,
    PoolNotFound,
    MemberNotFound,
    Conflict,
    NumericOutOfRange(&'static str),
}

impl From<DieselError> for SetPoolFundingRulePersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_set_pool_funding_rule(
    pool: &PgPool,
    command: SetPoolFundingRuleCommand,
) -> Result<PersistedPoolFundingRule, SetPoolFundingRulePersistenceError> {
    let actor_id = parse_uuid(&command.actor_id, "actor_id")?;
    let household_id = parse_uuid(&command.household_id, "household_id")?;
    let pool_id = parse_uuid(&command.pool_id, "pool_id")?;
    if command.expected_version < 0 {
        return Err(SetPoolFundingRulePersistenceError::InvalidConfiguration);
    }
    let ValidatedFundingConfiguration {
        owner_member_id,
        settlement_policy,
        shares,
    } = validate_funding_configuration(&command.configuration)?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SetPoolFundingRulePersistenceError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedPoolFundingRule, SetPoolFundingRulePersistenceError, _>(
            async move |conn| {
                use public::{
                    household_members as members, households, pool_funding_rules as rules,
                    pool_funding_shares as funding_shares, pools,
                };

                households::table
                    .filter(households::id.eq(household_id))
                    .for_update()
                    .select(households::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .ok_or(SetPoolFundingRulePersistenceError::HouseholdNotFound)?;
                let actor_is_active = members::table
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq(actor_id))
                    .filter(members::left_at.is_null())
                    .select(members::member_id)
                    .first::<Uuid>(&mut *conn)
                    .await
                    .optional()?
                    .is_some();
                if !actor_is_active {
                    return Err(SetPoolFundingRulePersistenceError::ActorNotFound);
                }

                let pool_state = pools::table
                    .filter(pools::household_id.eq(household_id))
                    .filter(pools::id.eq(pool_id))
                    .for_update()
                    .select((pools::kind, pools::current_funding_version))
                    .first::<(String, Option<i32>)>(&mut *conn)
                    .await
                    .optional()?
                    .ok_or(SetPoolFundingRulePersistenceError::PoolNotFound)?;
                let current_version = pool_state.1.unwrap_or(0);
                if current_version != command.expected_version {
                    return Err(SetPoolFundingRulePersistenceError::Conflict);
                }
                let (owner_member_id, settlement_policy, shares) = match pool_state.0.as_str() {
                    "PERSONAL" if owner_member_id.is_some() && settlement_policy.is_none() => {
                        (owner_member_id, settlement_policy, shares)
                    }
                    "SHARED" if owner_member_id.is_none() && settlement_policy.is_some() => {
                        (owner_member_id, settlement_policy, shares)
                    }
                    "PERSONAL" | "SHARED" => {
                        return Err(SetPoolFundingRulePersistenceError::InvalidConfiguration);
                    }
                    _ => return Err(SetPoolFundingRulePersistenceError::InvalidConfiguration),
                };

                let member_ids = owner_member_id
                    .into_iter()
                    .chain(shares.iter().map(|(id, _)| *id))
                    .collect::<BTreeSet<_>>();
                let active_member_ids = members::table
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq_any(&member_ids))
                    .filter(members::left_at.is_null())
                    .select(members::member_id)
                    .load::<Uuid>(&mut *conn)
                    .await?
                    .into_iter()
                    .collect::<BTreeSet<_>>();
                if active_member_ids != member_ids {
                    return Err(SetPoolFundingRulePersistenceError::MemberNotFound);
                }

                let version = current_version.checked_add(1).ok_or(
                    SetPoolFundingRulePersistenceError::NumericOutOfRange("funding_rule_version"),
                )?;
                let initial_placeholder = current_version == 0
                    && rules::table
                        .filter(rules::household_id.eq(household_id))
                        .filter(rules::pool_id.eq(pool_id))
                        .filter(rules::version.eq(version))
                        .select(rules::version)
                        .first::<i32>(&mut *conn)
                        .await
                        .optional()?
                        .is_some();
                if initial_placeholder {
                    diesel::update(
                        rules::table
                            .filter(rules::household_id.eq(household_id))
                            .filter(rules::pool_id.eq(pool_id))
                            .filter(rules::version.eq(version))
                            .filter(rules::owner_member_id.is_null())
                            .filter(rules::settlement_policy.is_null()),
                    )
                    .set((
                        rules::owner_member_id.eq(owner_member_id),
                        rules::settlement_policy.eq(settlement_policy.as_deref()),
                        rules::created_by.eq(actor_id),
                    ))
                    .execute(&mut *conn)
                    .await?;
                } else {
                    diesel::insert_into(rules::table)
                        .values((
                            rules::household_id.eq(household_id),
                            rules::pool_id.eq(pool_id),
                            rules::version.eq(version),
                            rules::owner_member_id.eq(owner_member_id),
                            rules::settlement_policy.eq(settlement_policy.as_deref()),
                            rules::created_by.eq(actor_id),
                        ))
                        .execute(&mut *conn)
                        .await?;
                }
                for (member_id, weight) in &shares {
                    diesel::insert_into(funding_shares::table)
                        .values((
                            funding_shares::household_id.eq(household_id),
                            funding_shares::pool_id.eq(pool_id),
                            funding_shares::version.eq(version),
                            funding_shares::member_id.eq(*member_id),
                            funding_shares::weight.eq(*weight),
                        ))
                        .execute(&mut *conn)
                        .await?;
                }
                diesel::update(
                    pools::table
                        .filter(pools::household_id.eq(household_id))
                        .filter(pools::id.eq(pool_id)),
                )
                .set(pools::current_funding_version.eq(Some(version)))
                .execute(&mut *conn)
                .await?;
                household_postgres::record_change::record_household_change(
                    &mut *conn,
                    household_application::record_change::RecordHouseholdChange {
                        household_id,
                        entity_id: pool_id,
                        actor_id,
                        occurred_at: Utc::now(),
                        change: policy_application::ui_changes::pool_funding_rule_updated(),
                    },
                )
                .await
                .map_err(|error| match error {
                    household_postgres::record_change::RecordHouseholdChangeError::Database(
                        error,
                    ) => SetPoolFundingRulePersistenceError::Database(error),
                    household_postgres::record_change::RecordHouseholdChangeError::HouseholdNotFound => {
                        SetPoolFundingRulePersistenceError::HouseholdNotFound
                    }
                    household_postgres::record_change::RecordHouseholdChangeError::NumericOutOfRange => {
                        SetPoolFundingRulePersistenceError::NumericOutOfRange("household_revision")
                    }
                })?;
                Ok(PersistedPoolFundingRule {
                    pool_id: pool_id.to_string(),
                    version,
                })
            },
        )
        .await
}

fn validate_funding_configuration(
    configuration: &PoolFundingConfiguration,
) -> Result<ValidatedFundingConfiguration, SetPoolFundingRulePersistenceError> {
    match configuration {
        PoolFundingConfiguration::Personal { owner_member_id } => {
            Ok(ValidatedFundingConfiguration {
                owner_member_id: Some(parse_uuid(owner_member_id, "owner_member_id")?),
                settlement_policy: None,
                shares: Vec::new(),
            })
        }
        PoolFundingConfiguration::Shared {
            funding_shares,
            settlement_policy,
        } => {
            if !matches!(
                settlement_policy.as_str(),
                "BALANCE_TO_STANDARD" | "REIMBURSE_PERSONAL"
            ) || funding_shares.is_empty()
            {
                return Err(SetPoolFundingRulePersistenceError::InvalidConfiguration);
            }
            let mut seen = BTreeSet::new();
            let mut shares = Vec::with_capacity(funding_shares.len());
            let mut total = 0_i64;
            for share in funding_shares {
                let member_id = parse_uuid(&share.member_id, "member_id")?;
                let weight = NonZeroI64::new(share.weight)
                    .filter(|weight| weight.get() > 0)
                    .ok_or(SetPoolFundingRulePersistenceError::InvalidConfiguration)?
                    .get();
                total = total
                    .checked_add(weight)
                    .ok_or(SetPoolFundingRulePersistenceError::InvalidConfiguration)?;
                if !seen.insert(member_id) {
                    return Err(SetPoolFundingRulePersistenceError::InvalidConfiguration);
                }
                shares.push((member_id, weight));
            }
            shares.sort_by_key(|(id, _)| *id);
            Ok(ValidatedFundingConfiguration {
                owner_member_id: None,
                settlement_policy: Some(settlement_policy.clone()),
                shares,
            })
        }
    }
}

fn parse_uuid(
    value: &str,
    field: &'static str,
) -> Result<Uuid, SetPoolFundingRulePersistenceError> {
    Uuid::parse_str(value).map_err(|_| SetPoolFundingRulePersistenceError::InvalidUuid(field))
}
