use async_graphql::{Data, http::ALL_WEBSOCKET_PROTOCOLS};
use async_graphql_axum::{GraphQLProtocol, GraphQLWebSocket};
use axum::response::sse::{Event, Sse};
use axum::{
    Json, Router,
    body::{Body, to_bytes},
    extract::{Multipart, Path, RawQuery, State, ws::WebSocketUpgrade},
    http::{HeaderMap, HeaderValue, Request, StatusCode, header},
    response::{IntoResponse, Response},
    routing::{get, post},
};
use diesel::{ExpressionMethods, QueryDsl};
use diesel_async::RunQueryDsl;
use horse_auth::{AuthService, Cookie, OidcError, resolve_cookie, revoke_cookie};
use horse_graphql_api::{Actor, Schema as GraphqlSchema, viewer_query::ViewerIdentity};
use horse_postgres_common::{PgPool, schema::public::households};
use horse_object_storage::ObjectStore;
use serde_json::{Value, json};
use std::sync::Arc;
use futures_util::{Stream, stream};
use std::convert::Infallible;
use url::Url;

#[derive(Clone, Default)]
pub struct Config {
    pub public_origin: String,
    pub frontend_origin: String,
    pub auth: Option<AuthService>,
    pub object_store: Option<Arc<ObjectStore>>,
}

#[derive(Clone)]
struct ApiState {
    pool: PgPool,
    config: Config,
    graphql_schema: GraphqlSchema,
}

pub fn router(pool: PgPool, config: Config, graphql_schema: GraphqlSchema) -> Router {
    Router::new()
        .route("/graphql", get(graphql_ws).post(graphql))
        .route("/healthz", get(health))
        .route("/readyz", get(ready))
        .route("/auth/login", get(login))
        .route("/auth/callback", get(callback))
        .route("/auth/me", get(me))
        .route("/auth/logout", axum::routing::post(logout))
        .route("/documents", post(upload_document))
        .route("/documents/{id}/content", get(document_content))
        .route("/documents/{id}/retry", post(retry_document))
        .route("/events", get(events))
        .with_state(ApiState {
            pool,
            config,
            graphql_schema,
        })
}

async fn graphql_ws(
    State(state): State<ApiState>,
    headers: HeaderMap,
    protocol: GraphQLProtocol,
    websocket: WebSocketUpgrade,
) -> Response {
    if !same_origin(&headers, &state.config) {
        return (StatusCode::FORBIDDEN, "request origin rejected").into_response();
    }

    if state.config.auth.is_none() {
        return unauthenticated_graphql();
    }
    let cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    let principal = match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => principal,
        Ok(None) => return unauthenticated_graphql(),
        Err(error) => {
            tracing::error!(error = ?error, "GraphQL session lookup failed");
            return (
                StatusCode::SERVICE_UNAVAILABLE,
                "authentication unavailable",
            )
                .into_response();
        }
    };

    let mut data = Data::default();
    data.insert(Actor(principal.member_id));
    data.insert(ViewerIdentity {
        member_id: principal.member_id,
        subject: principal.subject,
        display_name: principal.display_name,
    });
    websocket
        .protocols(ALL_WEBSOCKET_PROTOCOLS)
        .on_upgrade(move |stream| {
            GraphQLWebSocket::new(stream, state.graphql_schema, protocol)
                .with_data(data)
                .serve()
        })
        .into_response()
}

