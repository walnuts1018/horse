use bookkeeping_domain::create_account::{CreateAccountCommand, OpeningBalance};
use bookkeeping_postgres::{create_account::persist_create_account, schema::public};
use diesel::{ExpressionMethods, QueryDsl, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::connect;
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

#[tokio::test]
async fn create_account_persists_opening_balance_as_journal_entries() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();

    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    {
        let mut conn = pool.get().await.unwrap();
        use public::{household_members, households, members};

        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Opening balance test"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(member_id),
                members::oidc_subject.eq(format!("opening-balance-{member_id}")),
                members::display_name.eq("Opening balance test"),
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

    let account = persist_create_account(
        &pool,
        CreateAccountCommand {
            actor_id: member_id.to_string(),
            household_id: household_id.to_string(),
            name: "Checking account".to_owned(),
            kind: "BANK".to_owned(),
            card_last4: None,
            pool_id: None,
            opening_balance: Some(OpeningBalance {
                amount_yen: 42_500,
                at: "2026-10-16T12:00:00+09:00".to_owned(),
            }),
        },
    )
    .await
    .unwrap();

    let account_id = Uuid::parse_str(&account.id).unwrap();
    let mut conn = pool.get().await.unwrap();
    let journal_id = public::journals::table
        .filter(public::journals::household_id.eq(household_id))
        .filter(public::journals::source_kind.eq("ACCOUNT_OPENING_BALANCE"))
        .filter(public::journals::source_id.eq(account_id))
        .select(public::journals::id)
        .first::<Uuid>(&mut *conn)
        .await
        .unwrap();
    let entries = public::journal_entries::table
        .filter(public::journal_entries::household_id.eq(household_id))
        .filter(public::journal_entries::journal_id.eq(journal_id))
        .order(public::journal_entries::line_no)
        .select((
            public::journal_entries::account_id,
            public::journal_entries::debit_yen,
            public::journal_entries::credit_yen,
        ))
        .load::<(Option<Uuid>, i64, i64)>(&mut *conn)
        .await
        .unwrap();

    assert_eq!(
        entries,
        vec![(Some(account_id), 42_500, 0), (None, 0, 42_500),]
    );
}
