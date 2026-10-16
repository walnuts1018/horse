use async_graphql::{Context, ID, InputObject, OneofObject, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use cross_context_postgres::pool_mutations as cross_pool_queries;
use horse_postgres_common::PgPool;
use household_application::default_allocation::{
    DefaultAllocationWeight, SetDefaultAllocationCommand,
};
use household_postgres::create_household;
use household_postgres::set_default_allocation as postgres_default_allocation;
use policy_application::create_pool::{CreatePoolCommand, NewPoolId, PoolActorId, PoolHouseholdId};
use policy_application::{
    set_allocation_rule::{
        AllocationActorId, AllocationCategoryId, AllocationHouseholdId, AllocationPoolId,
        AllocationProductConceptId, AllocationProductVariantId,
        AllocationRuleTarget as WorkflowAllocationRuleTarget, AllocationRuleWeight,
        SetAllocationRuleCommand,
    },
    set_funding_schedule::{
        ActorId as FundingActorId, FundingScheduleAmount, FundingScheduleCadence as PolicyCadence,
        FundingScheduleDate, FundingScheduleId, HouseholdId as FundingHouseholdId,
        MemberId as FundingMemberId, PoolId as FundingPoolId, SetFundingScheduleCommand,
    },
    set_pool_funding_rule::{
        FundingRuleActorId, FundingRuleHouseholdId, FundingRuleMemberId, FundingRulePoolId,
        PoolFundingConfiguration as WorkflowPoolFundingConfiguration,
        PoolFundingShareInput as WorkflowPoolFundingShareInput, SetPoolFundingRuleCommand,
        SharedSettlementPolicy,
    },
};
use policy_postgres::{
    pool_mutations as postgres_pool_mutations, set_allocation_rule as postgres_allocation_rule,
    set_funding_schedule as postgres_funding_schedule,
};
use uuid::Uuid;

use crate::{Long, MutationErrorCode, member_pool};

#[derive(Clone, Debug, InputObject)]
pub struct CreatePoolInput {
    household_id: ID,
    name: String,
    kind: member_pool::PoolKind,
}

#[derive(Clone, Debug, InputObject)]
pub struct SetPoolFundingRuleInput {
    household_id: ID,
    pool_id: ID,
    configuration: PoolFundingRuleConfigurationInput,
    expected_version: i32,
}

#[derive(Clone, Debug, OneofObject)]
pub enum PoolFundingRuleConfigurationInput {
    Personal(PersonalPoolFundingRuleInput),
    Shared(SharedPoolFundingRuleInput),
}

#[derive(Clone, Debug, InputObject)]
pub struct PersonalPoolFundingRuleInput {
    owner_member_id: ID,
}

#[derive(Clone, Debug, InputObject)]
pub struct SharedPoolFundingRuleInput {
    funding_shares: Vec<PoolFundingShareInput>,
    settlement_policy: member_pool::SharedPoolSettlementPolicy,
}

#[derive(Clone, Debug, InputObject)]
pub struct PoolFundingShareInput {
    member_id: ID,
    weight: Long,
}

#[derive(Clone, Debug, InputObject)]
pub struct SetDefaultAllocationInput {
    household_id: ID,
    allocations: Vec<SetDefaultAllocationItemInput>,
}

#[derive(Clone, Debug, InputObject)]
pub struct SetDefaultAllocationItemInput {
    pool_id: ID,
    weight: Long,
}

#[derive(Clone, Debug, InputObject)]
pub struct SetAllocationRuleInput {
    household_id: ID,
    target: AllocationRuleTargetInput,
    allocations: Vec<SetAllocationRuleItemInput>,
}

#[derive(Clone, Debug, OneofObject)]
pub enum AllocationRuleTargetInput {
    CategoryId(ID),
    Merchant(String),
    ProductConceptId(ID),
    ProductVariantId(ID),
}

#[derive(Clone, Debug, InputObject)]
pub struct SetAllocationRuleItemInput {
    pool_id: ID,
    weight: Long,
}

#[derive(Clone, Debug, InputObject)]
pub struct SetFundingScheduleInput {
    household_id: ID,
    member_id: ID,
    pool_id: ID,
    amount_yen: Long,
    cadence: FundingScheduleCadence,
    starts_on: crate::policy_mutations::Date,
    ends_on: Option<crate::policy_mutations::Date>,
}

#[derive(Clone, Copy, Debug, async_graphql::Enum, Eq, PartialEq)]
pub enum FundingScheduleCadence {
    Monthly,
    Weekly,
    Custom,
}

#[derive(Clone, Debug, SimpleObject)]
#[graphql(name = "Allocation")]
pub struct AllocationObject {
    pub(crate) pool_id: ID,
    pub(crate) pool_name: String,
    pub(crate) weight: Long,
}

#[derive(Clone, Debug, SimpleObject)]
#[graphql(name = "AllocationRule")]
pub struct AllocationRuleObject {
    pub(crate) id: ID,
    pub(crate) household_id: ID,
    pub(crate) target: crate::catalog_mutations::AllocationRuleTarget,
    pub(crate) revision: i32,
    pub(crate) allocations: Vec<AllocationObject>,
}

#[derive(Clone, Debug, SimpleObject)]
#[graphql(name = "FundingSchedule")]
pub struct FundingScheduleObject {
    pub(crate) id: ID,
    pub(crate) household_id: ID,
    pub(crate) member_id: ID,
    pub(crate) pool_id: ID,
    pub(crate) amount_yen: Long,
    pub(crate) cadence: FundingScheduleCadence,
    pub(crate) starts_on: crate::policy_mutations::Date,
    pub(crate) ends_on: Option<crate::policy_mutations::Date>,
    pub(crate) active: bool,
    pub(crate) created_at: crate::DateTime,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CreatePoolSuccess {
    result: member_pool::Pool,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SetPoolFundingRuleSuccess {
    result: member_pool::Pool,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SetDefaultAllocationSuccess {
    result: crate::household_lifecycle::Household,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SetAllocationRuleSuccess {
    result: AllocationRuleObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SetFundingScheduleSuccess {
    result: FundingScheduleObject,
}

macro_rules! user_error {
    ($name:ident) => {
        #[derive(Clone, Debug, SimpleObject)]
        pub struct $name {
            pub code: MutationErrorCode,
            pub message: String,
        }
    };
}

user_error!(CreatePoolUserError);
user_error!(SetPoolFundingRuleUserError);
user_error!(SetDefaultAllocationUserError);
user_error!(SetAllocationRuleUserError);
user_error!(SetFundingScheduleUserError);

macro_rules! mutation_result {
    ($name:ident, $success:ident, $error:ident) => {
        #[derive(Union)]
        pub enum $name {
            Success(Box<$success>),
            UserError($error),
        }
    };
}

mutation_result!(CreatePoolResult, CreatePoolSuccess, CreatePoolUserError);
mutation_result!(
    SetPoolFundingRuleResult,
    SetPoolFundingRuleSuccess,
    SetPoolFundingRuleUserError
);
mutation_result!(
    SetDefaultAllocationResult,
    SetDefaultAllocationSuccess,
    SetDefaultAllocationUserError
);
mutation_result!(
    SetAllocationRuleResult,
    SetAllocationRuleSuccess,
    SetAllocationRuleUserError
);
mutation_result!(
    SetFundingScheduleResult,
    SetFundingScheduleSuccess,
    SetFundingScheduleUserError
);

pub async fn create_pool(ctx: &Context<'_>, input: CreatePoolInput) -> Result<CreatePoolResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(create_pool_error(MutationErrorCode::BadInput).into()),
    };
    let kind = match input.kind {
        member_pool::PoolKind::Personal => "PERSONAL",
        member_pool::PoolKind::Shared => "SHARED",
    };
    let pool_id = match postgres_pool_mutations::persist_create_pool(
        ctx.data::<PgPool>()?,
        CreatePoolCommand {
            actor_id: PoolActorId::from_uuid(actor.0),
            household_id: PoolHouseholdId::from_uuid(household_id),
            pool_id: NewPoolId::from_uuid(Uuid::now_v7()),
            name: input.name,
            kind: kind.to_owned(),
        },
    )
    .await
    {
        Ok(id) => id,
        Err(error) => return create_pool_error_result(error),
    };
    let projection = match cross_pool_queries::load_pool_read_model(
        ctx.data::<PgPool>()?,
        actor.0,
        household_id,
        pool_id,
    )
    .await
    {
        Ok(value) => value,
        Err(error) => return pool_read_error(error),
    };
    Ok(Box::new(CreatePoolSuccess {
        result: map_pool_projection(projection)?,
    })
    .into())
}

pub async fn set_pool_funding_rule(
    ctx: &Context<'_>,
    input: SetPoolFundingRuleInput,
) -> Result<SetPoolFundingRuleResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(set_pool_funding_rule_error(MutationErrorCode::BadInput).into()),
    };
    let pool_id = match decode_node_id(&input.pool_id, "Pool") {
        Ok(id) => id,
        Err(()) => return Ok(set_pool_funding_rule_error(MutationErrorCode::BadInput).into()),
    };
    let configuration = match input.configuration {
        PoolFundingRuleConfigurationInput::Personal(input) => {
            let owner_id = match decode_node_id(&input.owner_member_id, "Member") {
                Ok(id) => id,
                Err(()) => {
                    return Ok(set_pool_funding_rule_error(MutationErrorCode::BadInput).into());
                }
            };
            WorkflowPoolFundingConfiguration::Personal {
                owner_member_id: FundingRuleMemberId::new(owner_id),
            }
        }
        PoolFundingRuleConfigurationInput::Shared(input) => {
            let mut shares = Vec::with_capacity(input.funding_shares.len());
            for share in input.funding_shares {
                let member_id = match decode_node_id(&share.member_id, "Member") {
                    Ok(id) => id,
                    Err(()) => {
                        return Ok(set_pool_funding_rule_error(MutationErrorCode::BadInput).into());
                    }
                };
                shares.push(WorkflowPoolFundingShareInput {
                    member_id: FundingRuleMemberId::new(member_id),
                    weight: share.weight.0,
                });
            }
            WorkflowPoolFundingConfiguration::Shared {
                funding_shares: shares,
                settlement_policy: match input.settlement_policy {
                    member_pool::SharedPoolSettlementPolicy::BalanceToStandard => {
                        SharedSettlementPolicy::BalanceToStandard
                    }
                    member_pool::SharedPoolSettlementPolicy::ReimbursePersonal => {
                        SharedSettlementPolicy::ReimbursePersonal
                    }
                },
            }
        }
    };
    match policy_postgres::set_pool_funding_rule::persist_set_pool_funding_rule(
        ctx.data::<PgPool>()?,
        SetPoolFundingRuleCommand {
            actor_id: FundingRuleActorId::new(actor.0),
            household_id: FundingRuleHouseholdId::new(household_id),
            pool_id: FundingRulePoolId::new(pool_id),
            expected_version: input.expected_version,
            configuration,
        },
    )
    .await
    {
        Ok(_) => {
            let projection = match cross_pool_queries::load_pool_read_model(
                ctx.data::<PgPool>()?,
                actor.0,
                household_id,
                pool_id,
            )
            .await
            {
                Ok(value) => value,
                Err(error) => return pool_read_error(error),
            };
            Ok(Box::new(SetPoolFundingRuleSuccess {
                result: map_pool_projection(projection)?,
            })
            .into())
        }
        Err(error) => set_pool_funding_rule_workflow_error(error),
    }
}

pub async fn set_default_allocation(
    ctx: &Context<'_>,
    input: SetDefaultAllocationInput,
) -> Result<SetDefaultAllocationResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(set_default_allocation_error(MutationErrorCode::BadInput).into()),
    };
    let mut weights = Vec::with_capacity(input.allocations.len());
    for allocation in input.allocations {
        let pool_id = match decode_node_id(&allocation.pool_id, "Pool") {
            Ok(id) => id,
            Err(()) => {
                return Ok(set_default_allocation_error(MutationErrorCode::BadInput).into());
            }
        };
        weights.push(DefaultAllocationWeight {
            pool_id: pool_id.to_string(),
            weight: allocation.weight.0,
        });
    }
    match postgres_default_allocation::persist_set_default_allocation(
        ctx.data::<PgPool>()?,
        SetDefaultAllocationCommand {
            actor_id: actor.0.to_string(),
            household_id: household_id.to_string(),
            weights,
        },
    )
    .await
    {
        Ok(_) => Ok(Box::new(SetDefaultAllocationSuccess {
            result: crate::household_lifecycle::load_household_graphql(
                ctx.data::<PgPool>()?,
                actor.0,
                household_id,
            )
            .await?,
        })
        .into()),
        Err(error) => set_default_allocation_persistence_error(error),
    }
}

pub async fn set_allocation_rule(
    ctx: &Context<'_>,
    input: SetAllocationRuleInput,
) -> Result<SetAllocationRuleResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(set_allocation_rule_error(MutationErrorCode::BadInput).into()),
    };
    let target = match input.target {
        AllocationRuleTargetInput::CategoryId(id) => match decode_node_id(&id, "Category") {
            Ok(id) => WorkflowAllocationRuleTarget::Category(AllocationCategoryId::from_uuid(id)),
            Err(()) => return Ok(set_allocation_rule_error(MutationErrorCode::BadInput).into()),
        },
        AllocationRuleTargetInput::Merchant(value) => WorkflowAllocationRuleTarget::Merchant(value),
        AllocationRuleTargetInput::ProductConceptId(id) => {
            match decode_node_id(&id, "ProductConcept") {
                Ok(id) => WorkflowAllocationRuleTarget::ProductConcept(
                    AllocationProductConceptId::from_uuid(id),
                ),
                Err(()) => {
                    return Ok(set_allocation_rule_error(MutationErrorCode::BadInput).into());
                }
            }
        }
        AllocationRuleTargetInput::ProductVariantId(id) => {
            match decode_node_id(&id, "ProductVariant") {
                Ok(id) => WorkflowAllocationRuleTarget::ProductVariant(
                    AllocationProductVariantId::from_uuid(id),
                ),
                Err(()) => {
                    return Ok(set_allocation_rule_error(MutationErrorCode::BadInput).into());
                }
            }
        }
    };
    let mut weights = Vec::with_capacity(input.allocations.len());
    for allocation in input.allocations {
        let pool_id = match decode_node_id(&allocation.pool_id, "Pool") {
            Ok(id) => id,
            Err(()) => {
                return Ok(set_allocation_rule_error(MutationErrorCode::BadInput).into());
            }
        };
        weights.push(AllocationRuleWeight {
            pool_id: AllocationPoolId::from_uuid(pool_id),
            weight: allocation.weight.0,
        });
    }
    let command = SetAllocationRuleCommand {
        actor_id: AllocationActorId::from_uuid(actor.0),
        household_id: AllocationHouseholdId::from_uuid(household_id),
        target,
        weights,
    };
    let rule_id =
        match postgres_allocation_rule::persist_set_allocation_rule(ctx.data::<PgPool>()?, command)
            .await
        {
            Ok(value) => value.id.value(),
            Err(error) => {
                return workflow_failure_result(error, |code| {
                    set_allocation_rule_error(code).into()
                });
            }
        };
    let projection = match cross_pool_queries::load_allocation_rule_projection(
        ctx.data::<PgPool>()?,
        actor.0,
        rule_id,
    )
    .await
    {
        Ok(value) => value,
        Err(error) => return allocation_rule_projection_error(error),
    };
    Ok(Box::new(SetAllocationRuleSuccess {
        result: map_allocation_rule_projection(projection)?,
    })
    .into())
}

