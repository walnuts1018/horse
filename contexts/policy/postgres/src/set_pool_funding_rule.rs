use std::collections::BTreeSet;

use chrono::Utc;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_authorization::{Operation, authorize};
use policy_application::set_pool_funding_rule::{
    FundingRuleActorId, FundingRuleHouseholdId, FundingRuleMemberId, FundingRulePoolId,
    PersistedPoolFundingRule, PoolFundingConfiguration, PoolFundingKind, PoolFundingRuleContext,
    SetPoolFundingRuleCommand, SetPoolFundingRuleDecision, SetPoolFundingRuleOperations,
    SharedSettlementPolicy, set_pool_funding_rule,
};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug)]
pub enum SetPoolFundingRulePersistenceError {
    Pool(String),
    Database(DieselError),
    HouseholdNotFound,
    ActorNotFound,
    PoolNotFound,
    MemberNotFound,
    InvalidDecision,
    NumericOutOfRange(&'static str),
}

impl From<DieselError> for SetPoolFundingRulePersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

enum TransactionError {
    Persistence(SetPoolFundingRulePersistenceError),
    Workflow(policy_application::WorkflowFailure),
}

impl From<DieselError> for TransactionError {
    fn from(error: DieselError) -> Self {
        Self::Persistence(SetPoolFundingRulePersistenceError::Database(error))
    }
}

impl SetPoolFundingRuleOperations for PostgresSetPoolFundingRuleOperations<'_> {
    type Error = SetPoolFundingRulePersistenceError;

    fn classify_error(error: Self::Error) -> policy_application::WorkflowFailure {
        use policy_application::WorkflowFailure;
        match error {
            SetPoolFundingRulePersistenceError::HouseholdNotFound
            | SetPoolFundingRulePersistenceError::ActorNotFound
            | SetPoolFundingRulePersistenceError::PoolNotFound
            | SetPoolFundingRulePersistenceError::MemberNotFound => WorkflowFailure::NotFound,
            SetPoolFundingRulePersistenceError::Pool(message) => {
                WorkflowFailure::Infrastructure(message)
            }
            SetPoolFundingRulePersistenceError::Database(error) => {
                WorkflowFailure::Infrastructure(error.to_string())
            }
            SetPoolFundingRulePersistenceError::InvalidDecision => WorkflowFailure::Decision,
            SetPoolFundingRulePersistenceError::NumericOutOfRange(_) => {
                WorkflowFailure::Infrastructure("numeric value is out of range".to_owned())
            }
        }
    }

    async fn load_context(
        &mut self,
        actor_id: FundingRuleActorId,
        household_id: FundingRuleHouseholdId,
        pool_id: FundingRulePoolId,
    ) -> Result<PoolFundingRuleContext, Self::Error> {
        use public::{household_members as members, households, pools};
        let household_id = household_id.value();
        let actor_id = actor_id.value();
        let pool_id = pool_id.value();

        households::table
            .filter(households::id.eq(household_id))
            .for_update()
            .select(households::id)
            .first::<Uuid>(&mut *self.conn)
            .await
            .optional()?
            .ok_or(SetPoolFundingRulePersistenceError::HouseholdNotFound)?;

        let actor_role = members::table
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(actor_id))
            .filter(members::left_at.is_null())
            .select(members::role)
            .first::<String>(&mut *self.conn)
            .await
            .optional()?;
        if authorize(actor_role.as_deref(), Operation::ManageHousehold).is_err() {
            return Err(SetPoolFundingRulePersistenceError::ActorNotFound);
        }

        let (kind, current_version) = pools::table
            .filter(pools::household_id.eq(household_id))
            .filter(pools::id.eq(pool_id))
            .for_update()
            .select((pools::kind, pools::current_funding_version))
            .first::<(String, Option<i32>)>(&mut *self.conn)
            .await
            .optional()?
            .ok_or(SetPoolFundingRulePersistenceError::PoolNotFound)?;
        let kind = match kind.as_str() {
            "PERSONAL" => PoolFundingKind::Personal,
            "SHARED" => PoolFundingKind::Shared,
            _ => return Err(SetPoolFundingRulePersistenceError::InvalidDecision),
        };
        let active_member_ids = members::table
            .filter(members::household_id.eq(household_id))
            .filter(members::left_at.is_null())
            .select(members::member_id)
            .load::<Uuid>(&mut *self.conn)
            .await?
            .into_iter()
            .map(FundingRuleMemberId::new)
            .collect::<BTreeSet<_>>();

