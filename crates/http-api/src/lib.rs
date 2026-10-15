use axum::{
    Json, Router,
    extract::{RawQuery, State},
    http::{HeaderMap, HeaderValue, StatusCode, header},
    response::{IntoResponse, Response},
    routing::get,
};
use diesel::QueryDsl;
use diesel_async::RunQueryDsl;
use horse_auth::{AuthService, Cookie, OidcError, revoke_cookie, resolve_cookie};
use horse_postgres::{PgPool, schema::public::households};
use serde_json::json;
use url::Url;

#[derive(Clone, Default)]
pub struct Config {
    pub public_origin: String,
    pub frontend_origin: String,
    pub auth: Option<AuthService>,
}

#[derive(Clone)]
struct ApiState {
    pool: PgPool,
    config: Config,
}

pub fn router(pool: PgPool, config: Config) -> Router {
    Router::new()
        .route("/healthz", get(health))
        .route("/readyz", get(ready))
        .route("/auth/login", get(login))
        .route("/auth/callback", get(callback))
        .route("/auth/me", get(me))
        .route("/auth/logout", axum::routing::post(logout))
        .with_state(ApiState { pool, config })
}

async fn login(State(state): State<ApiState>, RawQuery(query): RawQuery) -> Response {
    let Some(auth) = &state.config.auth else {
        return StatusCode::NOT_FOUND.into_response();
    };
    let return_to = query.as_deref().and_then(|query| {
        url::form_urlencoded::parse(query.as_bytes())
            .find(|(key, _)| key == "returnTo")
            .map(|(_, value)| value.into_owned())
    });
    match auth.login(return_to.as_deref()).await {
        Ok(start) => {
            let mut response = StatusCode::FOUND.into_response();
            response.headers_mut().insert(
                header::LOCATION,
                HeaderValue::from_str(&start.authorization_url)
                    .unwrap_or_else(|_| HeaderValue::from_static("/")),
            );
            set_cookie(&mut response, &start.state_cookie);
            response
        }
        Err(error) => {
            tracing::error!(error = ?error, "OIDC login initialization failed");
            StatusCode::SERVICE_UNAVAILABLE.into_response()
        }
    }
}

async fn callback(
    State(state): State<ApiState>,
    RawQuery(query): RawQuery,
    headers: HeaderMap,
) -> Response {
    let Some(auth) = &state.config.auth else {
        return StatusCode::NOT_FOUND.into_response();
    };
    let values: std::collections::HashMap<String, String> = query
        .as_deref()
        .map(|query| {
            url::form_urlencoded::parse(query.as_bytes())
                .map(|(key, value)| (key.into_owned(), value.into_owned()))
                .collect()
        })
        .unwrap_or_default();
    let state_value = values.get("state").map(String::as_str).unwrap_or_default();
    let code = values.get("code").map(String::as_str).unwrap_or_default();
    let state_cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .and_then(|value| cookie_value(value, "horse_oidc_state"));
    let result = auth.callback(state_value, code, state_cookie).await;
    let state_clear = auth.state_cookie_clear();
    match result {
        Ok(result) => {
            let mut response = StatusCode::FOUND.into_response();
            response.headers_mut().insert(
                header::LOCATION,
                HeaderValue::from_str(&result.redirect_to)
                    .unwrap_or_else(|_| HeaderValue::from_static("/")),
            );
            set_cookie(&mut response, &result.session_cookie);
            set_cookie(&mut response, &state_clear);
            response
        }
        Err(error) => {
            let status = match &error {
                OidcError::InvalidResponse | OidcError::InvalidState => StatusCode::BAD_REQUEST,
                OidcError::AuthenticationFailed | OidcError::SessionExpired => {
                    StatusCode::UNAUTHORIZED
                }
                OidcError::IncompleteConfiguration(_) => StatusCode::SERVICE_UNAVAILABLE,
                OidcError::Provider(_) | OidcError::Database(_) | OidcError::Pool(_) => {
                    tracing::error!(error = ?error, "OIDC callback failed");
                    StatusCode::SERVICE_UNAVAILABLE
                }
            };
            let mut response = status.into_response();
            set_cookie(&mut response, &state_clear);
            response
        }
    }
}

async fn health() -> StatusCode {
    StatusCode::OK
}

async fn ready(State(state): State<ApiState>) -> StatusCode {
    let Ok(mut connection) = state.pool.get().await else {
        return StatusCode::SERVICE_UNAVAILABLE;
    };

    match households::table
        .count()
        .get_result::<i64>(&mut connection)
        .await
    {
        Ok(_) => StatusCode::OK,
        _ => StatusCode::SERVICE_UNAVAILABLE,
    }
}

async fn me(State(state): State<ApiState>, headers: HeaderMap) -> Response {
    let cookie = headers
        .get(axum::http::header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => Json(json!({
                "authenticated": true,
                "viewer": {
                    "id": principal.member_id.to_string(),
                    "subject": principal.subject,
                    "displayName": principal.display_name,
                },
            }))
            .into_response(),
        Ok(None) => Json(json!({
            "authenticated": false,
            "loginURL": "/auth/login",
        }))
        .into_response(),
        Err(error) => {
            tracing::error!(error = ?error, "OIDC session lookup failed");
            (StatusCode::SERVICE_UNAVAILABLE, "authentication unavailable").into_response()
        }
    }
}

async fn logout(State(state): State<ApiState>, headers: HeaderMap) -> Response {
    if !same_origin(&headers, &state.config) {
        return StatusCode::FORBIDDEN.into_response();
    }

    let cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    if let Err(error) = revoke_cookie(&state.pool, cookie).await {
        tracing::error!(error = ?error, "OIDC session revocation failed");
        return StatusCode::SERVICE_UNAVAILABLE.into_response();
    }

    let secure = Url::parse(&state.config.public_origin)
        .is_ok_and(|origin| origin.scheme() == "https");
    let cookie = if secure {
        "horse_session=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax"
    } else {
        "horse_session=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax"
    };
    let mut response = StatusCode::NO_CONTENT.into_response();
    response
        .headers_mut()
        .insert(header::SET_COOKIE, HeaderValue::from_static(cookie));
    response
}

fn cookie_value<'a>(header: &'a str, name: &str) -> Option<&'a str> {
    header.split(';').find_map(|part| {
        let (key, value) = part.trim().split_once('=')?;
        (key.trim() == name).then_some(value.trim())
    })
}

fn set_cookie(response: &mut Response, cookie: &Cookie) {
    let max_age = cookie
        .max_age_seconds
        .or_else(|| cookie.expires_at.map(|expiry| (expiry - chrono::Utc::now()).num_seconds()));
    let mut value = format!("{}={}; Path={}", cookie.name, cookie.value, cookie.path);
    if let Some(max_age) = max_age {
        value.push_str(&format!("; Max-Age={max_age}"));
    }
    if cookie.http_only {
        value.push_str("; HttpOnly");
    }
    if cookie.secure {
        value.push_str("; Secure");
    }
    if cookie.same_site_lax {
        value.push_str("; SameSite=Lax");
    }
    if let Ok(value) = HeaderValue::from_str(&value) {
        response.headers_mut().append(header::SET_COOKIE, value);
    }
}

fn same_origin(headers: &HeaderMap, config: &Config) -> bool {
    let Some(origin) = headers
        .get(header::ORIGIN)
        .and_then(|value| value.to_str().ok())
        .and_then(|value| Url::parse(value).ok())
    else {
        return false;
    };
    [&config.public_origin, &config.frontend_origin]
        .into_iter()
        .filter_map(|value| Url::parse(value).ok())
        .any(|expected| origin.origin() == expected.origin())
}