pub async fn set_funding_schedule(
    ctx: &Context<'_>,
    input: SetFundingScheduleInput,
) -> Result<SetFundingScheduleResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => return Ok(set_funding_schedule_error(MutationErrorCode::BadInput).into()),
    };
    let member_id = match decode_node_id(&input.member_id, "Member") {
        Ok(id) => id,
        Err(()) => return Ok(set_funding_schedule_error(MutationErrorCode::BadInput).into()),
    };
    let pool_id = match decode_node_id(&input.pool_id, "Pool") {
        Ok(id) => id,
        Err(()) => return Ok(set_funding_schedule_error(MutationErrorCode::BadInput).into()),
    };
    let amount = match FundingScheduleAmount::new(input.amount_yen.0) {
        Ok(amount) => amount,
        Err(_) => return Ok(set_funding_schedule_error(MutationErrorCode::BadInput).into()),
    };
    let command = SetFundingScheduleCommand {
        actor_id: FundingActorId::from(actor.0),
        household_id: FundingHouseholdId::from(household_id),
        member_id: FundingMemberId::from(member_id),
        pool_id: FundingPoolId::from(pool_id),
        schedule_id: FundingScheduleId::from(Uuid::now_v7()),
        amount,
        cadence: match input.cadence {
            FundingScheduleCadence::Monthly => PolicyCadence::Monthly,
            FundingScheduleCadence::Weekly => PolicyCadence::Weekly,
            FundingScheduleCadence::Custom => PolicyCadence::Custom,
        },
        starts_on: FundingScheduleDate::new(input.starts_on.0),
        ends_on: input.ends_on.map(|value| FundingScheduleDate::new(value.0)),
    };
    match postgres_funding_schedule::persist_set_funding_schedule(ctx.data::<PgPool>()?, command)
        .await
    {
        Ok(value) => Ok(Box::new(SetFundingScheduleSuccess {
            result: map_funding_schedule_projection(value)?,
        })
        .into()),
        Err(error) => {
            workflow_failure_result(error, |code| set_funding_schedule_error(code).into())
        }
    }
}

