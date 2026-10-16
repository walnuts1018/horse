use bookkeeping_postgres::{
    schema::public,
    settlement_preview::{
        FundingRuleRow, FundingScheduleRow, HouseholdFundingScheduleRow, PolicySettlementData,
        calculate_preview,
    },
};
use chrono::{NaiveDate, TimeZone, Utc};
use diesel::{ExpressionMethods, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::{AsyncPgConnection, connect};
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

struct OneMemberPolicy {
    member_id: Uuid,
}

#[async_trait::async_trait]
impl PolicySettlementData for OneMemberPolicy {
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
        _: Uuid,
        _: NaiveDate,
        _: NaiveDate,
    ) -> Result<Vec<FundingScheduleRow>, diesel::result::Error> {
        Ok(Vec::new())
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
        Ok(None)
    }

    async fn rules_and_shares(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
    ) -> Result<
        (
            Vec<FundingRuleRow>,
            Vec<bookkeeping_postgres::settlement_preview::FundingShareRow>,
        ),
        diesel::result::Error,
    > {
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
async fn preview_carries_forward_only_the_latest_closed_balance() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();
    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    let now = Utc.with_ymd_and_hms(2026, 11, 1, 0, 0, 0).unwrap();

    {
        let mut conn = pool.get().await.unwrap();
        use public::{
            household_members, households, members, settlement_run_lines, settlement_runs,
        };

        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Settlement carry"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(member_id),
                members::oidc_subject.eq("settlement-carry"),
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

        for (period_start, amount_due_yen) in [
            (NaiveDate::from_ymd_opt(2026, 9, 1).unwrap(), 9_000),
            (NaiveDate::from_ymd_opt(2026, 10, 1).unwrap(), 2_500),
        ] {
            insert_into(public::accounting_periods::table)
                .values((
                    public::accounting_periods::household_id.eq(household_id),
                    public::accounting_periods::period_start.eq(period_start),
                    public::accounting_periods::status.eq("CLOSED"),
                    public::accounting_periods::revision.eq(1_i64),
                    public::accounting_periods::closed_by.eq(Some(member_id)),
                    public::accounting_periods::closed_at.eq(Some(now)),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            let run_id = Uuid::now_v7();
            insert_into(settlement_runs::table)
                .values((
                    settlement_runs::id.eq(run_id),
                    settlement_runs::household_id.eq(household_id),
                    settlement_runs::period_start.eq(period_start),
                    settlement_runs::revision.eq(1_i64),
                    settlement_runs::status.eq("CLOSED"),
                    settlement_runs::funding_snapshot.eq(serde_json::json!({})),
                    settlement_runs::actor_id.eq(member_id),
                    settlement_runs::created_at.eq(now),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            insert_into(settlement_run_lines::table)
                .values((
                    settlement_run_lines::household_id.eq(household_id),
                    settlement_run_lines::settlement_run_id.eq(run_id),
                    settlement_run_lines::member_id.eq(member_id),
                    settlement_run_lines::member_name.eq("Member"),
                    settlement_run_lines::scheduled_yen.eq(0_i64),
                    settlement_run_lines::paid_yen.eq(0_i64),
                    settlement_run_lines::carry_yen.eq(0_i64),
                    settlement_run_lines::monthly_adjustment_yen.eq(0_i64),
                    settlement_run_lines::advance_credit_yen.eq(0_i64),
                    settlement_run_lines::amount_due_yen.eq(amount_due_yen),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
        }
    }

    let mut policy = OneMemberPolicy { member_id };
    let result = calculate_preview(&pool, &household_id.to_string(), "2026-11", &mut policy)
        .await
        .unwrap();

    assert_eq!(result.len(), 1);
    assert_eq!(result[0].carry_yen, 2_500);
    assert_eq!(result[0].amount_due_yen, 2_500);
}
