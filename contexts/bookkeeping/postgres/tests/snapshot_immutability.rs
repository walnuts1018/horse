use bookkeeping_postgres::schema::public;
use chrono::{NaiveDate, TimeZone, Utc};
use diesel::{ExpressionMethods, QueryDsl, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::connect;
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

#[tokio::test]
async fn funding_configuration_updates_preserve_purchase_and_closed_settlement_snapshots() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();
    let household_id = Uuid::now_v7();
    let owner_id = Uuid::now_v7();
    let other_member_id = Uuid::now_v7();
    let pool_id = Uuid::now_v7();
    let purchase_id = Uuid::now_v7();
    let item_id = Uuid::now_v7();
    let schedule_id = Uuid::now_v7();
    let run_id = Uuid::now_v7();
    let occurred_at = Utc.with_ymd_and_hms(2026, 10, 16, 12, 0, 0).unwrap();
    let period_start = NaiveDate::from_ymd_opt(2026, 10, 1).unwrap();

    {
        let mut conn = pool.get().await.unwrap();
        use public::{household_members, households, members, pool_funding_rules, pools};

        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Snapshot immutability"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        for (member_id, subject, display_name) in [
            (owner_id, "snapshot-owner", "Owner"),
            (other_member_id, "snapshot-member", "Member"),
        ] {
            insert_into(members::table)
                .values((
                    members::id.eq(member_id),
                    members::oidc_subject.eq(subject),
                    members::display_name.eq(display_name),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            insert_into(household_members::table)
                .values((
                    household_members::household_id.eq(household_id),
                    household_members::member_id.eq(member_id),
                    household_members::role.eq(if member_id == owner_id {
                        "OWNER"
                    } else {
                        "MEMBER"
                    }),
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
                pools::current_funding_version.eq(None::<i32>),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(pool_funding_rules::table)
            .values((
                pool_funding_rules::household_id.eq(household_id),
                pool_funding_rules::pool_id.eq(pool_id),
                pool_funding_rules::version.eq(1),
                pool_funding_rules::owner_member_id.eq(None::<Uuid>),
                pool_funding_rules::settlement_policy.eq(None::<String>),
                pool_funding_rules::created_by.eq(owner_id),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(public::pool_funding_shares::table)
            .values((
                public::pool_funding_shares::household_id.eq(household_id),
                public::pool_funding_shares::pool_id.eq(pool_id),
                public::pool_funding_shares::version.eq(1),
                public::pool_funding_shares::member_id.eq(owner_id),
                public::pool_funding_shares::weight.eq(1_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        diesel::update(pools::table.filter(pools::id.eq(pool_id)))
            .set(pools::current_funding_version.eq(Some(1)))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(public::purchases::table)
            .values((
                public::purchases::id.eq(purchase_id),
                public::purchases::household_id.eq(household_id),
                public::purchases::occurred_at.eq(occurred_at),
                public::purchases::merchant.eq("Market"),
                public::purchases::status.eq("POSTED"),
                public::purchases::revision.eq(2_i64),
                public::purchases::created_by.eq(owner_id),
                public::purchases::allocation_rule_version.eq(1_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(public::purchase_items::table)
            .values((
                public::purchase_items::id.eq(item_id),
                public::purchase_items::household_id.eq(household_id),
                public::purchase_items::purchase_id.eq(purchase_id),
                public::purchase_items::name.eq("Groceries"),
                public::purchase_items::amount_yen.eq(1_200_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(public::purchase_member_burdens::table)
            .values((
                public::purchase_member_burdens::household_id.eq(household_id),
                public::purchase_member_burdens::purchase_id.eq(purchase_id),
                public::purchase_member_burdens::item_id.eq(item_id),
                public::purchase_member_burdens::pool_id.eq(pool_id),
                public::purchase_member_burdens::member_id.eq(owner_id),
                public::purchase_member_burdens::amount_yen.eq(1_200_i64),
                public::purchase_member_burdens::funding_rule_version.eq(1_i32),
                public::purchase_member_burdens::funding_weight.eq(1_i64),
                public::purchase_member_burdens::settlement_policy.eq(None::<String>),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(public::funding_schedules::table)
            .values((
                public::funding_schedules::id.eq(schedule_id),
                public::funding_schedules::household_id.eq(household_id),
                public::funding_schedules::member_id.eq(owner_id),
                public::funding_schedules::pool_id.eq(pool_id),
                public::funding_schedules::amount_yen.eq(10_000_i64),
                public::funding_schedules::cadence.eq("MONTHLY"),
                public::funding_schedules::starts_on.eq(period_start),
                public::funding_schedules::ends_on.eq(None::<NaiveDate>),
                public::funding_schedules::active.eq(true),
                public::funding_schedules::created_at.eq(occurred_at),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(public::accounting_periods::table)
            .values((
                public::accounting_periods::household_id.eq(household_id),
                public::accounting_periods::period_start.eq(period_start),
                public::accounting_periods::status.eq("CLOSED"),
                public::accounting_periods::revision.eq(2_i64),
                public::accounting_periods::closed_by.eq(Some(owner_id)),
                public::accounting_periods::closed_at.eq(Some(occurred_at)),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        let funding_snapshot = serde_json::json!({
            pool_id.to_string(): {
                "Version": 1,
                "FundingShares": [{"MemberID": owner_id.to_string(), "Weight": 1}]
            }
        });
        insert_into(public::settlement_runs::table)
            .values((
                public::settlement_runs::id.eq(run_id),
                public::settlement_runs::household_id.eq(household_id),
                public::settlement_runs::period_start.eq(period_start),
                public::settlement_runs::revision.eq(2_i64),
                public::settlement_runs::status.eq("CLOSED"),
                public::settlement_runs::funding_snapshot.eq(funding_snapshot),
                public::settlement_runs::actor_id.eq(owner_id),
                public::settlement_runs::created_at.eq(occurred_at),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(public::settlement_run_lines::table)
            .values((
                public::settlement_run_lines::household_id.eq(household_id),
                public::settlement_run_lines::settlement_run_id.eq(run_id),
                public::settlement_run_lines::member_id.eq(owner_id),
                public::settlement_run_lines::member_name.eq("Owner"),
                public::settlement_run_lines::scheduled_yen.eq(10_000_i64),
                public::settlement_run_lines::paid_yen.eq(0_i64),
                public::settlement_run_lines::carry_yen.eq(0_i64),
                public::settlement_run_lines::monthly_adjustment_yen.eq(0_i64),
                public::settlement_run_lines::advance_credit_yen.eq(0_i64),
                public::settlement_run_lines::amount_due_yen.eq(10_000_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
    }

    {
        let mut conn = pool.get().await.unwrap();
        use public::{pool_funding_rules, pools};
        insert_into(pool_funding_rules::table)
            .values((
                pool_funding_rules::household_id.eq(household_id),
                pool_funding_rules::pool_id.eq(pool_id),
                pool_funding_rules::version.eq(2),
                pool_funding_rules::owner_member_id.eq(None::<Uuid>),
                pool_funding_rules::settlement_policy.eq(None::<String>),
                pool_funding_rules::created_by.eq(owner_id),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(public::pool_funding_shares::table)
            .values((
                public::pool_funding_shares::household_id.eq(household_id),
                public::pool_funding_shares::pool_id.eq(pool_id),
                public::pool_funding_shares::version.eq(2),
                public::pool_funding_shares::member_id.eq(other_member_id),
                public::pool_funding_shares::weight.eq(1_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        diesel::update(pools::table.filter(pools::id.eq(pool_id)))
            .set(pools::current_funding_version.eq(Some(2)))
            .execute(&mut *conn)
            .await
            .unwrap();
        diesel::update(
            public::funding_schedules::table.filter(public::funding_schedules::id.eq(schedule_id)),
        )
        .set(public::funding_schedules::amount_yen.eq(20_000_i64))
        .execute(&mut *conn)
        .await
        .unwrap();

        let burden = public::purchase_member_burdens::table
            .filter(public::purchase_member_burdens::purchase_id.eq(purchase_id))
            .select((
                public::purchase_member_burdens::member_id,
                public::purchase_member_burdens::amount_yen,
                public::purchase_member_burdens::funding_rule_version,
            ))
            .first::<(Uuid, i64, i32)>(&mut *conn)
            .await
            .unwrap();
        assert_eq!(burden, (owner_id, 1_200, 1));

        let settlement = public::settlement_runs::table
            .filter(public::settlement_runs::id.eq(run_id))
            .select(public::settlement_runs::funding_snapshot)
            .first::<serde_json::Value>(&mut *conn)
            .await
            .unwrap();
        assert_eq!(settlement[pool_id.to_string()]["Version"], 1);
        assert_eq!(
            settlement[pool_id.to_string()]["FundingShares"][0]["MemberID"],
            owner_id.to_string()
        );
        let settlement_line = public::settlement_run_lines::table
            .filter(public::settlement_run_lines::settlement_run_id.eq(run_id))
            .select((
                public::settlement_run_lines::scheduled_yen,
                public::settlement_run_lines::amount_due_yen,
            ))
            .first::<(i64, i64)>(&mut *conn)
            .await
            .unwrap();
        assert_eq!(settlement_line, (10_000, 10_000));
    }
}
