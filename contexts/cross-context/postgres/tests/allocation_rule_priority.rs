use cross_context_postgres::{
    purchase_command_context::{PurchaseItemAllocationInput, load_purchase_command_context},
    schema::public,
};
use diesel::{ExpressionMethods, QueryDsl, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::connect;
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

#[tokio::test]
async fn allocation_rule_priority_is_variant_concept_category_merchant_then_default() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();

    let household_id = Uuid::now_v7();
    let other_household_id = Uuid::now_v7();
    let pool_id = Uuid::now_v7();
    let other_pool_id = Uuid::now_v7();
    let category_id = Uuid::now_v7();
    let concept_id = Uuid::now_v7();
    let variant_id = Uuid::now_v7();
    let category_rule_id = Uuid::now_v7();
    let concept_rule_id = Uuid::now_v7();
    let variant_rule_id = Uuid::now_v7();
    let merchant_rule_id = Uuid::now_v7();
    let other_merchant_rule_id = Uuid::now_v7();

    {
        let mut conn = pool.get().await.unwrap();
        use public::{
            allocation_defaults, allocation_rule_weights, allocation_rules, categories, households,
            pools, product_concepts, product_variants,
        };

        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Priority"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(households::table)
            .values((
                households::id.eq(other_household_id),
                households::name.eq("Other household"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(pools::table)
            .values((
                pools::id.eq(pool_id),
                pools::household_id.eq(household_id),
                pools::name.eq("Default"),
                pools::kind.eq("PERSONAL"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(pools::table)
            .values((
                pools::id.eq(other_pool_id),
                pools::household_id.eq(other_household_id),
                pools::name.eq("Other"),
                pools::kind.eq("PERSONAL"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(allocation_defaults::table)
            .values((
                allocation_defaults::household_id.eq(household_id),
                allocation_defaults::version.eq(1_i64),
                allocation_defaults::pool_id.eq(pool_id),
                allocation_defaults::weight.eq(1_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(allocation_defaults::table)
            .values((
                allocation_defaults::household_id.eq(other_household_id),
                allocation_defaults::version.eq(1_i64),
                allocation_defaults::pool_id.eq(other_pool_id),
                allocation_defaults::weight.eq(1_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(categories::table)
            .values((
                categories::id.eq(category_id),
                categories::household_id.eq(household_id),
                categories::name.eq("Food"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(product_concepts::table)
            .values((
                product_concepts::id.eq(concept_id),
                product_concepts::household_id.eq(household_id),
                product_concepts::name.eq("Rice"),
                product_concepts::normalized_name.eq("rice"),
                product_concepts::category_id.eq(Some(category_id)),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(product_variants::table)
            .values((
                product_variants::id.eq(variant_id),
                product_variants::household_id.eq(household_id),
                product_variants::concept_id.eq(concept_id),
                product_variants::name.eq("Rice 5kg"),
                product_variants::normalized_name.eq("rice 5kg"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();

        for (rule_id, category, concept, variant, merchant) in [
            (category_rule_id, Some(category_id), None, None, None),
            (concept_rule_id, None, Some(concept_id), None, None),
            (variant_rule_id, None, None, Some(variant_id), None),
            (merchant_rule_id, None, None, None, Some("market")),
        ] {
            insert_into(allocation_rules::table)
                .values((
                    allocation_rules::id.eq(rule_id),
                    allocation_rules::household_id.eq(household_id),
                    allocation_rules::category_id.eq(category),
                    allocation_rules::product_concept_id.eq(concept),
                    allocation_rules::product_variant_id.eq(variant),
                    allocation_rules::merchant_key.eq(merchant),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            insert_into(allocation_rule_weights::table)
                .values((
                    allocation_rule_weights::household_id.eq(household_id),
                    allocation_rule_weights::rule_id.eq(rule_id),
                    allocation_rule_weights::pool_id.eq(pool_id),
                    allocation_rule_weights::weight.eq(1_i64),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
        }

        insert_into(allocation_rules::table)
            .values((
                allocation_rules::id.eq(other_merchant_rule_id),
                allocation_rules::household_id.eq(other_household_id),
                allocation_rules::merchant_key.eq(Some("market".to_owned())),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(allocation_rule_weights::table)
            .values((
                allocation_rule_weights::household_id.eq(other_household_id),
                allocation_rule_weights::rule_id.eq(other_merchant_rule_id),
                allocation_rule_weights::pool_id.eq(other_pool_id),
                allocation_rule_weights::weight.eq(1_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
    }

    let item = PurchaseItemAllocationInput {
        category_id: Some(category_id),
        product_variant_id: Some(variant_id),
        merchant_key: "market".to_owned(),
        allocations: Vec::new(),
    };
    let cases = [
        (None, variant_rule_id),
        (Some(variant_rule_id), concept_rule_id),
        (Some(concept_rule_id), category_rule_id),
        (Some(category_rule_id), merchant_rule_id),
    ];
    let mut removed = None;
    for (previous, expected) in cases {
        if let Some(rule_id) = previous {
            let mut conn = pool.get().await.unwrap();
            diesel::delete(
                public::allocation_rules::table.filter(public::allocation_rules::id.eq(rule_id)),
            )
            .execute(&mut *conn)
            .await
            .unwrap();
        }
        let context = load_purchase_command_context(
            &pool,
            household_id,
            "market",
            std::slice::from_ref(&item),
        )
        .await
        .unwrap();
        assert_eq!(context.items[0].allocations[0].rule_id, Some(expected));
        removed = Some(expected);
    }

    let mut conn = pool.get().await.unwrap();
    diesel::delete(
        public::allocation_rules::table.filter(public::allocation_rules::id.eq(removed.unwrap())),
    )
    .execute(&mut *conn)
    .await
    .unwrap();
    drop(conn);
    let context = load_purchase_command_context(&pool, household_id, "market", &[item])
        .await
        .unwrap();
    assert_eq!(context.items[0].allocations[0].rule_id, None);
    assert_eq!(context.items[0].allocations[0].pool_id, pool_id);
}
