use diesel::{
    BoolExpressionMethods, ExpressionMethods, JoinOnDsl, OptionalExtension, QueryDsl,
    result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AllocationProjection {
    pub pool_id: Uuid,
    pub pool_name: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AllocationRuleTargetProjection {
    Category {
        id: Uuid,
        household_id: Uuid,
        parent_id: Option<Uuid>,
        name: String,
        archived: bool,
    },
    Merchant {
        name: String,
    },
    ProductConcept(ProductConceptProjection),
    ProductVariant(ProductVariantProjection),
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
pub struct AllocationRuleProjection {
    pub id: Uuid,
    pub household_id: Uuid,
    pub revision: i64,
    pub target: AllocationRuleTargetProjection,
    pub allocations: Vec<AllocationProjection>,
}

#[derive(Debug)]
pub enum AllocationRuleProjectionError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    HouseholdNotFound,
    MemberNotFound,
    RuleNotFound,
    TargetNotFound,
}

impl From<DieselError> for AllocationRuleProjectionError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn load_allocation_rule_projection(
    pool: &PgPool,
    actor_id: Uuid,
    rule_id: Uuid,
) -> Result<AllocationRuleProjection, AllocationRuleProjectionError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| AllocationRuleProjectionError::Pool(error.to_string()))?;
    use public::{
        allocation_rule_weights as weights, allocation_rules as rules,
        household_members as members, households, pools,
    };
    let (id, household_id, category_id, merchant, concept_id, variant_id, revision) = rules::table
        .filter(rules::id.eq(rule_id))
        .select((
            rules::id,
            rules::household_id,
            rules::category_id,
            rules::merchant_key,
            rules::product_concept_id,
            rules::product_variant_id,
            rules::revision,
        ))
        .first::<(
            Uuid,
            Uuid,
            Option<Uuid>,
            Option<String>,
            Option<Uuid>,
            Option<Uuid>,
            i64,
        )>(&mut *conn)
        .await
        .optional()?
        .ok_or(AllocationRuleProjectionError::RuleNotFound)?;
    let household_exists = households::table
        .filter(households::id.eq(household_id))
        .select(households::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .is_some();
    if !household_exists {
        return Err(AllocationRuleProjectionError::HouseholdNotFound);
    }
    let member_exists = members::table
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .filter(members::left_at.is_null())
        .select(members::member_id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .is_some();
    if !member_exists {
        return Err(AllocationRuleProjectionError::MemberNotFound);
    }
    let selected_targets = [
        category_id.is_some(),
        merchant.is_some(),
        concept_id.is_some(),
        variant_id.is_some(),
    ]
    .into_iter()
    .filter(|selected| *selected)
    .count();
    if selected_targets != 1 {
        return Err(AllocationRuleProjectionError::TargetNotFound);
    }
    let target = if let Some(target_id) = category_id {
        let (target_household_id, parent_id, name, archived) = public::categories::table
            .filter(public::categories::household_id.eq(household_id))
            .filter(public::categories::id.eq(target_id))
            .select((
                public::categories::household_id,
                public::categories::parent_id,
                public::categories::name,
                public::categories::archived,
            ))
            .first::<(Uuid, Option<Uuid>, String, bool)>(&mut *conn)
            .await
            .optional()?
            .ok_or(AllocationRuleProjectionError::TargetNotFound)?;
        AllocationRuleTargetProjection::Category {
            id: target_id,
            household_id: target_household_id,
            parent_id,
            name,
            archived,
        }
    } else if let Some(name) = merchant {
        AllocationRuleTargetProjection::Merchant { name }
    } else if let Some(target_id) = concept_id {
        let (concept_household_id, name, normalized_name, category_id, archived, concept_revision) =
            public::product_concepts::table
                .filter(public::product_concepts::household_id.eq(household_id))
                .filter(public::product_concepts::id.eq(target_id))
                .select((
                    public::product_concepts::household_id,
                    public::product_concepts::name,
                    public::product_concepts::normalized_name,
                    public::product_concepts::category_id,
                    public::product_concepts::archived,
                    public::product_concepts::revision,
                ))
                .first::<(Uuid, String, String, Option<Uuid>, bool, i64)>(&mut *conn)
                .await
                .optional()?
                .ok_or(AllocationRuleProjectionError::TargetNotFound)?;
        let variant_rows = public::product_variants::table
            .filter(public::product_variants::household_id.eq(household_id))
            .filter(public::product_variants::concept_id.eq(target_id))
            .order_by((
                public::product_variants::name.asc(),
                public::product_variants::id.asc(),
            ))
            .select((
                public::product_variants::id,
                public::product_variants::household_id,
                public::product_variants::concept_id,
                public::product_variants::name,
                public::product_variants::normalized_name,
                public::product_variants::brand,
                public::product_variants::package_size,
                public::product_variants::barcode,
                public::product_variants::archived,
                public::product_variants::revision,
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
        let variant_ids = variant_rows.iter().map(|row| row.0).collect::<Vec<_>>();
        let aliases = if variant_ids.is_empty() {
            Vec::new()
        } else {
            public::product_aliases::table
                .filter(public::product_aliases::household_id.eq(household_id))
                .filter(public::product_aliases::variant_id.eq_any(&variant_ids))
                .order_by((
                    public::product_aliases::merchant_key.asc(),
                    public::product_aliases::alias_text.asc(),
                    public::product_aliases::id.asc(),
                ))
                .select((
                    public::product_aliases::id,
                    public::product_aliases::household_id,
                    public::product_aliases::variant_id,
                    public::product_aliases::merchant_key,
                    public::product_aliases::alias_text,
                    public::product_aliases::normalized_alias,
                    public::product_aliases::revision,
                ))
                .load::<(Uuid, Uuid, Uuid, String, String, String, i64)>(&mut *conn)
                .await?
        };
        let variants = variant_rows
            .into_iter()
            .map(|row| ProductVariantProjection {
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
                aliases: aliases
                    .iter()
                    .filter(|alias| alias.2 == row.0)
                    .map(|alias| ProductAliasProjection {
                        id: alias.0,
                        household_id: alias.1,
                        variant_id: alias.2,
                        merchant_key: alias.3.clone(),
                        alias: alias.4.clone(),
                        normalized_alias: alias.5.clone(),
                        revision: alias.6,
                    })
                    .collect(),
            })
            .collect();
        AllocationRuleTargetProjection::ProductConcept(ProductConceptProjection {
            id: target_id,
            household_id: concept_household_id,
            name,
            normalized_name,
            category_id,
            archived,
            revision: concept_revision,
            variants,
        })
    } else if let Some(target_id) = variant_id {
        let row = public::product_variants::table
            .filter(public::product_variants::household_id.eq(household_id))
            .filter(public::product_variants::id.eq(target_id))
            .select((
                public::product_variants::id,
                public::product_variants::household_id,
                public::product_variants::concept_id,
                public::product_variants::name,
                public::product_variants::normalized_name,
                public::product_variants::brand,
                public::product_variants::package_size,
                public::product_variants::barcode,
                public::product_variants::archived,
                public::product_variants::revision,
            ))
            .first::<(
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
            .await
            .optional()?
            .ok_or(AllocationRuleProjectionError::TargetNotFound)?;
        let alias_rows = public::product_aliases::table
            .filter(public::product_aliases::household_id.eq(household_id))
            .filter(public::product_aliases::variant_id.eq(target_id))
            .order_by((
                public::product_aliases::merchant_key.asc(),
                public::product_aliases::alias_text.asc(),
                public::product_aliases::id.asc(),
            ))
            .select((
                public::product_aliases::id,
                public::product_aliases::household_id,
                public::product_aliases::variant_id,
                public::product_aliases::merchant_key,
                public::product_aliases::alias_text,
                public::product_aliases::normalized_alias,
                public::product_aliases::revision,
            ))
            .load::<(Uuid, Uuid, Uuid, String, String, String, i64)>(&mut *conn)
            .await?
            .into_iter()
            .map(|alias| ProductAliasProjection {
                id: alias.0,
                household_id: alias.1,
                variant_id: alias.2,
                merchant_key: alias.3,
                alias: alias.4,
                normalized_alias: alias.5,
                revision: alias.6,
            })
            .collect();
        AllocationRuleTargetProjection::ProductVariant(ProductVariantProjection {
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
            aliases: alias_rows,
        })
    } else {
        return Err(AllocationRuleProjectionError::TargetNotFound);
    };
    let allocations = weights::table
        .inner_join(
            pools::table.on(weights::pool_id
                .eq(pools::id)
                .and(weights::household_id.eq(pools::household_id))),
        )
        .filter(weights::household_id.eq(household_id))
        .filter(weights::rule_id.eq(rule_id))
        .order_by((pools::name.asc(), pools::id.asc()))
        .select((weights::pool_id, pools::name, weights::weight))
        .load::<(Uuid, String, i64)>(&mut *conn)
        .await?
        .into_iter()
        .map(|(pool_id, pool_name, weight)| AllocationProjection {
            pool_id,
            pool_name,
            weight,
        })
        .collect();
    Ok(AllocationRuleProjection {
        id,
        household_id,
        revision,
        target,
        allocations,
    })
}
