use std::{future::Future, pin::Pin};

use diesel::{ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use policy_application::node_catalog_planning_query::{
    AllocationRuleProjection, AllocationRuleWeightProjection, PolicyNodeBatchInput,
    PolicyNodeBatchProjection, PolicyNodeBatchQuery, RecurringExpenseProjection,
};
use uuid::Uuid;

use crate::{PgPool, schema::public};

pub struct PostgresPolicyNodeBatchQuery {
    pool: PgPool,
}

impl PostgresPolicyNodeBatchQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl PolicyNodeBatchQuery for PostgresPolicyNodeBatchQuery {
    fn load<'a>(
        &'a self,
        input: PolicyNodeBatchInput,
    ) -> Pin<Box<dyn Future<Output = Result<PolicyNodeBatchProjection, String>> + Send + 'a>> {
        Box::pin(async move {
            let mut result = PolicyNodeBatchProjection::default();
            if input.household_ids.is_empty() {
                return Ok(result);
            }
            let mut conn = self
                .pool
                .get()
                .await
                .map_err(|error| format!("{error:?}"))?;

            if !input.recurring_expense_ids.is_empty() {
                let rows = public::recurring_expenses::table
                    .filter(public::recurring_expenses::household_id.eq_any(&input.household_ids))
                    .filter(public::recurring_expenses::id.eq_any(&input.recurring_expense_ids))
                    .select((
                        public::recurring_expenses::id,
                        public::recurring_expenses::household_id,
                        public::recurring_expenses::name,
                        public::recurring_expenses::merchant,
                        public::recurring_expenses::amount_yen,
                        public::recurring_expenses::currency,
                        public::recurring_expenses::category_id,
                        public::recurring_expenses::payment_account_id,
                        public::recurring_expenses::cadence,
                        public::recurring_expenses::starts_on,
                        public::recurring_expenses::next_due_on,
                        public::recurring_expenses::ends_on,
                        public::recurring_expenses::archived,
                        public::recurring_expenses::revision,
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
                        chrono::NaiveDate,
                        chrono::NaiveDate,
                        Option<chrono::NaiveDate>,
                        bool,
                        i64,
                    )>(&mut conn)
                    .await
                    .map_err(|error| format!("{error:?}"))?;
                result.recurring_expenses = rows
                    .into_iter()
                    .map(
                        |(
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
                        )| RecurringExpenseProjection {
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
                        },
                    )
                    .collect();
            }

            if !input.allocation_rule_ids.is_empty() {
                let rules = public::allocation_rules::table
                    .filter(public::allocation_rules::household_id.eq_any(&input.household_ids))
                    .filter(public::allocation_rules::id.eq_any(&input.allocation_rule_ids))
                    .select((
                        public::allocation_rules::id,
                        public::allocation_rules::household_id,
                        public::allocation_rules::category_id,
                        public::allocation_rules::merchant_key,
                        public::allocation_rules::revision,
                        public::allocation_rules::product_concept_id,
                        public::allocation_rules::product_variant_id,
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
                    .map_err(|error| format!("{error:?}"))?;
                let rule_ids = rules.iter().map(|rule| rule.0).collect::<Vec<_>>();
                let weights = if rule_ids.is_empty() {
                    Vec::new()
                } else {
                    public::allocation_rule_weights::table
                        .filter(public::allocation_rule_weights::rule_id.eq_any(&rule_ids))
                        .order_by(public::allocation_rule_weights::pool_id)
                        .select((
                            public::allocation_rule_weights::rule_id,
                            public::allocation_rule_weights::pool_id,
                            public::allocation_rule_weights::weight,
                        ))
                        .load::<(Uuid, Uuid, i64)>(&mut conn)
                        .await
                        .map_err(|error| format!("{error:?}"))?
                };
                let pool_ids = weights.iter().map(|row| row.1).collect::<Vec<_>>();
                let pool_names = if pool_ids.is_empty() {
                    std::collections::HashMap::new()
                } else {
                    public::pools::table
                        .filter(public::pools::household_id.eq_any(&input.household_ids))
                        .filter(public::pools::id.eq_any(&pool_ids))
                        .select((public::pools::id, public::pools::name))
                        .load::<(Uuid, String)>(&mut conn)
                        .await
                        .map_err(|error| format!("{error:?}"))?
                        .into_iter()
                        .collect()
                };
                let category_ids = rules.iter().filter_map(|rule| rule.2).collect::<Vec<_>>();
                let categories = if category_ids.is_empty() {
                    std::collections::HashMap::new()
                } else {
                    public::categories::table
                        .filter(public::categories::household_id.eq_any(&input.household_ids))
                        .filter(public::categories::id.eq_any(&category_ids))
                        .select((
                            public::categories::id,
                            public::categories::name,
                            public::categories::parent_id,
                            public::categories::archived,
                        ))
                        .load::<(Uuid, String, Option<Uuid>, bool)>(&mut conn)
                        .await
                        .map_err(|error| format!("{error:?}"))?
                        .into_iter()
                        .map(|(id, name, parent_id, archived)| (id, (name, parent_id, archived)))
                        .collect::<std::collections::HashMap<_, _>>()
                };
                let mut allocations_by_rule = std::collections::HashMap::new();
                for (rule_id, pool_id, weight) in weights {
                    let pool_name = pool_names
                        .get(&pool_id)
                        .ok_or_else(|| format!("allocation rule weight pool {pool_id} is missing"))?
                        .clone();
                    allocations_by_rule
                        .entry(rule_id)
                        .or_insert_with(Vec::new)
                        .push(AllocationRuleWeightProjection {
                            pool_id,
                            pool_name,
                            weight,
                        });
                }
                result.allocation_rules = rules
                    .into_iter()
                    .map(
                        |(
                            id,
                            household_id,
                            category_id,
                            merchant_key,
                            revision,
                            product_concept_id,
                            product_variant_id,
                        )| {
                            let category = category_id.and_then(|id| categories.get(&id));
                            if category_id.is_some() && category.is_none() {
                                return Err("allocation rule category is missing".to_owned());
                            }
                            Ok(AllocationRuleProjection {
                                id,
                                household_id,
                                category_id,
                                category_name: category.map(|value| value.0.clone()),
                                category_parent_id: category.and_then(|value| value.1),
                                category_archived: category.map(|value| value.2),
                                merchant_key,
                                revision,
                                product_concept_id,
                                product_variant_id,
                                allocations: allocations_by_rule.remove(&id).unwrap_or_default(),
                            })
                        },
                    )
                    .collect::<Result<Vec<_>, String>>()?;
            }
            Ok(result)
        })
    }
}
