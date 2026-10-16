use diesel::{ExpressionMethods, QueryDsl, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::schema::public::{allocation_defaults, households, members, pools};
use household_application::create_household::CreateHouseholdCommand;
use policy_application::create_pool::{CreatePoolCommand, NewPoolId, PoolActorId, PoolHouseholdId};
use uuid::Uuid;

#[tokio::test]
async fn pool_creation_updates_household_defaults_atomically() {
    let postgres = horse_test_infra::TestPostgres::start().await.unwrap();
    horse_test_infra::apply_migrations(postgres.connection_url()).unwrap();
    let pool = horse_postgres_common::connect(postgres.connection_url())
        .await
        .unwrap();
    let actor_id = Uuid::now_v7();
    {
        let mut conn = pool.get().await.unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(actor_id),
                members::oidc_subject.eq("pool-creation-atomicity"),
                members::display_name.eq("Pool test actor"),
            ))
            .execute(&mut conn)
            .await
            .unwrap();
    }
    let household = household_postgres::create_household::persist_create_household(
        &pool,
        CreateHouseholdCommand {
            actor_id: actor_id.into(),
            name: "Test household".to_owned(),
            member_names: Vec::new(),
        },
    )
    .await
    .unwrap();
    let household_id = Uuid::parse_str(&household.household_id).unwrap();
    let first_pool_id = Uuid::now_v7();

    policy_postgres::pool_mutations::persist_create_pool(
        &pool,
        CreatePoolCommand {
            actor_id: PoolActorId::from_uuid(actor_id),
            household_id: PoolHouseholdId::from_uuid(household_id),
            pool_id: NewPoolId::from_uuid(first_pool_id),
            name: "Shared".to_owned(),
            kind: "SHARED".to_owned(),
        },
    )
    .await
    .unwrap();

    let current_defaults = {
        let mut conn = pool.get().await.unwrap();
        allocation_defaults::table
            .filter(allocation_defaults::household_id.eq(household_id))
            .filter(allocation_defaults::version.eq(2_i64))
            .select((allocation_defaults::pool_id, allocation_defaults::weight))
            .load::<(Uuid, i64)>(&mut conn)
            .await
            .unwrap()
    };
    assert_eq!(current_defaults, vec![(first_pool_id, 1)]);

    {
        let mut conn = pool.get().await.unwrap();
        diesel::update(households::table.filter(households::id.eq(household_id)))
            .set(households::default_allocation_version.eq(i64::MAX))
            .execute(&mut conn)
            .await
            .unwrap();
    }
    let failed_pool_id = Uuid::now_v7();
    let result = policy_postgres::pool_mutations::persist_create_pool(
        &pool,
        CreatePoolCommand {
            actor_id: PoolActorId::from_uuid(actor_id),
            household_id: PoolHouseholdId::from_uuid(household_id),
            pool_id: NewPoolId::from_uuid(failed_pool_id),
            name: "Must roll back".to_owned(),
            kind: "SHARED".to_owned(),
        },
    )
    .await;
    assert!(result.is_err());

    let persisted_pool = {
        let mut conn = pool.get().await.unwrap();
        pools::table
            .filter(pools::id.eq(failed_pool_id))
            .select(pools::id)
            .first::<Uuid>(&mut conn)
            .await
    };
    assert!(persisted_pool.is_err());
    let defaults_after_failure = {
        let mut conn = pool.get().await.unwrap();
        allocation_defaults::table
            .filter(allocation_defaults::household_id.eq(household_id))
            .select((allocation_defaults::pool_id, allocation_defaults::weight))
            .load::<(Uuid, i64)>(&mut conn)
            .await
            .unwrap()
    };
    assert_eq!(defaults_after_failure, current_defaults);

    postgres.terminate().await.unwrap();
}