        Ok(PoolFundingRuleContext {
            kind,
            current_version: current_version.unwrap_or(0),
            active_member_ids,
        })
    }

    async fn persist(
        &mut self,
        decision: SetPoolFundingRuleDecision,
    ) -> Result<PersistedPoolFundingRule, Self::Error> {
        use public::{pool_funding_rules as rules, pool_funding_shares as funding_shares, pools};
        let actor_id = decision.actor_id.value();
        let household_id = decision.household_id.value();
        let pool_id = decision.pool_id.value();
        let version = decision.version;
        let (owner_member_id, settlement_policy) = match &decision.configuration {
            PoolFundingConfiguration::Personal { owner_member_id } => {
                (Some(owner_member_id.value()), None)
            }
            PoolFundingConfiguration::Shared {
                settlement_policy, ..
            } => {
                let policy = match settlement_policy {
                    SharedSettlementPolicy::BalanceToStandard => "BALANCE_TO_STANDARD",
                    SharedSettlementPolicy::ReimbursePersonal => "REIMBURSE_PERSONAL",
                };
                (None, Some(policy))
            }
        };

        let initial_placeholder = version == 1
            && rules::table
                .filter(rules::household_id.eq(household_id))
                .filter(rules::pool_id.eq(pool_id))
                .filter(rules::version.eq(version))
                .select(rules::version)
                .first::<i32>(&mut *self.conn)
                .await
                .optional()?
                .is_some();
        if initial_placeholder {
            let affected = diesel::update(
                rules::table
                    .filter(rules::household_id.eq(household_id))
                    .filter(rules::pool_id.eq(pool_id))
                    .filter(rules::version.eq(version))
                    .filter(rules::owner_member_id.is_null())
                    .filter(rules::settlement_policy.is_null()),
            )
            .set((
                rules::owner_member_id.eq(owner_member_id),
                rules::settlement_policy.eq(settlement_policy),
                rules::created_by.eq(actor_id),
            ))
            .execute(&mut *self.conn)
            .await?;
            if affected != 1 {
                return Err(SetPoolFundingRulePersistenceError::InvalidDecision);
            }
        } else {
            diesel::insert_into(rules::table)
                .values((
                    rules::household_id.eq(household_id),
                    rules::pool_id.eq(pool_id),
                    rules::version.eq(version),
                    rules::owner_member_id.eq(owner_member_id),
                    rules::settlement_policy.eq(settlement_policy),
                    rules::created_by.eq(actor_id),
                ))
                .execute(&mut *self.conn)
                .await?;
        }
        for share in &decision.shares {
            diesel::insert_into(funding_shares::table)
                .values((
                    funding_shares::household_id.eq(household_id),
                    funding_shares::pool_id.eq(pool_id),
                    funding_shares::version.eq(version),
                    funding_shares::member_id.eq(share.member_id.value()),
                    funding_shares::weight.eq(share.weight),
                ))
                .execute(&mut *self.conn)
                .await?;
        }
        let affected = diesel::update(
            pools::table
                .filter(pools::household_id.eq(household_id))
                .filter(pools::id.eq(pool_id)),
        )
        .set(pools::current_funding_version.eq(Some(version)))
        .execute(&mut *self.conn)
        .await?;
        if affected != 1 {
            return Err(SetPoolFundingRulePersistenceError::PoolNotFound);
        }

        household_postgres::record_change::record_household_change(
            &mut *self.conn,
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
            household_postgres::record_change::RecordHouseholdChangeError::Database(error) => {
                SetPoolFundingRulePersistenceError::Database(error)
            }
            household_postgres::record_change::RecordHouseholdChangeError::HouseholdNotFound => {
                SetPoolFundingRulePersistenceError::HouseholdNotFound
            }
            household_postgres::record_change::RecordHouseholdChangeError::NumericOutOfRange => {
                SetPoolFundingRulePersistenceError::NumericOutOfRange("household_revision")
            }
        })?;
        Ok(PersistedPoolFundingRule {
            pool_id: FundingRulePoolId::new(pool_id),
            version,
        })
    }
}

struct PostgresSetPoolFundingRuleOperations<'a> {
    conn: &'a mut crate::AsyncPgConnection,
}

pub async fn persist_set_pool_funding_rule(
    pool: &PgPool,
    command: SetPoolFundingRuleCommand,
) -> Result<PersistedPoolFundingRule, policy_application::WorkflowFailure> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| policy_application::WorkflowFailure::Infrastructure(error.to_string()))?;
    (*pooled)
        .transaction::<PersistedPoolFundingRule, TransactionError, _>(async move |conn| {
            let mut operations = PostgresSetPoolFundingRuleOperations { conn };
            set_pool_funding_rule(&mut operations, command)
                .await
                .map_err(TransactionError::Workflow)
        })
        .await
        .map_err(|error| match error {
            TransactionError::Workflow(error) => error,
            TransactionError::Persistence(error) => {
                PostgresSetPoolFundingRuleOperations::classify_error(error)
            }
        })
}

pub struct PostgresSetPoolFundingRuleHandler {
    pool: PgPool,
}

impl PostgresSetPoolFundingRuleHandler {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl policy_application::set_pool_funding_rule::SetPoolFundingRuleHandler
    for PostgresSetPoolFundingRuleHandler
{
    fn set<'a>(
        &'a self,
        command: SetPoolFundingRuleCommand,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<PersistedPoolFundingRule, policy_application::WorkflowFailure>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(persist_set_pool_funding_rule(&self.pool, command))
    }
}