async fn graphql(State(state): State<ApiState>, request: Request<Body>) -> Response {
    if !same_origin(request.headers(), &state.config) {
        return (StatusCode::FORBIDDEN, "request origin rejected").into_response();
    }

    if state.config.auth.is_none() {
        return unauthenticated_graphql();
    }
    let cookie = request
        .headers()
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    let principal = match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => principal,
        Ok(None) => return unauthenticated_graphql(),
        Err(error) => {
            tracing::error!(error = ?error, "GraphQL session lookup failed");
            return (
                StatusCode::SERVICE_UNAVAILABLE,
                Json(json!({
                    "errors": [{
                        "message": "service unavailable",
                        "extensions": {"code": "SERVICE_UNAVAILABLE"}
                    }]
                })),
            )
                .into_response();
        }
    };

    let is_json = request
        .headers()
        .get(header::CONTENT_TYPE)
        .and_then(|value| value.to_str().ok())
        .is_some_and(|value| {
            value.split(';').next().is_some_and(|media_type| {
                media_type.trim().eq_ignore_ascii_case("application/json")
            })
        });
    if !is_json {
        return StatusCode::UNSUPPORTED_MEDIA_TYPE.into_response();
    }

    let body = match to_bytes(request.into_body(), 2 << 20).await {
        Ok(body) => body,
        Err(_) => return StatusCode::PAYLOAD_TOO_LARGE.into_response(),
    };
    let request = match serde_json::from_slice::<async_graphql::Request>(&body) {
        Ok(request) => request,
        Err(_) => return StatusCode::BAD_REQUEST.into_response(),
    };
    let response = GraphqlSchema::execute(
        &state.graphql_schema,
        request
            .data(Actor(principal.member_id))
            .data(ViewerIdentity {
                member_id: principal.member_id,
                subject: principal.subject,
                display_name: principal.display_name,
            }),
    )
    .await;
    Json(response).into_response()
}

