use std::future::Future;

use bookkeeping_application::post_purchase::PostPurchaseWorkflowCommand;
use bookkeeping_postgres::{
    account_balances::account_balances,
    post_purchase::post_purchase_workflow,
    post_purchase_loader::{FundingRuleSnapshot, PostPurchaseReferenceData},
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

struct FundingRules {
    owner: Uuid,
    shared_pool: Uuid,
    first_member: Uuid,
    second_member: Uuid,
}

impl PostPurchaseReferenceData for FundingRules {
    fn load_funding_rule(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        pool_id: Uuid,
        _version: i32,
    ) -> impl Future<Output = Result<Option<FundingRuleSnapshot>, diesel::result::Error>> + Send
    {
        let result = if pool_id == self.shared_pool {
            Some((
                None,
                Some("BALANCE_TO_STANDARD".to_owned()),
                vec![(self.first_member, 60), (self.second_member, 40)],
            ))
        } else {
            Some((Some(self.owner), None, Vec::new()))
        };
        async move { Ok(result) }
    }
}

#[async_trait::async_trait]
impl PolicySettlementData for FundingRules {
    async fn household_members_for_settlement(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
    ) -> Result<Vec<(Uuid, String, Option<chrono::DateTime<Utc>>)>, diesel::result::Error> {
        Ok(vec![
            (self.first_member, "A".to_owned(), None),
            (self.second_member, "B".to_owned(), None),
        ])
    }

    async fn household_members_present_after(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        _period_start_utc: chrono::DateTime<Utc>,
    ) -> Result<Vec<(Uuid, String)>, diesel::result::Error> {
        Ok(vec![
            (self.first_member, "A".to_owned()),
            (self.second_member, "B".to_owned()),
        ])
    }

    async fn schedules_for_period(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        _start: NaiveDate,
        _end: NaiveDate,
    ) -> Result<Vec<FundingScheduleRow>, diesel::result::Error> {
        Ok(Vec::new())
    }

    async fn schedules_overlapping(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_ids: &[Uuid],
        _start: NaiveDate,
        _end: NaiveDate,
    ) -> Result<Vec<HouseholdFundingScheduleRow>, diesel::result::Error> {
        Ok(Vec::new())
    }

    async fn earliest_schedule_start(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
    ) -> Result<Option<NaiveDate>, diesel::result::Error> {
        Ok(None)
    }

    async fn rules_and_shares(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
    ) -> Result<(Vec<FundingRuleRow>, Vec<FundingShareRow>), diesel::result::Error> {
        Ok((
            vec![(
                self.shared_pool,
                1,
                None,
                Some("BALANCE_TO_STANDARD".to_owned()),
            )],
            vec![
                (self.shared_pool, 1, self.first_member, 60),
                (self.shared_pool, 1, self.second_member, 40),
            ],
        ))
    }

    async fn historical_rules(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        pool_ids: &[Uuid],
        versions: &[i32],
    ) -> Result<Vec<FundingRuleRow>, diesel::result::Error> {
        self.rules_and_shares(conn, household_id)
            .await
            .map(|(rules, _)| {
                rules
                    .into_iter()
                    .filter(|(pool, version, _, _)| {
                        pool_ids.contains(pool) && versions.contains(version)
                    })
                    .collect()
            })
    }

    async fn rule_owner(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        pool_id: Uuid,
        _version: i32,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        Ok((pool_id != self.shared_pool).then_some(self.owner))
    }
}

#[tokio::test]
async fn personal_card_payment_of_shared_purchase_persists_balanced_journal_balance_and_settlement()
{
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();

    let household_id = Uuid::now_v7();
    let first_member = Uuid::now_v7();
    let second_member = Uuid::now_v7();
    let shared_pool = Uuid::now_v7();
    let personal_pool = Uuid::now_v7();
    let account_id = Uuid::now_v7();
    let purchase_id = Uuid::now_v7();
    let item_id = Uuid::now_v7();
    let occurred_at = Utc.with_ymd_and_hms(2026, 10, 16, 12, 0, 0).unwrap();

    {
        let mut conn = pool.get().await.unwrap();
        use public::{
            accounts, household_members, households, members, pool_funding_rules, pools,
            purchase_allocations, purchase_items, purchase_payments, purchases,
        };

        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Settlement test"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        for (member_id, label) in [(first_member, "A"), (second_member, "B")] {
            insert_into(members::table)
                .values((
                    members::id.eq(member_id),
                    members::oidc_subject.eq(format!("shared-purchase-{member_id}")),
                    members::display_name.eq(label),
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
        }
        for (pool_id, name, kind) in [
            (shared_pool, "Shared", "SHARED"),
            (personal_pool, "A personal", "PERSONAL"),
        ] {
            insert_into(pools::table)
                .values((
                    pools::id.eq(pool_id),
                    pools::household_id.eq(household_id),
                    pools::name.eq(name),
                    pools::kind.eq(kind),
                    pools::current_funding_version.eq(None::<i32>),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
        }
        insert_into(pool_funding_rules::table)
            .values(vec![
                (
                    pool_funding_rules::household_id.eq(household_id),
                    pool_funding_rules::pool_id.eq(shared_pool),
                    pool_funding_rules::version.eq(1),
                    pool_funding_rules::owner_member_id.eq(None::<Uuid>),
                    pool_funding_rules::settlement_policy.eq(Some("BALANCE_TO_STANDARD")),
                    pool_funding_rules::created_by.eq(first_member),
                ),
                (
                    pool_funding_rules::household_id.eq(household_id),
                    pool_funding_rules::pool_id.eq(personal_pool),
                    pool_funding_rules::version.eq(1),
                    pool_funding_rules::owner_member_id.eq(Some(first_member)),
                    pool_funding_rules::settlement_policy.eq(None::<&str>),
                    pool_funding_rules::created_by.eq(first_member),
                ),
            ])
            .execute(&mut *conn)
            .await
            .unwrap();
        for pool_id in [shared_pool, personal_pool] {
            diesel::update(pools::table.filter(pools::id.eq(pool_id)))
                .set(pools::current_funding_version.eq(Some(1)))
                .execute(&mut *conn)
                .await
                .unwrap();
        }
        insert_into(accounts::table)
            .values((
                accounts::id.eq(account_id),
                accounts::household_id.eq(household_id),
                accounts::pool_id.eq(Some(personal_pool)),
                accounts::name.eq("A card"),
                accounts::kind.eq("CARD"),
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
                purchases::created_by.eq(first_member),
                purchases::allocation_rule_version.eq(1_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(purchase_items::table)
            .values((
                purchase_items::id.eq(item_id),
                purchase_items::household_id.eq(household_id),
                purchase_items::purchase_id.eq(purchase_id),
                purchase_items::name.eq("Shared groceries"),
                purchase_items::amount_yen.eq(1_000_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(purchase_allocations::table)
            .values((
                purchase_allocations::household_id.eq(household_id),
                purchase_allocations::purchase_id.eq(purchase_id),
                purchase_allocations::item_id.eq(item_id),
                purchase_allocations::pool_id.eq(shared_pool),
                purchase_allocations::amount_yen.eq(1_000_i64),
                purchase_allocations::weight.eq(1_i64),
                purchase_allocations::rule_version.eq(1_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(purchase_payments::table)
            .values((
                purchase_payments::household_id.eq(household_id),
                purchase_payments::purchase_id.eq(purchase_id),
                purchase_payments::line_no.eq(1_i32),
                purchase_payments::account_id.eq(account_id),
                purchase_payments::amount_yen.eq(1_000_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
    }

    let mut rules = FundingRules {
        owner: first_member,
        shared_pool,
        first_member,
        second_member,
    };
    post_purchase_workflow(
        &pool,
        PostPurchaseWorkflowCommand {
            household_id: household_id.to_string(),
            actor_id: first_member.to_string(),
            purchase_id: purchase_id.to_string(),
            expected_revision: 1,
            ledger_description: "Market purchase".to_owned(),
            idempotency_key: "personal-card-shared-purchase".to_owned(),
            request_fingerprint: [3; 32],
        },
        &mut rules,
    )
    .await
    .unwrap();

    let balances = account_balances(&pool, household_id).await.unwrap();
    assert_eq!(balances.len(), 1);
    assert_eq!(balances[0].account_id, account_id);
    assert_eq!(balances[0].balance_yen, 1_000);

    let mut conn = pool.get().await.unwrap();
    let burdens = public::purchase_member_burdens::table
        .filter(public::purchase_member_burdens::household_id.eq(household_id))
        .filter(public::purchase_member_burdens::purchase_id.eq(purchase_id))
        .order(public::purchase_member_burdens::member_id)
        .select((
            public::purchase_member_burdens::member_id,
            public::purchase_member_burdens::amount_yen,
        ))
        .load::<(Uuid, i64)>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(burdens, vec![(first_member, 600), (second_member, 400)]);

    let preview = calculate_preview(&pool, &household_id.to_string(), "2026-10", &mut rules)
        .await
        .unwrap();
    let first = preview
        .iter()
        .find(|line| line.member_id == first_member.to_string())
        .unwrap();
    let second = preview
        .iter()
        .find(|line| line.member_id == second_member.to_string())
        .unwrap();
    assert_eq!(first.advance_credit_yen, 400);
    assert_eq!(first.amount_due_yen, -400);
    assert_eq!(second.monthly_adjustment_yen, 400);
    assert_eq!(second.amount_due_yen, 400);

    let journal = public::journals::table
        .filter(public::journals::source_kind.eq("purchase"))
        .filter(public::journals::source_id.eq(purchase_id))
        .select(public::journals::id)
        .first::<Uuid>(&mut *conn)
        .await
        .unwrap();
    let entries = public::journal_entries::table
        .filter(public::journal_entries::journal_id.eq(journal))
        .select((
            public::journal_entries::debit_yen,
            public::journal_entries::credit_yen,
        ))
        .load::<(i64, i64)>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(entries.iter().map(|line| line.0).sum::<i64>(), 1_000);
    assert_eq!(entries.iter().map(|line| line.1).sum::<i64>(), 1_000);
}
