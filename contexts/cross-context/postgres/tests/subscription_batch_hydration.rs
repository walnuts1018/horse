use std::sync::atomic::{AtomicUsize, Ordering};

use cross_context_application::subscriptions::SubscriptionQuery;
use cross_context_postgres::{
    schema::public::{household_events, household_members, households, members, purchases},
    subscriptions::PostgresSubscriptionQuery,
};
use diesel::connection::{Instrumentation, InstrumentationEvent, set_default_instrumentation};
use diesel::{ExpressionMethods, insert_into};
use diesel_async::{
    RunQueryDsl,
    pooled_connection::{AsyncDieselConnectionManager, RecyclingMethod, bb8::Pool},
};
use horse_postgres_common::AsyncPgConnection;
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

static QUERY_COUNT: AtomicUsize = AtomicUsize::new(0);

fn count_queries() -> Option<Box<dyn Instrumentation>> {
    Some(Box::new(|event: InstrumentationEvent<'_>| {
        if let InstrumentationEvent::StartQuery { .. } = event {
            QUERY_COUNT.fetch_add(1, Ordering::Relaxed);
        }
    }))
}

#[tokio::test]
async fn purchase_events_and_multiple_purchase_snapshots_use_bounded_queries() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    set_default_instrumentation(count_queries).unwrap();
    let mut manager_config =
        diesel_async::pooled_connection::ManagerConfig::<AsyncPgConnection>::default();
    manager_config.recycling_method = RecyclingMethod::Fast;
    let manager = AsyncDieselConnectionManager::<AsyncPgConnection>::new_with_config(
        postgres.connection_url(),
        manager_config,
    );
    let pool = Pool::builder().build(manager).await.unwrap();

    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    let purchase_ids = [Uuid::now_v7(), Uuid::now_v7()];
    let occurred_at = "2026-10-16T12:00:00Z"
        .parse::<chrono::DateTime<chrono::Utc>>()
        .unwrap();
    {
        let mut conn = pool.get().await.unwrap();
        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Subscription batch hydration"),
                households::default_allocation_version.eq(1_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(member_id),
                members::oidc_subject.eq(format!("subscription-batch-{member_id}")),
                members::display_name.eq("Subscription batch test"),
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
        for (index, purchase_id) in purchase_ids.iter().copied().enumerate() {
            insert_into(purchases::table)
                .values((
                    purchases::id.eq(purchase_id),
                    purchases::household_id.eq(household_id),
                    purchases::occurred_at.eq(occurred_at),
                    purchases::merchant.eq(format!("Market {index}")),
                    purchases::status.eq("DRAFT"),
                    purchases::revision.eq(1_i64),
                    purchases::created_by.eq(member_id),
                    purchases::allocation_rule_version.eq(0_i64),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            insert_into(household_events::table)
                .values((
                    household_events::household_id.eq(household_id),
                    household_events::event_type.eq("purchase.updated"),
                    household_events::entity_kind.eq("purchase"),
                    household_events::entity_id.eq(purchase_id),
                    household_events::revision.eq(2_i64),
                    household_events::changed_fields.eq(vec![Some("merchant".to_owned())]),
                    household_events::payload.eq(serde_json::json!({})),
                    household_events::actor_id.eq(member_id),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
        }
    }

    QUERY_COUNT.store(0, Ordering::Relaxed);
    let query = PostgresSubscriptionQuery::new(pool);
    let events = query
        .purchase_events(vec![(household_id, 0)])
        .await
        .unwrap();
    assert_eq!(events.len(), 2);
    let snapshots = query
        .purchase_snapshots(household_id, purchase_ids.to_vec())
        .await
        .unwrap();
    assert_eq!(snapshots.len(), 2);
    assert_eq!(snapshots[&purchase_ids[0]].merchant, "Market 0");
    assert_eq!(snapshots[&purchase_ids[1]].merchant, "Market 1");
    let two_purchase_query_count = AtomicUsize::load(&QUERY_COUNT, Ordering::Relaxed);
    assert!(two_purchase_query_count <= 8);

    let additional_purchase_ids = (0..6).map(|_| Uuid::now_v7()).collect::<Vec<_>>();
    {
        let mut conn = pool.get().await.unwrap();
        for (index, purchase_id) in additional_purchase_ids.iter().copied().enumerate() {
            let purchase_index = index + purchase_ids.len();
            insert_into(purchases::table)
                .values((
                    purchases::id.eq(purchase_id),
                    purchases::household_id.eq(household_id),
                    purchases::occurred_at.eq(occurred_at),
                    purchases::merchant.eq(format!("Market {purchase_index}")),
                    purchases::status.eq("DRAFT"),
                    purchases::revision.eq(1_i64),
                    purchases::created_by.eq(member_id),
                    purchases::allocation_rule_version.eq(0_i64),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            insert_into(household_events::table)
                .values((
                    household_events::household_id.eq(household_id),
                    household_events::event_type.eq("purchase.updated"),
                    household_events::entity_kind.eq("purchase"),
                    household_events::entity_id.eq(purchase_id),
                    household_events::revision.eq(2_i64),
                    household_events::changed_fields.eq(vec![Some("merchant".to_owned())]),
                    household_events::payload.eq(serde_json::json!({})),
                    household_events::actor_id.eq(member_id),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
        }
    }

    QUERY_COUNT.store(0, Ordering::Relaxed);
    let all_purchase_ids = purchase_ids
        .into_iter()
        .chain(additional_purchase_ids)
        .collect::<Vec<_>>();
    let events = query
        .purchase_events(vec![(household_id, 0)])
        .await
        .unwrap();
    assert_eq!(events.len(), all_purchase_ids.len());
    let snapshots = query
        .purchase_snapshots(household_id, all_purchase_ids.clone())
        .await
        .unwrap();
    assert_eq!(snapshots.len(), all_purchase_ids.len());
    let eight_purchase_query_count = AtomicUsize::load(&QUERY_COUNT, Ordering::Relaxed);
    assert!(eight_purchase_query_count <= 8);
    assert_eq!(eight_purchase_query_count, two_purchase_query_count);
}
