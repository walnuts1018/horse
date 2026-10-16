use bookkeeping_postgres::{purchase_analytics_query::purchase_analytics, schema::public};
use chrono::{FixedOffset, NaiveDate, TimeZone};
use diesel::{ExpressionMethods, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::connect;
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

#[tokio::test]
async fn purchase_correction_changes_analysis_for_purchase_date() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();
    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    let purchase_id = Uuid::now_v7();
    let item_id = Uuid::now_v7();
    let correction_id = Uuid::now_v7();
    let journal_id = Uuid::now_v7();
    let occurred_at = FixedOffset::east_opt(9 * 60 * 60)
        .unwrap()
        .with_ymd_and_hms(2026, 9, 15, 12, 0, 0)
        .unwrap()
        .with_timezone(&chrono::Utc);

    {
        let mut conn = pool.get().await.unwrap();
        use public::{
            accounting_periods, household_members, households, journals, members,
            purchase_corrections, purchase_items, purchases,
        };

        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Analysis"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(member_id),
                members::oidc_subject.eq(format!("analysis-{member_id}")),
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
        insert_into(accounting_periods::table)
            .values(vec![
                (
                    accounting_periods::household_id.eq(household_id),
                    accounting_periods::period_start
                        .eq(NaiveDate::from_ymd_opt(2026, 9, 1).unwrap()),
                    accounting_periods::status.eq("CLOSED"),
                    accounting_periods::revision.eq(1_i64),
                    accounting_periods::closed_by.eq(Some(member_id)),
                    accounting_periods::closed_at.eq(Some(occurred_at)),
                ),
                (
                    accounting_periods::household_id.eq(household_id),
                    accounting_periods::period_start
                        .eq(NaiveDate::from_ymd_opt(2026, 10, 1).unwrap()),
                    accounting_periods::status.eq("OPEN"),
                    accounting_periods::revision.eq(1_i64),
                    accounting_periods::closed_by.eq(None::<Uuid>),
                    accounting_periods::closed_at.eq(None::<chrono::DateTime<chrono::Utc>>),
                ),
            ])
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(purchases::table)
            .values((
                purchases::id.eq(purchase_id),
                purchases::household_id.eq(household_id),
                purchases::occurred_at.eq(occurred_at),
                purchases::merchant.eq("Market"),
                purchases::status.eq("POSTED"),
                purchases::allocation_rule_version.eq(1_i64),
                purchases::created_by.eq(member_id),
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
                purchase_items::amount_yen.eq(1_000_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(journals::table)
            .values((
                journals::id.eq(journal_id),
                journals::household_id.eq(household_id),
                journals::source_kind.eq("CORRECTION"),
                journals::source_id.eq(correction_id),
                journals::occurred_at.eq(occurred_at),
                journals::created_by.eq(member_id),
                journals::idempotency_key.eq(format!("correction-{correction_id}")),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(purchase_corrections::table)
            .values((
                purchase_corrections::id.eq(correction_id),
                purchase_corrections::household_id.eq(household_id),
                purchase_corrections::purchase_id.eq(purchase_id),
                purchase_corrections::item_id.eq(Some(item_id)),
                purchase_corrections::amount_delta_yen.eq(-200_i64),
                purchase_corrections::reason.eq("Refund"),
                purchase_corrections::effective_period_start
                    .eq(NaiveDate::from_ymd_opt(2026, 10, 1).unwrap()),
                purchase_corrections::journal_id.eq(journal_id),
                purchase_corrections::created_by.eq(member_id),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
    }

    let analysis = purchase_analytics(&pool, member_id, household_id, "2026-09", "2026-09")
        .await
        .unwrap();
    assert_eq!(
        analysis
            .iter()
            .filter(|row| row.dimension == "merchant")
            .map(|row| row.amount_yen)
            .collect::<Vec<_>>(),
        vec![800]
    );
    assert_eq!(
        analysis
            .iter()
            .filter(|row| row.dimension == "item")
            .map(|row| row.amount_yen)
            .collect::<Vec<_>>(),
        vec![800]
    );

    let effective_month_analysis =
        purchase_analytics(&pool, member_id, household_id, "2026-10", "2026-10")
            .await
            .unwrap();
    assert!(effective_month_analysis.is_empty());
}
