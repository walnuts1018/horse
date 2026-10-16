use std::future::Future;

use bookkeeping_application::post_purchase::PostPurchaseWorkflowCommand;
use bookkeeping_postgres::{
    post_purchase::{find_idempotent_post_purchase, post_purchase_workflow},
    post_purchase_loader::{FundingRuleSnapshot, PostPurchaseReferenceData},
    schema::public,
};
use chrono::{TimeZone, Utc};
use diesel::{ExpressionMethods, QueryDsl, dsl::count_star, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::{AsyncPgConnection, connect};
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

struct PersonalFundingRule {
    owner_id: Uuid,
}

impl PostPurchaseReferenceData for PersonalFundingRule {
    fn load_funding_rule(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        _pool_id: Uuid,
        _version: i32,
    ) -> impl Future<Output = Result<Option<FundingRuleSnapshot>, diesel::result::Error>> + Send
    {
        let owner_id = self.owner_id;
        async move { Ok(Some((Some(owner_id), None, Vec::new()))) }
    }
}

#[tokio::test]
async fn post_purchase_workflow_persists_balanced_journal_and_purchase_snapshots() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();

    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    let pool_id = Uuid::now_v7();
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
                households::name.eq("Posting test"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(member_id),
                members::oidc_subject.eq(format!("posting-test-{member_id}")),
                members::display_name.eq("Posting test"),
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
                pools::name.eq("Personal"),
                pools::kind.eq("PERSONAL"),
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
                pool_funding_rules::owner_member_id.eq(Some(member_id)),
                pool_funding_rules::settlement_policy.eq(None::<String>),
                pool_funding_rules::created_by.eq(member_id),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        diesel::update(pools::table.filter(pools::id.eq(pool_id)))
            .set(pools::current_funding_version.eq(Some(1)))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(accounts::table)
            .values((
                accounts::id.eq(account_id),
                accounts::household_id.eq(household_id),
                accounts::pool_id.eq(Some(pool_id)),
                accounts::name.eq("Personal bank"),
                accounts::kind.eq("BANK"),
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
                purchases::created_by.eq(member_id),
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
                purchase_items::name.eq("Groceries"),
                purchase_items::amount_yen.eq(1200_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(purchase_allocations::table)
            .values((
                purchase_allocations::household_id.eq(household_id),
                purchase_allocations::purchase_id.eq(purchase_id),
                purchase_allocations::item_id.eq(item_id),
                purchase_allocations::pool_id.eq(pool_id),
                purchase_allocations::amount_yen.eq(1200_i64),
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
                purchase_payments::amount_yen.eq(1200_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
    }

    let command = PostPurchaseWorkflowCommand {
        household_id: household_id.to_string(),
        actor_id: member_id.to_string(),
        purchase_id: purchase_id.to_string(),
        expected_revision: 1,
        ledger_description: "Market purchase".to_owned(),
        idempotency_key: "post-purchase-persistence".to_owned(),
        request_fingerprint: [9; 32],
    };
    let mut reference_data = PersonalFundingRule {
        owner_id: member_id,
    };
    let result = post_purchase_workflow(&pool, command, &mut reference_data)
        .await
        .unwrap();

    assert_eq!(result.purchase_id, purchase_id.to_string());
    let replay =
        find_idempotent_post_purchase(&pool, household_id, "post-purchase-persistence", [9; 32])
            .await
            .unwrap()
            .unwrap();
    assert_eq!(replay.purchase_id, result.purchase_id);
    assert_eq!(replay.journal_id, result.journal_id);
    assert_eq!(replay.ledger_entry_id, result.ledger_entry_id);

    let mut conn = pool.get().await.unwrap();
    let purchase = public::purchases::table
        .filter(public::purchases::id.eq(purchase_id))
        .select((public::purchases::status, public::purchases::revision))
        .first::<(String, i64)>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(purchase, ("POSTED".to_owned(), 2));

    let journal_entries = public::journal_entries::table
        .filter(
            public::journal_entries::journal_id.eq(Uuid::parse_str(&result.journal_id).unwrap()),
        )
        .select((
            public::journal_entries::debit_yen,
            public::journal_entries::credit_yen,
        ))
        .load::<(i64, i64)>(&mut *conn)
        .await
        .unwrap();
    assert!(!journal_entries.is_empty());
    assert_eq!(
        journal_entries.iter().map(|entry| entry.0).sum::<i64>(),
        1200
    );
    assert_eq!(
        journal_entries.iter().map(|entry| entry.1).sum::<i64>(),
        1200
    );

    let payment_count = public::purchase_payments::table
        .filter(public::purchase_payments::purchase_id.eq(purchase_id))
        .filter(public::purchase_payments::account_id.eq(account_id))
        .filter(public::purchase_payments::amount_yen.eq(1200_i64))
        .select(count_star())
        .first::<i64>(&mut *conn)
        .await
        .unwrap();
    let allocation_count = public::purchase_allocations::table
        .filter(public::purchase_allocations::purchase_id.eq(purchase_id))
        .filter(public::purchase_allocations::item_id.eq(item_id))
        .filter(public::purchase_allocations::pool_id.eq(pool_id))
        .filter(public::purchase_allocations::amount_yen.eq(1200_i64))
        .select(count_star())
        .first::<i64>(&mut *conn)
        .await
        .unwrap();
    let burden_snapshot_count = public::purchase_member_burdens::table
        .filter(public::purchase_member_burdens::purchase_id.eq(purchase_id))
        .filter(public::purchase_member_burdens::member_id.eq(member_id))
        .filter(public::purchase_member_burdens::amount_yen.eq(1200_i64))
        .select(count_star())
        .first::<i64>(&mut *conn)
        .await
        .unwrap();
    let payment_snapshot_count = public::purchase_payment_funding_snapshots::table
        .filter(public::purchase_payment_funding_snapshots::purchase_id.eq(purchase_id))
        .filter(public::purchase_payment_funding_snapshots::payment_line_no.eq(1_i32))
        .filter(public::purchase_payment_funding_snapshots::member_id.eq(member_id))
        .select(count_star())
        .first::<i64>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(payment_count, 1);
    assert_eq!(allocation_count, 1);
    assert_eq!(burden_snapshot_count, 1);
    assert_eq!(payment_snapshot_count, 1);
}
