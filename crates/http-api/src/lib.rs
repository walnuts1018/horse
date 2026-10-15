use axum::{Router, extract::State, http::StatusCode, routing::get};
use diesel::dsl::select;
use diesel_async::RunQueryDsl;
use horse_postgres::PgPool;

pub fn router(pool: PgPool) -> Router {
    Router::new()
        .route("/healthz", get(health))
        .route("/readyz", get(ready))
        .with_state(pool)
}

async fn health() -> StatusCode {
    StatusCode::OK
}

async fn ready(State(pool): State<PgPool>) -> StatusCode {
    let Ok(mut connection) = pool.get().await else {
        return StatusCode::SERVICE_UNAVAILABLE;
    };

    match select(1_i32).get_result::<i32>(&mut connection).await {
        Ok(1) => StatusCode::OK,
        _ => StatusCode::SERVICE_UNAVAILABLE,
    }
}
