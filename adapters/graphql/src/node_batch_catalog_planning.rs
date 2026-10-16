use std::collections::{HashMap, HashSet};

use async_graphql::{Context, ID, Result};
use base64::Engine;
use catalog_application::node_batch_query::{
    CatalogNodeBatch, CatalogNodeBatchInput, CatalogNodeBatchQuery,
};
use chrono::{NaiveDate, Utc};
use diesel::{ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use horse_postgres_common::{PgPool, schema::public};
use uuid::Uuid;

use crate::{
    catalog_mutations::{self, AllocationRuleTarget},
    ledger_entry::Node,
};

type NodeKey = (String, Uuid);

pub(crate) async fn load(
    ctx: &Context<'_>,
    household_ids: &[Uuid],
    requested: &[(String, Uuid)],
) -> Result<HashMap<NodeKey, Node>> {
    use public::{
        allocation_rule_weights::dsl as weights, allocation_rules::dsl as rules,
        categories::dsl as categories, household_invitations::dsl as invitations,
        household_members::dsl as members, recurring_expenses::dsl as expenses,
    };

    if household_ids.is_empty() || requested.is_empty() {
        return Ok(HashMap::new());
    }
    let mut ids_by_type: HashMap<&str, Vec<Uuid>> = HashMap::new();
    for (type_name, id) in requested {
        ids_by_type.entry(type_name.as_str()).or_default().push(*id);
    }
    for ids in ids_by_type.values_mut() {
        let mut seen = HashSet::new();
        ids.retain(|id| seen.insert(*id));
    }

    let mut loaded = HashMap::new();

    let tag_ids = ids_by_type.get("Tag").cloned().unwrap_or_default();
    let concept_ids = ids_by_type
        .get("ProductConcept")
        .cloned()
        .unwrap_or_default();
    let variant_ids = ids_by_type
        .get("ProductVariant")
        .cloned()
        .unwrap_or_default();
    let alias_ids = ids_by_type.get("ProductAlias").cloned().unwrap_or_default();
    if !tag_ids.is_empty()
        || !concept_ids.is_empty()
        || !variant_ids.is_empty()
        || !alias_ids.is_empty()
    {
        let catalog_batch = ctx
            .data::<std::sync::Arc<dyn CatalogNodeBatchQuery>>()?
            .query(CatalogNodeBatchInput {
                household_ids: household_ids.to_vec(),
                tag_ids,
                concept_ids,
                concept_variant_ids: variant_ids.clone(),
                variant_ids,
                alias_ids,
            })
            .await
            .map_err(crate::internal_error)?;
        loaded.extend(map_catalog_nodes(catalog_batch)?);
    }

    let has_policy_nodes = ids_by_type.contains_key("RecurringExpense")
        || ids_by_type.contains_key("HouseholdInvitation")
        || ids_by_type.contains_key("AllocationRule");
    if !has_policy_nodes {
        return Ok(loaded);
    }
    let pool = ctx.data::<PgPool>()?;
    let mut conn = pool.get().await.map_err(crate::internal_error)?;

    if let Some(ids) = ids_by_type.get("RecurringExpense") {
        let rows = expenses::recurring_expenses
            .filter(expenses::household_id.eq_any(household_ids))
            .filter(expenses::id.eq_any(ids))
            .select((
                expenses::id,
                expenses::household_id,
                expenses::name,
                expenses::merchant,
                expenses::amount_yen,
                expenses::currency,
                expenses::category_id,
                expenses::payment_account_id,
                expenses::cadence,
                expenses::starts_on,
                expenses::next_due_on,
                expenses::ends_on,
                expenses::archived,
                expenses::revision,
            ))
            .load::<(
                Uuid,
                Uuid,
                String,
                String,
                i64,
                String,
                Option<Uuid>,
                Option<Uuid>,
                String,
                NaiveDate,
                NaiveDate,
                Option<NaiveDate>,
                bool,
                i64,
            )>(&mut conn)
            .await
            .map_err(crate::internal_error)?;
        for (
            id,
            household_id,
            name,
            merchant,
            amount_yen,
            currency,
            category_id,
            payment_account_id,
            cadence,
            starts_on,
            next_due_on,
            ends_on,
            archived,
            revision,
        ) in rows
        {
            let cadence = match cadence.as_str() {
                "WEEKLY" => crate::policy_mutations::RecurringExpenseCadence::Weekly,
                "MONTHLY" => crate::policy_mutations::RecurringExpenseCadence::Monthly,
                "YEARLY" => crate::policy_mutations::RecurringExpenseCadence::Yearly,
                _ => {
                    return Err(crate::internal_error(
                        "invalid persisted recurring expense cadence",
                    ));
                }
            };
            let value = crate::policy_mutations::RecurringExpenseObject {
                id: relay_id("RecurringExpense", id)?,
                household_id: relay_id("Household", household_id)?,
                name,
                merchant,
                amount_yen: crate::Long(amount_yen),
                currency,
                category_id: category_id
                    .map(|value| relay_id("Category", value))
                    .transpose()?,
                payment_account_id: payment_account_id
                    .map(|value| relay_id("Account", value))
                    .transpose()?,
                cadence,
                starts_on: crate::policy_mutations::Date(starts_on),
                next_due_on: crate::policy_mutations::Date(next_due_on),
                ends_on: ends_on.map(crate::policy_mutations::Date),
                archived,
                revision: graphql_revision(revision, "RecurringExpense")?,
            };
            loaded.insert(
                ("RecurringExpense".to_owned(), id),
                Node::RecurringExpense(value),
            );
        }
    }

    if let Some(ids) = ids_by_type.get("HouseholdInvitation") {
        let actor = crate::actor(ctx)?;
        let owner_household_ids = members::household_members
            .filter(members::household_id.eq_any(household_ids))
            .filter(members::member_id.eq(actor.0))
            .filter(members::role.eq("OWNER"))
            .filter(members::left_at.is_null())
            .select(members::household_id)
            .distinct()
            .load::<Uuid>(&mut conn)
            .await
            .map_err(crate::internal_error)?;
        if !owner_household_ids.is_empty() {
            let rows = invitations::household_invitations
                .filter(invitations::household_id.eq_any(owner_household_ids))
                .filter(invitations::id.eq_any(ids))
                .select((
                    invitations::id,
                    invitations::household_id,
                    invitations::created_at,
                    invitations::expires_at,
                    invitations::accepted_at,
                    invitations::revoked_at,
                ))
                .load::<(
                    Uuid,
                    Uuid,
                    chrono::DateTime<Utc>,
                    chrono::DateTime<Utc>,
                    Option<chrono::DateTime<Utc>>,
                    Option<chrono::DateTime<Utc>>,
                )>(&mut conn)
                .await
                .map_err(crate::internal_error)?;
            let now = Utc::now();
            for (id, household_id, created_at, expires_at, accepted_at, revoked_at) in rows {
                let mapped = crate::household_lifecycle::map_invitation(
                    household_postgres::household_invitation::InvitationStateDto {
                        id: id.to_string(),
                        household_id: household_id.to_string(),
                        created_at,
                        expires_at,
                        accepted_at,
                        revoked_at,
                    },
                    now,
                )
                .map_err(crate::internal_error)?;
                loaded.insert(
                    ("HouseholdInvitation".to_owned(), id),
                    Node::HouseholdInvitation(mapped),
                );
            }
        }
    }

    // Allocation rules need their target and ordered pool weights as part of the node value.
    if let Some(ids) = ids_by_type.get("AllocationRule") {
        let rule_rows = rules::allocation_rules
            .filter(rules::household_id.eq_any(household_ids))
            .filter(rules::id.eq_any(ids))
            .select((
                rules::id,
                rules::household_id,
                rules::category_id,
                rules::merchant_key,
                rules::revision,
                rules::product_concept_id,
                rules::product_variant_id,
            ))
            .load::<(
                Uuid,
                Uuid,
                Option<Uuid>,
                Option<String>,
                i64,
                Option<Uuid>,
                Option<Uuid>,
            )>(&mut conn)
            .await
            .map_err(crate::internal_error)?;
        let rule_ids: Vec<Uuid> = rule_rows.iter().map(|row| row.0).collect();
        let weight_rows = if rule_ids.is_empty() {
            Vec::new()
        } else {
            weights::allocation_rule_weights
                .filter(weights::rule_id.eq_any(&rule_ids))
                .order_by(weights::pool_id)
                .select((weights::rule_id, weights::pool_id, weights::weight))
                .load::<(Uuid, Uuid, i64)>(&mut conn)
                .await
                .map_err(crate::internal_error)?
        };
        let pool_ids: Vec<Uuid> = weight_rows.iter().map(|row| row.1).collect();
        let pool_names = if pool_ids.is_empty() {
            HashMap::new()
        } else {
            use public::pools::dsl as pools;
            pools::pools
                .filter(pools::household_id.eq_any(household_ids))
                .filter(pools::id.eq_any(pool_ids))
                .select((pools::id, pools::name))
                .load::<(Uuid, String)>(&mut conn)
                .await
                .map_err(crate::internal_error)?
                .into_iter()
                .collect()
        };
        let category_ids: Vec<Uuid> = rule_rows.iter().filter_map(|row| row.2).collect();
        let category_values = if category_ids.is_empty() {
            HashMap::new()
        } else {
            categories::categories
                .filter(categories::household_id.eq_any(household_ids))
                .filter(categories::id.eq_any(category_ids))
                .select((
                    categories::id,
                    categories::name,
                    categories::parent_id,
                    categories::archived,
                ))
                .load::<(Uuid, String, Option<Uuid>, bool)>(&mut conn)
                .await
                .map_err(crate::internal_error)?
                .into_iter()
                .map(|(id, name, parent_id, archived)| {
                    let value = crate::category::Category {
                        id: relay_id("Category", id)?,
                        name,
                        parent_id: parent_id
                            .map(|parent_id| relay_id("Category", parent_id))
                            .transpose()?,
                        archived,
                    };
                    Ok((id, value))
                })
                .collect::<Result<HashMap<_, _>>>()?
        };
        let concept_targets: Vec<Uuid> = rule_rows.iter().filter_map(|row| row.5).collect();
        let variant_targets: Vec<Uuid> = rule_rows.iter().filter_map(|row| row.6).collect();
        let products =
            load_products_for_targets(ctx, household_ids, &concept_targets, &variant_targets)
                .await?;
        let mut weights_by_rule: HashMap<Uuid, Vec<crate::pool_mutations::AllocationObject>> =
            HashMap::new();
        for (rule_id, pool_id, weight) in weight_rows {
            let pool_name = pool_names
                .get(&pool_id)
                .ok_or_else(|| crate::internal_error("allocation rule weight pool is missing"))?
                .clone();
            weights_by_rule.entry(rule_id).or_default().push(
                crate::pool_mutations::AllocationObject {
                    pool_id: relay_id("Pool", pool_id)?,
                    pool_name,
                    weight: crate::Long(weight),
                },
            );
        }
        for (id, household_id, category_id, merchant_key, revision, concept_id, variant_id) in
            rule_rows
        {
            let target = if let Some(category_id) = category_id {
                AllocationRuleTarget::category(
                    category_values.get(&category_id).cloned().ok_or_else(|| {
                        crate::internal_error("allocation rule category is missing")
                    })?,
                )
            } else if let Some(concept_id) = concept_id {
                AllocationRuleTarget::product_concept(
                    products
                        .get(&("ProductConcept".to_owned(), concept_id))
                        .and_then(|node| match node {
                            Node::ProductConcept(value) => Some(value.clone()),
                            _ => None,
                        })
                        .ok_or_else(|| {
                            crate::internal_error("allocation rule product concept is missing")
                        })?,
                )
            } else if let Some(variant_id) = variant_id {
                AllocationRuleTarget::product_variant(
                    products
                        .get(&("ProductVariant".to_owned(), variant_id))
                        .and_then(|node| match node {
                            Node::ProductVariant(value) => Some(value.clone()),
                            _ => None,
                        })
                        .ok_or_else(|| {
                            crate::internal_error("allocation rule product variant is missing")
                        })?,
                )
            } else if let Some(name) = merchant_key {
                AllocationRuleTarget::merchant(name)
            } else {
                return Err(crate::internal_error("allocation rule has no target"));
            };
            let value = crate::pool_mutations::AllocationRuleObject {
                id: relay_id("AllocationRule", id)?,
                household_id: relay_id("Household", household_id)?,
                target,
                revision: graphql_revision(revision, "AllocationRule")?,
                allocations: weights_by_rule.remove(&id).unwrap_or_default(),
            };
            loaded.insert(
                ("AllocationRule".to_owned(), id),
                Node::AllocationRule(value),
            );
        }
    }
    Ok(loaded)
}

async fn load_products_for_targets(
    ctx: &Context<'_>,
    household_ids: &[Uuid],
    concept_ids: &[Uuid],
    variant_ids: &[Uuid],
) -> Result<HashMap<NodeKey, Node>> {
    if concept_ids.is_empty() && variant_ids.is_empty() {
        return Ok(HashMap::new());
    }
    let batch = ctx
        .data::<std::sync::Arc<dyn CatalogNodeBatchQuery>>()?
        .query(CatalogNodeBatchInput {
            household_ids: household_ids.to_vec(),
            tag_ids: Vec::new(),
            concept_ids: concept_ids.to_vec(),
            variant_ids: variant_ids.to_vec(),
            concept_variant_ids: Vec::new(),
            alias_ids: Vec::new(),
        })
        .await
        .map_err(crate::internal_error)?;
    map_catalog_nodes(batch)
}

fn map_catalog_nodes(batch: CatalogNodeBatch) -> Result<HashMap<NodeKey, Node>> {
    let mut result = HashMap::new();
    for tag in batch.tags {
        let id = tag.id;
        let value = catalog_mutations::Tag {
            id: relay_id("Tag", id)?,
            household_id: relay_id("Household", tag.household_id)?,
            name: tag.name,
            archived: tag.archived,
            revision: graphql_revision(tag.revision, "Tag")?,
        };
        result.insert(("Tag".to_owned(), id), Node::Tag(value));
    }
    for concept in batch.concepts {
        let id = concept.id;
        result.insert(
            ("ProductConcept".to_owned(), id),
            Node::ProductConcept(map_product_concept(concept)?),
        );
    }
    for variant in batch.variants {
        let id = variant.id;
        result.insert(
            ("ProductVariant".to_owned(), id),
            Node::ProductVariant(map_product_variant(variant)?),
        );
    }
    for alias in batch.aliases {
        let id = alias.id;
        result.insert(
            ("ProductAlias".to_owned(), id),
            Node::ProductAlias(map_product_alias(alias)?),
        );
    }
    Ok(result)
}

fn map_product_concept(
    value: catalog_application::product_query::ProductConceptProjection,
) -> Result<catalog_mutations::ProductConcept> {
    Ok(catalog_mutations::ProductConcept {
        id: relay_id("ProductConcept", value.id)?,
        household_id: relay_id("Household", value.household_id)?,
        name: value.name,
        normalized_name: value.normalized_name,
        category_id: value
            .category_id
            .map(|id| relay_id("Category", id))
            .transpose()?,
        archived: value.archived,
        revision: graphql_revision(value.revision, "ProductConcept")?,
        variants: value
            .variants
            .into_iter()
            .map(map_product_variant)
            .collect::<Result<Vec<_>>>()?,
    })
}

fn map_product_variant(
    value: catalog_application::product_query::ProductVariantProjection,
) -> Result<catalog_mutations::ProductVariant> {
    Ok(catalog_mutations::ProductVariant {
        id: relay_id("ProductVariant", value.id)?,
        household_id: relay_id("Household", value.household_id)?,
        concept_id: relay_id("ProductConcept", value.concept_id)?,
        name: value.name,
        normalized_name: value.normalized_name,
        brand: nonempty(value.brand),
        package_size: nonempty(value.package_size),
        barcode: value.barcode,
        archived: value.archived,
        revision: graphql_revision(value.revision, "ProductVariant")?,
        aliases: value
            .aliases
            .into_iter()
            .map(map_product_alias)
            .collect::<Result<Vec<_>>>()?,
    })
}

fn map_product_alias(
    value: catalog_application::product_query::ProductAliasProjection,
) -> Result<catalog_mutations::ProductAlias> {
    Ok(catalog_mutations::ProductAlias {
        id: relay_id("ProductAlias", value.id)?,
        household_id: relay_id("Household", value.household_id)?,
        variant_id: relay_id("ProductVariant", value.variant_id)?,
        merchant_key: value.merchant_key,
        alias: value.alias,
        normalized_alias: value.normalized_alias,
        revision: graphql_revision(value.revision, "ProductAlias")?,
    })
}

fn relay_id(type_name: &str, id: Uuid) -> Result<ID> {
    let mut payload = Vec::with_capacity(type_name.len() + 17);
    payload.extend_from_slice(type_name.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    Ok(ID(
        base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(payload)
    ))
}

fn graphql_revision(value: i64, type_name: &str) -> Result<i32> {
    i32::try_from(value)
        .map_err(|_| crate::internal_error(format!("{type_name} revision exceeds GraphQL Int")))
}

fn nonempty(value: String) -> Option<String> {
    (!value.is_empty()).then_some(value)
}
