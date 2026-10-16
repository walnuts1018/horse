use bookkeeping_application::funding_record::{
    FundingAccount, RecordFundingCommand, RecordFundingWorkflowCommand,
};
use bookkeeping_domain::{AccountingPeriod, YearMonth};
use bookkeeping_postgres::{funding_record, policy_data::PolicyData, schema::public};
use chrono::{TimeZone, Utc};
use diesel::{ExpressionMethods, QueryDsl, dsl::count_star, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::{AsyncPgConnection, connect};
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

struct FailingFundingPolicy;

#[async_trait::async_trait]
impl PolicyData for FailingFundingPolicy {
    async fn pool_funding_owner(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        _pool_id: Uuid,
        _version: i32,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        Ok(None)
    }

    async fn active_funding_schedule(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        _member_id: Uuid,
        _pool_id: Uuid,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        Err(diesel::result::Error::RollbackTransaction)
    }
}

#[tokio::test]
async fn workflow_error_rolls_back_the_idempotency_record() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();

    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    let pool_id = Uuid::now_v7();
    let occurred_at = Utc.with_ymd_and_hms(2026, 10, 16, 12, 0, 0).unwrap();
    {
        let mut conn = pool.get().await.unwrap();
        use public::{household_members, households, members, pools};

        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Atomicity test"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(member_id),
                members::oidc_subject.eq("atomicity-test"),
                members::display_name.eq("Atomicity test"),
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
                pools::name.eq("Shared"),
                pools::kind.eq("SHARED"),
                pools::current_funding_version.eq(None::<i32>),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
    }

    let command = RecordFundingWorkflowCommand {
        record: RecordFundingCommand {
            actor_id: member_id.to_string(),
            member_id: member_id.to_string(),
            pool_id: pool_id.to_string(),
            amount_yen: 1200,
            accounting_period: AccountingPeriod::open(YearMonth::new(2026, 10).unwrap(), 1)
                .unwrap()
                .into_open()
                .unwrap(),
            source_account: None::<FundingAccount>,
            destination_account: None::<FundingAccount>,
        },
        household_id: household_id.to_string(),
        occurred_at,
        idempotency_key: "funding-atomicity".into(),
        request_fingerprint: [7; 32],
        source_account_id: None,
        destination_account_id: None,
    };

    let result =
        funding_record::record_funding_workflow(&pool, command, FailingFundingPolicy).await;
    assert!(result.is_err());

    let mut conn = pool.get().await.unwrap();
    let idempotency_records = public::idempotency_records::table
        .filter(public::idempotency_records::household_id.eq(household_id))
        .filter(public::idempotency_records::key.eq("funding-atomicity"))
        .select(count_star())
        .first::<i64>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(idempotency_records, 0);
}
