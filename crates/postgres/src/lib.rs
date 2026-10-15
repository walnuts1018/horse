use diesel_async::pooled_connection::{AsyncDieselConnectionManager, PoolError, bb8::Pool};

pub use diesel_async::AsyncPgConnection;
pub mod close_period;
pub mod correct_purchase;
pub mod create_purchase;
pub mod post_purchase;
pub mod post_purchase_loader;
pub mod record_repayment;
pub mod record_transfer;
pub mod schema;
pub mod update_purchase;

pub type PgPool = Pool<AsyncPgConnection>;

pub async fn connect(database_url: impl Into<String>) -> Result<PgPool, PoolError> {
    let manager = AsyncDieselConnectionManager::<AsyncPgConnection>::new(database_url.into());
    Pool::builder().build(manager).await
}
