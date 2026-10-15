use axum::{
    Json, Router,
    extract::State,
    http::{HeaderMap, StatusCode},
    routing::get,
};
use diesel::dsl::select;
use diesel_async::RunQueryDsl;
use horse_auth::resolve_cookie;
use horse_postgres::PgPool;
use serde_json::{Value, json};

pub fn router(pool: PgPool) -> Router {
    Router::new()
        .route("/healthz", get(health))
        .route("/readyz", get(ready))
        .route("/auth/me", get(me))
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

async fn me(State(pool): State<PgPool>, headers: HeaderMap) -> (StatusCode, Json<Value>) {
    let cookie = headers
        .get(axum::http::header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    match resolve_cookie(&pool, cookie).await {
        Ok(Some(principal)) => (
            StatusCode::OK,
            Json(json!({
                "authenticated": true,
                "viewer": {
                    "id": principal.member_id,
                    "subject": principal.subject,
                    "displayName": principal.display_name,
                },
            })),
        ),
        Ok(None) => (
            StatusCode::OK,
            Json(json!({"authenticated": false, "loginURL": "/auth/login"})),
        ),
        Err(error) => {
            tracing::error!(error = ?error, "OIDC session lookup failed");
            (StatusCode::SERVICE_UNAVAILABLE, Json(json!({})))
        }
    }
}
