use bookkeeping_domain::purchase_commands::{
    UpdatePurchaseCommand, UpdatePurchaseDraft, decide_update_purchase,
};
use bookkeeping_domain::{AccountingPeriod, PurchaseLifecycle, YearMonth};
use bookkeeping_postgres::{post_purchase_loader::PostPurchaseReferenceData, schema::public};
use diesel::{ExpressionMethods, QueryDsl, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::{AsyncPgConnection, connect};
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

struct NoFundingRules;

impl PostPurchaseReferenceData for NoFundingRules {
    async fn load_funding_rule(
        &mut self,
        _conn: &mut AsyncPgConnection,
        _household_id: Uuid,
        _pool_id: Uuid,
        _version: i32,
    ) -> Result<
        Option<bookkeeping_postgres::post_purchase_loader::FundingRuleSnapshot>,
        diesel::result::Error,
    > {
        Ok(None)
    }
}

fn draft(revision: u64, merchant: &str) -> UpdatePurchaseDraft {
    UpdatePurchaseDraft {
        revision,
        lifecycle: PurchaseLifecycle::Draft,
        accounting_period: AccountingPeriod::open(YearMonth::new(2026, 10).unwrap(), 1)
            .unwrap()
            .into_open()
            .unwrap(),
        occurred_at: "2026-10-16T12:00:00Z".to_owned(),
        date_precision: "DATE_TIME".to_owned(),
        merchant: merchant.to_owned(),
        field_revisions: Default::default(),
    }
}

fn command(
    purchase_id: Uuid,
    base_revision: u64,
    changed_field: &str,
    occurred_at: Option<&str>,
    date_precision: Option<&str>,
    merchant: Option<&str>,
) -> UpdatePurchaseCommand {
    UpdatePurchaseCommand {
        purchase_id: purchase_id.to_string(),
        base_revision,
        changed_fields: vec![changed_field.to_owned()],
        occurred_at: occurred_at.map(str::to_owned),
        date_precision: date_precision.map(str::to_owned),
        merchant: merchant.map(str::to_owned),
        payment_allocations: None,
    }
}

#[tokio::test]
async fn concurrent_purchase_updates_merge_disjoint_fields_and_reject_same_field() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();

    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    let purchase_id = Uuid::now_v7();
    {
        let mut conn = pool.get().await.unwrap();
        use public::{household_members, households, members, purchases};

        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Purchase update concurrency"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(member_id),
                members::oidc_subject.eq(format!("purchase-update-{member_id}")),
                members::display_name.eq("Concurrency test"),
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
        insert_into(purchases::table)
            .values((
                purchases::id.eq(purchase_id),
                purchases::household_id.eq(household_id),
                purchases::occurred_at.eq("2026-10-16T12:00:00Z"
                    .parse::<chrono::DateTime<chrono::Utc>>()
                    .unwrap()),
                purchases::status.eq("DRAFT"),
                purchases::revision.eq(1_i64),
                purchases::allocation_rule_version.eq(1_i64),
                purchases::created_by.eq(member_id),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
    }

    let merchant_update = command(purchase_id, 1, "merchant", None, None, Some("Supermarket"));
    let occurred_at_update = command(
        purchase_id,
        1,
        "occurredAt",
        Some("2026-10-17T12:00:00Z"),
        Some("DATE_ONLY"),
        None,
    );
    let merchant_decision = decide_update_purchase(merchant_update.clone(), draft(1, "")).unwrap();
    let occurred_at_decision =
        decide_update_purchase(occurred_at_update.clone(), draft(1, "")).unwrap();
    let input = |actor_id: Uuid| bookkeeping_postgres::update_purchase::PersistUpdatePurchase {
        household_id: household_id.to_string(),
        actor_id: actor_id.to_string(),
    };
    let mut merchant_reference_data = NoFundingRules;
    let mut occurred_at_reference_data = NoFundingRules;

    let (merchant_result, occurred_at_result) = tokio::join!(
        bookkeeping_postgres::update_purchase::persist_update_purchase(
            &pool,
            input(member_id),
            merchant_update,
            merchant_decision,
            &mut merchant_reference_data,
        ),
        bookkeeping_postgres::update_purchase::persist_update_purchase(
            &pool,
            input(member_id),
            occurred_at_update,
            occurred_at_decision,
            &mut occurred_at_reference_data,
        ),
    );
    assert!(merchant_result.is_ok());
    assert!(occurred_at_result.is_ok());

    {
        let mut conn = pool.get().await.unwrap();
        let saved = public::purchases::table
            .filter(public::purchases::id.eq(purchase_id))
            .select((
                public::purchases::revision,
                public::purchases::merchant,
                public::purchases::occurred_at,
                public::purchases::date_precision,
                public::purchases::field_revisions,
            ))
            .first::<(
                i64,
                String,
                chrono::DateTime<chrono::Utc>,
                String,
                serde_json::Value,
            )>(&mut *conn)
            .await
            .unwrap();
        assert_eq!(saved.0, 3);
        assert_eq!(saved.1, "Supermarket");
        assert_eq!(
            saved.2,
            "2026-10-17T12:00:00Z"
                .parse::<chrono::DateTime<chrono::Utc>>()
                .unwrap()
        );
        assert_eq!(saved.3, "DATE_ONLY");
        assert_eq!(saved.4["merchant"], 2);
        assert_eq!(saved.4["occurredAt"], 3);
    }

    let first_command = command(purchase_id, 3, "merchant", None, None, Some("Store A"));
    let second_command = command(purchase_id, 3, "merchant", None, None, Some("Store B"));
    let current = draft(3, "Supermarket");
    let first_decision = decide_update_purchase(first_command.clone(), current.clone()).unwrap();
    let second_decision = decide_update_purchase(second_command.clone(), current).unwrap();
    let mut first_reference_data = NoFundingRules;
    let mut second_reference_data = NoFundingRules;
    let (first_result, second_result) = tokio::join!(
        bookkeeping_postgres::update_purchase::persist_update_purchase(
            &pool,
            input(member_id),
            first_command,
            first_decision,
            &mut first_reference_data,
        ),
        bookkeeping_postgres::update_purchase::persist_update_purchase(
            &pool,
            input(member_id),
            second_command,
            second_decision,
            &mut second_reference_data,
        ),
    );
    assert_ne!(first_result.is_ok(), second_result.is_ok());

    let mut conn = pool.get().await.unwrap();
    let saved = public::purchases::table
        .filter(public::purchases::id.eq(purchase_id))
        .select((public::purchases::revision, public::purchases::merchant))
        .first::<(i64, String)>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(saved.0, 4);
    assert!(saved.1 == "Store A" || saved.1 == "Store B");
}

#[tokio::test]
async fn purchase_update_rejects_closed_accounting_period_without_changing_purchase() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();

    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    let purchase_id = Uuid::now_v7();
    {
        let mut conn = pool.get().await.unwrap();
        use public::{accounting_periods, household_members, households, members, purchases};

        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Closed purchase update"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(member_id),
                members::oidc_subject.eq(format!("closed-purchase-update-{member_id}")),
                members::display_name.eq("Owner"),
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
        insert_into(accounting_periods::table)
            .values((
                accounting_periods::household_id.eq(household_id),
                accounting_periods::period_start
                    .eq("2026-10-01".parse::<chrono::NaiveDate>().unwrap()),
                accounting_periods::status.eq("CLOSED"),
                accounting_periods::revision.eq(2_i64),
                accounting_periods::closed_by.eq(Some(member_id)),
                accounting_periods::closed_at.eq(Some(chrono::Utc::now())),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(purchases::table)
            .values((
                purchases::id.eq(purchase_id),
                purchases::household_id.eq(household_id),
                purchases::occurred_at.eq("2026-10-16T12:00:00Z"
                    .parse::<chrono::DateTime<chrono::Utc>>()
                    .unwrap()),
                purchases::status.eq("POSTED"),
                purchases::revision.eq(1_i64),
                purchases::allocation_rule_version.eq(1_i64),
                purchases::created_by.eq(member_id),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
    }

    let update = command(purchase_id, 1, "merchant", None, None, Some("Changed"));
    let decision = decide_update_purchase(update.clone(), draft(1, "Original")).unwrap();
    let mut reference_data = NoFundingRules;
    let result = bookkeeping_postgres::update_purchase::persist_update_purchase(
        &pool,
        bookkeeping_postgres::update_purchase::PersistUpdatePurchase {
            household_id: household_id.to_string(),
            actor_id: member_id.to_string(),
        },
        update,
        decision,
        &mut reference_data,
    )
    .await;

    assert!(matches!(
        result,
        Err(bookkeeping_postgres::update_purchase::UpdatePurchasePersistenceError::AccountingPeriodClosed)
    ));
    let mut conn = pool.get().await.unwrap();
    let saved = public::purchases::table
        .filter(public::purchases::id.eq(purchase_id))
        .select((public::purchases::revision, public::purchases::status))
        .first::<(i64, String)>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(saved, (1, "POSTED".to_owned()));
}