fn map_pool_projection(
    value: create_household::HouseholdPoolReadModel,
) -> Result<member_pool::Pool> {
    let kind = match value.kind.as_str() {
        "PERSONAL" => member_pool::PoolKind::Personal,
        "SHARED" => member_pool::PoolKind::Shared,
        _ => return Err(crate::internal_error("invalid persisted pool kind")),
    };
    let settlement_policy = value
        .settlement_policy
        .as_deref()
        .map(|value| match value {
            "BALANCE_TO_STANDARD" => Ok(member_pool::SharedPoolSettlementPolicy::BalanceToStandard),
            "REIMBURSE_PERSONAL" => Ok(member_pool::SharedPoolSettlementPolicy::ReimbursePersonal),
            _ => Err(crate::internal_error("invalid persisted settlement policy")),
        })
        .transpose()?;
    Ok(member_pool::PoolProjection {
        id: value.id,
        kind,
        name: value.name,
        default_share_weight: value.default_share_weight,
        funding_shares: value
            .funding_shares
            .into_iter()
            .map(|share| member_pool::PoolFundingShareProjection {
                member_id: share.member_id,
                member_name: share.member_name,
                weight: share.weight,
            })
            .collect(),
        funding_rule_version: value.funding_rule_version,
        owner_member_id: value.owner_member_id,
        settlement_policy,
    }
    .into())
}

