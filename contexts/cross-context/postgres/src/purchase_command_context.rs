use diesel::{
    ExpressionMethods, JoinOnDsl, OptionalExtension, QueryDsl, result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use bookkeeping_domain::purchase_commands::{AllocationRuleCandidate, select_allocation_rule};
use policy_application::purchase_allocation::PurchaseAllocationRulesRead;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone)]
pub struct PurchaseItemAllocationInput {
    pub category_id: Option<Uuid>,
    pub product_variant_id: Option<Uuid>,
    pub merchant_key: String,
    pub allocations: Vec<(Uuid, i64)>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ResolvedPurchaseAllocation {
    pub pool_id: Uuid,
    pub weight: i64,
    pub rule_version: u64,
    pub rule_id: Option<Uuid>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseItemAllocationContext {
    pub category_id: Option<Uuid>,
    pub allocations: Vec<ResolvedPurchaseAllocation>,
    pub allocation_override: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseCommandContext {
    pub allocation_rule_version: u64,
    pub items: Vec<PurchaseItemAllocationContext>,
}

#[derive(Debug)]
pub enum PurchaseCommandContextError {
    Pool(String),
    Database(DieselError),
    HouseholdNotFound,
    MissingDefaultAllocation,
    InvalidVersion,
    MissingProductVariant,
    MissingAllocationRule,
}

impl From<DieselError> for PurchaseCommandContextError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn load_purchase_command_context(
    pool: &PgPool,
    household_id: Uuid,
    merchant_key: &str,
    item_inputs: &[PurchaseItemAllocationInput],
) -> Result<PurchaseCommandContext, PurchaseCommandContextError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseCommandContextError::Pool(error.to_string()))?;
    load_purchase_command_context_in_transaction(&mut conn, household_id, merchant_key, item_inputs)
        .await
}

pub async fn load_purchase_command_context_in_transaction(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    merchant_key: &str,
    item_inputs: &[PurchaseItemAllocationInput],
) -> Result<PurchaseCommandContext, PurchaseCommandContextError> {
    use public::{
        allocation_defaults::dsl as defaults, households::dsl as households,
        product_concepts::dsl as concepts, product_variants::dsl as variants,
    };

    let version = households::households
        .filter(households::id.eq(household_id))
        .select(households::default_allocation_version)
        .first::<i64>(&mut *conn)
        .await
        .optional()?
        .ok_or(PurchaseCommandContextError::HouseholdNotFound)?;
    let version =
        u64::try_from(version).map_err(|_| PurchaseCommandContextError::InvalidVersion)?;
    let default_weights =
        defaults::allocation_defaults
            .filter(defaults::household_id.eq(household_id))
            .filter(defaults::version.eq(
                i64::try_from(version).map_err(|_| PurchaseCommandContextError::InvalidVersion)?,
            ))
            .select((defaults::pool_id, defaults::weight))
            .load::<(Uuid, i64)>(&mut *conn)
            .await?;
    if default_weights.is_empty() {
        return Err(PurchaseCommandContextError::MissingDefaultAllocation);
    }

    let mut item_targets = Vec::with_capacity(item_inputs.len());
    let mut policy_targets = Vec::new();
    for item in item_inputs {
        let mut category_id = item.category_id;
        let mut concept_id = None;
        if let Some(variant_id) = item.product_variant_id {
            let variant = variants::product_variants
                .filter(variants::household_id.eq(household_id))
                .filter(variants::id.eq(variant_id))
                .filter(variants::archived.eq(false))
                .inner_join(concepts::product_concepts.on(concepts::id.eq(variants::concept_id)))
                .filter(concepts::household_id.eq(household_id))
                .filter(concepts::archived.eq(false))
                .select((variants::concept_id, concepts::category_id))
                .first::<(Uuid, Option<Uuid>)>(&mut *conn)
                .await
                .optional()?
                .ok_or(PurchaseCommandContextError::MissingProductVariant)?;
            concept_id = Some(variant.0);
            if category_id.is_none() {
                category_id = variant.1;
            }
        }
        item_targets.push((category_id, concept_id));
        if item.allocations.is_empty() {
            policy_targets.push(
                policy_application::purchase_allocation::AllocationRuleTargetQuery {
                    product_variant_id: item.product_variant_id,
                    product_concept_id: concept_id,
                    category_id,
                    merchant_key: (!merchant_key.is_empty()).then(|| merchant_key.to_owned()),
                },
            );
        }
    }

    let policy_rules = policy_postgres::purchase_allocation::PostgresPurchaseAllocationRulesRead
        .read(conn, household_id, &policy_targets)
        .await
        .map_err(|error| {
            match error {
            policy_application::purchase_allocation::PurchaseAllocationRulesFailure::Infrastructure(
                error,
            ) => PurchaseCommandContextError::Database(DieselError::SerializationError(
                Box::new(std::io::Error::other(error)),
            )),
        }
        })?;
    let candidates = policy_rules
        .into_iter()
        .map(|rule| {
            Ok((
                AllocationRuleCandidate {
                    rule_id: rule.id.to_string(),
                    revision: u64::try_from(rule.revision)
                        .map_err(|_| PurchaseCommandContextError::InvalidVersion)?,
                    product_variant_id: rule.product_variant_id.map(|id| id.to_string()),
                    product_concept_id: rule.product_concept_id.map(|id| id.to_string()),
                    category_id: rule.category_id.map(|id| id.to_string()),
                    merchant_key: rule.merchant_key,
                },
                rule.allocations
                    .into_iter()
                    .map(|allocation| (allocation.pool_id, allocation.weight))
                    .collect::<Vec<_>>(),
            ))
        })
        .collect::<Result<Vec<_>, PurchaseCommandContextError>>()?;

    let mut items = Vec::with_capacity(item_inputs.len());
    for (index, item) in item_inputs.iter().enumerate() {
        let (category_id, concept_id) = item_targets[index];
        if !item.allocations.is_empty() {
            items.push(PurchaseItemAllocationContext {
                category_id,
                allocations: item
                    .allocations
                    .iter()
                    .map(|(pool_id, weight)| ResolvedPurchaseAllocation {
                        pool_id: *pool_id,
                        weight: *weight,
                        rule_version: version,
                        rule_id: None,
                    })
                    .collect(),
                allocation_override: true,
            });
            continue;
        }

        let item_candidates = &candidates;
        let allocation_candidates = item_candidates
            .iter()
            .map(|(candidate, _)| candidate.clone())
            .collect::<Vec<_>>();
        let selected = select_allocation_rule(
            &allocation_candidates,
            item.product_variant_id.map(|id| id.to_string()).as_deref(),
            concept_id.map(|id| id.to_string()).as_deref(),
            category_id.map(|id| id.to_string()).as_deref(),
            merchant_key,
        );
        let (rule_id, rule_version, resolved_weights) = match selected {
            Some(selected) => {
                let rule_id = Uuid::parse_str(&selected.rule_id)
                    .map_err(|_| PurchaseCommandContextError::MissingAllocationRule)?;
                let resolved_weights = item_candidates
                    .iter()
                    .find(|(candidate, _)| candidate.rule_id == selected.rule_id)
                    .map(|(_, allocations)| allocations.clone())
                    .filter(|allocations| !allocations.is_empty())
                    .ok_or(PurchaseCommandContextError::MissingAllocationRule)?;
                (Some(rule_id), selected.revision, resolved_weights)
            }
            None => (None, version, default_weights.clone()),
        };
        items.push(PurchaseItemAllocationContext {
            category_id,
            allocations: resolved_weights
                .into_iter()
                .map(|(pool_id, weight)| ResolvedPurchaseAllocation {
                    pool_id,
                    weight,
                    rule_version,
                    rule_id,
                })
                .collect(),
            allocation_override: false,
        });
    }
    Ok(PurchaseCommandContext {
        allocation_rule_version: version,
        items,
    })
}
