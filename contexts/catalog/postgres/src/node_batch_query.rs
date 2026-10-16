use std::collections::HashMap;

use catalog_application::{
    node_batch_query::{
        CatalogNodeBatch, CatalogNodeBatchError, CatalogNodeBatchInput, CatalogNodeBatchQuery,
    },
    product_query::{ProductAliasProjection, ProductConceptProjection, ProductVariantProjection},
    tag_query::TagProjection,
};
use diesel::{BoolExpressionMethods, ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

pub struct PostgresCatalogNodeBatchQuery {
    pool: PgPool,
}

impl PostgresCatalogNodeBatchQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl CatalogNodeBatchQuery for PostgresCatalogNodeBatchQuery {
    fn query<'a>(
        &'a self,
        input: CatalogNodeBatchInput,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<CatalogNodeBatch, CatalogNodeBatchError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move { load(&self.pool, input).await })
    }
}

async fn load(
    pool: &PgPool,
    input: CatalogNodeBatchInput,
) -> Result<CatalogNodeBatch, CatalogNodeBatchError> {
    use public::{
        product_aliases::dsl as aliases, product_concepts::dsl as concepts,
        product_variants::dsl as variants, tags::dsl as tags,
    };

    let mut conn = pool
        .get()
        .await
        .map_err(|error| CatalogNodeBatchError::Infrastructure(error.to_string()))?;
    let CatalogNodeBatchInput {
        household_ids,
        tag_ids,
        concept_ids,
        variant_ids,
        concept_variant_ids,
        alias_ids,
    } = input;

    let tags = if tag_ids.is_empty() {
        Vec::new()
    } else {
        tags::tags
            .filter(tags::household_id.eq_any(&household_ids))
            .filter(tags::id.eq_any(&tag_ids))
            .select((
                tags::id,
                tags::household_id,
                tags::name,
                tags::archived,
                tags::revision,
            ))
            .load::<(Uuid, Uuid, String, bool, i64)>(&mut conn)
            .await
            .map_err(infrastructure)?
            .into_iter()
            .map(
                |(id, household_id, name, archived, revision)| TagProjection {
                    id,
                    household_id,
                    name,
                    archived,
                    revision,
                },
            )
            .collect()
    };

    let concept_rows = if concept_ids.is_empty() {
        Vec::new()
    } else {
        concepts::product_concepts
            .filter(concepts::household_id.eq_any(&household_ids))
            .filter(concepts::id.eq_any(&concept_ids))
            .select((
                concepts::id,
                concepts::household_id,
                concepts::name,
                concepts::normalized_name,
                concepts::category_id,
                concepts::archived,
                concepts::revision,
            ))
            .load::<(Uuid, Uuid, String, String, Option<Uuid>, bool, i64)>(&mut conn)
            .await
            .map_err(infrastructure)?
    };
    let mut variant_filter_ids = concept_variant_ids;
    variant_filter_ids.extend(concept_rows.iter().map(|row| row.0));
    let variant_rows = if variant_filter_ids.is_empty() {
        Vec::new()
    } else {
        variants::product_variants
            .filter(variants::household_id.eq_any(&household_ids))
            .filter(
                variants::id
                    .eq_any(&variant_ids)
                    .or(variants::concept_id.eq_any(&variant_filter_ids)),
            )
            .order_by(variants::normalized_name)
            .then_order_by(variants::id)
            .select((
                variants::id,
                variants::household_id,
                variants::concept_id,
                variants::name,
                variants::normalized_name,
                variants::brand,
                variants::package_size,
                variants::barcode,
                variants::archived,
                variants::revision,
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
            )>(&mut conn)
            .await
            .map_err(infrastructure)?
    };

    let variant_row_ids = variant_rows.iter().map(|row| row.0).collect::<Vec<_>>();
    let alias_rows = if alias_ids.is_empty() && variant_row_ids.is_empty() {
        Vec::new()
    } else {
        aliases::product_aliases
            .filter(aliases::household_id.eq_any(&household_ids))
            .filter(
                aliases::id
                    .eq_any(&alias_ids)
                    .or(aliases::variant_id.eq_any(&variant_row_ids)),
            )
            .order_by(aliases::merchant_key)
            .then_order_by(aliases::normalized_alias)
            .select((
                aliases::id,
                aliases::household_id,
                aliases::variant_id,
                aliases::merchant_key,
                aliases::alias_text,
                aliases::normalized_alias,
                aliases::revision,
            ))
            .load::<(Uuid, Uuid, Uuid, String, String, String, i64)>(&mut conn)
            .await
            .map_err(infrastructure)?
    };
    let mut aliases_by_variant = HashMap::<Uuid, Vec<ProductAliasProjection>>::new();
    for (id, household_id, variant_id, merchant_key, alias, normalized_alias, revision) in
        alias_rows
    {
        aliases_by_variant
            .entry(variant_id)
            .or_default()
            .push(ProductAliasProjection {
                id,
                household_id,
                variant_id,
                merchant_key,
                alias,
                normalized_alias,
                revision,
            });
    }

    let mut variants_by_concept = HashMap::<Uuid, Vec<ProductVariantProjection>>::new();
    let mut variants_by_id = HashMap::<Uuid, ProductVariantProjection>::new();
    for (
        id,
        household_id,
        concept_id,
        name,
        normalized_name,
        brand,
        package_size,
        barcode,
        archived,
        revision,
    ) in variant_rows
    {
        let projection = ProductVariantProjection {
            id,
            household_id,
            concept_id,
            name,
            normalized_name,
            brand,
            package_size,
            barcode,
            archived,
            revision,
            aliases: aliases_by_variant.remove(&id).unwrap_or_default(),
        };
        variants_by_concept
            .entry(concept_id)
            .or_default()
            .push(projection.clone());
        variants_by_id.insert(id, projection);
    }
    let concepts = concept_rows
        .into_iter()
        .map(
            |(id, household_id, name, normalized_name, category_id, archived, revision)| {
                ProductConceptProjection {
                    id,
                    household_id,
                    name,
                    normalized_name,
                    category_id,
                    archived,
                    revision,
                    variants: variants_by_concept.remove(&id).unwrap_or_default(),
                }
            },
        )
        .collect();
    let variants = variant_ids
        .into_iter()
        .filter_map(|id| variants_by_id.remove(&id))
        .collect();

    let aliases = if alias_ids.is_empty() {
        Vec::new()
    } else {
        aliases::product_aliases
            .filter(aliases::household_id.eq_any(&household_ids))
            .filter(aliases::id.eq_any(&alias_ids))
            .select((
                aliases::id,
                aliases::household_id,
                aliases::variant_id,
                aliases::merchant_key,
                aliases::alias_text,
                aliases::normalized_alias,
                aliases::revision,
            ))
            .load::<(Uuid, Uuid, Uuid, String, String, String, i64)>(&mut conn)
            .await
            .map_err(infrastructure)?
            .into_iter()
            .map(
                |(
                    id,
                    household_id,
                    variant_id,
                    merchant_key,
                    alias,
                    normalized_alias,
                    revision,
                )| {
                    ProductAliasProjection {
                        id,
                        household_id,
                        variant_id,
                        merchant_key,
                        alias,
                        normalized_alias,
                        revision,
                    }
                },
            )
            .collect()
    };

    Ok(CatalogNodeBatch {
        tags,
        concepts,
        variants,
        aliases,
    })
}

fn infrastructure(error: diesel::result::Error) -> CatalogNodeBatchError {
    CatalogNodeBatchError::Infrastructure(format!("{error:?}"))
}
