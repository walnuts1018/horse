use std::collections::BTreeSet;

use bookkeeping_workflows::set_allocation_rule::{
    AllocationRuleTarget, SetAllocationRuleCommand, SetAllocationRuleDecision,
    SetAllocationRuleError, decide_set_allocation_rule, validate_set_allocation_rule,
};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedAllocationRule {
    pub id: String,
    pub revision: u64,
}

#[derive(Debug)]
pub enum SetAllocationRulePersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    NumericOutOfRange(&'static str),
    HouseholdNotFound,
    ActorNotFound,
    ReferenceNotFound,
    PoolNotFound,
    InvalidDecision,
    InvalidWorkflow(SetAllocationRuleError),
}

impl From<DieselError> for SetAllocationRulePersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_set_allocation_rule(
    pool: &PgPool,
    command: SetAllocationRuleCommand,
) -> Result<PersistedAllocationRule, SetAllocationRulePersistenceError> {
    let validated = validate_set_allocation_rule(command)
        .map_err(SetAllocationRulePersistenceError::InvalidWorkflow)?;
    let household_id = parse_uuid(&validated.household_id, "household_id")?;
    let actor_id = parse_uuid(&validated.actor_id, "actor_id")?;
    let category_id = parse_optional_uuid(validated.target.category_id.as_deref(), "category_id")?;
    let product_concept_id = parse_optional_uuid(
        validated.target.product_concept_id.as_deref(),
        "product_concept_id",
    )?;
    let product_variant_id = parse_optional_uuid(
        validated.target.product_variant_id.as_deref(),
        "product_variant_id",
    )?;
    let pool_ids = validated
        .weights
        .iter()
        .map(|weight| parse_uuid(&weight.pool_id, "pool_id"))
        .collect::<Result<Vec<_>, _>>()?;
    let expected_pool_ids = pool_ids.iter().copied().collect::<BTreeSet<_>>();
    if expected_pool_ids.len() != pool_ids.len() {
        return Err(SetAllocationRulePersistenceError::InvalidDecision);
    }

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| SetAllocationRulePersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedAllocationRule, SetAllocationRulePersistenceError, _>(
            async move |conn| {
                use public::allocation_rule_weights::dsl as weights;
                use public::allocation_rules::dsl as rules;
                use public::household_events::dsl as events;
                use public::household_members::dsl as members;
                use public::households::dsl as households;
                use public::pools::dsl as pools;

                households::households
                    .filter(households::id.eq(household_id))
                    .for_update()
                    .select(households::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .ok_or(SetAllocationRulePersistenceError::HouseholdNotFound)?;

                let actor_is_active = members::household_members
                    .filter(members::household_id.eq(household_id))
                    .filter(members::member_id.eq(actor_id))
                    .filter(members::left_at.is_null())
                    .select(members::member_id)
                    .first::<Uuid>(&mut *conn)
                    .await
                    .optional()?
                    .is_some();
                if !actor_is_active {
                    return Err(SetAllocationRulePersistenceError::ActorNotFound);
                }

                let scoped_pool_ids = pools::pools
                    .filter(pools::household_id.eq(household_id))
                    .filter(pools::id.eq_any(&pool_ids))
                    .select(pools::id)
                    .load::<Uuid>(&mut *conn)
                    .await?
                    .into_iter()
                    .collect::<BTreeSet<_>>();
                if scoped_pool_ids != expected_pool_ids {
                    return Err(SetAllocationRulePersistenceError::PoolNotFound);
                }

                validate_target_reference(
                    &mut *conn,
                    household_id,
                    category_id,
                    product_concept_id,
                    product_variant_id,
                )
                .await?;

                let existing = find_rule(
                    &mut *conn,
                    household_id,
                    &validated.target,
                    category_id,
                    product_concept_id,
                    product_variant_id,
                )
                .await?;
                let current = existing
                    .map(|(id, revision)| {
                        u64::try_from(revision)
                            .map(|revision| (id.to_string(), revision))
                            .map_err(|_| SetAllocationRulePersistenceError::InvalidDecision)
                    })
                    .transpose()?;
                let decision = decide_set_allocation_rule(validated, current)
                    .map_err(SetAllocationRulePersistenceError::InvalidWorkflow)?;
                validate_decision(
                    &decision,
                    actor_id,
                    household_id,
                    category_id,
                    product_concept_id,
                    product_variant_id,
                )?;
                let revision = i64::try_from(decision.revision).map_err(|_| {
                    SetAllocationRulePersistenceError::NumericOutOfRange("revision")
                })?;
                let rule_id = match decision.rule_id.as_deref() {
                    Some(value) => parse_uuid(value, "rule_id")?,
                    None => Uuid::now_v7(),
                };

                if existing.is_some() {
                    diesel::update(rules::allocation_rules.filter(rules::id.eq(rule_id)))
                        .set((rules::revision.eq(revision), rules::updated_at.eq(now)))
                        .execute(&mut *conn)
                        .await?;
                    diesel::delete(
                        weights::allocation_rule_weights.filter(weights::rule_id.eq(rule_id)),
                    )
                    .execute(&mut *conn)
                    .await?;
                } else {
                    diesel::insert_into(rules::allocation_rules)
                        .values((
                            rules::id.eq(rule_id),
                            rules::household_id.eq(household_id),
                            rules::category_id.eq(category_id),
                            rules::merchant_key.eq(decision.target.merchant.as_deref()),
                            rules::product_concept_id.eq(product_concept_id),
                            rules::product_variant_id.eq(product_variant_id),
                            rules::revision.eq(revision),
                        ))
                        .execute(&mut *conn)
                        .await?;
                }

                for weight in &decision.weights {
                    let pool_id = parse_uuid(&weight.pool_id, "pool_id")?;
                    diesel::insert_into(weights::allocation_rule_weights)
                        .values((
                            weights::household_id.eq(household_id),
                            weights::rule_id.eq(rule_id),
                            weights::pool_id.eq(pool_id),
                            weights::weight.eq(weight.weight),
                        ))
                        .execute(&mut *conn)
                        .await?;
                }

                diesel::insert_into(events::household_events)
                    .values((
                        events::household_id.eq(household_id),
                        events::event_type.eq("allocation.rule.updated"),
                        events::entity_kind.eq("allocation_rule"),
                        events::entity_id.eq(rule_id),
                        events::revision.eq(revision),
                        events::changed_fields.eq(decision
                            .changed_fields
                            .iter()
                            .cloned()
                            .map(Some)
                            .collect::<Vec<_>>()),
                        events::actor_id.eq(actor_id),
                    ))
                    .execute(&mut *conn)
                    .await?;

                Ok(PersistedAllocationRule {
                    id: rule_id.to_string(),
                    revision: decision.revision,
                })
            },
        )
        .await
}

async fn validate_target_reference(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    category_id: Option<Uuid>,
    product_concept_id: Option<Uuid>,
    product_variant_id: Option<Uuid>,
) -> Result<(), SetAllocationRulePersistenceError> {
    if let Some(id) = category_id {
        let exists = public::categories::table
            .filter(public::categories::household_id.eq(household_id))
            .filter(public::categories::id.eq(id))
            .select(public::categories::id)
            .first::<Uuid>(conn)
            .await
            .optional()?
            .is_some();
        if !exists {
            return Err(SetAllocationRulePersistenceError::ReferenceNotFound);
        }
    }
    if let Some(id) = product_concept_id {
        let exists = public::product_concepts::table
            .filter(public::product_concepts::household_id.eq(household_id))
            .filter(public::product_concepts::id.eq(id))
            .select(public::product_concepts::id)
            .first::<Uuid>(conn)
            .await
            .optional()?
            .is_some();
        if !exists {
            return Err(SetAllocationRulePersistenceError::ReferenceNotFound);
        }
    }
    if let Some(id) = product_variant_id {
        let exists = public::product_variants::table
            .filter(public::product_variants::household_id.eq(household_id))
            .filter(public::product_variants::id.eq(id))
            .select(public::product_variants::id)
            .first::<Uuid>(conn)
            .await
            .optional()?
            .is_some();
        if !exists {
            return Err(SetAllocationRulePersistenceError::ReferenceNotFound);
        }
    }
    Ok(())
}

async fn find_rule(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    target: &AllocationRuleTarget,
    category_id: Option<Uuid>,
    product_concept_id: Option<Uuid>,
    product_variant_id: Option<Uuid>,
) -> Result<Option<(Uuid, i64)>, SetAllocationRulePersistenceError> {
    use public::allocation_rules::dsl as rules;

    let query = rules::allocation_rules.filter(rules::household_id.eq(household_id));
    let result = if category_id.is_some() {
        query
            .filter(rules::category_id.eq(category_id))
            .for_update()
            .select((rules::id, rules::revision))
            .first::<(Uuid, i64)>(conn)
            .await
            .optional()?
    } else if product_concept_id.is_some() {
        query
            .filter(rules::product_concept_id.eq(product_concept_id))
            .for_update()
            .select((rules::id, rules::revision))
            .first::<(Uuid, i64)>(conn)
            .await
            .optional()?
    } else if product_variant_id.is_some() {
        query
            .filter(rules::product_variant_id.eq(product_variant_id))
            .for_update()
            .select((rules::id, rules::revision))
            .first::<(Uuid, i64)>(conn)
            .await
            .optional()?
    } else {
        let merchant = target
            .merchant
            .as_deref()
            .ok_or(SetAllocationRulePersistenceError::InvalidDecision)?;
        query
            .filter(rules::merchant_key.eq(Some(merchant)))
            .for_update()
            .select((rules::id, rules::revision))
            .first::<(Uuid, i64)>(conn)
            .await
            .optional()?
    };
    Ok(result)
}

fn validate_decision(
    decision: &SetAllocationRuleDecision,
    actor_id: Uuid,
    household_id: Uuid,
    category_id: Option<Uuid>,
    product_concept_id: Option<Uuid>,
    product_variant_id: Option<Uuid>,
) -> Result<(), SetAllocationRulePersistenceError> {
    let selected_targets = [
        decision.target.category_id.is_some(),
        decision.target.merchant.is_some(),
        decision.target.product_concept_id.is_some(),
        decision.target.product_variant_id.is_some(),
    ]
    .into_iter()
    .filter(|selected| *selected)
    .count();
    let target_field = if decision.target.product_variant_id.is_some() {
        "productVariantId"
    } else if decision.target.product_concept_id.is_some() {
        "productConceptId"
    } else if decision.target.category_id.is_some() {
        "categoryId"
    } else {
        "merchant"
    };
    if parse_uuid(&decision.actor_id, "actor_id")? != actor_id
        || parse_uuid(&decision.household_id, "household_id")? != household_id
        || decision.weights.is_empty()
        || selected_targets != 1
        || decision.changed_fields != ["allocations", target_field]
        || decision.weights.iter().any(|weight| weight.weight <= 0)
        || decision
            .target
            .category_id
            .as_deref()
            .map(|id| parse_uuid(id, "category_id"))
            .transpose()?
            != category_id
        || decision
            .target
            .product_concept_id
            .as_deref()
            .map(|id| parse_uuid(id, "product_concept_id"))
            .transpose()?
            != product_concept_id
        || decision
            .target
            .product_variant_id
            .as_deref()
            .map(|id| parse_uuid(id, "product_variant_id"))
            .transpose()?
            != product_variant_id
    {
        return Err(SetAllocationRulePersistenceError::InvalidDecision);
    }
    Ok(())
}

fn parse_optional_uuid(
    value: Option<&str>,
    field: &'static str,
) -> Result<Option<Uuid>, SetAllocationRulePersistenceError> {
    value.map(|value| parse_uuid(value, field)).transpose()
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, SetAllocationRulePersistenceError> {
    Uuid::parse_str(value).map_err(|_| SetAllocationRulePersistenceError::InvalidUuid(field))
}