fn unauthenticated_graphql() -> Response {
    (
        StatusCode::UNAUTHORIZED,
        Json(json!({
            "errors": [{
                "message": "authentication required",
                "extensions": {"code": "UNAUTHENTICATED"}
            }]
        })),
    )
        .into_response()
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

async fn upload_document(
    State(state): State<ApiState>,
    headers: HeaderMap,
    mut multipart: Multipart,
) -> Response {
    if !same_origin(&headers, &state.config) {
        return (StatusCode::FORBIDDEN, "request origin rejected").into_response();
    }
    let Some(store) = &state.config.object_store else {
        return (StatusCode::SERVICE_UNAVAILABLE, "document storage unavailable").into_response();
    };
    let cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    let principal = match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => principal,
        Ok(None) => return StatusCode::UNAUTHORIZED.into_response(),
        Err(error) => {
            tracing::error!(error = ?error, "Document session lookup failed");
            return StatusCode::SERVICE_UNAVAILABLE.into_response();
        }
    };

    let mut household_id = None;
    let mut source_kind = None;
    let mut purpose = "interactive".to_owned();
    let mut model = "free".to_owned();
    let mut filename = String::new();
    let mut content_type = String::new();
    let mut data = None;
    loop {
        let field = match multipart.next_field().await {
            Ok(Some(field)) => field,
            Ok(None) => break,
            Err(_) => return (StatusCode::BAD_REQUEST, "invalid document upload").into_response(),
        };
        let name = field.name().unwrap_or_default().to_owned();
        match name.as_str() {
            "householdId" => household_id = field.text().await.ok(),
            "sourceKind" => source_kind = field.text().await.ok(),
            "purpose" => purpose = field.text().await.unwrap_or_default(),
            "model" => model = field.text().await.unwrap_or_default(),
            "file" => {
                filename = field.file_name().unwrap_or_default().to_owned();
                content_type = field.content_type().unwrap_or_default().to_owned();
                data = match field.bytes().await {
                    Ok(bytes) if !bytes.is_empty() && bytes.len() <= 20 << 20 => Some(bytes.to_vec()),
                    _ => return (StatusCode::BAD_REQUEST, "invalid document size").into_response(),
                };
            }
            _ => {}
        }
    }
    let household_id = match household_id.and_then(|value| uuid::Uuid::parse_str(&value).ok()) {
        Some(id) => id,
        None => return (StatusCode::NOT_FOUND, "household not found").into_response(),
    };
    let source_kind = match source_kind.as_deref() {
        Some("receipt" | "order" | "email" | "card" | "bank") => source_kind.unwrap_or_default(),
        _ => return (StatusCode::BAD_REQUEST, "invalid source kind").into_response(),
    };
    if !matches!(purpose.as_str(), "interactive" | "background")
        || !matches!(model.as_str(), "free" | "pro" | "flash" | "flash-lite")
    {
        return (StatusCode::BAD_REQUEST, "invalid extraction options").into_response();
    }
    let Some(data) = data else {
        return (StatusCode::BAD_REQUEST, "document file is required").into_response();
    };
    let mime_type = match document_mime(&filename, &content_type, &data) {
        Some(mime_type) => mime_type,
        None => return StatusCode::UNSUPPORTED_MEDIA_TYPE.into_response(),
    };
    use horse_postgres_common::schema::public::household_members;
    let mut pooled = match state.pool.get().await {
        Ok(pooled) => pooled,
        Err(error) => {
            tracing::error!(error = ?error, "Document database connection unavailable");
            return StatusCode::SERVICE_UNAVAILABLE.into_response();
        }
    };
    let member = household_members::table
        .filter(household_members::household_id.eq(household_id))
        .filter(household_members::member_id.eq(principal.member_id))
        .select(household_members::member_id)
        .first::<uuid::Uuid>(&mut *pooled)
        .await;
    match member {
        Ok(_) => {}
        Err(diesel::result::Error::NotFound) => {
            return (StatusCode::NOT_FOUND, "household not found").into_response();
        }
        Err(error) => {
            tracing::error!(error = ?error, "Document household membership lookup failed");
            return StatusCode::SERVICE_UNAVAILABLE.into_response();
        }
    }
    let (object_key, sha256) = match store.put(household_id, mime_type, &data).await {
        Ok(stored) => stored,
        Err(error) => {
            tracing::error!(error = ?error, "Document object storage write failed");
            return StatusCode::BAD_GATEWAY.into_response();
        }
    };
    let result = evidence_postgres::document_registration::register_document(
        &state.pool,
        evidence_application::RegisterDocumentCommand {
            household_id,
            created_by: principal.member_id,
            source_kind: source_kind.clone(),
            original_filename: filename.clone(),
            mime_type: mime_type.to_owned(),
            sha256: sha256.clone(),
            object_key,
            metadata: json!({}),
            provider: "gemini_developer_api".to_owned(),
            model,
            purpose,
            idempotency_key: format!("upload:{}", uuid::Uuid::now_v7()),
            request: Value::Object(Default::default()),
            attachments: vec![],
        },
    )
    .await;
    match result {
        Ok(document) => {
            let response = json!({
                "id": document.document_id,
                "householdId": household_id,
                "name": filename,
                "sha256": sha256,
                "mimeType": mime_type,
                "sourceKind": source_kind,
                "status": "queued",
                "jobId": document.job_id,
                "duplicate": document.duplicate,
                "createdAt": chrono::Utc::now(),
                "confirmations": [],
                "hasEvidence": false
            });
            (if document.duplicate { StatusCode::OK } else { StatusCode::ACCEPTED }, Json(response)).into_response()
        }
        Err(error) => {
            tracing::error!(error = ?error, "Document registration failed");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

fn document_mime(filename: &str, declared: &str, data: &[u8]) -> Option<&'static str> {
    let lower = filename.to_ascii_lowercase();
    if declared == "message/rfc822" && (lower.ends_with(".eml") || lower.ends_with(".rfc822")) {
        Some("message/rfc822")
    } else if declared == "text/csv" && lower.ends_with(".csv") {
        Some("text/csv")
    } else if data.starts_with(b"%PDF-") && (declared == "application/pdf" || lower.ends_with(".pdf")) {
        Some("application/pdf")
    } else if data.starts_with(&[0xff, 0xd8, 0xff])
        && (declared == "image/jpeg" || lower.ends_with(".jpg") || lower.ends_with(".jpeg"))
    {
        Some("image/jpeg")
    } else if data.starts_with(b"\x89PNG\r\n\x1a\n")
        && (declared == "image/png" || lower.ends_with(".png"))
    {
        Some("image/png")
    } else if data.len() > 12
        && &data[..4] == b"RIFF"
        && &data[8..12] == b"WEBP"
        && (declared == "image/webp" || lower.ends_with(".webp"))
    {
        Some("image/webp")
    } else {
        None
    }
}

async fn document_content(
    State(state): State<ApiState>,
    Path(raw_id): Path<String>,
    headers: HeaderMap,
) -> Response {
    let Some(store) = &state.config.object_store else {
        return StatusCode::SERVICE_UNAVAILABLE.into_response();
    };
    let Ok(document_id) = uuid::Uuid::parse_str(&raw_id) else {
        return StatusCode::NOT_FOUND.into_response();
    };
    let cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    let principal = match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => principal,
        Ok(None) => return StatusCode::UNAUTHORIZED.into_response(),
        Err(error) => {
            tracing::error!(error = ?error, "Document session lookup failed");
            return StatusCode::SERVICE_UNAVAILABLE.into_response();
        }
    };
    use horse_postgres_common::schema::public::{household_members, source_documents};
    let mut pooled = match state.pool.get().await {
        Ok(pooled) => pooled,
        Err(error) => {
            tracing::error!(error = ?error, "Document database connection unavailable");
            return StatusCode::SERVICE_UNAVAILABLE.into_response();
        }
    };
    let document = source_documents::table
        .inner_join(
            household_members::table.on(household_members::household_id
                .eq(source_documents::household_id)),
        )
        .filter(source_documents::id.eq(document_id))
        .filter(household_members::member_id.eq(principal.member_id))
        .select((source_documents::object_key, source_documents::mime_type))
        .first::<(String, String)>(&mut *pooled)
        .await;
    let (object_key, mime_type) = match document {
        Ok(document) => document,
        Err(diesel::result::Error::NotFound) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!(error = ?error, "Document content lookup failed");
            return StatusCode::SERVICE_UNAVAILABLE.into_response();
        }
    };
    drop(pooled);
    match store.get(&object_key, &mime_type).await {
        Ok(document) => {
            let mut response = Response::new(axum::body::Body::from(document.data));
            *response.status_mut() = StatusCode::OK;
            response.headers_mut().insert(
                header::CONTENT_TYPE,
                HeaderValue::from_str(&document.mime_type)
                    .unwrap_or_else(|_| HeaderValue::from_static("application/octet-stream")),
            );
            response.headers_mut().insert(
                header::CACHE_CONTROL,
                HeaderValue::from_static("private, no-store"),
            );
            response.headers_mut().insert(
                header::CONTENT_DISPOSITION,
                HeaderValue::from_static("inline"),
            );
            response
        }
        Err(error) => {
            tracing::error!(error = ?error, "Document content storage read failed");
            StatusCode::BAD_GATEWAY.into_response()
        }
    }
}

