use async_graphql::{Context, Error, ID, Result, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use policy_application::pool_queries::{
    AllocationRule, AllocationRuleTarget, AllocationRulesQuery, PoolRulesQuery,
    PoolRulesQueryFailure,
};
use std::sync::Arc;
use uuid::Uuid;

use crate::{pagination::PageInfo, pool_mutations::AllocationRuleObject};

#[derive(Clone, Debug, SimpleObject)]
pub struct AllocationRuleEdge {
    cursor: String,
    node: AllocationRuleObject,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct AllocationRuleConnection {
    edges: Vec<AllocationRuleEdge>,
    page_info: PageInfo,
    total_count: i32,
}

pub async fn graphql_allocation_rules(
    ctx: &Context<'_>,
    household_id: ID,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<AllocationRuleConnection> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let after_id = decode_cursor(after.as_deref())?;
    let before_id = decode_cursor(before.as_deref())?;
    if first.is_some() && last.is_some() {
        return Err(Error::new("first and last cannot be used together"));
    }
    let backward = last.is_some();
    let page_size = last.or(first).unwrap_or(50);
    if !(1..=100).contains(&page_size) {
        return Err(Error::new("page size must be between 1 and 100"));
    }
    let page = ctx
        .data::<Arc<dyn AllocationRulesQuery>>()?
        .query(PoolRulesQuery {
            actor_id: actor.0,
            household_id,
            active: None,
            after_id,
            before_id,
            page_size: page_size as usize,
            backward,
        })
        .await
        .map_err(map_query_error)?;
    let edges = page
        .rows
        .into_iter()
        .map(|projection| {
            let cursor = encode_cursor(projection.id);
            let node = map_allocation_rule(projection)?;
            Ok(AllocationRuleEdge { cursor, node })
        })
        .collect::<Result<Vec<_>>>()?;
    let total_count = i32::try_from(page.total_count).map_err(crate::internal_error)?;
    Ok(AllocationRuleConnection {
        page_info: PageInfo {
            has_next_page: page.has_next_page,
            has_previous_page: page.has_previous_page,
            start_cursor: edges.first().map(|edge| edge.cursor.clone()),
            end_cursor: edges.last().map(|edge| edge.cursor.clone()),
        },
        edges,
        total_count,
    })
}

fn decode_cursor(cursor: Option<&str>) -> Result<Option<Uuid>> {
    let Some(cursor) = cursor.filter(|cursor| !cursor.is_empty()) else {
        return Ok(None);
    };
    let decoded = URL_SAFE_NO_PAD
        .decode(cursor)
        .map_err(|_| Error::new("invalid cursor"))?;
    let value = std::str::from_utf8(&decoded).map_err(|_| Error::new("invalid cursor"))?;
    let id = value
        .strip_prefix("AllocationRule:")
        .ok_or_else(|| Error::new("invalid cursor"))?;
    Uuid::parse_str(id)
        .map(Some)
        .map_err(|_| Error::new("invalid cursor"))
}

fn encode_cursor(id: Uuid) -> String {
    URL_SAFE_NO_PAD.encode(format!("AllocationRule:{id}"))
}

fn map_query_error(error: PoolRulesQueryFailure) -> Error {
    match error {
        PoolRulesQueryFailure::NotFound => Error::new("not found"),
        PoolRulesQueryFailure::AccessDenied => Error::new("household access denied"),
        PoolRulesQueryFailure::Infrastructure(error) => crate::internal_error(error),
    }
}

fn map_allocation_rule(value: AllocationRule) -> Result<AllocationRuleObject> {
    let target = crate::catalog_mutations::allocation_rule_target_from_projection(match value.target {
        AllocationRuleTarget::Category {
            id,
            household_id,
            parent_id,
            name,
            archived,
        } => cross_context_postgres::pool_mutations::AllocationRuleTargetProjection::Category {
            id,
            household_id,
            parent_id,
            name,
            archived,
        },
        AllocationRuleTarget::Merchant { name } => {
            cross_context_postgres::pool_mutations::AllocationRuleTargetProjection::Merchant { name }
        }
        AllocationRuleTarget::ProductConcept(value) => {
            cross_context_postgres::pool_mutations::AllocationRuleTargetProjection::ProductConcept(
                cross_context_postgres::pool_mutations::ProductConceptProjection {
                    id: value.id,
                    household_id: value.household_id,
                    name: value.name,
                    normalized_name: value.normalized_name,
                    category_id: value.category_id,
                    archived: value.archived,
                    revision: value.revision,
                    variants: value
                        .variants
                        .into_iter()
                        .map(map_product_variant)
                        .collect(),
                },
            )
        }
        AllocationRuleTarget::ProductVariant(value) => {
            cross_context_postgres::pool_mutations::AllocationRuleTargetProjection::ProductVariant(
                map_product_variant(value),
            )
        }
    })
    .map_err(crate::internal_error)?;
    Ok(AllocationRuleObject {
        id: crate::catalog_mutations::encode_node_id("AllocationRule", &value.id.to_string())
            .map_err(crate::internal_error)?,
        household_id: crate::catalog_mutations::encode_node_id(
            "Household",
            &value.household_id.to_string(),
        )
        .map_err(crate::internal_error)?,
        target,
        revision: i32::try_from(value.revision).map_err(crate::internal_error)?,
        allocations: value
            .allocations
            .into_iter()
            .map(|allocation| {
                Ok(crate::pool_mutations::AllocationObject {
                    pool_id: crate::catalog_mutations::encode_node_id(
                        "Pool",
                        &allocation.pool_id.to_string(),
                    )
                    .map_err(crate::internal_error)?,
                    pool_name: allocation.pool_name,
                    weight: crate::Long(allocation.weight),
                })
            })
            .collect::<Result<Vec<_>>>()?,
    })
}

fn map_product_variant(
    value: policy_application::pool_queries::ProductVariant,
) -> cross_context_postgres::pool_mutations::ProductVariantProjection {
    cross_context_postgres::pool_mutations::ProductVariantProjection {
        id: value.id,
        household_id: value.household_id,
        concept_id: value.concept_id,
        name: value.name,
        normalized_name: value.normalized_name,
        brand: value.brand,
        package_size: value.package_size,
        barcode: value.barcode,
        archived: value.archived,
        revision: value.revision,
        aliases: value
            .aliases
            .into_iter()
            .map(
                |alias| cross_context_postgres::pool_mutations::ProductAliasProjection {
                    id: alias.id,
                    household_id: alias.household_id,
                    variant_id: alias.variant_id,
                    merchant_key: alias.merchant_key,
                    alias: alias.alias,
                    normalized_alias: alias.normalized_alias,
                    revision: alias.revision,
                },
            )
            .collect(),
    }
}
