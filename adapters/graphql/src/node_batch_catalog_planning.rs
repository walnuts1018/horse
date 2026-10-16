use std::collections::{HashMap, HashSet};

use async_graphql::{Context, ID, Result};
use base64::Engine;
use catalog_application::node_batch_query::{
    CatalogNodeBatch, CatalogNodeBatchInput, CatalogNodeBatchQuery,
};
use chrono::Utc;
use household_application::{
    invitation::InvitationState,
    invitation_node_query::{HouseholdInvitationNodeQuery, HouseholdInvitationNodeQueryInput},
};
use policy_application::node_catalog_planning_query::{PolicyNodeBatchInput, PolicyNodeBatchQuery};
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

    let recurring_expense_ids = ids_by_type
        .get("RecurringExpense")
        .cloned()
        .unwrap_or_default();
    let allocation_rule_ids = ids_by_type
        .get("AllocationRule")
        .cloned()
        .unwrap_or_default();
    if !recurring_expense_ids.is_empty() || !allocation_rule_ids.is_empty() {
        let batch = ctx
            .data::<std::sync::Arc<dyn PolicyNodeBatchQuery>>()?
            .load(PolicyNodeBatchInput {
                household_ids: household_ids.to_vec(),
                recurring_expense_ids,
                allocation_rule_ids,
            })
            .await
            .map_err(crate::internal_error)?;
        for row in batch.recurring_expenses {
            let cadence = match row.cadence.as_str() {
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
                id: relay_id("RecurringExpense", row.id)?,
                household_id: relay_id("Household", row.household_id)?,
                name: row.name,
                merchant: row.merchant,
                amount_yen: crate::Long(row.amount_yen),
                currency: row.currency,
                category_id: row
                    .category_id
                    .map(|value| relay_id("Category", value))
                    .transpose()?,
                payment_account_id: row
                    .payment_account_id
                    .map(|value| relay_id("Account", value))
                    .transpose()?,
                cadence,
                starts_on: crate::policy_mutations::Date(row.starts_on),
                next_due_on: crate::policy_mutations::Date(row.next_due_on),
                ends_on: row.ends_on.map(crate::policy_mutations::Date),
                archived: row.archived,
                revision: graphql_revision(row.revision, "RecurringExpense")?,
            };
            loaded.insert(
                ("RecurringExpense".to_owned(), row.id),
                Node::RecurringExpense(value),
            );
        }
        let concept_targets = batch
            .allocation_rules
            .iter()
            .filter_map(|row| row.product_concept_id)
            .collect::<Vec<_>>();
        let variant_targets = batch
            .allocation_rules
            .iter()
            .filter_map(|row| row.product_variant_id)
            .collect::<Vec<_>>();
        let products =
            load_products_for_targets(ctx, household_ids, &concept_targets, &variant_targets)
                .await?;
        for row in batch.allocation_rules {
            let target = if let Some(category_id) = row.category_id {
                AllocationRuleTarget::category(crate::category::Category {
                    id: relay_id("Category", category_id)?,
                    name: row.category_name.ok_or_else(|| {
                        crate::internal_error("allocation rule category name is missing")
                    })?,
                    parent_id: row
                        .category_parent_id
                        .map(|id| relay_id("Category", id))
                        .transpose()?,
                    archived: row.category_archived.ok_or_else(|| {
                        crate::internal_error("allocation rule category state is missing")
                    })?,
                })
            } else if let Some(concept_id) = row.product_concept_id {
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
            } else if let Some(variant_id) = row.product_variant_id {
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
            } else if let Some(merchant_key) = row.merchant_key {
                AllocationRuleTarget::merchant(merchant_key)
            } else {
                return Err(crate::internal_error("allocation rule has no target"));
            };
            let allocations = row
                .allocations
                .into_iter()
                .map(|value| {
                    Ok(crate::pool_mutations::AllocationObject {
                        pool_id: relay_id("Pool", value.pool_id)?,
                        pool_name: value.pool_name,
                        weight: crate::Long(value.weight),
                    })
                })
                .collect::<Result<Vec<_>>>()?;
            let value = crate::pool_mutations::AllocationRuleObject {
                id: relay_id("AllocationRule", row.id)?,
                household_id: relay_id("Household", row.household_id)?,
                target,
                revision: graphql_revision(row.revision, "AllocationRule")?,
                allocations,
            };
            loaded.insert(
                ("AllocationRule".to_owned(), row.id),
                Node::AllocationRule(value),
            );
        }
    }

    if let Some(ids) = ids_by_type.get("HouseholdInvitation") {
        let actor = crate::actor(ctx)?;
        let rows = ctx
            .data::<std::sync::Arc<dyn HouseholdInvitationNodeQuery>>()?
            .load(HouseholdInvitationNodeQueryInput {
                actor_id: actor.0,
                household_ids: household_ids.to_vec(),
                invitation_ids: ids.clone(),
            })
            .await
            .map_err(crate::internal_error)?;
        let now = Utc::now();
        for row in rows {
            let mapped = crate::household_lifecycle::map_invitation(
                InvitationState {
                    id: row.id.to_string(),
                    household_id: row.household_id.to_string(),
                    created_at: row.created_at,
                    expires_at: row.expires_at,
                    accepted_at: row.accepted_at,
                    revoked_at: row.revoked_at,
                },
                now,
            )
            .map_err(crate::internal_error)?;
            loaded.insert(
                ("HouseholdInvitation".to_owned(), row.id),
                Node::HouseholdInvitation(mapped),
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
