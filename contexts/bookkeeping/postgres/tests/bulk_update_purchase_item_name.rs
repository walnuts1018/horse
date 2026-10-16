use bookkeeping_postgres::{
    bulk_update_purchase_items::{
        PersistBulkUpdatePurchaseItems, persist_bulk_update_purchase_items,
    },
    post_purchase_loader::{FundingRuleSnapshot, PostPurchaseReferenceData},
    schema::public,
    update_purchase_item::{
        AutomaticAllocationSnapshot, ProductLearningRequest, UpdatePurchaseItemPersistenceError,
        UpdatePurchaseItemReferenceData,
    },
};
use diesel::{ExpressionMethods, QueryDsl, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::{AsyncPgConnection, connect};
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

struct ItemReferences {
    pool_id: Uuid,
}

impl PostPurchaseReferenceData for ItemReferences {
    async fn load_funding_rule(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        _pool_id: Uuid,
        _version: i32,
    ) -> Result<Option<FundingRuleSnapshot>, diesel::result::Error> {
        Ok(None)
    }
}

impl UpdatePurchaseItemReferenceData for ItemReferences {
    async fn load_default_allocation(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        _version: i64,
    ) -> Result<Vec<(Uuid, i64)>, diesel::result::Error> {
        Ok(vec![(self.pool_id, 1)])
    }

    async fn load_automatic_allocation(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        _category_id: Option<Uuid>,
        _variant_id: Option<Uuid>,
        _merchant: &str,
    ) -> Result<Option<AutomaticAllocationSnapshot>, diesel::result::Error> {
        Ok(None)
    }

    async fn active_category(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        _category_id: Uuid,
    ) -> Result<bool, diesel::result::Error> {
        Ok(false)
    }

    async fn active_variant(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        _variant_id: Uuid,
    ) -> Result<bool, diesel::result::Error> {
        Ok(false)
    }

    async fn active_tags(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        tag_ids: Vec<Uuid>,
    ) -> Result<Vec<Uuid>, diesel::result::Error> {
        Ok(tag_ids)
    }

    async fn persist_product_learning(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _context: ProductLearningRequest,
    ) -> Result<(), UpdatePurchaseItemPersistenceError> {
        Ok(())
    }
}

#[tokio::test]
async fn bulk_update_purchase_item_name_persists_name_and_revisions() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();

    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    let pool_id = Uuid::now_v7();
    let purchase_id = Uuid::now_v7();
    let item_id = Uuid::now_v7();
    let occurred_at = "2026-10-16T12:00:00Z"
        .parse::<chrono::DateTime<chrono::Utc>>()
        .unwrap();

    {
        let mut conn = pool.get().await.unwrap();
        use public::{
            household_members, households, members, pools, purchase_allocations, purchase_items,
            purchases,
        };

        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Bulk item name update"),
                households::default_allocation_version.eq(1_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(member_id),
                members::oidc_subject.eq(format!("bulk-item-name-{member_id}")),
                members::display_name.eq("Bulk item name test"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(household_members::table)
            .values((
                household_members::household_id.eq(household_id),
                household_members::member_id.eq(member_id),
                household_members::role.eq("OWNER"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(pools::table)
            .values((
                pools::id.eq(pool_id),
                pools::household_id.eq(household_id),
                pools::name.eq("Personal"),
                pools::kind.eq("PERSONAL"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(purchases::table)
            .values((
                purchases::id.eq(purchase_id),
                purchases::household_id.eq(household_id),
                purchases::occurred_at.eq(occurred_at),
                purchases::merchant.eq("Market"),
                purchases::status.eq("DRAFT"),
                purchases::revision.eq(1_i64),
                purchases::allocation_rule_version.eq(1_i64),
                purchases::created_by.eq(member_id),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(purchase_items::table)
            .values((
                purchase_items::id.eq(item_id),
                purchase_items::household_id.eq(household_id),
                purchase_items::purchase_id.eq(purchase_id),
                purchase_items::name.eq("Old name"),
                purchase_items::amount_yen.eq(500_i64),
                purchase_items::revision.eq(1_i64),
                purchase_items::allocation_override.eq(true),
                purchase_items::field_revisions.eq(serde_json::json!({})),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(purchase_allocations::table)
            .values((
                purchase_allocations::household_id.eq(household_id),
                purchase_allocations::purchase_id.eq(purchase_id),
                purchase_allocations::item_id.eq(item_id),
                purchase_allocations::pool_id.eq(pool_id),
                purchase_allocations::amount_yen.eq(500_i64),
                purchase_allocations::weight.eq(1_i64),
                purchase_allocations::rule_version.eq(1_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
    }

    let mut references = ItemReferences { pool_id };
    let result = persist_bulk_update_purchase_items(
        &pool,
        PersistBulkUpdatePurchaseItems {
            household_id: household_id.to_string(),
            actor_id: member_id.to_string(),
        },
        vec![bookkeeping_domain::purchase_commands::BulkUpdateItemTarget {
            item_id: item_id.to_string(),
            base_revision: 1,
        }],
        bookkeeping_domain::purchase_commands::BulkUpdateItemPatch {
            name_set: true,
            name: Some("  New name  ".to_owned()),
            ..bookkeeping_domain::purchase_commands::BulkUpdateItemPatch::default()
        },
        &mut references,
    )
    .await
    .unwrap();

    assert_eq!(result.items, vec![(item_id.to_string(), purchase_id.to_string())]);

    let mut conn = pool.get().await.unwrap();
    let saved = public::purchase_items::table
        .filter(public::purchase_items::id.eq(item_id))
        .select((
            public::purchase_items::name,
            public::purchase_items::revision,
            public::purchase_items::field_revisions,
        ))
        .first::<(String, i64, serde_json::Value)>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(saved.0, "New name");
    assert_eq!(saved.1, 2);
    assert_eq!(saved.2["name"], 2);
}
