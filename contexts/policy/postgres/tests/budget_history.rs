use chrono::NaiveDate;
use diesel::{ExpressionMethods, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::schema::public::{categories, members};
use household_application::create_household::CreateHouseholdCommand;
use policy_application::budget::{BudgetActorId, BudgetCategoryId, BudgetHouseholdId, BudgetId};
use policy_domain::budget;
use policy_postgres::{policy, policy_query};
use uuid::Uuid;

#[tokio::test]
async fn budget_edits_apply_from_the_selected_month_and_preserve_historical_values() {
    let postgres = horse_test_infra::TestPostgres::start().await.unwrap();
    horse_test_infra::apply_migrations(postgres.connection_url()).unwrap();
    let pool = horse_postgres_common::connect(postgres.connection_url())
        .await
        .unwrap();
    let actor_id = Uuid::now_v7();
    {
        let mut conn = pool.get().await.unwrap();
        insert_into(members::table)
            .values((
                members::id.eq(actor_id),
                members::oidc_subject.eq("budget-history-actor"),
                members::display_name.eq("Budget history actor"),
            ))
            .execute(&mut conn)
            .await
            .unwrap();
    }
    let household = household_postgres::create_household::persist_create_household(
        &pool,
        CreateHouseholdCommand {
            actor_id: actor_id.into(),
            name: "Budget history household".to_owned(),
            member_names: Vec::new(),
        },
    )
    .await
    .unwrap();
    let household_id = Uuid::parse_str(&household.household_id).unwrap();
    let original_category = Uuid::now_v7();
    let updated_category = Uuid::now_v7();
    {
        let mut conn = pool.get().await.unwrap();
        for (id, name) in [
            (original_category, "Original"),
            (updated_category, "Updated"),
        ] {
            insert_into(categories::table)
                .values((
                    categories::id.eq(id),
                    categories::household_id.eq(household_id),
                    categories::name.eq(name),
                ))
                .execute(&mut conn)
                .await
                .unwrap();
        }
    }
    let budget_id = Uuid::now_v7();
    let starts_on = NaiveDate::from_ymd_opt(2026, 1, 1).unwrap();
    let initial = budget::decide_create(budget::CreateBudgetCommand {
        actor_id: BudgetActorId::from_persisted(actor_id),
        household_id: BudgetHouseholdId::from_persisted(household_id),
        id: BudgetId::from_persisted(budget_id),
        category_id: Some(BudgetCategoryId::from_persisted(original_category)),
        name: "Monthly budget".to_owned(),
        amount_yen: 10_000,
        starts_on,
        ends_on: None,
    })
    .unwrap();
    policy::persist_create_budget(&pool, initial).await.unwrap();
    let update = budget::decide_update(budget::UpdateBudgetCommand {
        actor_id: BudgetActorId::from_persisted(actor_id),
        household_id: BudgetHouseholdId::from_persisted(household_id),
        id: BudgetId::from_persisted(budget_id),
        expected_revision: 1,
        category_id: Some(BudgetCategoryId::from_persisted(updated_category)),
        name: "Monthly budget".to_owned(),
        amount_yen: 15_000,
        starts_on,
        effective_from: NaiveDate::from_ymd_opt(2026, 4, 1).unwrap(),
        ends_on: None,
    })
    .unwrap();
    policy::persist_update_budget(&pool, update).await.unwrap();

    let march = policy_query::budgets(
        &pool,
        policy_query::BudgetsQuery {
            actor_id,
            household_id,
            period: "2026-03",
            after_id: None,
            before_id: None,
            first: None,
            last: None,
        },
    )
    .await
    .unwrap();
    let april = policy_query::budgets(
        &pool,
        policy_query::BudgetsQuery {
            actor_id,
            household_id,
            period: "2026-04",
            after_id: None,
            before_id: None,
            first: None,
            last: None,
        },
    )
    .await
    .unwrap();
    let march_single = policy_query::budget(&pool, actor_id, budget_id, "2026-03")
        .await
        .unwrap();
    let april_single = policy_query::budget(&pool, actor_id, budget_id, "2026-04")
        .await
        .unwrap();
    let march_budget = &march.rows[0].budget;
    let april_budget = &april.rows[0].budget;
    assert_eq!(march_budget.amount_yen.value(), 10_000);
    assert_eq!(march_budget.category_id.unwrap().value(), original_category);
    assert_eq!(april_budget.amount_yen.value(), 15_000);
    assert_eq!(april_budget.category_id.unwrap().value(), updated_category);
    assert_eq!(march_single.budget.amount_yen.value(), 10_000);
    assert_eq!(
        march_single.budget.category_id.unwrap().value(),
        original_category
    );
    assert_eq!(april_single.budget.amount_yen.value(), 15_000);
    assert_eq!(
        april_single.budget.category_id.unwrap().value(),
        updated_category
    );

    postgres.terminate().await.unwrap();
}
