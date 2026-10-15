use diesel_async::pooled_connection::{AsyncDieselConnectionManager, PoolError, bb8::Pool};

pub use diesel_async::AsyncPgConnection;
pub mod account_balance_analysis;
pub mod account_balances;
pub mod account_card_last4;
pub mod account_command_context;
pub mod account_command_result;
pub mod account_income;
pub mod account_movement;
pub mod bulk_delete_purchase_items;
pub mod bulk_update_purchase_items;
pub mod category;
pub mod close_period;
pub mod correct_purchase;
pub mod create_account;
pub mod create_household;
pub mod create_pool;
pub mod create_purchase;
pub mod funding_command_context;
pub mod funding_command_result;
pub mod funding_record;
pub mod household_access;
pub mod household_invitation;
pub mod household_membership;
pub mod ingestion_job;
pub mod mail_forwarding;
pub mod policy;
pub mod post_purchase;
pub mod post_purchase_loader;
pub mod product_alias;
pub mod record_repayment;
pub mod record_transfer;
pub mod schema;
pub mod set_allocation_rule;
pub mod set_default_allocation;
pub mod set_funding_schedule;
pub mod settlement_preview;
pub mod settlement_query;
pub mod shared_draft;
pub mod statement_transfer;
pub mod tag;
pub mod update_purchase;
pub mod update_purchase_item;

pub type PgPool = Pool<AsyncPgConnection>;

pub async fn connect(database_url: impl Into<String>) -> Result<PgPool, PoolError> {
    let manager = AsyncDieselConnectionManager::<AsyncPgConnection>::new(database_url.into());
    Pool::builder().build(manager).await
}