pub(crate) fn map_allocation_rule_projection(
    value: cross_pool_queries::AllocationRuleProjection,
) -> Result<AllocationRuleObject> {
    let target = crate::catalog_mutations::allocation_rule_target_from_projection(value.target)
        .map_err(crate::internal_error)?;
    Ok(AllocationRuleObject {
        id: encode_node_id("AllocationRule", &value.id.to_string())?,
        household_id: encode_node_id("Household", &value.household_id.to_string())?,
        target,
        revision: i32::try_from(value.revision).map_err(crate::internal_error)?,
        allocations: value
            .allocations
            .into_iter()
            .map(|allocation| {
                Ok(AllocationObject {
                    pool_id: encode_node_id("Pool", &allocation.pool_id.to_string())?,
                    pool_name: allocation.pool_name,
                    weight: Long(allocation.weight),
                })
            })
            .collect::<Result<Vec<_>>>()?,
    })
}

pub(crate) fn map_funding_schedule_projection(
    value: postgres_funding_schedule::PersistedFundingSchedule,
) -> Result<FundingScheduleObject> {
    Ok(FundingScheduleObject {
        id: encode_node_id("FundingSchedule", &value.id)?,
        household_id: encode_node_id("Household", &value.household_id)?,
        member_id: encode_node_id("Member", &value.member_id)?,
        pool_id: encode_node_id("Pool", &value.pool_id)?,
        amount_yen: Long(value.amount_yen),
        cadence: match value.cadence.as_str() {
            "MONTHLY" => FundingScheduleCadence::Monthly,
            "WEEKLY" => FundingScheduleCadence::Weekly,
            "CUSTOM" => FundingScheduleCadence::Custom,
            cadence => {
                return Err(crate::internal_error(format!(
                    "invalid funding schedule cadence {cadence}"
                )));
            }
        },
        starts_on: crate::policy_mutations::Date(
            chrono::NaiveDate::parse_from_str(&value.starts_on, "%Y-%m-%d")
                .map_err(crate::internal_error)?,
        ),
        ends_on: value
            .ends_on
            .as_deref()
            .map(|date| chrono::NaiveDate::parse_from_str(date, "%Y-%m-%d"))
            .transpose()
            .map_err(crate::internal_error)?
            .map(crate::policy_mutations::Date),
        active: value.active,
        created_at: value.created_at.into(),
    })
}

