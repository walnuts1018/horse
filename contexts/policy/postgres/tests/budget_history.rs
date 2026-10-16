use chrono::{Datelike, FixedOffset, Months, NaiveDate, Utc};
use diesel::{ExpressionMethods, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::schema::public::{categories, members};
use household_application::create_household::CreateHouseholdCommand;
use policy_application::budget::{
    BudgetActorId, BudgetCategoryId, BudgetHouseholdId, BudgetId, BudgetMutationHandler,
};
use policy_domain::budget;
use policy_postgres::{policy, policy_mutations::PostgresBudgetMutationHandler, policy_query};
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
    let later_name_budget_id = Uuid::now_v7();
    let later_name_budget = budget::decide_create(budget::CreateBudgetCommand {
        actor_id: BudgetActorId::from_persisted(actor_id),
        household_id: BudgetHouseholdId::from_persisted(household_id),
        id: BudgetId::from_persisted(later_name_budget_id),
        category_id: Some(BudgetCategoryId::from_persisted(original_category)),
        name: "Zzz budget".to_owned(),
        amount_yen: 2_000,
        starts_on,
        ends_on: None,
    })
    .unwrap();
    policy::persist_create_budget(&pool, later_name_budget)
        .await
        .unwrap();
    let tokyo = FixedOffset::east_opt(9 * 60 * 60).unwrap();
    let current_month = Utc::now()
        .with_timezone(&tokyo)
        .date_naive()
        .with_day(1)
        .unwrap();
    let future_month = current_month.checked_add_months(Months::new(2)).unwrap();
    let future_start = current_month.checked_add_months(Months::new(4)).unwrap();
    let mutation_handler = PostgresBudgetMutationHandler::new(pool.clone());
    let future_start_budget_id = Uuid::now_v7();
    let future_start_budget = mutation_handler
        .create(budget::CreateBudgetCommand {
            actor_id: BudgetActorId::from_persisted(actor_id),
            household_id: BudgetHouseholdId::from_persisted(household_id),
            id: BudgetId::from_persisted(future_start_budget_id),
            category_id: Some(BudgetCategoryId::from_persisted(original_category)),
            name: "Future start budget".to_owned(),
            amount_yen: 3_000,
            starts_on: future_start,
            ends_on: None,
        })
        .await
        .unwrap();
    let expired_budget_id = Uuid::now_v7();
    let expired_starts_on = current_month.checked_sub_months(Months::new(3)).unwrap();
    let expired_ends_on = current_month.pred_opt().unwrap();
    let expired_budget = mutation_handler
        .create(budget::CreateBudgetCommand {
            actor_id: BudgetActorId::from_persisted(actor_id),
            household_id: BudgetHouseholdId::from_persisted(household_id),
            id: BudgetId::from_persisted(expired_budget_id),
            category_id: Some(BudgetCategoryId::from_persisted(original_category)),
            name: "Expired budget".to_owned(),
            amount_yen: 1_200,
            starts_on: expired_starts_on,
            ends_on: Some(expired_ends_on),
        })
        .await
        .unwrap();
    let effective_after_end = current_month.checked_add_months(Months::new(5)).unwrap();
    let updated_expired_budget = mutation_handler
        .update(budget::UpdateBudgetCommand {
            actor_id: BudgetActorId::from_persisted(actor_id),
            household_id: BudgetHouseholdId::from_persisted(household_id),
            id: BudgetId::from_persisted(expired_budget_id),
            expected_revision: 1,
            category_id: Some(BudgetCategoryId::from_persisted(updated_category)),
            name: "Expired budget updated".to_owned(),
            amount_yen: 1_500,
            starts_on: expired_starts_on,
            effective_from: effective_after_end,
            ends_on: None,
        })
        .await
        .unwrap();
    let archived_expired_budget = mutation_handler
        .archive(
            BudgetActorId::from_persisted(actor_id),
            BudgetId::from_persisted(expired_budget_id),
            2,
            true,
        )
        .await
        .unwrap();
    let future_update = budget::decide_update(budget::UpdateBudgetCommand {
        actor_id: BudgetActorId::from_persisted(actor_id),
        household_id: BudgetHouseholdId::from_persisted(household_id),
        id: BudgetId::from_persisted(later_name_budget_id),
        expected_revision: 1,
        category_id: Some(BudgetCategoryId::from_persisted(original_category)),
        name: "Zzz budget".to_owned(),
        amount_yen: 2_500,
        starts_on,
        effective_from: future_month,
        ends_on: None,
    })
    .unwrap();
    policy::persist_update_budget(&pool, future_update)
        .await
        .unwrap();
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
    let range = policy_query::budget_periods(
        &pool,
        policy_query::BudgetPeriodsQuery {
            actor_id,
            household_id,
            from: "2026-03",
            to: "2026-05",
        },
    )
    .await
    .unwrap();
    let backwards_page = policy_query::budgets(
        &pool,
        policy_query::BudgetsQuery {
            actor_id,
            household_id,
            period: "2026-03",
            after_id: None,
            before_id: None,
            first: None,
            last: Some(2),
        },
    )
    .await
    .unwrap();
    let oversized_range = policy_query::budget_periods(
        &pool,
        policy_query::BudgetPeriodsQuery {
            actor_id,
            household_id,
            from: "2024-01",
            to: "2026-01",
        },
    )
    .await;
    let archive = budget::decide_archive(budget::ArchiveBudgetCommand {
        actor_id: BudgetActorId::from_persisted(actor_id),
        household_id: BudgetHouseholdId::from_persisted(household_id),
        id: BudgetId::from_persisted(later_name_budget_id),
        expected_revision: 2,
        archived: true,
    })
    .unwrap();
    policy::persist_archive_budget(&pool, archive)
        .await
        .unwrap();
    let archived_current = policy_query::budget_for_current_period_including_archived(
        &pool,
        actor_id,
        later_name_budget_id,
    )
    .await
    .unwrap();
    let archived_future_start = mutation_handler
        .archive(
            BudgetActorId::from_persisted(actor_id),
            BudgetId::from_persisted(future_start_budget_id),
            1,
            true,
        )
        .await
        .unwrap();
    let future_range = policy_query::budget_periods(
        &pool,
        policy_query::BudgetPeriodsQuery {
            actor_id,
            household_id,
            from: &future_month.format("%Y-%m").to_string(),
            to: &future_month
                .checked_add_months(Months::new(1))
                .unwrap()
                .format("%Y-%m")
                .to_string(),
        },
    )
    .await
    .unwrap();
    let march_budget = &march.rows[0].budget;
    let april_budget = &april.rows[0].budget;
    assert_eq!(march_budget.amount_yen().value(), 10_000);
    assert_eq!(
        march_budget.category_id().unwrap().value(),
        original_category
    );
    assert_eq!(april_budget.amount_yen().value(), 15_000);
    assert_eq!(
        april_budget.category_id().unwrap().value(),
        updated_category
    );
    assert_eq!(march_single.budget.amount_yen().value(), 10_000);
    assert_eq!(
        march_single.budget.category_id().unwrap().value(),
        original_category
    );
    assert_eq!(april_single.budget.amount_yen().value(), 15_000);
    assert_eq!(
        april_single.budget.category_id().unwrap().value(),
        updated_category
    );
    assert_eq!(march_budget.revision().value(), 2);
    assert_eq!(march_single.budget.revision().value(), 2);
    assert_eq!(
        range
            .iter()
            .map(|period| period.period.as_str())
            .collect::<Vec<_>>(),
        vec!["2026-03", "2026-04", "2026-05"]
    );
    assert_eq!(range[0].budgets.len(), 2);
    assert_eq!(range[0].budgets[0].budget.id().value(), budget_id);
    assert_eq!(range[0].budgets[0].budget.amount_yen().value(), 10_000);
    assert_eq!(range[1].budgets[0].budget.amount_yen().value(), 15_000);
    assert_eq!(range[2].budgets[0].budget.amount_yen().value(), 15_000);
    assert_eq!(
        backwards_page
            .rows
            .iter()
            .map(|row| row.budget.name().as_str())
            .collect::<Vec<_>>(),
        vec!["Monthly budget", "Zzz budget"]
    );
    assert!(matches!(
        oversized_range,
        Err(policy_query::PolicyQueryError::InvalidPeriod)
    ));
    assert!(future_range.iter().all(|period| {
        period
            .budgets
            .iter()
            .all(|row| row.budget.id().value() != later_name_budget_id)
    }));
    assert!(archived_current.budget.archived());
    assert_eq!(future_start_budget.id().value(), future_start_budget_id);
    assert_eq!(future_start_budget.starts_on(), future_start);
    assert_eq!(expired_budget.amount_yen().value(), 1_200);
    assert_eq!(updated_expired_budget.amount_yen().value(), 1_500);
    assert_eq!(updated_expired_budget.ends_on(), Some(expired_ends_on));
    assert!(archived_expired_budget.archived());
    assert_eq!(archived_expired_budget.revision().value(), 3);
    assert!(archived_future_start.archived());

    postgres.terminate().await.unwrap();
}
