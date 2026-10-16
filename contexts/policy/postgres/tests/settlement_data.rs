use chrono::NaiveDate;
use diesel::{ExpressionMethods, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::connect;
use horse_test_infra::{TestPostgres, apply_migrations};
use policy_application::settlement_data::SettlementDataRead;
use policy_postgres::settlement_data::PostgresSettlementDataRead;
use uuid::Uuid;

#[tokio::test]
async fn settlement_data_reads_policy_owned_schedules_and_funding_rules() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();
    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    let pool_id = Uuid::now_v7();
    let schedule_id = Uuid::now_v7();
    let household_ids = [household_id];
    let owner_id = Uuid::now_v7();
    let start = NaiveDate::from_ymd_opt(2026, 1, 1).unwrap();
    let end = NaiveDate::from_ymd_opt(2026, 2, 1).unwrap();

    {
        let mut conn = pool.get().await.unwrap();
        use horse_postgres_common::schema::public::{
            funding_schedules, household_members, households, members, pool_funding_rules,
            pool_funding_shares, pools,
        };
        insert_into(members::table)
            .values((
                members::id.eq(member_id),
                members::oidc_subject.eq(format!("member-{member_id}")),
                members::display_name.eq("Member"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(owner_id),
                members::oidc_subject.eq(format!("member-{owner_id}")),
                members::display_name.eq("Owner"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Settlement"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        for id in [member_id, owner_id] {
            insert_into(household_members::table)
                .values((
                    household_members::household_id.eq(household_id),
                    household_members::member_id.eq(id),
                    household_members::role.eq("OWNER"),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
        }
        insert_into(pools::table)
            .values((
                pools::id.eq(pool_id),
                pools::household_id.eq(household_id),
                pools::name.eq("Shared"),
                pools::kind.eq("SHARED"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(pool_funding_rules::table)
            .values((
                pool_funding_rules::household_id.eq(household_id),
                pool_funding_rules::pool_id.eq(pool_id),
                pool_funding_rules::version.eq(1),
                pool_funding_rules::settlement_policy.eq(Some("BALANCE_TO_STANDARD".to_owned())),
                pool_funding_rules::created_by.eq(owner_id),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(pool_funding_rules::table)
            .values((
                pool_funding_rules::household_id.eq(household_id),
                pool_funding_rules::pool_id.eq(pool_id),
                pool_funding_rules::version.eq(2),
                pool_funding_rules::owner_member_id.eq(Some(owner_id)),
                pool_funding_rules::created_by.eq(owner_id),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(pool_funding_shares::table)
            .values((
                pool_funding_shares::household_id.eq(household_id),
                pool_funding_shares::pool_id.eq(pool_id),
                pool_funding_shares::version.eq(1),
                pool_funding_shares::member_id.eq(member_id),
                pool_funding_shares::weight.eq(3_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(funding_schedules::table)
            .values((
                funding_schedules::id.eq(schedule_id),
                funding_schedules::household_id.eq(household_id),
                funding_schedules::member_id.eq(member_id),
                funding_schedules::pool_id.eq(pool_id),
                funding_schedules::amount_yen.eq(12000_i64),
                funding_schedules::cadence.eq("MONTHLY"),
                funding_schedules::starts_on.eq(start),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
    }

    let reader = PostgresSettlementDataRead::new();
    let mut conn = pool.get().await.unwrap();
    let schedules = reader
        .schedules_for_period(&mut *conn, household_id, start, end)
        .await
        .unwrap();
    assert_eq!(schedules.len(), 1);
    assert_eq!(schedules[0].id, schedule_id);
    assert_eq!(schedules[0].amount_yen, 12000);
    assert_eq!(
        reader
            .earliest_schedule_start(&mut *conn, household_id)
            .await
            .unwrap(),
        Some(start)
    );
    let policy = reader
        .rules_and_shares(&mut *conn, household_id)
        .await
        .unwrap();
    assert_eq!(policy.rules[0].owner_member_id, None);
    assert_eq!(
        policy.rules[0].settlement_policy.as_deref(),
        Some("BALANCE_TO_STANDARD")
    );
    assert_eq!(policy.shares[0].weight, 3);
    assert_eq!(
        reader
            .pool_funding_owner(&mut *conn, household_id, pool_id, 2)
            .await
            .unwrap(),
        Some(owner_id)
    );
    assert_eq!(
        reader
            .active_funding_schedule(&mut *conn, household_id, member_id, pool_id)
            .await
            .unwrap(),
        Some(schedule_id)
    );
    let overlap = reader
        .schedules_overlapping(&mut *conn, &household_ids, start, end)
        .await
        .unwrap();
    assert_eq!(overlap[0].household_id, household_id);
    assert_eq!(
        reader
            .historical_rules(&mut *conn, household_id, &[pool_id], &[1, 2])
            .await
            .unwrap()
            .len(),
        2
    );
}
