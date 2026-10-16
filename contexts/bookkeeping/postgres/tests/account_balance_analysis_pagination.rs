use bookkeeping_postgres::{
    account_balance_analysis::account_balance_analysis_for_period, schema::public,
};
use chrono::{TimeZone, Utc};
use diesel::{ExpressionMethods, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::connect;
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

#[tokio::test]
async fn account_balance_analysis_pages_changes_without_gaps_or_duplicates() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();
    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    let account_id = Uuid::now_v7();
    let start = Utc.with_ymd_and_hms(2026, 10, 1, 0, 0, 0).unwrap();
    let end = Utc.with_ymd_and_hms(2026, 11, 1, 0, 0, 0).unwrap();
    let fixtures = [
        (Utc.with_ymd_and_hms(2026, 9, 30, 12, 0, 0).unwrap(), 500),
        (Utc.with_ymd_and_hms(2026, 10, 2, 12, 0, 0).unwrap(), 100),
        (Utc.with_ymd_and_hms(2026, 10, 7, 12, 0, 0).unwrap(), -30),
        (Utc.with_ymd_and_hms(2026, 10, 14, 12, 0, 0).unwrap(), 200),
        (Utc.with_ymd_and_hms(2026, 10, 21, 12, 0, 0).unwrap(), 20),
        (Utc.with_ymd_and_hms(2026, 10, 28, 12, 0, 0).unwrap(), -10),
    ];

    {
        let mut conn = pool.get().await.unwrap();
        use public::{accounts, household_members, households, members};
        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Balance pagination"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(member_id),
                members::oidc_subject.eq(format!("balance-pagination-{member_id}")),
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
        insert_into(accounts::table)
            .values((
                accounts::id.eq(account_id),
                accounts::household_id.eq(household_id),
                accounts::pool_id.eq(None::<Uuid>),
                accounts::name.eq("Checking"),
                accounts::kind.eq("BANK"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();

        for (index, (occurred_at, amount)) in fixtures.iter().enumerate() {
            let journal_id = Uuid::now_v7();
            let source_id = Uuid::now_v7();
            insert_into(public::journals::table)
                .values((
                    public::journals::id.eq(journal_id),
                    public::journals::household_id.eq(household_id),
                    public::journals::source_kind.eq("BALANCE_PAGINATION_TEST"),
                    public::journals::source_id.eq(source_id),
                    public::journals::idempotency_key.eq(format!("balance-page-{index}")),
                    public::journals::occurred_at.eq(*occurred_at),
                    public::journals::created_by.eq(member_id),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            insert_into(public::journal_entries::table)
                .values((
                    public::journal_entries::household_id.eq(household_id),
                    public::journal_entries::journal_id.eq(journal_id),
                    public::journal_entries::line_no.eq(1),
                    public::journal_entries::account_id.eq(Some(account_id)),
                    public::journal_entries::pool_id.eq(None::<Uuid>),
                    public::journal_entries::member_id.eq(None::<Uuid>),
                    public::journal_entries::debit_yen.eq(if amount > &0 { *amount } else { 0 }),
                    public::journal_entries::credit_yen.eq(if amount < &0 {
                        amount.abs()
                    } else {
                        0
                    }),
                    public::journal_entries::memo.eq(""),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
        }
    }

    let mut after = None;
    let mut pages = Vec::new();
    loop {
        let page = account_balance_analysis_for_period(
            &pool,
            household_id,
            account_id,
            start,
            end,
            after,
            2,
        )
        .await
        .unwrap();
        assert_eq!(page.opening_balance_yen, 500);
        assert_eq!(page.closing_balance_yen, 780);
        assert_eq!(page.total_count, 5);
        after = page.changes.last().map(|change| change.journal_id);
        let has_next_page = page.has_next_page;
        pages.extend(page.changes);
        if !has_next_page {
            break;
        }
    }

    assert_eq!(pages.len(), 5);
    assert_eq!(
        pages
            .iter()
            .map(|change| change.amount_yen)
            .collect::<Vec<_>>(),
        vec![-10, 20, 200, -30, 100]
    );
    assert_eq!(
        pages
            .iter()
            .map(|change| change.balance_yen)
            .collect::<Vec<_>>(),
        vec![780, 790, 770, 570, 600]
    );
    let unique_journals = pages
        .iter()
        .map(|change| change.journal_id)
        .collect::<std::collections::HashSet<_>>();
    assert_eq!(unique_journals.len(), pages.len());
}
