use diesel::{ExpressionMethods, QueryDsl, dsl::count_star, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::{
    TransactionError, connect, schema::public::households, with_transaction,
};
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

#[tokio::test]
async fn workflow_error_rolls_back_inserted_household() {
    let postgres = TestPostgres::start().await.expect("start PostgreSQL");
    apply_migrations(postgres.connection_url()).expect("apply migrations");
    let pool = connect(postgres.connection_url()).await.expect("connect");
    let household_id = Uuid::now_v7();

    let result = with_transaction::<(), _>(&pool, |conn| {
        Box::pin(async move {
            insert_into(households::table)
                .values((
                    households::id.eq(household_id),
                    households::name.eq("rollback regression"),
                ))
                .execute(conn)
                .await
                .map_err(TransactionError::Database)?;
            Err(TransactionError::Workflow("workflow failure"))
        })
    })
    .await;

    assert!(matches!(
        result,
        Err(TransactionError::Workflow("workflow failure"))
    ));

    let count = households::table
        .filter(households::id.eq(household_id))
        .select(count_star())
        .get_result::<i64>(&mut pool.get().await.expect("get connection"))
        .await
        .expect("count household");
    assert_eq!(count, 0);
    postgres.terminate().await.expect("stop PostgreSQL");
}
