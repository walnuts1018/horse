use std::{collections::BTreeSet, num::NonZeroI64};

use bookkeeping_application::create_pool::{CreatePoolCommand, decide_create_pool};
use chrono::Utc;
use diesel::{
    BoolExpressionMethods, ExpressionMethods, JoinOnDsl, OptionalExtension, QueryDsl, dsl::now,
    result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use uuid::Uuid;

use crate::{PgPool, create_household, schema::public};

#[derive(Debug)]
pub enum CreatePoolMutationError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidWorkflow(bookkeeping_application::create_pool::CreatePoolError),
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
) -> Result<String, CreatePoolMutationError> {
    let decision = decide_create_pool(command).map_err(CreatePoolMutationError::InvalidWorkflow)?;
    let actor_id = parse_uuid_for_create(&decision.actor_id, "actor_id")?;
    let household_id = parse_uuid_for_create(&decision.household_id, "household_id")?;
    let pool_id = parse_uuid_for_create(&decision.pool_id, "pool_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| CreatePoolMutationError::Pool(error.to_string()))?;
    (*pooled)
        .transaction::<String, CreatePoolMutationError, _>(async move |conn| {
            use public::allocation_defaults as defaults;

            let household = public::households::table
                .filter(public::households::id.eq(household_id))
                .for_update()
                .select((
                    public::households::revision,
                    public::households::default_allocation_version,
                ))
                .first::<(i64, i64)>(conn)
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
                    public::pools::kind.eq(&decision.kind),
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
            let version =
                household
                    .1
                    .checked_add(1)
                    .ok_or(CreatePoolMutationError::NumericOutOfRange(
                        "default_allocation_version",
                    ))?;
            let prior_defaults = defaults::table
                .filter(defaults::household_id.eq(household_id))
                .filter(defaults::version.eq(household.1))
                .select((defaults::pool_id, defaults::weight))
                .load::<(Uuid, i64)>(&mut *conn)
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
                diesel::insert_into(defaults::table)
                    .values(rows)
                    .execute(&mut *conn)
                    .await?;
            }
            diesel::insert_into(defaults::table)
                .values((
                    defaults::household_id.eq(household_id),
                    defaults::version.eq(version),
                    defaults::pool_id.eq(pool_id),
                    defaults::weight.eq(1_i64),
                ))
                .execute(&mut *conn)
                .await?;
            let revision = household
                .0
                .checked_add(1)
                .ok_or(CreatePoolMutationError::NumericOutOfRange("revision"))?;
            diesel::update(
                public::households::table.filter(public::households::id.eq(household_id)),
            )
            .set((
                public::households::default_allocation_version.eq(version),
                public::households::revision.eq(revision),
                public::households::updated_at.eq(now),
            ))
            .execute(&mut *conn)
            .await?;
            horse_postgres_common::insert_ui_change(
                &mut *conn,
                household_id,
                pool_id,
                version,
                actor_id,
                Utc::now(),
                bookkeeping_application::ui_changes::pool_created(),
            )
            .await?;
            Ok(pool_id.to_string())
        })
        .await
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

                let household = households::table
                    .filter(households::id.eq(household_id))
                    .for_update()
                    .select((households::revision, households::id))
                    .first::<(i64, Uuid)>(conn)
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
                let revision = household.0.checked_add(1).ok_or(
                    SetPoolFundingRulePersistenceError::NumericOutOfRange("household_revision"),
                )?;
                diesel::update(households::table.filter(households::id.eq(household_id)))
                    .set((
                        households::revision.eq(revision),
                        households::updated_at.eq(now),
                    ))
                    .execute(&mut *conn)
                    .await?;
                horse_postgres_common::insert_ui_change(
                    &mut *conn,
                    household_id,
                    pool_id,
                    revision,
                    actor_id,
                    Utc::now(),
                    bookkeeping_application::ui_changes::pool_funding_rule_updated(),
                )
                .await?;
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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocationProjection {
    pub pool_id: Uuid,
    pub pool_name: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AllocationRuleTargetProjection {
    Category {
        id: Uuid,
        household_id: Uuid,
        parent_id: Option<Uuid>,
        name: String,
        archived: bool,
    },
    Merchant {
        name: String,
    },
    ProductConcept(ProductConceptProjection),
    ProductVariant(ProductVariantProjection),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductConceptProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub name: String,
    pub normalized_name: String,
    pub category_id: Option<Uuid>,
    pub archived: bool,
    pub revision: i64,
    pub variants: Vec<ProductVariantProjection>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductVariantProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub concept_id: Uuid,
    pub name: String,
    pub normalized_name: String,
    pub brand: String,
    pub package_size: String,
    pub barcode: Option<String>,
    pub archived: bool,
    pub revision: i64,
    pub aliases: Vec<ProductAliasProjection>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductAliasProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub variant_id: Uuid,
    pub merchant_key: String,
    pub alias: String,
    pub normalized_alias: String,
    pub revision: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocationRuleProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub revision: i64,
    pub target: AllocationRuleTargetProjection,
    pub allocations: Vec<AllocationProjection>,
}

#[derive(Debug)]
pub enum AllocationRuleProjectionError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    HouseholdNotFound,
    MemberNotFound,
    RuleNotFound,
    TargetNotFound,
}

impl From<DieselError> for AllocationRuleProjectionError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn load_allocation_rule_projection(
    pool: &PgPool,
    actor_id: Uuid,
    rule_id: Uuid,
) -> Result<AllocationRuleProjection, AllocationRuleProjectionError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| AllocationRuleProjectionError::Pool(error.to_string()))?;
    use public::{
        allocation_rule_weights as weights, allocation_rules as rules,
        household_members as members, households, pools,
    };
    let (id, household_id, category_id, merchant, concept_id, variant_id, revision) = rules::table
        .filter(rules::id.eq(rule_id))
        .select((
            rules::id,
            rules::household_id,
            rules::category_id,
            rules::merchant_key,
            rules::product_concept_id,
            rules::product_variant_id,
            rules::revision,
        ))
        .first::<(
            Uuid,
            Uuid,
            Option<Uuid>,
            Option<String>,
            Option<Uuid>,
            Option<Uuid>,
            i64,
        )>(&mut *conn)
        .await
        .optional()?
        .ok_or(AllocationRuleProjectionError::RuleNotFound)?;
    let household_exists = households::table
        .filter(households::id.eq(household_id))
        .select(households::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .is_some();
    if !household_exists {
        return Err(AllocationRuleProjectionError::HouseholdNotFound);
    }
    let member_exists = members::table
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .filter(members::left_at.is_null())
        .select(members::member_id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .is_some();
    if !member_exists {
        return Err(AllocationRuleProjectionError::MemberNotFound);
    }
    let selected_targets = [
        category_id.is_some(),
        merchant.is_some(),
        concept_id.is_some(),
        variant_id.is_some(),
    ]
    .into_iter()
    .filter(|selected| *selected)
    .count();
    if selected_targets != 1 {
        return Err(AllocationRuleProjectionError::TargetNotFound);
    }
    let target = if let Some(target_id) = category_id {
        let (target_household_id, parent_id, name, archived) = public::categories::table
            .filter(public::categories::household_id.eq(household_id))
            .filter(public::categories::id.eq(target_id))
            .select((
                public::categories::household_id,
                public::categories::parent_id,
                public::categories::name,
                public::categories::archived,
            ))
            .first::<(Uuid, Option<Uuid>, String, bool)>(&mut *conn)
            .await
            .optional()?
            .ok_or(AllocationRuleProjectionError::TargetNotFound)?;
        AllocationRuleTargetProjection::Category {
            id: target_id,
            household_id: target_household_id,
            parent_id,
            name,
            archived,
        }
    } else if let Some(name) = merchant {
        AllocationRuleTargetProjection::Merchant { name }
    } else if let Some(target_id) = concept_id {
        let (concept_household_id, name, normalized_name, category_id, archived, concept_revision) =
            public::product_concepts::table
                .filter(public::product_concepts::household_id.eq(household_id))
                .filter(public::product_concepts::id.eq(target_id))
                .select((
                    public::product_concepts::household_id,
                    public::product_concepts::name,
                    public::product_concepts::normalized_name,
                    public::product_concepts::category_id,
                    public::product_concepts::archived,
                    public::product_concepts::revision,
                ))
                .first::<(Uuid, String, String, Option<Uuid>, bool, i64)>(&mut *conn)
                .await
                .optional()?
                .ok_or(AllocationRuleProjectionError::TargetNotFound)?;
        let variant_rows = public::product_variants::table
            .filter(public::product_variants::household_id.eq(household_id))
            .filter(public::product_variants::concept_id.eq(target_id))
            .order_by((
                public::product_variants::name.asc(),
                public::product_variants::id.asc(),
            ))
            .select((
                public::product_variants::id,
                public::product_variants::household_id,
                public::product_variants::concept_id,
                public::product_variants::name,
                public::product_variants::normalized_name,
                public::product_variants::brand,
                public::product_variants::package_size,
                public::product_variants::barcode,
                public::product_variants::archived,
                public::product_variants::revision,
            ))
            .load::<(
                Uuid,
                Uuid,
                Uuid,
                String,
                String,
                String,
                String,
                Option<String>,
                bool,
                i64,
            )>(&mut *conn)
            .await?;
        let variant_ids = variant_rows.iter().map(|row| row.0).collect::<Vec<_>>();
        let aliases = if variant_ids.is_empty() {
            Vec::new()
        } else {
            public::product_aliases::table
                .filter(public::product_aliases::household_id.eq(household_id))
                .filter(public::product_aliases::variant_id.eq_any(&variant_ids))
                .order_by((
                    public::product_aliases::merchant_key.asc(),
                    public::product_aliases::alias_text.asc(),
                    public::product_aliases::id.asc(),
                ))
                .select((
                    public::product_aliases::id,
                    public::product_aliases::household_id,
                    public::product_aliases::variant_id,
                    public::product_aliases::merchant_key,
                    public::product_aliases::alias_text,
                    public::product_aliases::normalized_alias,
                    public::product_aliases::revision,
                ))
                .load::<(Uuid, Uuid, Uuid, String, String, String, i64)>(&mut *conn)
                .await?
        };
        let variants = variant_rows
            .into_iter()
            .map(|row| ProductVariantProjection {
                id: row.0,
                household_id: row.1,
                concept_id: row.2,
                name: row.3,
                normalized_name: row.4,
                brand: row.5,
                package_size: row.6,
                barcode: row.7,
                archived: row.8,
                revision: row.9,
                aliases: aliases
                    .iter()
                    .filter(|alias| alias.2 == row.0)
                    .map(|alias| ProductAliasProjection {
                        id: alias.0,
                        household_id: alias.1,
                        variant_id: alias.2,
                        merchant_key: alias.3.clone(),
                        alias: alias.4.clone(),
                        normalized_alias: alias.5.clone(),
                        revision: alias.6,
                    })
                    .collect(),
            })
            .collect();
        AllocationRuleTargetProjection::ProductConcept(ProductConceptProjection {
            id: target_id,
            household_id: concept_household_id,
            name,
            normalized_name,
            category_id,
            archived,
            revision: concept_revision,
            variants,
        })
    } else if let Some(target_id) = variant_id {
        let row = public::product_variants::table
            .filter(public::product_variants::household_id.eq(household_id))
            .filter(public::product_variants::id.eq(target_id))
            .select((
                public::product_variants::id,
                public::product_variants::household_id,
                public::product_variants::concept_id,
                public::product_variants::name,
                public::product_variants::normalized_name,
                public::product_variants::brand,
                public::product_variants::package_size,
                public::product_variants::barcode,
                public::product_variants::archived,
                public::product_variants::revision,
            ))
            .first::<(
                Uuid,
                Uuid,
                Uuid,
                String,
                String,
                String,
                String,
                Option<String>,
                bool,
                i64,
            )>(&mut *conn)
            .await
            .optional()?
            .ok_or(AllocationRuleProjectionError::TargetNotFound)?;
        let alias_rows = public::product_aliases::table
            .filter(public::product_aliases::household_id.eq(household_id))
            .filter(public::product_aliases::variant_id.eq(target_id))
            .order_by((
                public::product_aliases::merchant_key.asc(),
                public::product_aliases::alias_text.asc(),
                public::product_aliases::id.asc(),
            ))
            .select((
                public::product_aliases::id,
                public::product_aliases::household_id,
                public::product_aliases::variant_id,
                public::product_aliases::merchant_key,
                public::product_aliases::alias_text,
                public::product_aliases::normalized_alias,
                public::product_aliases::revision,
            ))
            .load::<(Uuid, Uuid, Uuid, String, String, String, i64)>(&mut *conn)
            .await?
            .into_iter()
            .map(|alias| ProductAliasProjection {
                id: alias.0,
                household_id: alias.1,
                variant_id: alias.2,
                merchant_key: alias.3,
                alias: alias.4,
                normalized_alias: alias.5,
                revision: alias.6,
            })
            .collect();
        AllocationRuleTargetProjection::ProductVariant(ProductVariantProjection {
            id: row.0,
            household_id: row.1,
            concept_id: row.2,
            name: row.3,
            normalized_name: row.4,
            brand: row.5,
            package_size: row.6,
            barcode: row.7,
            archived: row.8,
            revision: row.9,
            aliases: alias_rows,
        })
    } else {
        return Err(AllocationRuleProjectionError::TargetNotFound);
    };
    let allocations = weights::table
        .inner_join(
            pools::table.on(weights::pool_id
                .eq(pools::id)
                .and(weights::household_id.eq(pools::household_id))),
        )
        .filter(weights::household_id.eq(household_id))
        .filter(weights::rule_id.eq(rule_id))
        .order_by((pools::name.asc(), pools::id.asc()))
        .select((weights::pool_id, pools::name, weights::weight))
        .load::<(Uuid, String, i64)>(&mut *conn)
        .await?
        .into_iter()
        .map(|(pool_id, pool_name, weight)| AllocationProjection {
            pool_id,
            pool_name,
            weight,
        })
        .collect();
    Ok(AllocationRuleProjection {
        id,
        household_id,
        revision,
        target,
        allocations,
    })
}

pub async fn load_pool_read_model(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    pool_id: Uuid,
) -> Result<create_household::HouseholdPoolReadModel, PoolReadModelError> {
    let household = create_household::load_household(pool, actor_id, household_id)
        .await
        .map_err(PoolReadModelError::Household)?;
    household
        .pools
        .into_iter()
        .find(|pool| pool.id == pool_id)
        .ok_or(PoolReadModelError::PoolNotFound)
}

#[derive(Debug)]
pub enum PoolReadModelError {
    Household(create_household::CreateHouseholdPersistenceError),
    PoolNotFound,
}

fn parse_uuid_for_create(
    value: &str,
    field: &'static str,
) -> Result<Uuid, CreatePoolMutationError> {
    Uuid::parse_str(value).map_err(|_| CreatePoolMutationError::InvalidUuid(field))
}

fn parse_uuid(
    value: &str,
    field: &'static str,
) -> Result<Uuid, SetPoolFundingRulePersistenceError> {
    Uuid::parse_str(value).map_err(|_| SetPoolFundingRulePersistenceError::InvalidUuid(field))
}