fn encode_node_id(kind: &str, raw_id: &str) -> Result<ID> {
    crate::catalog_mutations::encode_node_id(kind, raw_id)
        .map_err(|_| crate::internal_error("invalid persisted node ID"))
}

fn decode_node_id(value: &ID, expected_kind: &str) -> std::result::Result<Uuid, ()> {
    let bytes = URL_SAFE_NO_PAD.decode(value.as_str()).map_err(|_| ())?;
    let separator = bytes.iter().position(|byte| *byte == b':').ok_or(())?;
    let (kind, payload_with_separator) = bytes.split_at(separator);
    let payload = &payload_with_separator[1..];
    if kind != expected_kind.as_bytes() || payload.len() != 16 {
        return Err(());
    }
    let id = Uuid::from_slice(payload).map_err(|_| ())?;
    if id.is_nil() {
        return Err(());
    }
    Ok(id)
}

fn create_pool_error_result(
    error: postgres_pool_mutations::CreatePoolMutationError,
) -> Result<CreatePoolResult> {
    use postgres_pool_mutations::CreatePoolMutationError as E;
    match error {
        E::InvalidWorkflow(_) => Ok(create_pool_error(MutationErrorCode::BadInput).into()),
        E::HouseholdNotFound | E::MemberNotFound => {
            Ok(create_pool_error(MutationErrorCode::NotFound).into())
        }
        E::Pool(_) | E::Database(_) | E::NumericOutOfRange(_) => Err(crate::internal_error(error)),
    }
}

