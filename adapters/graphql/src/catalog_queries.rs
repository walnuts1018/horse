use async_graphql::{Context, Error, ID, Result, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use catalog_application::tag_query::{TagCursor, TagQuery, TagQueryError, TagQueryInput};
use catalog_postgres::catalog_query as postgres_query;
use horse_postgres_common::PgPool;
use uuid::Uuid;

use crate::{Actor, catalog_mutations, pagination::PageInfo};

#[derive(Clone, Debug, SimpleObject)]
pub struct TagEdge {
    cursor: String,
    node: catalog_mutations::Tag,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct TagConnection {
    edges: Vec<TagEdge>,
    page_info: PageInfo,
    total_count: i32,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ProductConceptEdge {
    cursor: String,
    node: catalog_mutations::ProductConcept,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ProductConceptConnection {
    edges: Vec<ProductConceptEdge>,
    page_info: PageInfo,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ProductSuggestion {
    concept: catalog_mutations::ProductConcept,
    variant: catalog_mutations::ProductVariant,
    matched_name: String,
    score: i32,
}

pub async fn graphql_tags(
    ctx: &Context<'_>,
    household_id: ID,
    include_archived: bool,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<TagConnection> {
    let actor = actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let after_cursor = decode_cursor(after.as_deref(), "Tag")?.map(|cursor| TagCursor {
        sort_key: cursor.sort_key,
        id: cursor.id,
    });
    let before_cursor = decode_cursor(before.as_deref(), "Tag")?.map(|cursor| TagCursor {
        sort_key: cursor.sort_key,
        id: cursor.id,
    });
    let page = ctx
        .data::<std::sync::Arc<dyn TagQuery>>()?
        .query(
            actor.0,
            household_id,
            TagQueryInput {
                include_archived,
                after: after_cursor,
                before: before_cursor,
                first,
                last,
            },
        )
        .await
        .map_err(map_tag_query_error)?;
    let edges = page
        .rows
        .into_iter()
        .map(|row| {
            let cursor = encode_cursor("Tag", &row.name, row.id);
            let node = catalog_mutations::map_tag(catalog_domain::tag::Tag {
                id: row.id.to_string(),
                household_id: row.household_id.to_string(),
                name: row.name,
                archived: row.archived,
                revision: row.revision,
            })?;
            Ok(TagEdge { cursor, node })
        })
        .collect::<Result<Vec<_>>>()?;
    let total_count = i32::try_from(page.total_count)
        .map_err(|_| crate::internal_error("tag count exceeds GraphQL Int"))?;
    Ok(TagConnection {
        page_info: page_info(&edges, page.has_next_page, page.has_previous_page),
        edges,
        total_count,
    })
}

pub async fn graphql_product_catalog(
    ctx: &Context<'_>,
    household_id: ID,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<ProductConceptConnection> {
    let actor = actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let after_cursor = decode_cursor(after.as_deref(), "ProductConcept")?.map(|cursor| {
        postgres_query::CatalogCursor {
            sort_key: cursor.sort_key,
            id: cursor.id,
        }
    });
    let before_cursor = decode_cursor(before.as_deref(), "ProductConcept")?.map(|cursor| {
        postgres_query::CatalogCursor {
            sort_key: cursor.sort_key,
            id: cursor.id,
        }
    });
    let page = postgres_query::product_catalog(
        ctx.data::<PgPool>()?,
        actor.0,
        household_id,
        after_cursor,
        before_cursor,
        first,
        last,
    )
    .await
    .map_err(map_query_error)?;
    let edges = page
        .rows
        .into_iter()
        .map(|row| {
            let cursor = encode_cursor("ProductConcept", &row.normalized_name, row.id);
            let node = map_concept(row)?;
            Ok(ProductConceptEdge { cursor, node })
        })
        .collect::<Result<Vec<_>>>()?;
    Ok(ProductConceptConnection {
        page_info: page_info(&edges, page.has_next_page, page.has_previous_page),
        edges,
    })
}

pub async fn graphql_product_suggestions(
    ctx: &Context<'_>,
    household_id: ID,
    query: String,
    merchant: Option<String>,
    first: i32,
) -> Result<Vec<ProductSuggestion>> {
    let actor = actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let rows = postgres_query::product_suggestions(
        ctx.data::<PgPool>()?,
        actor.0,
        household_id,
        &query,
        merchant.as_deref(),
        first,
    )
    .await
    .map_err(map_query_error)?;
    rows.into_iter()
        .map(|row| {
            Ok(ProductSuggestion {
                concept: map_concept(row.concept)?,
                variant: map_variant(row.variant)?,
                matched_name: row.matched_name,
                score: row.score,
            })
        })
        .collect()
}

fn map_concept(
    row: postgres_query::ProductConceptProjection,
) -> Result<catalog_mutations::ProductConcept> {
    Ok(catalog_mutations::ProductConcept {
        id: node_id("ProductConcept", row.id)?,
        household_id: node_id("Household", row.household_id)?,
        name: row.name,
        normalized_name: row.normalized_name,
        category_id: row
            .category_id
            .map(|id| node_id("Category", id))
            .transpose()?,
        archived: row.archived,
        revision: revision(row.revision, "product concept")?,
        variants: row
            .variants
            .into_iter()
            .map(map_variant)
            .collect::<Result<Vec<_>>>()?,
    })
}

fn map_variant(
    row: postgres_query::ProductVariantProjection,
) -> Result<catalog_mutations::ProductVariant> {
    Ok(catalog_mutations::ProductVariant {
        id: node_id("ProductVariant", row.id)?,
        household_id: node_id("Household", row.household_id)?,
        concept_id: node_id("ProductConcept", row.concept_id)?,
        name: row.name,
        normalized_name: row.normalized_name,
        brand: nonempty(row.brand),
        package_size: nonempty(row.package_size),
        barcode: row.barcode,
        archived: row.archived,
        revision: revision(row.revision, "product variant")?,
        aliases: row
            .aliases
            .into_iter()
            .map(map_alias)
            .collect::<Result<Vec<_>>>()?,
    })
}

fn map_alias(
    row: postgres_query::ProductAliasProjection,
) -> Result<catalog_mutations::ProductAlias> {
    Ok(catalog_mutations::ProductAlias {
        id: node_id("ProductAlias", row.id)?,
        household_id: node_id("Household", row.household_id)?,
        variant_id: node_id("ProductVariant", row.variant_id)?,
        merchant_key: row.merchant_key,
        alias: row.alias,
        normalized_alias: row.normalized_alias,
        revision: revision(row.revision, "product alias")?,
    })
}

fn actor(ctx: &Context<'_>) -> Result<Actor> {
    ctx.data::<Actor>()
        .copied()
        .map_err(|_| Error::new("authentication required"))
}

fn page_info<E>(edges: &[E], has_next_page: bool, has_previous_page: bool) -> PageInfo
where
    E: EdgeCursor,
{
    PageInfo {
        has_next_page,
        has_previous_page,
        start_cursor: edges.first().map(EdgeCursor::cursor),
        end_cursor: edges.last().map(EdgeCursor::cursor),
    }
}

trait EdgeCursor {
    fn cursor(&self) -> String;
}

impl EdgeCursor for TagEdge {
    fn cursor(&self) -> String {
        self.cursor.clone()
    }
}

impl EdgeCursor for ProductConceptEdge {
    fn cursor(&self) -> String {
        self.cursor.clone()
    }
}

struct DecodedCursor {
    sort_key: String,
    id: Uuid,
}

fn decode_cursor(value: Option<&str>, kind: &str) -> Result<Option<DecodedCursor>> {
    let Some(value) = value.filter(|value| !value.is_empty()) else {
        return Ok(None);
    };
    let bytes = URL_SAFE_NO_PAD
        .decode(value)
        .map_err(|_| Error::new("invalid cursor"))?;
    let value = std::str::from_utf8(&bytes).map_err(|_| Error::new("invalid cursor"))?;
    let payload = value
        .strip_prefix(&format!("{kind}:"))
        .ok_or_else(|| Error::new("invalid cursor"))?;
    let (sort_key, id) = payload
        .split_once('\0')
        .ok_or_else(|| Error::new("invalid cursor"))?;
    if sort_key.is_empty() {
        return Err(Error::new("invalid cursor"));
    }
    let id = Uuid::parse_str(id).map_err(|_| Error::new("invalid cursor"))?;
    if id.is_nil() {
        return Err(Error::new("invalid cursor"));
    }
    Ok(Some(DecodedCursor {
        sort_key: sort_key.to_owned(),
        id,
    }))
}

fn encode_cursor(kind: &str, sort_key: &str, id: Uuid) -> String {
    URL_SAFE_NO_PAD.encode(format!("{kind}:{sort_key}\0{id}"))
}

fn node_id(kind: &str, id: Uuid) -> Result<ID> {
    catalog_mutations::encode_node_id(kind, &id.to_string())
        .map_err(|_| crate::internal_error("failed to encode catalog node ID"))
}

fn revision(value: i64, field: &str) -> Result<i32> {
    i32::try_from(value)
        .map_err(|_| crate::internal_error(format!("{field} revision exceeds GraphQL Int")))
}

fn nonempty(value: String) -> Option<String> {
    (!value.is_empty()).then_some(value)
}

fn map_query_error(error: postgres_query::CatalogQueryError) -> Error {
    use postgres_query::CatalogQueryError as QueryError;
    match error {
        QueryError::HouseholdNotFound => Error::new("household not found"),
        QueryError::InvalidPagination => Error::new("invalid pagination"),
        QueryError::InvalidCursor => Error::new("invalid cursor"),
        error @ (QueryError::Pool(_)
        | QueryError::Database(_)
        | QueryError::InvalidPersistedData) => crate::internal_error(error),
    }
}

fn map_tag_query_error(error: TagQueryError) -> Error {
    match error {
        TagQueryError::HouseholdNotFound => Error::new("household not found"),
        TagQueryError::InvalidPagination => Error::new("invalid pagination"),
        TagQueryError::InvalidCursor => Error::new("invalid cursor"),
        TagQueryError::Infrastructure(error) => crate::internal_error(error),
    }
}
