use std::{env, error::Error, net::SocketAddr, sync::Arc};

use axum::middleware;
use horse_auth::{AuthConfig, AuthService};
use horse_extraction_queue::ExtractionQueue;
use horse_graphql_api::{Config as GraphqlConfig, schema};
use horse_http_api::{Config, router};
use horse_object_storage::ObjectStore;
use horse_observability::Telemetry;
use horse_postgres_common::connect;
use tokio::net::TcpListener;
use tracing_subscriber::EnvFilter;

mod telemetry;

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error + Send + Sync>> {
    let filter = EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info"));
    let telemetry = Telemetry::init("horse-api", filter)?;

    let database_url = env::var("DATABASE_URL")?;
    let port = env::var("PORT")
        .ok()
        .and_then(|value| value.parse::<u16>().ok())
        .unwrap_or(8080);
    let pool = connect(database_url).await?;
    let public_origin = env::var("PUBLIC_ORIGIN").unwrap_or_default();
    let frontend_origin = env::var("FRONTEND_ORIGIN").unwrap_or_default();
    let auth = AuthService::new(
        pool.clone(),
        AuthConfig {
            issuer_url: env::var("OIDC_ISSUER_URL").unwrap_or_default(),
            client_id: env::var("OIDC_CLIENT_ID").unwrap_or_default(),
            client_secret: env::var("OIDC_CLIENT_SECRET").unwrap_or_default(),
            redirect_url: env::var("OIDC_REDIRECT_URL").unwrap_or_default(),
            public_origin: public_origin.clone(),
            frontend_origin: frontend_origin.clone(),
        },
    )
    .await?;
    let mailbox_domain = env::var("MAIL_FORWARDING_MAILBOX_DOMAIN").unwrap_or_default();
    let local_part_prefix =
        env::var("MAIL_FORWARDING_ADDRESS_LOCAL_PART_PREFIX").unwrap_or_default();
    let recipient_base = env::var("MAILBOX_RECIPIENT_BASE").unwrap_or_default();
    let (recipient_local_part, recipient_domain) =
        recipient_base.split_once('@').unwrap_or_default();
    let mailbox_enabled = [
        "MAILBOX_IMAPS_ADDRESS",
        "MAILBOX_IMAPS_SERVER_NAME",
        "MAILBOX_IMAPS_MAILBOX",
        "MAILBOX_IMAPS_USERNAME",
        "MAILBOX_IMAPS_PASSWORD",
    ]
    .iter()
    .all(|key| env::var(key).is_ok_and(|value| !value.is_empty()))
        && !mailbox_domain.is_empty()
        && local_part_prefix.ends_with('+')
        && recipient_local_part == local_part_prefix.trim_end_matches('+')
        && recipient_domain.eq_ignore_ascii_case(&mailbox_domain);
    let graphql_schema = schema(
        GraphqlConfig {
            mail_forwarding_enabled: mailbox_enabled,
            mailbox_domain,
            local_part_prefix,
        },
        horse_graphql_api::Ports {
            account_history_query: Arc::new(
                bookkeeping_postgres::account_history_query::PostgresAccountHistoryQuery::new(
                    pool.clone(),
                ),
            ),
            account_node_batch_query: Arc::new(
                bookkeeping_postgres::account_node_batch_query::PostgresAccountNodeBatchQuery::new(
                    pool.clone(),
                ),
            ),
            account_balances_query: Arc::new(
                bookkeeping_postgres::account_balances::PostgresAccountBalancesQuery::new(
                    pool.clone(),
                ),
            ),
            create_account: Arc::new(
                bookkeeping_postgres::create_account::PostgresCreateAccountHandler::new(
                    pool.clone(),
                ),
            ),
            set_card_last4: Arc::new(
                bookkeeping_postgres::account_card_last4::PostgresSetCardLast4Handler::new(
                    pool.clone(),
                ),
            ),
            financial_mutations: Arc::new(
                cross_context_postgres::financial_mutations::PostgresFinancialMutations::new(
                    pool.clone(),
                ),
            ),
            funding_mutation: Arc::new(
                cross_context_postgres::funding_mutation::PostgresFundingMutationHandler::new(
                    pool.clone(),
                ),
            ),
            settlement_workflow: Arc::new(
                cross_context_postgres::settlement_workflow::PostgresSettlementWorkflow::new(
                    pool.clone(),
                ),
            ),
            account_financial_mutations: Arc::new(
                cross_context_postgres::account_financial_mutations::PostgresAccountFinancialMutations::new(
                    pool.clone(),
                ),
            ),
            subscription_query: Arc::new(
                cross_context_postgres::subscriptions::PostgresSubscriptionQuery::new(
                    pool.clone(),
                ),
            ),
            home_summary_query: Arc::new(
                cross_context_postgres::home_summary_query::PostgresHomeSummaryQuery::new(
                    pool.clone(),
                ),
            ),
            unmatched_observations_query: Arc::new(
                cross_context_postgres::unmatched_observations_query_port::PostgresUnmatchedObservationsQuery::new(
                    pool.clone(),
                ),
            ),
            purchase_correction_query: Arc::new(
                cross_context_postgres::purchase_correction_query::PostgresPurchaseCorrectionQuery::new(
                    pool.clone(),
                ),
            ),
            create_pool: Arc::new(
                policy_postgres::pool_mutations::PostgresCreatePoolHandler::new(pool.clone()),
            ),
            set_pool_funding_rule: Arc::new(
                policy_postgres::set_pool_funding_rule::PostgresSetPoolFundingRuleHandler::new(
                    pool.clone(),
                ),
            ),
            set_default_allocation: Arc::new(
                household_postgres::set_default_allocation::PostgresDefaultAllocationHandler::new(
                    pool.clone(),
                ),
            ),
            account_balance_analysis_query: Arc::new(
                bookkeeping_postgres::account_balance_analysis::PostgresAccountBalanceAnalysisQuery::new(
                    pool.clone(),
                ),
            ),
            ledger_entry_batch_query: Arc::new(
                bookkeeping_postgres::ledger_entry_batch_query::PostgresLedgerEntryBatchQuery::new(
                    pool.clone(),
                ),
            ),
            ledger_query: Arc::new(
                cross_context_postgres::ledger_query_port::PostgresLedgerQueryHandler::new(
                    pool.clone(),
                ),
            ),
            node_financial_batch_query: Arc::new(
                cross_context_postgres::node_financial_batch::PostgresNodeFinancialBatchQuery::new(
                    pool.clone(),
                ),
            ),
            purchase_node_batch_query: Arc::new(
                bookkeeping_postgres::purchase_node_batch_query::PostgresPurchaseNodeBatchQuery::new(
                    pool.clone(),
                ),
            ),
            observation_node_batch_query: Arc::new(
                evidence_postgres::observation_node_batch_query::PostgresObservationNodeBatchQuery::new(
                    pool.clone(),
                ),
            ),
            household_query: Arc::new(
                household_postgres::household_query::PostgresHouseholdQuery::new(pool.clone()),
            ),
            create_household: Arc::new(
                household_postgres::create_household_handler::PostgresCreateHouseholdHandler::new(
                    pool.clone(),
                ),
            ),
            mail_forwarding_handler: Arc::new(
                household_postgres::mail_forwarding_handler::PostgresMailForwardingHandler::new(
                    pool.clone(),
                ),
            ),
            household_membership: Arc::new(
                household_postgres::household_membership::PostgresHouseholdMembership::new(
                    pool.clone(),
                ),
            ),
            household_invitation: Arc::new(
                household_postgres::household_invitation::PostgresHouseholdInvitationHandler::new(
                    pool.clone(),
                ),
            ),
            household_invitation_node_query: Arc::new(
                household_postgres::invitation_node_query::PostgresHouseholdInvitationNodeQuery::new(
                    pool.clone(),
                ),
            ),
            policy_node_catalog_planning_query: Arc::new(
                policy_postgres::node_catalog_planning_query::PostgresPolicyNodeBatchQuery::new(
                    pool.clone(),
                ),
            ),
            budget_mutation: Arc::new(
                policy_postgres::policy_mutations::PostgresBudgetMutationHandler::new(
                    pool.clone(),
                ),
            ),
            budget_query: Arc::new(
                policy_postgres::policy_mutations::PostgresBudgetMutationHandler::new(
                    pool.clone(),
                ),
            ),
            recurring_expense_mutation: Arc::new(
                policy_postgres::policy_mutations::PostgresRecurringExpenseMutationHandler::new(
                    pool.clone(),
                ),
            ),
            recurring_expense_query: Arc::new(
                policy_postgres::policy_mutations::PostgresRecurringExpenseMutationHandler::new(
                    pool.clone(),
                ),
            ),
            allocation_rules_query: Arc::new(
                policy_postgres::pool_queries::PostgresAllocationRulesQuery::new(pool.clone()),
            ),
            funding_schedules_query: Arc::new(
                policy_postgres::pool_queries::PostgresFundingSchedulesQuery::new(pool.clone()),
            ),
            monthly_spending_query: Arc::new(
                bookkeeping_postgres::monthly_spending_query::PostgresMonthlySpendingQuery::new(
                    pool.clone(),
                ),
            ),
            purchase_defaults_query: Arc::new(
                bookkeeping_postgres::purchase_defaults_query::PostgresPurchaseDefaultsQuery::new(
                    pool.clone(),
                ),
            ),
            purchase_analytics_query: Arc::new(
                bookkeeping_postgres::purchase_analytics_query::PostgresPurchaseAnalyticsQuery::new(
                    pool.clone(),
                ),
            ),
            purchase_edit_mutations: Arc::new(
                cross_context_postgres::purchase_edit_mutations::PostgresPurchaseEditMutationHandler::new(
                    pool.clone(),
                ),
            ),
            purchase_mutations: Arc::new(
                cross_context_postgres::purchase_mutations::PostgresPurchaseMutationHandler::new(
                    pool.clone(),
                ),
            ),
            purchase_search_query: Arc::new(
                cross_context_postgres::purchase_search_query::PostgresPurchaseSearchQuery::new(
                    pool.clone(),
                ),
            ),
            purchase_bulk_mutations: Arc::new(
                cross_context_postgres::purchase_bulk_mutations::PostgresPurchaseBulkMutationHandler::new(
                    pool.clone(),
                ),
            ),
            statement_link_mutations: Arc::new(
                cross_context_postgres::statement_link_mutations::PostgresStatementLinkMutations::new(
                    pool.clone(),
                ),
            ),
            statement_refund_correction_query: Arc::new(
                cross_context_postgres::statement_refund_correction_query::PostgresStatementRefundCorrectionQuery::new(
                    pool.clone(),
                ),
            ),
            set_allocation_rule: Arc::new(
                policy_postgres::set_allocation_rule::PostgresSetAllocationRuleHandler::new(
                    pool.clone(),
                ),
            ),
            set_funding_schedule: Arc::new(
                policy_postgres::set_funding_schedule::PostgresSetFundingScheduleHandler::new(
                    pool.clone(),
                ),
            ),
            tag_query: Arc::new(catalog_postgres::tag_query::PostgresTagQuery::new(
                pool.clone(),
            )),
            product_query: Arc::new(catalog_postgres::product_query::PostgresProductQuery::new(
                pool.clone(),
            )),
            category_mutation: Arc::new(
                catalog_postgres::category_mutation::PostgresCategoryMutation::new(pool.clone()),
            ),
            catalog_mutation: Arc::new(
                catalog_postgres::catalog_mutation::PostgresCatalogMutation::new(pool.clone()),
            ),
            catalog_node_batch_query: Arc::new(
                catalog_postgres::node_batch_query::PostgresCatalogNodeBatchQuery::new(
                    pool.clone(),
                ),
            ),
        },
    );
    let object_store = match ObjectStore::from_env().await {
        Ok(store) => Some(Arc::new(store)),
        Err(error) => {
            tracing::warn!(error = %error, "Object storage is unavailable; document HTTP routes are disabled");
            None
        }
    };
    let extraction_queue = match ExtractionQueue::from_env().await {
        Ok(queue) => queue.map(Arc::new),
        Err(error) => {
            tracing::warn!(error = %error, "Extraction queue is unavailable; database jobs will be recovered by the worker");
            None
        }
    };
    let listener = TcpListener::bind(SocketAddr::from(([0, 0, 0, 0], port))).await?;
    let server_result = axum::serve(
        listener,
        router(
            pool,
            Config {
                public_origin,
                frontend_origin,
                ocr_endpoint: std::env::var("OCR_URL").unwrap_or_default(),
                auth,
                object_store,
                extraction_queue,
            },
            graphql_schema,
        )
        .layer(middleware::from_fn(telemetry::trace_request)),
    )
    .with_graceful_shutdown(shutdown_signal())
    .await;
    let telemetry_result = telemetry.shutdown();
    server_result?;
    telemetry_result?;
    Ok(())
}

async fn shutdown_signal() {
    let ctrl_c = async {
        let _ = tokio::signal::ctrl_c().await;
    };

    #[cfg(unix)]
    let terminate = async {
        if let Ok(mut signal) =
            tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
        {
            let _ = signal.recv().await;
        }
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {},
        _ = terminate => {},
    }
}