fn pool_read_error<T>(error: cross_pool_queries::PoolReadModelError) -> Result<T> {
    match error {
        cross_pool_queries::PoolReadModelError::PoolNotFound => {
            Err(crate::internal_error("created pool projection is missing"))
        }
        cross_pool_queries::PoolReadModelError::Household(error) => {
            Err(crate::internal_error(error))
        }
    }
}

fn set_pool_funding_rule_workflow_error(
    error: policy_application::WorkflowFailure,
) -> Result<SetPoolFundingRuleResult> {
    match error {
        policy_application::WorkflowFailure::Decision => {
            Ok(set_pool_funding_rule_error(MutationErrorCode::BadInput).into())
        }
        policy_application::WorkflowFailure::NotFound => {
            Ok(set_pool_funding_rule_error(MutationErrorCode::NotFound).into())
        }
        policy_application::WorkflowFailure::Conflict => {
            Ok(set_pool_funding_rule_error(MutationErrorCode::Conflict).into())
        }
        policy_application::WorkflowFailure::Infrastructure(message) => {
            Err(crate::internal_error(message))
        }
    }
}

fn set_default_allocation_persistence_error(
    error: postgres_default_allocation::SetDefaultAllocationPersistenceError,
) -> Result<SetDefaultAllocationResult> {
    use postgres_default_allocation::SetDefaultAllocationPersistenceError as E;
    match error {
        E::InvalidUuid(_) | E::InvalidWorkflow(_) => {
            Ok(set_default_allocation_error(MutationErrorCode::BadInput).into())
        }
        E::HouseholdNotFound | E::MemberNotFound | E::PoolNotFound => {
            Ok(set_default_allocation_error(MutationErrorCode::NotFound).into())
        }
        E::Pool(_) | E::Database(_) | E::NumericOutOfRange(_) | E::InvalidDecision => {
            Err(crate::internal_error(error))
        }
    }
}

