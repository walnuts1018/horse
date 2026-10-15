use chrono::{DateTime, Utc};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, OptionalExtension, PgTextExpressionMethods, QueryDsl,
    result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use std::collections::{HashMap, HashSet};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CatalogCursor {
    pub sort_key: String,
    pub id: Uuid,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TagProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub name: String,
    pub archived: bool,
    pub revision: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TagPage {
    pub rows: Vec<TagProjection>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductAliasProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub variant_id: Uuid,
    pub merchant_key: String,
    pub alias: String,
    pub normalized_alias: String,
    pub revision: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductVariantProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub concept_id: Uuid,
    pub name: String,
    pub normalized_name: String,
    pub brand: String,
    pub package_size: String,
    pub barcode: Option<String>,
    pub archived: bool,
    pub revision: i64,
    pub aliases: Vec<ProductAliasProjection>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductConceptProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub name: String,
    pub normalized_name: String,
    pub category_id: Option<Uuid>,
    pub archived: bool,
    pub revision: i64,
    pub variants: Vec<ProductVariantProjection>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductCatalogPage {
    pub rows: Vec<ProductConceptProjection>,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProductSuggestionProjection {
    pub concept: ProductConceptProjection,
    pub variant: ProductVariantProjection,
    pub matched_name: String,
    pub score: i32,
}

#[derive(Debug)]
pub enum CatalogQueryError {
    Pool(String),
    Database(DieselError),
    HouseholdNotFound,
    InvalidPagination,
    InvalidCursor,
    InvalidPersistedData,
}

type VariantRow = (
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
);
type ConceptRow = (Uuid, Uuid, String, String, Option<Uuid>, bool, i64);

#[derive(Debug)]
struct VariantRecord {
    id: Uuid,
    household_id: Uuid,
    concept_id: Uuid,
    name: String,
    normalized_name: String,
    brand: String,
    package_size: String,
    barcode: Option<String>,
    archived: bool,
    revision: i64,
}

impl From<VariantRow> for VariantRecord {
    fn from(row: VariantRow) -> Self {
        Self {
            id: row.0,
            household_id: row.1,
            concept_id: row.2,
            name: row.3,
            normalized_name: row.4,
            brand: row.5,
            package_size: row.6,
            barcode: row.7,
            archived: row.8,
            revision: row.9,
        }
    }
}

#[derive(Debug)]
struct ConceptRecord {
    id: Uuid,
    household_id: Uuid,
    name: String,
    normalized_name: String,
    category_id: Option<Uuid>,
    archived: bool,
    revision: i64,
}

impl From<ConceptRow> for ConceptRecord {
    fn from(row: ConceptRow) -> Self {
        Self {
            id: row.0,
            household_id: row.1,
            name: row.2,
            normalized_name: row.3,
            category_id: row.4,
            archived: row.5,
            revision: row.6,
        }
    }
}

#[derive(Debug)]
struct AliasRecord {
    id: Uuid,
    variant_id: Uuid,
    alias: String,
    normalized_alias: String,
    revision: i64,
}

#[derive(Debug, Clone, Copy, Default)]
struct PurchaseStats {
    member_purchase_count: i64,
    member_last_purchased_at: i64,
    same_merchant: bool,
    purchase_count: i64,
    last_purchased_at: i64,
}

#[derive(Debug)]
struct RankedSuggestion {
    concept_id: Uuid,
    variant_id: Uuid,
    matched_name: String,
    score: i32,
    member_purchase_count: i64,
    member_last_purchased_at: i64,
    same_merchant: bool,
    purchase_count: i64,
    last_purchased_at: i64,
    concept_name: String,
    variant_name: String,
}

async fn query_variant_matches(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    pattern: &str,
    exclude_pattern: Option<&str>,
) -> Result<Vec<VariantRecord>, CatalogQueryError> {
    use public::product_variants as v;
    let mut query = v::table
        .filter(v::household_id.eq(household_id))
        .filter(v::archived.eq(false))
        .filter(v::normalized_name.like(pattern))
        .into_boxed();
    if let Some(exclude) = exclude_pattern {
        query = query.filter(diesel::dsl::not(v::normalized_name.like(exclude)));
    }
    let rows = query
        .order_by(v::normalized_name.asc())
        .then_order_by(v::id.asc())
        .select((
            v::id,
            v::household_id,
            v::concept_id,
            v::name,
            v::normalized_name,
            v::brand,
            v::package_size,
            v::barcode,
            v::archived,
            v::revision,
        ))
        .load::<VariantRow>(&mut *conn)
        .await?;
    Ok(rows.into_iter().map(VariantRecord::from).collect())
}

async fn query_concept_matches(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    pattern: &str,
    exclude_pattern: Option<&str>,
) -> Result<Vec<ConceptRecord>, CatalogQueryError> {
    use public::product_concepts as c;
    let mut query = c::table
        .filter(c::household_id.eq(household_id))
        .filter(c::archived.eq(false))
        .filter(c::normalized_name.like(pattern))
        .into_boxed();
    if let Some(exclude) = exclude_pattern {
        query = query.filter(diesel::dsl::not(c::normalized_name.like(exclude)));
    }
    let rows = query
        .order_by(c::normalized_name.asc())
        .then_order_by(c::id.asc())
        .select((
            c::id,
            c::household_id,
            c::name,
            c::normalized_name,
            c::category_id,
            c::archived,
            c::revision,
        ))
        .load::<ConceptRow>(&mut *conn)
        .await?;
    Ok(rows.into_iter().map(ConceptRecord::from).collect())
}

async fn query_alias_matches(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    merchant_key: &str,
    pattern: &str,
    exclude_pattern: Option<&str>,
) -> Result<Vec<AliasRecord>, CatalogQueryError> {
    use public::product_aliases as a;
    let mut query = a::table
        .filter(a::household_id.eq(household_id))
        .filter(a::merchant_key.eq(merchant_key))
        .filter(a::normalized_alias.like(pattern))
        .into_boxed();
    if let Some(exclude) = exclude_pattern {
        query = query.filter(diesel::dsl::not(a::normalized_alias.like(exclude)));
    }
    query
        .order_by(a::normalized_alias.asc())
        .then_order_by(a::id.asc())
        .select((
            a::id,
            a::variant_id,
            a::alias_text,
            a::normalized_alias,
            a::revision,
        ))
        .load::<(Uuid, Uuid, String, String, i64)>(&mut *conn)
        .await?
        .into_iter()
        .map(
            |(id, variant_id, alias, normalized_alias, revision)| AliasRecord {
                id,
                variant_id,
                alias,
                normalized_alias,
                revision,
            },
        )
        .collect::<Vec<_>>();
    Ok(rows)
}

async fn load_purchase_stats(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
    variant_ids: &[Uuid],
    merchant_key: &str,
) -> Result<HashMap<Uuid, PurchaseStats>, CatalogQueryError> {
    use public::{purchase_items as items, purchases};
    let mut variants_by_purchase = HashMap::<Uuid, HashSet<Uuid>>::new();
    let mut all_purchase_ids = HashSet::new();
    for ids in variant_ids.chunks(500) {
        if ids.is_empty() {
            continue;
        }
        let nullable_ids = ids.iter().copied().map(Some).collect::<Vec<_>>();
        let rows = items::table
            .filter(items::household_id.eq(household_id))
            .filter(items::product_variant_id.eq_any(&nullable_ids))
            .select((items::product_variant_id, items::purchase_id))
            .load::<(Option<Uuid>, Uuid)>(&mut *conn)
            .await?;
        for (variant_id, purchase_id) in rows {
            if let Some(variant_id) = variant_id {
                variants_by_purchase
                    .entry(purchase_id)
                    .or_default()
                    .insert(variant_id);
                all_purchase_ids.insert(purchase_id);
            }
        }
    }
    let mut stats = HashMap::<Uuid, PurchaseStats>::new();
    let purchase_ids = all_purchase_ids.into_iter().collect::<Vec<_>>();
    for ids in purchase_ids.chunks(500) {
        if ids.is_empty() {
            continue;
        }
        let rows = purchases::table
            .filter(purchases::household_id.eq(household_id))
            .filter(purchases::id.eq_any(ids))
            .filter(purchases::status.eq_any(["POSTED", "CLOSED"]))
            .select((
                purchases::id,
                purchases::created_by,
                purchases::occurred_at,
                purchases::merchant,
            ))
            .load::<(Uuid, Uuid, DateTime<Utc>, String)>(&mut *conn)
            .await?;
        for (purchase_id, created_by, occurred_at, merchant) in rows {
            let timestamp = occurred_at.timestamp_nanos_opt().unwrap_or_default();
            for variant_id in variants_by_purchase.get(&purchase_id).into_iter().flatten() {
                let stat = stats.entry(*variant_id).or_default();
                stat.purchase_count += 1;
                stat.last_purchased_at = stat.last_purchased_at.max(timestamp);
                if created_by == actor_id {
                    stat.member_purchase_count += 1;
                    stat.member_last_purchased_at = stat.member_last_purchased_at.max(timestamp);
                }
                if !merchant_key.is_empty() && normalize_text(&merchant) == merchant_key {
                    stat.same_merchant = true;
                }
            }
        }
    }
    Ok(stats)
}

async fn load_product_concepts(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    concept_ids: &[Uuid],
) -> Result<Vec<ProductConceptProjection>, CatalogQueryError> {
    use public::product_concepts as c;
    let concept_rows = c::table
        .filter(c::household_id.eq(household_id))
        .filter(c::id.eq_any(concept_ids))
        .order_by(c::normalized_name.asc())
        .then_order_by(c::id.asc())
        .select((
            c::id,
            c::household_id,
            c::name,
            c::normalized_name,
            c::category_id,
            c::archived,
            c::revision,
        ))
        .load::<ConceptRow>(&mut *conn)
        .await?;
    let mut variants_by_concept = HashMap::<Uuid, Vec<ProductVariantProjection>>::new();
    let mut variant_ids = Vec::new();
    for ids in concept_ids.chunks(500) {
        use public::product_variants as v;
        let variant_rows = v::table
            .filter(v::household_id.eq(household_id))
            .filter(v::concept_id.eq_any(ids))
            .order_by(v::concept_id.asc())
            .then_order_by(v::normalized_name.asc())
            .then_order_by(v::id.asc())
            .select((
                v::id,
                v::household_id,
                v::concept_id,
                v::name,
                v::normalized_name,
                v::brand,
                v::package_size,
                v::barcode,
                v::archived,
                v::revision,
            ))
            .load::<VariantRow>(&mut *conn)
            .await?;
        variant_ids.extend(variant_rows.iter().map(|row| row.0));
        for row in variant_rows {
            let row = VariantRecord::from(row);
            variants_by_concept
                .entry(row.concept_id)
                .or_default()
                .push(ProductVariantProjection {
                    id: row.id,
                    household_id: row.household_id,
                    concept_id: row.concept_id,
                    name: row.name,
                    normalized_name: row.normalized_name,
                    brand: row.brand,
                    package_size: row.package_size,
                    barcode: row.barcode,
                    archived: row.archived,
                    revision: row.revision,
                    aliases: Vec::new(),
                });
        }
    }
    let mut aliases_by_variant = HashMap::<Uuid, Vec<ProductAliasProjection>>::new();
    for ids in variant_ids.chunks(500) {
        if ids.is_empty() {
            continue;
        }
        use public::product_aliases as a;
        let alias_rows = a::table
            .filter(a::household_id.eq(household_id))
            .filter(a::variant_id.eq_any(ids))
            .order_by(a::variant_id.asc())
            .then_order_by(a::merchant_key.asc())
            .then_order_by(a::normalized_alias.asc())
            .select((
                a::id,
                a::household_id,
                a::variant_id,
                a::merchant_key,
                a::alias_text,
                a::normalized_alias,
                a::revision,
            ))
            .load::<(Uuid, Uuid, Uuid, String, String, String, i64)>(&mut *conn)
            .await?;
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
    }
    Ok(concept_rows
        .into_iter()
        .map(ConceptRecord::from)
        .map(|concept| ProductConceptProjection {
            id: concept.id,
            household_id: concept.household_id,
            name: concept.name,
            normalized_name: concept.normalized_name,
            category_id: concept.category_id,
            archived: concept.archived,
            revision: concept.revision,
            variants: variants_by_concept
                .remove(&concept.id)
                .unwrap_or_default()
                .into_iter()
                .map(|mut variant| {
                    variant.aliases = aliases_by_variant.remove(&variant.id).unwrap_or_default();
                    variant
                })
                .collect(),
        })
        .collect())
}

fn preferred_alias(candidate: &AliasRecord, current: &AliasRecord, search_key: &str) -> bool {
    let candidate_exact = candidate.normalized_alias == search_key;
    let current_exact = current.normalized_alias == search_key;
    if candidate_exact != current_exact {
        return candidate_exact;
    }
    candidate.revision > current.revision
        || candidate.revision == current.revision
            && candidate.id.to_string() < current.id.to_string()
}

fn suggestion_score(
    variant: &VariantRecord,
    concept: &ConceptRecord,
    alias: Option<&AliasRecord>,
    search_key: &str,
) -> (String, i32) {
    let matched_name = alias.map_or_else(|| variant.name.clone(), |alias| alias.alias.clone());
    let score = if alias.is_some_and(|alias| alias.normalized_alias == search_key) {
        100
    } else if variant.normalized_name == search_key {
        95
    } else if concept.normalized_name == search_key {
        90
    } else if alias.is_some_and(|alias| alias.normalized_alias.starts_with(search_key)) {
        80
    } else if variant.normalized_name.starts_with(search_key) {
        75
    } else if concept.normalized_name.starts_with(search_key) {
        70
    } else {
        50
    };
    (matched_name, score)
}

fn compare_suggestions(a: &RankedSuggestion, b: &RankedSuggestion) -> std::cmp::Ordering {
    b.score
        .cmp(&a.score)
        .then_with(|| b.member_purchase_count.cmp(&a.member_purchase_count))
        .then_with(|| b.member_last_purchased_at.cmp(&a.member_last_purchased_at))
        .then_with(|| b.same_merchant.cmp(&a.same_merchant))
        .then_with(|| b.purchase_count.cmp(&a.purchase_count))
        .then_with(|| b.last_purchased_at.cmp(&a.last_purchased_at))
        .then_with(|| a.concept_name.cmp(&b.concept_name))
        .then_with(|| a.variant_name.cmp(&b.variant_name))
        .then_with(|| a.variant_id.to_string().cmp(&b.variant_id.to_string()))
}

fn normalize_text(value: &str) -> String {
    value
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ")
        .to_lowercase()
}

fn escape_like(value: &str) -> String {
    value
        .replace('\\', "\\\\")
        .replace('%', "\\%")
        .replace('_', "\\_")
}

impl From<DieselError> for CatalogQueryError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn tags(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    include_archived: bool,
    after: Option<CatalogCursor>,
    before: Option<CatalogCursor>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<TagPage, CatalogQueryError> {
    let (size, backward) = page_size(first, last)?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| CatalogQueryError::Pool(error.to_string()))?;
    use public::tags as t;
    require_member(&mut conn, actor_id, household_id).await?;

    let has_after = after.is_some();
    let has_before = before.is_some();
    let mut base = t::table
        .filter(t::household_id.eq(household_id))
        .into_boxed();
    if !include_archived {
        base = base.filter(t::archived.eq(false));
    }
    let mut count_query = t::table
        .filter(t::household_id.eq(household_id))
        .into_boxed();
    if !include_archived {
        count_query = count_query.filter(t::archived.eq(false));
    }
    let total_count = count_query.count().get_result::<i64>(&mut *conn).await?;
    let after = validate_tag_cursor(&mut conn, household_id, include_archived, after).await?;
    let before = validate_tag_cursor(&mut conn, household_id, include_archived, before).await?;
    let mut query = base;
    if let Some(cursor) = after {
        query = query.filter(
            t::name
                .gt(&cursor.sort_key)
                .or(t::name.eq(&cursor.sort_key).and(t::id.gt(cursor.id))),
        );
    }
    if let Some(cursor) = before {
        query = query.filter(
            t::name
                .lt(&cursor.sort_key)
                .or(t::name.eq(&cursor.sort_key).and(t::id.lt(cursor.id))),
        );
    }
    let query = if backward {
        query
            .order_by(t::name.desc())
            .then_order_by(t::id.desc())
            .limit(size as i64 + 1)
    } else {
        query
            .order_by(t::name.asc())
            .then_order_by(t::id.asc())
            .limit(size as i64 + 1)
    };
    let mut rows = query
        .select((t::id, t::household_id, t::name, t::archived, t::revision))
        .load::<(Uuid, Uuid, String, bool, i64)>(&mut *conn)
        .await?;
    let has_extra = rows.len() > size;
    rows.truncate(size);
    if backward {
        rows.reverse();
    }
    Ok(TagPage {
        rows: rows
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
            .collect(),
        total_count,
        has_next_page: if backward { has_before } else { has_extra },
        has_previous_page: if backward { has_extra } else { has_after },
    })
}

pub async fn product_catalog(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    after: Option<CatalogCursor>,
    before: Option<CatalogCursor>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<ProductCatalogPage, CatalogQueryError> {
    let (size, backward) = page_size(first, last)?;
    let mut conn = pool
        .get()
        .await
        .map_err(|error| CatalogQueryError::Pool(error.to_string()))?;
    use public::product_concepts as c;
    require_member(&mut conn, actor_id, household_id).await?;
    validate_concept_cursor(&mut conn, household_id, after.as_ref()).await?;
    validate_concept_cursor(&mut conn, household_id, before.as_ref()).await?;

    let mut query = c::table
        .filter(c::household_id.eq(household_id))
        .into_boxed();
    if let Some(cursor) = &after {
        query = query.filter(
            c::normalized_name
                .gt(&cursor.sort_key)
                .or(c::normalized_name
                    .eq(&cursor.sort_key)
                    .and(c::id.gt(cursor.id))),
        );
    }
    if let Some(cursor) = &before {
        query = query.filter(
            c::normalized_name
                .lt(&cursor.sort_key)
                .or(c::normalized_name
                    .eq(&cursor.sort_key)
                    .and(c::id.lt(cursor.id))),
        );
    }
    let query = if backward {
        query
            .order_by(c::normalized_name.desc())
            .then_order_by(c::id.desc())
            .limit(size as i64 + 1)
    } else {
        query
            .order_by(c::normalized_name.asc())
            .then_order_by(c::id.asc())
            .limit(size as i64 + 1)
    };
    let mut rows = query
        .select((
            c::id,
            c::household_id,
            c::name,
            c::normalized_name,
            c::category_id,
            c::archived,
            c::revision,
        ))
        .load::<(Uuid, Uuid, String, String, Option<Uuid>, bool, i64)>(&mut *conn)
        .await?;
    let has_extra = rows.len() > size;
    rows.truncate(size);
    if backward {
        rows.reverse();
    }

    let concept_ids: Vec<Uuid> = rows.iter().map(|row| row.0).collect();
    let mut variants_by_concept: std::collections::HashMap<Uuid, Vec<ProductVariantProjection>> =
        std::collections::HashMap::new();
    if !concept_ids.is_empty() {
        use public::product_variants as v;
        let variant_rows = v::table
            .filter(v::household_id.eq(household_id))
            .filter(v::concept_id.eq_any(&concept_ids))
            .order_by(v::concept_id.asc())
            .then_order_by(v::normalized_name.asc())
            .then_order_by(v::id.asc())
            .select((
                v::id,
                v::household_id,
                v::concept_id,
                v::name,
                v::normalized_name,
                v::brand,
                v::package_size,
                v::barcode,
                v::archived,
                v::revision,
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
            )>(&mut *conn)
            .await?;
        let variant_ids: Vec<Uuid> = variant_rows.iter().map(|row| row.0).collect();
        let mut aliases_by_variant: std::collections::HashMap<Uuid, Vec<ProductAliasProjection>> =
            std::collections::HashMap::new();
        if !variant_ids.is_empty() {
            use public::product_aliases as a;
            let alias_rows = a::table
                .filter(a::household_id.eq(household_id))
                .filter(a::variant_id.eq_any(&variant_ids))
                .order_by(a::variant_id.asc())
                .then_order_by(a::merchant_key.asc())
                .then_order_by(a::normalized_alias.asc())
                .then_order_by(a::id.asc())
                .select((
                    a::id,
                    a::household_id,
                    a::variant_id,
                    a::merchant_key,
                    a::alias_text,
                    a::normalized_alias,
                    a::revision,
                ))
                .load::<(Uuid, Uuid, Uuid, String, String, String, i64)>(&mut *conn)
                .await?;
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
        }
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
            variants_by_concept
                .entry(concept_id)
                .or_default()
                .push(ProductVariantProjection {
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
                });
        }
    }
    let rows = rows
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
    Ok(ProductCatalogPage {
        rows,
        has_next_page: if backward {
            before.is_some()
        } else {
            has_extra
        },
        has_previous_page: if backward { has_extra } else { after.is_some() },
    })
}

pub async fn product_suggestions(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    query_text: &str,
    merchant: Option<&str>,
    first: i32,
) -> Result<Vec<ProductSuggestionProjection>, CatalogQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| CatalogQueryError::Pool(error.to_string()))?;
    require_member(&mut conn, actor_id, household_id).await?;
    let search_key = normalize_text(query_text);
    if search_key.is_empty() || search_key.len() > 240 {
        return Ok(Vec::new());
    }
    if !(1..=20).contains(&first) {
        return Err(CatalogQueryError::InvalidPagination);
    }
    let merchant_key = merchant.map(normalize_text).unwrap_or_default();
    let prefix_pattern = format!("{}%", escape_like(&search_key));
    let search_pattern = format!("%{}%", escape_like(&search_key));

    let mut variants = HashMap::<Uuid, VariantRecord>::new();
    let variant_rows =
        query_variant_matches(&mut conn, household_id, &prefix_pattern, None).await?;
    variants.extend(variant_rows.into_iter().map(|row| (row.id, row)));
    let variant_rows = query_variant_matches(
        &mut conn,
        household_id,
        &search_pattern,
        Some(&prefix_pattern),
    )
    .await?;
    variants.extend(variant_rows.into_iter().map(|row| (row.id, row)));

    let mut concepts = HashMap::<Uuid, ConceptRecord>::new();
    let concept_rows =
        query_concept_matches(&mut conn, household_id, &prefix_pattern, None).await?;
    concepts.extend(concept_rows.into_iter().map(|row| (row.id, row)));
    let concept_rows = query_concept_matches(
        &mut conn,
        household_id,
        &search_pattern,
        Some(&prefix_pattern),
    )
    .await?;
    concepts.extend(concept_rows.into_iter().map(|row| (row.id, row)));
    let matching_concept_ids = concepts.keys().copied().collect::<Vec<_>>();
    for ids in matching_concept_ids.chunks(500) {
        if ids.is_empty() {
            continue;
        }
        use public::product_variants as v;
        let rows = v::table
            .filter(v::household_id.eq(household_id))
            .filter(v::archived.eq(false))
            .filter(v::concept_id.eq_any(ids))
            .select((
                v::id,
                v::household_id,
                v::concept_id,
                v::name,
                v::normalized_name,
                v::brand,
                v::package_size,
                v::barcode,
                v::archived,
                v::revision,
            ))
            .load::<VariantRow>(&mut *conn)
            .await?;
        variants.extend(
            rows.into_iter()
                .map(|row| (row.id, VariantRecord::from(row))),
        );
    }

    let mut matched_aliases = HashMap::<Uuid, AliasRecord>::new();
    if !merchant_key.is_empty() {
        let aliases = query_alias_matches(
            &mut conn,
            household_id,
            &merchant_key,
            &prefix_pattern,
            None,
        )
        .await?;
        let aliases_partial = query_alias_matches(
            &mut conn,
            household_id,
            &merchant_key,
            &search_pattern,
            Some(&prefix_pattern),
        )
        .await?;
        for alias in aliases.into_iter().chain(aliases_partial) {
            let replace = matched_aliases
                .get(&alias.variant_id)
                .map(|current| preferred_alias(&alias, current, &search_key))
                .unwrap_or(true);
            if replace {
                matched_aliases.insert(alias.variant_id, alias);
            }
        }
        let alias_variant_ids = matched_aliases.keys().copied().collect::<Vec<_>>();
        for ids in alias_variant_ids.chunks(500) {
            use public::product_variants as v;
            let rows = v::table
                .filter(v::household_id.eq(household_id))
                .filter(v::archived.eq(false))
                .filter(v::id.eq_any(ids))
                .select((
                    v::id,
                    v::household_id,
                    v::concept_id,
                    v::name,
                    v::normalized_name,
                    v::brand,
                    v::package_size,
                    v::barcode,
                    v::archived,
                    v::revision,
                ))
                .load::<VariantRow>(&mut *conn)
                .await?;
            variants.extend(
                rows.into_iter()
                    .map(|row| (row.id, VariantRecord::from(row))),
            );
        }
    }

    let candidate_variant_ids = variants.keys().copied().collect::<Vec<_>>();
    let mut concept_ids = concepts.keys().copied().collect::<HashSet<_>>();
    concept_ids.extend(variants.values().map(|row| row.concept_id));
    let concept_ids = concept_ids.into_iter().collect::<Vec<_>>();
    for ids in concept_ids.chunks(500) {
        if ids.is_empty() {
            continue;
        }
        use public::product_concepts as c;
        let rows = c::table
            .filter(c::household_id.eq(household_id))
            .filter(c::archived.eq(false))
            .filter(c::id.eq_any(ids))
            .select((
                c::id,
                c::household_id,
                c::name,
                c::normalized_name,
                c::category_id,
                c::archived,
                c::revision,
            ))
            .load::<ConceptRow>(&mut *conn)
            .await?;
        concepts.extend(
            rows.into_iter()
                .map(|row| (row.id, ConceptRecord::from(row))),
        );
    }
    let stats = load_purchase_stats(
        &mut conn,
        household_id,
        actor_id,
        &candidate_variant_ids,
        &merchant_key,
    )
    .await?;

    let mut ranked = variants
        .values()
        .filter_map(|variant| {
            let concept = concepts.get(&variant.concept_id)?;
            let alias = matched_aliases.get(&variant.id);
            let (matched_name, score) = suggestion_score(variant, concept, alias, &search_key);
            let stat = stats.get(&variant.id).copied().unwrap_or_default();
            Some(RankedSuggestion {
                concept_id: concept.id,
                variant_id: variant.id,
                matched_name,
                score,
                member_purchase_count: stat.member_purchase_count,
                member_last_purchased_at: stat.member_last_purchased_at,
                same_merchant: stat.same_merchant,
                purchase_count: stat.purchase_count,
                last_purchased_at: stat.last_purchased_at,
                concept_name: concept.normalized_name.clone(),
                variant_name: variant.normalized_name.clone(),
            })
        })
        .collect::<Vec<_>>();
    ranked.sort_by(compare_suggestions);
    ranked.truncate(first as usize);
    if ranked.is_empty() {
        return Ok(Vec::new());
    }

    let selected_concept_ids = ranked
        .iter()
        .map(|row| row.concept_id)
        .collect::<HashSet<_>>()
        .into_iter()
        .collect::<Vec<_>>();
    let nested = load_product_concepts(&mut conn, household_id, &selected_concept_ids).await?;
    let concepts_by_id = nested
        .into_iter()
        .map(|concept| (concept.id, concept))
        .collect::<HashMap<_, _>>();
    let variants_by_id = concepts_by_id
        .values()
        .flat_map(|concept| concept.variants.iter())
        .map(|variant| (variant.id, variant.clone()))
        .collect::<HashMap<_, _>>();
    ranked
        .into_iter()
        .map(|row| {
            Ok(ProductSuggestionProjection {
                concept: concepts_by_id
                    .get(&row.concept_id)
                    .cloned()
                    .ok_or(CatalogQueryError::InvalidPersistedData)?,
                variant: variants_by_id
                    .get(&row.variant_id)
                    .cloned()
                    .ok_or(CatalogQueryError::InvalidPersistedData)?,
                matched_name: row.matched_name,
                score: row.score,
            })
        })
        .collect()
}

async fn validate_tag_cursor(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    include_archived: bool,
    cursor: Option<CatalogCursor>,
) -> Result<Option<CatalogCursor>, CatalogQueryError> {
    let Some(cursor) = cursor else {
        return Ok(None);
    };
    use public::tags as t;
    let mut query = t::table
        .filter(t::household_id.eq(household_id))
        .filter(t::id.eq(cursor.id))
        .into_boxed();
    if !include_archived {
        query = query.filter(t::archived.eq(false));
    }
    let name = query
        .select(t::name)
        .first::<String>(&mut *conn)
        .await
        .optional()?
        .ok_or(CatalogQueryError::InvalidCursor)?;
    if name != cursor.sort_key {
        return Err(CatalogQueryError::InvalidCursor);
    }
    Ok(Some(cursor))
}

async fn validate_concept_cursor(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    cursor: Option<&CatalogCursor>,
) -> Result<(), CatalogQueryError> {
    let Some(cursor) = cursor else { return Ok(()) };
    use public::product_concepts as c;
    let key = c::table
        .filter(c::household_id.eq(household_id))
        .filter(c::id.eq(cursor.id))
        .select(c::normalized_name)
        .first::<String>(&mut *conn)
        .await
        .optional()?
        .ok_or(CatalogQueryError::InvalidCursor)?;
    if key != cursor.sort_key {
        return Err(CatalogQueryError::InvalidCursor);
    }
    Ok(())
}

async fn require_member(
    conn: &mut diesel_async::AsyncPgConnection,
    actor_id: Uuid,
    household_id: Uuid,
) -> Result<(), CatalogQueryError> {
    use public::household_members as m;
    let exists = diesel::select(diesel::dsl::exists(
        m::table
            .filter(m::household_id.eq(household_id))
            .filter(m::member_id.eq(actor_id))
            .filter(m::left_at.is_null()),
    ))
    .get_result::<bool>(&mut *conn)
    .await?;
    if exists {
        Ok(())
    } else {
        Err(CatalogQueryError::HouseholdNotFound)
    }
}

fn page_size(first: Option<i32>, last: Option<i32>) -> Result<(usize, bool), CatalogQueryError> {
    if first.is_some() && last.is_some() {
        return Err(CatalogQueryError::InvalidPagination);
    }
    let backward = last.is_some();
    let size = first.or(last).unwrap_or(50);
    if !(1..=100).contains(&size) {
        return Err(CatalogQueryError::InvalidPagination);
    }
    Ok((size as usize, backward))
}