async fn retry_document(
    State(state): State<ApiState>,
    Path(raw_id): Path<String>,
    headers: HeaderMap,
    Json(input): Json<Value>,
) -> Response {
    if !same_origin(&headers, &state.config) {
        return (StatusCode::FORBIDDEN, "request origin rejected").into_response();
    }
    let Ok(document_id) = uuid::Uuid::parse_str(&raw_id) else {
        return StatusCode::NOT_FOUND.into_response();
    };
    let cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    let principal = match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => principal,
        Ok(None) => return StatusCode::UNAUTHORIZED.into_response(),
        Err(error) => {
            tracing::error!(error = ?error, "Document session lookup failed");
            return StatusCode::SERVICE_UNAVAILABLE.into_response();
        }
    };
    let model = input.get("model").and_then(Value::as_str).unwrap_or("free");
    if !matches!(model, "free" | "pro" | "flash" | "flash-lite") {
        return (StatusCode::BAD_REQUEST, "invalid extraction retry input").into_response();
    }
    if let Some(crop) = input.get("crop") {
        let valid = ["top", "left", "bottom", "right"]
            .iter()
            .all(|key| crop.get(key).and_then(Value::as_f64).is_some_and(f64::is_finite));
        let page = crop.get("page").and_then(Value::as_u64).unwrap_or(0);
        let top = crop.get("top").and_then(Value::as_f64).unwrap_or(0.0);
        let left = crop.get("left").and_then(Value::as_f64).unwrap_or(0.0);
        let bottom = crop.get("bottom").and_then(Value::as_f64).unwrap_or(0.0);
        let right = crop.get("right").and_then(Value::as_f64).unwrap_or(0.0);
        if !valid || page > u64::from(u32::MAX) || top < 0.0 || left < 0.0 || bottom > 1.0 || right > 1.0 || top >= bottom || left >= right {
            return (StatusCode::BAD_REQUEST, "invalid extraction crop").into_response();
        }
    }
    use horse_postgres_common::schema::public::{household_members, source_documents};
    let mut pooled = match state.pool.get().await {
        Ok(pooled) => pooled,
        Err(error) => {
            tracing::error!(error = ?error, "Document database connection unavailable");
            return StatusCode::SERVICE_UNAVAILABLE.into_response();
        }
    };
    let household_id = source_documents::table
        .inner_join(
            household_members::table.on(household_members::household_id
                .eq(source_documents::household_id)),
        )
        .filter(source_documents::id.eq(document_id))
        .filter(household_members::member_id.eq(principal.member_id))
        .select(source_documents::household_id)
        .first::<uuid::Uuid>(&mut *pooled)
        .await;
    let household_id = match household_id {
        Ok(household_id) => household_id,
        Err(diesel::result::Error::NotFound) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!(error = ?error, "Document retry authorization lookup failed");
            return StatusCode::SERVICE_UNAVAILABLE.into_response();
        }
    };
    drop(pooled);
    let mut request = serde_json::Map::new();
    request.insert("purpose".to_owned(), Value::String("interactive".to_owned()));
    if model != "free" {
        request.insert("model".to_owned(), Value::String(model.to_owned()));
    }
    if let Some(crop) = input.get("crop") {
        request.insert("crop".to_owned(), crop.clone());
    }
    let job = evidence_postgres::document_registration::enqueue_extraction_job(
        &state.pool,
        household_id,
        document_id,
        principal.member_id,
        "gemini_developer_api",
        if model == "free" { "" } else { model },
        &format!("retry:{}", uuid::Uuid::now_v7()),
        Value::Object(request),
    )
    .await;
    match job {
        Ok(job_id) => (StatusCode::ACCEPTED, Json(json!({"jobId": job_id, "status": "queued"}))).into_response(),
        Err(error) => {
            tracing::error!(error = ?error, "Document retry registration failed");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

async fn events(
    State(state): State<ApiState>,
    headers: HeaderMap,
    axum::extract::Query(query): axum::extract::Query<std::collections::HashMap<String, String>>,
) -> Result<Response, Response> {
    let cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    let principal = match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => principal,
        Ok(None) => return Err(StatusCode::UNAUTHORIZED.into_response()),
        Err(error) => {
            tracing::error!(error = ?error, "Event stream session lookup failed");
            return Err(StatusCode::SERVICE_UNAVAILABLE.into_response());
        }
    };
    let household_id = query
        .get("householdId")
        .and_then(|value| uuid::Uuid::parse_str(value).ok())
        .ok_or_else(|| (StatusCode::NOT_FOUND, "household not found").into_response())?;
    use horse_postgres_common::schema::public::household_members;
    let mut pooled = state
        .pool
        .get()
        .await
        .map_err(|_| StatusCode::SERVICE_UNAVAILABLE.into_response())?;
    let member = household_members::table
        .filter(household_members::household_id.eq(household_id))
        .filter(household_members::member_id.eq(principal.member_id))
        .select(household_members::member_id)
        .first::<uuid::Uuid>(&mut *pooled)
        .await;
    match member {
        Ok(_) => {}
        Err(diesel::result::Error::NotFound) => {
            return Err((StatusCode::NOT_FOUND, "household not found").into_response());
        }
        Err(error) => {
            tracing::error!(error = ?error, "Event stream membership lookup failed");
            return Err(StatusCode::SERVICE_UNAVAILABLE.into_response());
        }
    }
    let mut cursor = query
        .get("after")
        .and_then(|value| value.parse::<i64>().ok())
        .unwrap_or(0);
    if let Some(last_event_id) = headers.get("last-event-id").and_then(|value| value.to_str().ok()) {
        cursor = last_event_id.parse::<i64>().unwrap_or(cursor);
    }
    if cursor < 0 {
        return Err((StatusCode::BAD_REQUEST, "invalid event cursor").into_response());
    }
    drop(pooled);
    use horse_postgres_common::schema::public::household_events;
    let stream = stream::unfold(
        (state.pool, household_id, cursor),
        |(pool, household_id, cursor)| async move {
            let mut connection = match pool.get().await {
                Ok(connection) => connection,
                Err(_) => {
                    tokio::time::sleep(std::time::Duration::from_secs(1)).await;
                    return Some((Ok(Event::default().comment("keepalive")), (pool, household_id, cursor)));
                }
            };
            let events = household_events::table
                .filter(household_events::household_id.eq(household_id))
                .filter(household_events::cursor.gt(cursor))
                .order_by(household_events::cursor.asc())
                .limit(100)
                .select((
                    household_events::cursor,
                    household_events::event_type,
                    household_events::entity_id,
                    household_events::revision,
                    household_events::changed_fields,
                    household_events::created_at,
                ))
                .load::<(i64, String, uuid::Uuid, i64, Vec<Option<String>>, chrono::DateTime<chrono::Utc>)>(&mut *connection)
                .await
                .unwrap_or_default();
            if let Some((event_cursor, event_type, entity_id, revision, changed_fields, created_at)) = events.first() {
                let event = json!({
                    "cursor": event_cursor,
                    "type": event_type,
                    "entityId": entity_id,
                    "revision": revision,
                    "changedFields": changed_fields.iter().flatten().collect::<Vec<_>>(),
                    "occurredAt": created_at,
                });
                let stream_event = Event::default()
                    .id(event_cursor.to_string())
                    .event(event_type.clone())
                    .json_data(event)
                    .unwrap_or_else(|_| Event::default().comment("event encoding failed"));
                return Some((Ok(stream_event), (pool, household_id, *event_cursor)));
            }
            drop(connection);
            tokio::time::sleep(std::time::Duration::from_secs(1)).await;
            Some((Ok(Event::default().comment("keepalive")), (pool, household_id, cursor)))
        },
    );
    let stream: std::pin::Pin<Box<dyn Stream<Item = Result<Event, Infallible>> + Send>> =
        Box::pin(stream);
    Ok(Sse::new(stream)
        .keep_alive(axum::response::sse::KeepAlive::new().interval(std::time::Duration::from_secs(15)))
        .into_response())
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
            (
                StatusCode::SERVICE_UNAVAILABLE,
                "authentication unavailable",
            )
                .into_response()
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

    let secure =
        Url::parse(&state.config.public_origin).is_ok_and(|origin| origin.scheme() == "https");
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
    let max_age = cookie.max_age_seconds.or_else(|| {
        cookie
            .expires_at
            .map(|expiry| (expiry - chrono::Utc::now()).num_seconds())
    });
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