fn allocation_rule_projection_error(
    error: cross_pool_queries::AllocationRuleProjectionError,
) -> Result<SetAllocationRuleResult> {
    use cross_pool_queries::AllocationRuleProjectionError as E;
    match error {
        E::HouseholdNotFound | E::MemberNotFound | E::RuleNotFound | E::TargetNotFound => {
            Ok(set_allocation_rule_error(MutationErrorCode::NotFound).into())
        }
        E::Pool(_) | E::Database(_) | E::InvalidUuid(_) => Err(crate::internal_error(error)),
    }
}

fn workflow_failure_result<T>(
    error: policy_application::WorkflowFailure,
    build_user_error: impl FnOnce(MutationErrorCode) -> T,
) -> Result<T> {
    match error {
        policy_application::WorkflowFailure::Decision => {
            Ok(build_user_error(MutationErrorCode::BadInput))
        }
        policy_application::WorkflowFailure::NotFound => {
            Ok(build_user_error(MutationErrorCode::NotFound))
        }
        policy_application::WorkflowFailure::Conflict => {
            Ok(build_user_error(MutationErrorCode::Conflict))
        }
        policy_application::WorkflowFailure::Infrastructure(message) => {
            Err(crate::internal_error(message))
        }
    }
}

macro_rules! create_error_constructor {
    ($fn_name:ident, $type_name:ident) => {
        fn $fn_name(code: MutationErrorCode) -> $type_name {
            $type_name {
                code,
                message: error_message(code).to_owned(),
            }
        }
    };
}

create_error_constructor!(create_pool_error, CreatePoolUserError);
create_error_constructor!(set_pool_funding_rule_error, SetPoolFundingRuleUserError);
create_error_constructor!(set_default_allocation_error, SetDefaultAllocationUserError);
create_error_constructor!(set_allocation_rule_error, SetAllocationRuleUserError);
create_error_constructor!(set_funding_schedule_error, SetFundingScheduleUserError);

fn error_message(code: MutationErrorCode) -> &'static str {
    match code {
        MutationErrorCode::BadInput => "入力を確認してください。",
        MutationErrorCode::Conflict => "最新の状態を読み込んでから再試行してください。",
        MutationErrorCode::NotFound => "対象が見つかりません。",
        MutationErrorCode::MailForwardingDisabled => "この操作は現在利用できません。",
    }
}
