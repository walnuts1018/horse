use bookkeeping_application::funding_record::{RecordFundingCommand, RecordFundingWorkflowCommand};
use bookkeeping_domain::{AccountingPeriod, YearMonth};
use bookkeeping_postgres::{
    close_period, funding_record,
    policy_data::PolicyData,
    schema::public,
    settlement_preview::{
        FundingRuleRow, FundingScheduleRow, FundingShareRow, HouseholdFundingScheduleRow,
        PolicySettlementData, calculate_preview,
    },
};
use chrono::{NaiveDate, TimeZone, Utc};
use diesel::{ExpressionMethods, QueryDsl, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::{AsyncPgConnection, connect};
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

struct FundingPolicy {
    member_id: Uuid,
    schedule_id: Uuid,
    pool_id: Uuid,
}

#[async_trait::async_trait]
impl PolicyData for FundingPolicy {
    async fn pool_funding_owner(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
        _: Uuid,
        _: i32,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        Ok(None)
    }

    async fn active_funding_schedule(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
        _: Uuid,
        _: Uuid,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        Ok(Some(self.schedule_id))
    }
}

#[async_trait::async_trait]
impl PolicySettlementData for FundingPolicy {
    async fn household_members_for_settlement(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
    ) -> Result<Vec<(Uuid, String, Option<chrono::DateTime<Utc>>)>, diesel::result::Error> {
        Ok(vec![(self.member_id, "Member".to_owned(), None)])
    }

    async fn household_members_present_after(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
        _: chrono::DateTime<Utc>,
    ) -> Result<Vec<(Uuid, String)>, diesel::result::Error> {
        Ok(vec![(self.member_id, "Member".to_owned())])
    }

    async fn schedules_for_period(
        &mut self,
        _: &mut AsyncPgConnection,
        _household_id: Uuid,
        start: NaiveDate,
        end: NaiveDate,
    ) -> Result<Vec<FundingScheduleRow>, diesel::result::Error> {
        if start == NaiveDate::from_ymd_opt(2026, 9, 1).unwrap()
            || start == NaiveDate::from_ymd_opt(2026, 10, 1).unwrap()
        {
            Ok(vec![(
                self.schedule_id,
                self.member_id,
                self.pool_id,
                10_000,
                "MONTHLY".to_owned(),
                start,
                Some(end - chrono::Duration::days(1)),
                true,
                Utc.with_ymd_and_hms(2026, 8, 1, 0, 0, 0).unwrap(),
            )])
        } else {
            Ok(Vec::new())
        }
    }

    async fn schedules_overlapping(
        &mut self,
        _: &mut AsyncPgConnection,
        _: &[Uuid],
        _: NaiveDate,
        _: NaiveDate,
    ) -> Result<Vec<HouseholdFundingScheduleRow>, diesel::result::Error> {
        Ok(Vec::new())
    }

    async fn earliest_schedule_start(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
    ) -> Result<Option<NaiveDate>, diesel::result::Error> {
        Ok(Some(NaiveDate::from_ymd_opt(2026, 9, 1).unwrap()))
    }

    async fn rules_and_shares(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
    ) -> Result<(Vec<FundingRuleRow>, Vec<FundingShareRow>), diesel::result::Error> {
        Ok((Vec::new(), Vec::new()))
    }

    async fn historical_rules(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
        _: &[Uuid],
        _: &[i32],
    ) -> Result<Vec<FundingRuleRow>, diesel::result::Error> {
        Ok(Vec::new())
    }

    async fn rule_owner(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
        _: Uuid,
        _: i32,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        Ok(None)
    }
}

#[tokio::test]
async fn funding_overpayment_carries_through_closed_periods() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();
    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    let pool_id = Uuid::now_v7();
    let schedule_id = Uuid::now_v7();
    let now = Utc.with_ymd_and_hms(2026, 9, 10, 12, 0, 0).unwrap();

    {
        let mut conn = pool.get().await.unwrap();
        use public::{household_members, households, members, pools};
        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Funding carry"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(member_id),
                members::oidc_subject.eq("funding-carry"),
                members::display_name.eq("Member"),
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
        insert_into(public::funding_schedules::table)
            .values((
                public::funding_schedules::id.eq(schedule_id),
                public::funding_schedules::household_id.eq(household_id),
                public::funding_schedules::member_id.eq(member_id),
                public::funding_schedules::pool_id.eq(pool_id),
                public::funding_schedules::amount_yen.eq(10_000_i64),
                public::funding_schedules::cadence.eq("MONTHLY"),
                public::funding_schedules::starts_on
                    .eq(NaiveDate::from_ymd_opt(2026, 9, 1).unwrap()),
                public::funding_schedules::ends_on.eq(None::<NaiveDate>),
                public::funding_schedules::active.eq(true),
                public::funding_schedules::created_at.eq(now),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
    }

    let mut policy = FundingPolicy {
        member_id,
        schedule_id,
        pool_id,
    };
    let funding_command = RecordFundingWorkflowCommand {
        record: RecordFundingCommand {
            actor_id: member_id.to_string(),
            member_id: member_id.to_string(),
            pool_id: pool_id.to_string(),
            amount_yen: 15_000,
            accounting_period: AccountingPeriod::open(YearMonth::new(2026, 9).unwrap(), 1)
                .unwrap()
                .into_open()
                .unwrap(),
            source_account: None,
            destination_account: None,
        },
        household_id: household_id.to_string(),
        occurred_at: now,
        idempotency_key: "overpaid-september".into(),
        request_fingerprint: [9; 32],
        source_account_id: None,
        destination_account_id: None,
    };
    funding_record::record_funding_workflow(
        &pool,
        funding_command,
        FundingPolicy {
            member_id,
            schedule_id,
            pool_id,
        },
    )
    .await
    .unwrap();

    let september = close_period::close_period(
        &pool,
        &household_id.to_string(),
        &member_id.to_string(),
        "2026-09",
        1,
        &mut policy,
    )
    .await
    .unwrap();
    assert_eq!(september.period, "2026-09");
    let october = close_period::close_period(
        &pool,
        &household_id.to_string(),
        &member_id.to_string(),
        "2026-10",
        1,
        &mut policy,
    )
    .await
    .unwrap();
    assert_eq!(october.period, "2026-10");

    let preview = calculate_preview(&pool, &household_id.to_string(), "2026-11", &mut policy)
        .await
        .unwrap();
    assert_eq!(preview.len(), 1);
    assert_eq!(preview[0].scheduled_yen, 10_000);
    assert_eq!(preview[0].paid_yen, 0);
    assert_eq!(preview[0].carry_yen, 5_000);
    assert_eq!(preview[0].amount_due_yen, 15_000);

    let mut conn = pool.get().await.unwrap();
    let september_paid = public::funding_obligations::table
        .filter(public::funding_obligations::household_id.eq(household_id))
        .filter(
            public::funding_obligations::period_start
                .eq(NaiveDate::from_ymd_opt(2026, 9, 1).unwrap()),
        )
        .select((
            public::funding_obligations::amount_yen,
            public::funding_obligations::paid_yen,
        ))
        .first::<(i64, i64)>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(september_paid, (10_000, 15_000));
}
