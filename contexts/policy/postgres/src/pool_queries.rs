use crate::PgPool;
use policy_application::pool_queries::{
    Allocation, AllocationRule, AllocationRuleTarget, FundingSchedule, PolicyPage, PoolRulesQuery,
    PoolRulesQueryFailure,
};

pub struct PostgresAllocationRulesQuery {
    pool: PgPool,
}

impl PostgresAllocationRulesQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl policy_application::pool_queries::AllocationRulesQuery for PostgresAllocationRulesQuery {
    fn query<'a>(
        &'a self,
        query: PoolRulesQuery,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<PolicyPage<AllocationRule>, PoolRulesQueryFailure>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let page = crate::allocation_rule_query::allocation_rules(
                &self.pool,
                query.actor_id,
                query.household_id,
                query.after_id,
                query.before_id,
                query.page_size,
                query.backward,
            )
            .await
            .map_err(map_allocation_query_error)?;
            Ok(PolicyPage {
                rows: page.rows.into_iter().map(map_allocation_rule).collect(),
                total_count: page.total_count,
                has_next_page: page.has_next_page,
                has_previous_page: page.has_previous_page,
            })
        })
    }
}

pub struct PostgresFundingSchedulesQuery {
    pool: PgPool,
}

impl PostgresFundingSchedulesQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl policy_application::pool_queries::FundingSchedulesQuery for PostgresFundingSchedulesQuery {
    fn query<'a>(
        &'a self,
        query: PoolRulesQuery,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<PolicyPage<FundingSchedule>, PoolRulesQueryFailure>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let page = crate::funding_schedule_query::funding_schedules(
                &self.pool,
                crate::funding_schedule_query::FundingSchedulesQuery {
                    actor_id: query.actor_id,
                    household_id: query.household_id,
                    active: query.active,
                    after_id: query.after_id,
                    before_id: query.before_id,
                    page_size: query.page_size,
                    backward: query.backward,
                },
            )
            .await
            .map_err(map_funding_query_error)?;
            Ok(PolicyPage {
                rows: page.rows.into_iter().map(map_funding_schedule).collect(),
                total_count: page.total_count,
                has_next_page: page.has_next_page,
                has_previous_page: page.has_previous_page,
            })
        })
    }
}

fn map_allocation_query_error(
    error: crate::allocation_rule_query::AllocationRulesQueryError,
) -> PoolRulesQueryFailure {
    use crate::allocation_rule_query::AllocationRulesQueryError as Error;
    match error {
        Error::HouseholdNotFound | Error::CursorNotFound => PoolRulesQueryFailure::NotFound,
        Error::MemberNotFound => PoolRulesQueryFailure::AccessDenied,
        Error::Projection(error) => match error {
            cross_context_postgres::pool_mutations::AllocationRuleProjectionError::HouseholdNotFound
            | cross_context_postgres::pool_mutations::AllocationRuleProjectionError::RuleNotFound
            | cross_context_postgres::pool_mutations::AllocationRuleProjectionError::TargetNotFound => {
                PoolRulesQueryFailure::NotFound
            }
            cross_context_postgres::pool_mutations::AllocationRuleProjectionError::MemberNotFound => {
                PoolRulesQueryFailure::AccessDenied
            }
            error => PoolRulesQueryFailure::Infrastructure(format!("{error:?}")),
        },
        error @ (Error::Pool(_) | Error::Database(_)) => {
            PoolRulesQueryFailure::Infrastructure(format!("{error:?}"))
        }
    }
}

fn map_funding_query_error(
    error: crate::funding_schedule_query::FundingSchedulesQueryError,
) -> PoolRulesQueryFailure {
    use crate::funding_schedule_query::FundingSchedulesQueryError as Error;
    match error {
        Error::HouseholdNotFound | Error::CursorNotFound => PoolRulesQueryFailure::NotFound,
        Error::MemberNotFound => PoolRulesQueryFailure::AccessDenied,
        error @ (Error::Pool(_) | Error::Database(_)) => {
            PoolRulesQueryFailure::Infrastructure(format!("{error:?}"))
        }
    }
}

fn map_allocation_rule(
    value: cross_context_postgres::pool_mutations::AllocationRuleProjection,
) -> AllocationRule {
    use cross_context_postgres::pool_mutations::AllocationRuleTargetProjection as Target;
    AllocationRule {
        id: value.id,
        household_id: value.household_id,
        revision: value.revision,
        target: match value.target {
            Target::Category {
                id,
                household_id,
                parent_id,
                name,
                archived,
            } => AllocationRuleTarget::Category {
                id,
                household_id,
                parent_id,
                name,
                archived,
            },
            Target::Merchant { name } => AllocationRuleTarget::Merchant { name },
            Target::ProductConcept(value) => {
                AllocationRuleTarget::ProductConcept(map_concept(value))
            }
            Target::ProductVariant(value) => {
                AllocationRuleTarget::ProductVariant(map_variant(value))
            }
        },
        allocations: value
            .allocations
            .into_iter()
            .map(|value| Allocation {
                pool_id: value.pool_id,
                pool_name: value.pool_name,
                weight: value.weight,
            })
            .collect(),
    }
}

fn map_concept(
    value: cross_context_postgres::pool_mutations::ProductConceptProjection,
) -> policy_application::pool_queries::ProductConcept {
    policy_application::pool_queries::ProductConcept {
        id: value.id,
        household_id: value.household_id,
        name: value.name,
        normalized_name: value.normalized_name,
        category_id: value.category_id,
        archived: value.archived,
        revision: value.revision,
        variants: value.variants.into_iter().map(map_variant).collect(),
    }
}

fn map_variant(
    value: cross_context_postgres::pool_mutations::ProductVariantProjection,
) -> policy_application::pool_queries::ProductVariant {
    policy_application::pool_queries::ProductVariant {
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
            .map(|value| policy_application::pool_queries::ProductAlias {
                id: value.id,
                household_id: value.household_id,
                variant_id: value.variant_id,
                merchant_key: value.merchant_key,
                alias: value.alias,
                normalized_alias: value.normalized_alias,
                revision: value.revision,
            })
            .collect(),
    }
}

fn map_funding_schedule(
    value: crate::set_funding_schedule::PersistedFundingSchedule,
) -> FundingSchedule {
    FundingSchedule {
        id: value.id,
        household_id: value.household_id,
        member_id: value.member_id,
        pool_id: value.pool_id,
        amount_yen: value.amount_yen,
        cadence: value.cadence,
        starts_on: value.starts_on,
        ends_on: value.ends_on,
        active: value.active,
        created_at: value.created_at,
    }
}
