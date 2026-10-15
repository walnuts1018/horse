use diesel_async::pooled_connection::{AsyncDieselConnectionManager, PoolError, bb8::Pool};

pub use diesel_async::AsyncPgConnection;

pub type PgPool = Pool<AsyncPgConnection>;

pub async fn connect(database_url: impl Into<String>) -> Result<PgPool, PoolError> {
    let manager = AsyncDieselConnectionManager::<AsyncPgConnection>::new(database_url.into());
    Pool::builder().build(manager).await
}
