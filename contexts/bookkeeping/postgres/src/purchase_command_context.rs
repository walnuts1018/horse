use diesel::{
    ExpressionMethods, JoinOnDsl, OptionalExtension, QueryDsl, result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

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
    use public::{
        allocation_defaults::dsl as defaults, allocation_rule_weights::dsl as weights,
        allocation_rules::dsl as rules, households::dsl as households,
        product_concepts::dsl as concepts, product_variants::dsl as variants,
    };

    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseCommandContextError::Pool(error.to_string()))?;
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

    let mut items = Vec::with_capacity(item_inputs.len());
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

        let mut selected = None;
        if let Some(variant_id) = item.product_variant_id {
            selected = rules::allocation_rules
                .filter(rules::household_id.eq(household_id))
                .filter(rules::product_variant_id.eq(Some(variant_id)))
                .select((rules::id, rules::revision))
                .first::<(Uuid, i64)>(&mut *conn)
                .await
                .optional()?;
        }
        if selected.is_none()
            && let Some(concept_id) = concept_id
        {
            selected = rules::allocation_rules
                .filter(rules::household_id.eq(household_id))
                .filter(rules::product_concept_id.eq(Some(concept_id)))
                .select((rules::id, rules::revision))
                .first::<(Uuid, i64)>(&mut *conn)
                .await
                .optional()?;
        }
        if selected.is_none()
            && let Some(category_id) = category_id
        {
            selected = rules::allocation_rules
                .filter(rules::household_id.eq(household_id))
                .filter(rules::category_id.eq(Some(category_id)))
                .select((rules::id, rules::revision))
                .first::<(Uuid, i64)>(&mut *conn)
                .await
                .optional()?;
        }
        if selected.is_none() && !merchant_key.is_empty() {
            selected = rules::allocation_rules
                .filter(rules::household_id.eq(household_id))
                .filter(rules::merchant_key.eq(Some(merchant_key.to_owned())))
                .select((rules::id, rules::revision))
                .first::<(Uuid, i64)>(&mut *conn)
                .await
                .optional()?;
        }
        let (rule_id, rule_version, resolved_weights) = match selected {
            Some((rule_id, rule_revision)) => {
                let rule_version = u64::try_from(rule_revision)
                    .map_err(|_| PurchaseCommandContextError::InvalidVersion)?;
                let resolved_weights = weights::allocation_rule_weights
                    .filter(weights::household_id.eq(household_id))
                    .filter(weights::rule_id.eq(rule_id))
                    .select((weights::pool_id, weights::weight))
                    .load::<(Uuid, i64)>(&mut *conn)
                    .await?;
                if resolved_weights.is_empty() {
                    return Err(PurchaseCommandContextError::MissingAllocationRule);
                }
                (Some(rule_id), rule_version, resolved_weights)
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
