use std::collections::HashMap;

use diesel::{ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use policy_application::purchase_allocation::{
    AllocationRuleTargetQuery, AllocationRuleWeight, PurchaseAllocationRule,
    PurchaseAllocationRulesFailure, PurchaseAllocationRulesRead,
};
use uuid::Uuid;

#[derive(Default)]
pub struct PostgresPurchaseAllocationRulesRead;

impl PurchaseAllocationRulesRead<horse_postgres_common::AsyncPgConnection>
    for PostgresPurchaseAllocationRulesRead
{
    async fn read(
        &self,
        conn: &mut horse_postgres_common::AsyncPgConnection,
        household_id: Uuid,
        targets: &[AllocationRuleTargetQuery],
    ) -> Result<Vec<PurchaseAllocationRule>, PurchaseAllocationRulesFailure> {
        use horse_postgres_common::schema::public::allocation_rules as rules;
        let mut query = rules::table.into_boxed();
        let mut has_target = false;
        for target in targets {
            if let Some(id) = target.product_variant_id {
                query = query.or_filter(rules::product_variant_id.eq(Some(id)));
                has_target = true;
            }
            if let Some(id) = target.product_concept_id {
                query = query.or_filter(rules::product_concept_id.eq(Some(id)));
                has_target = true;
            }
            if let Some(id) = target.category_id {
                query = query.or_filter(rules::category_id.eq(Some(id)));
                has_target = true;
            }
            if let Some(merchant) = &target.merchant_key
                && !merchant.is_empty()
            {
                query = query.or_filter(rules::merchant_key.eq(Some(merchant.clone())));
                has_target = true;
            }
        }
        if !has_target {
            return Ok(Vec::new());
        }
        let rows = query
            .filter(rules::household_id.eq(household_id))
            .select((
                rules::id,
                rules::revision,
                rules::product_variant_id,
                rules::product_concept_id,
                rules::category_id,
                rules::merchant_key,
            ))
            .load::<(
                Uuid,
                i64,
                Option<Uuid>,
                Option<Uuid>,
                Option<Uuid>,
                Option<String>,
            )>(conn)
            .await
            .map_err(|error| PurchaseAllocationRulesFailure::Infrastructure(error.to_string()))?;
        if rows.is_empty() {
            return Ok(Vec::new());
        }
        let rule_ids = rows.iter().map(|row| row.0).collect::<Vec<_>>();
        let weights = horse_postgres_common::schema::public::allocation_rule_weights::table
            .filter(
                horse_postgres_common::schema::public::allocation_rule_weights::household_id
                    .eq(household_id),
            )
            .filter(
                horse_postgres_common::schema::public::allocation_rule_weights::rule_id
                    .eq_any(&rule_ids),
            )
            .order_by((
                horse_postgres_common::schema::public::allocation_rule_weights::rule_id.asc(),
                horse_postgres_common::schema::public::allocation_rule_weights::pool_id.asc(),
            ))
            .select((
                horse_postgres_common::schema::public::allocation_rule_weights::rule_id,
                horse_postgres_common::schema::public::allocation_rule_weights::pool_id,
                horse_postgres_common::schema::public::allocation_rule_weights::weight,
            ))
            .load::<(Uuid, Uuid, i64)>(conn)
            .await
            .map_err(|error| PurchaseAllocationRulesFailure::Infrastructure(error.to_string()))?;
        let mut weights_by_rule = HashMap::<Uuid, Vec<AllocationRuleWeight>>::new();
        for (rule_id, pool_id, weight) in weights {
            weights_by_rule
                .entry(rule_id)
                .or_default()
                .push(AllocationRuleWeight { pool_id, weight });
        }
        Ok(rows
            .into_iter()
            .map(
                |(
                    id,
                    revision,
                    product_variant_id,
                    product_concept_id,
                    category_id,
                    merchant_key,
                )| {
                    PurchaseAllocationRule {
                        id,
                        revision,
                        product_variant_id,
                        product_concept_id,
                        category_id,
                        merchant_key,
                        allocations: weights_by_rule.remove(&id).unwrap_or_default(),
                    }
                },
            )
            .collect())
    }
}
