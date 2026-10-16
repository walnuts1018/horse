use async_graphql::{Executor, Request, Schema};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use chrono::Utc;
use diesel::{ExpressionMethods, QueryDsl, dsl::count_star, insert_into};
use diesel_async::RunQueryDsl;
use horse_graphql_api::{Actor, MutationRoot, QueryRoot, subscriptions::SubscriptionRoot};
use household_application::create_household::{ActorId, CreateHouseholdCommand};
use uuid::Uuid;

fn household_global_id(id: Uuid) -> String {
    let mut value = b"Household:".to_vec();
    value.extend_from_slice(id.as_bytes());
    URL_SAFE_NO_PAD.encode(value)
}

#[tokio::test]
async fn graphql_member_cannot_read_or_create_resources_in_another_household() {
    let postgres = horse_test_infra::TestPostgres::start().await.unwrap();
    horse_test_infra::apply_migrations(postgres.connection_url()).unwrap();
    let pool = horse_postgres_common::connect(postgres.connection_url())
        .await
        .unwrap();
    let target_owner = Uuid::now_v7();
    let actor = Uuid::now_v7();
    let target_member = Uuid::now_v7();
    {
        let mut conn = pool.get().await.unwrap();
        for (id, subject) in [
            (target_owner, "graphql-boundary-target"),
            (actor, "graphql-boundary-actor"),
            (target_member, "graphql-boundary-member"),
        ] {
            insert_into(horse_postgres_common::schema::public::members::table)
                .values((
                    horse_postgres_common::schema::public::members::id.eq(id),
                    horse_postgres_common::schema::public::members::oidc_subject.eq(subject),
                    horse_postgres_common::schema::public::members::display_name.eq(subject),
                    horse_postgres_common::schema::public::members::created_at.eq(Utc::now()),
                ))
                .execute(&mut conn)
                .await
                .unwrap();
        }
    }
    let target = household_postgres::create_household::persist_create_household(
        &pool,
        CreateHouseholdCommand {
            actor_id: ActorId::from(target_owner),
            name: "Target household".to_owned(),
            member_names: vec![],
        },
    )
    .await
    .unwrap();
    household_postgres::create_household::persist_create_household(
        &pool,
        CreateHouseholdCommand {
            actor_id: ActorId::from(actor),
            name: "Actor household".to_owned(),
            member_names: vec![],
        },
    )
    .await
    .unwrap();
    let target_id = Uuid::parse_str(&target.household_id).unwrap();
    {
        let mut conn = pool.get().await.unwrap();
        insert_into(horse_postgres_common::schema::public::household_members::table)
            .values((
                horse_postgres_common::schema::public::household_members::household_id
                    .eq(target_id),
                horse_postgres_common::schema::public::household_members::member_id
                    .eq(target_member),
                horse_postgres_common::schema::public::household_members::role.eq("MEMBER"),
                horse_postgres_common::schema::public::household_members::joined_at.eq(Utc::now()),
            ))
            .execute(&mut conn)
            .await
            .unwrap();
    }
    let schema = Schema::build(QueryRoot, MutationRoot, SubscriptionRoot)
        .data(std::sync::Arc::new(
            household_postgres::household_query::PostgresHouseholdQuery::new(pool.clone()),
        )
            as std::sync::Arc<
                dyn household_application::household_query::HouseholdQuery,
            >)
        .data(std::sync::Arc::new(
            policy_postgres::pool_mutations::PostgresCreatePoolHandler::new(pool.clone()),
        )
            as std::sync::Arc<
                dyn policy_application::create_pool::CreatePoolHandler,
            >)
        .finish();
    let household_id = household_global_id(target_id);

    let query = Executor::execute(
        &schema,
        Request::new(format!(
            "{{ household(id: \"{household_id}\") {{ id name }} }}"
        ))
        .data(Actor(actor)),
    )
    .await;
    assert!(query.errors.is_empty());
    assert_eq!(
        query.data.into_json().unwrap()["household"],
        serde_json::Value::Null
    );

    let mutation = Executor::execute(
        &schema,
        Request::new(format!(
            "mutation {{ createPool(input: {{ householdId: \"{household_id}\", name: \"Unauthorized pool\", kind: PERSONAL }}) {{ __typename ... on CreatePoolUserError {{ code }} }} }}"
        ))
        .data(Actor(actor)),
    )
    .await;
    assert!(mutation.errors.is_empty());
    assert_eq!(
        mutation.data.into_json().unwrap()["createPool"]["code"],
        "NOT_FOUND"
    );
    let mut conn = pool.get().await.unwrap();
    let pool_count = horse_postgres_common::schema::public::pools::table
        .filter(horse_postgres_common::schema::public::pools::household_id.eq(target_id))
        .select(count_star())
        .first::<i64>(&mut conn)
        .await
        .unwrap();
    assert_eq!(pool_count, 0);

    let member_query = Executor::execute(
        &schema,
        Request::new(format!(
            "{{ household(id: \"{household_id}\") {{ id name }} }}"
        ))
        .data(Actor(target_member)),
    )
    .await;
    assert!(member_query.errors.is_empty());
    assert_eq!(
        member_query.data.into_json().unwrap()["household"]["id"],
        household_id
    );

    let member_mutation = Executor::execute(
        &schema,
        Request::new(format!(
            "mutation {{ createPool(input: {{ householdId: \"{household_id}\", name: \"Member pool\", kind: PERSONAL }}) {{ __typename ... on CreatePoolUserError {{ code }} }} }}"
        ))
        .data(Actor(target_member)),
    )
    .await;
    assert!(member_mutation.errors.is_empty());
    assert_eq!(
        member_mutation.data.into_json().unwrap()["createPool"]["code"],
        "NOT_FOUND"
    );

    let owner_mutation = Executor::execute(
        &schema,
        Request::new(format!(
            "mutation {{ createPool(input: {{ householdId: \"{household_id}\", name: \"Owner pool\", kind: PERSONAL }}) {{ __typename ... on CreatePoolSuccess {{ result {{ name }} }} }} }}"
        ))
        .data(Actor(target_owner)),
    )
    .await;
    assert!(owner_mutation.errors.is_empty());
    assert_eq!(
        owner_mutation.data.into_json().unwrap()["createPool"]["result"]["name"],
        "Owner pool"
    );
    let mut conn = pool.get().await.unwrap();
    let pool_count = horse_postgres_common::schema::public::pools::table
        .filter(horse_postgres_common::schema::public::pools::household_id.eq(target_id))
        .select(count_star())
        .first::<i64>(&mut conn)
        .await
        .unwrap();
    assert_eq!(pool_count, 1);

    postgres.terminate().await.unwrap();
}
