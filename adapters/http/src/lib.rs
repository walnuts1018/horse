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
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl};
use diesel_async::{AsyncConnection, RunQueryDsl};
use futures_util::{Stream, stream};
use horse_auth::{AuthService, Cookie, OidcError, resolve_cookie, revoke_cookie};
use horse_graphql_api::{Actor, Schema as GraphqlSchema, viewer_query::ViewerIdentity};
use horse_object_storage::ObjectStore;
use horse_postgres_common::{PgPool, schema::public::households};
use serde_json::{Value, json};
use std::convert::Infallible;
use std::sync::Arc;
use url::Url;

#[derive(Clone, Default)]
pub struct Config {
    pub public_origin: String,
    pub frontend_origin: String,
    pub ocr_endpoint: String,
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
        .route(
            "/documents",
            get(list_documents)
                .post(upload_document)
                .layer(axum::extract::DefaultBodyLimit::max(42 << 20)),
        )
        .route("/documents/{id}/content", get(document_content))
        .route("/documents/{id}/preview", get(document_preview))
        .route("/documents/{id}/matches", get(document_matches))
        .route(
            "/documents/{id}",
            get(read_document).delete(delete_document),
        )
        .route("/documents/{id}/extractions", get(document_extractions))
        .route(
            "/documents/{id}/extractions/{version_id}",
            get(document_extraction),
        )
        .route("/documents/{id}/retry", post(retry_document))
        .route("/documents/{id}/interaction", post(interact_with_document))
        .route("/documents/{id}/confirm", post(confirm_document_evidence))
        .route("/events", get(events))
        .route(
            "/documents/{id}/interactions/{job_id}/events",
            get(document_interaction_events),
        )
        .route(
            "/purchases/{id}/evidence",
            get(read_purchase_evidence).post(link_purchase_evidence),
        )
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
                OidcError::InvalidGrant => StatusCode::UNAUTHORIZED,
                OidcError::AuthenticationFailed | OidcError::SessionExpired => {
                    StatusCode::UNAUTHORIZED
                }
                OidcError::TransientProvider(_) => {
                    tracing::error!(error = ?error, "OIDC callback provider request failed");
                    StatusCode::BAD_GATEWAY
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
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            "document storage unavailable",
        )
            .into_response();
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
    let mut analysis_filename = String::new();
    let mut analysis_mime_type = String::new();
    let mut analysis_data = None;
    let mut analysis_transform = Value::Null;
    let mut analysis_transform_was_supplied = false;
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
                    Ok(bytes) if !bytes.is_empty() && bytes.len() <= 20 << 20 => {
                        Some(bytes.to_vec())
                    }
                    _ => return (StatusCode::BAD_REQUEST, "invalid document size").into_response(),
                };
            }
            "analysisFile" => {
                analysis_filename = field.file_name().unwrap_or_default().to_owned();
                analysis_mime_type = field.content_type().unwrap_or_default().to_owned();
                analysis_data = match field.bytes().await {
                    Ok(bytes) if !bytes.is_empty() && bytes.len() <= 20 << 20 => {
                        Some(bytes.to_vec())
                    }
                    _ => {
                        return (StatusCode::BAD_REQUEST, "invalid analysis image").into_response();
                    }
                };
            }
            "analysisTransform" => {
                analysis_transform_was_supplied = true;
                let value = match field
                    .text()
                    .await
                    .ok()
                    .and_then(|text| serde_json::from_str::<Value>(&text).ok())
                {
                    Some(Value::Array(values))
                        if values.len() == 9
                            && values
                                .iter()
                                .all(|value| value.as_f64().is_some_and(f64::is_finite)) =>
                    {
                        Value::Array(values)
                    }
                    _ => {
                        return (StatusCode::BAD_REQUEST, "invalid analysis image").into_response();
                    }
                };
                analysis_transform = value;
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
    if analysis_data.is_some() != analysis_transform_was_supplied {
        return (StatusCode::BAD_REQUEST, "invalid analysis image").into_response();
    }
    let mime_type = match document_mime(&filename, &content_type, &data) {
        Some(mime_type) => mime_type,
        None => return StatusCode::UNSUPPORTED_MEDIA_TYPE.into_response(),
    };
    let analysis_mime = if let Some(analysis_data) = analysis_data.as_ref() {
        let Some(analysis_mime) =
            document_mime(&analysis_filename, &analysis_mime_type, analysis_data)
        else {
            return StatusCode::UNSUPPORTED_MEDIA_TYPE.into_response();
        };
        if !matches!(mime_type, "image/jpeg" | "image/png" | "image/webp")
            || !matches!(analysis_mime, "image/jpeg" | "image/png" | "image/webp")
            || !valid_analysis_transform(&analysis_transform)
        {
            return (
                StatusCode::UNSUPPORTED_MEDIA_TYPE,
                "unsupported analysis image type",
            )
                .into_response();
        }
        Some(analysis_mime)
    } else {
        None
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
    drop(pooled);
    let (object_key, sha256) = match store.put(household_id, mime_type, &data).await {
        Ok(stored) => stored,
        Err(error) => {
            tracing::error!(error = ?error, "Document object storage write failed");
            return StatusCode::BAD_GATEWAY.into_response();
        }
    };
    let stored_analysis = match (analysis_data.as_deref(), analysis_mime) {
        (Some(bytes), Some(analysis_mime)) => {
            match store.put(household_id, analysis_mime, bytes).await {
                Ok((key, hash)) => Some((key, hash, analysis_mime.to_owned())),
                Err(error) => {
                    tracing::error!(error = ?error, "Document analysis image storage write failed");
                    delete_unreferenced_object(&state.pool, store, &object_key).await;
                    return StatusCode::BAD_GATEWAY.into_response();
                }
            }
        }
        _ => None,
    };
    let cleanup_object_key = object_key.clone();
    let cleanup_analysis_key = stored_analysis.as_ref().map(|(key, _, _)| key.clone());
    let mut cleanup_attachment_keys = Vec::new();
    let mut attachments = Vec::new();
    if mime_type == "message/rfc822" {
        let parsed = match horse_worker_runtime::parse_email_attachments(&data) {
            Ok(attachments) => attachments,
            Err(_) => {
                delete_unreferenced_object(&state.pool, store, &cleanup_object_key).await;
                if let Some(key) = cleanup_analysis_key.as_deref() { delete_unreferenced_object(&state.pool, store, key).await; }
                return (StatusCode::BAD_REQUEST, "invalid email message").into_response();
            }
        };
        for (index, attachment) in parsed.into_iter().enumerate() {
            let (attachment_key, attachment_hash) = match store.put(household_id, &attachment.mime_type, &attachment.data).await {
                Ok(stored) => stored,
                Err(error) => {
                    tracing::error!(error = ?error, "Email attachment storage write failed");
                    delete_unreferenced_object(&state.pool, store, &cleanup_object_key).await;
                    if let Some(key) = cleanup_analysis_key.as_deref() { delete_unreferenced_object(&state.pool, store, key).await; }
                    for key in &cleanup_attachment_keys { delete_unreferenced_object(&state.pool, store, key).await; }
                    return StatusCode::BAD_GATEWAY.into_response();
                }
            };
            cleanup_attachment_keys.push(attachment_key.clone());
            attachments.push(evidence_application::RegisterDocumentAttachment {
                source_kind: "receipt".to_owned(),
                original_filename: attachment.filename,
                mime_type: attachment.mime_type,
                sha256: attachment_hash,
                object_key: attachment_key,
                metadata: json!({}),
                provider: "gemini_developer_api".to_owned(),
                model: model.clone(),
                purpose: purpose.clone(),
                idempotency_key: format!("upload:{}:attachment:{index}", uuid::Uuid::now_v7()),
                request: json!({}),
            });
        }
    }
    let mut metadata = json!({});
    if let Some((key, hash, analysis_mime)) = &stored_analysis {
        metadata["analysisObjectKey"] = json!(key);
        metadata["analysisSha256"] = json!(hash);
        metadata["analysisMimeType"] = json!(analysis_mime);
        metadata["analysisTransform"] = analysis_transform.clone();
    }
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
            metadata,
            provider: "gemini_developer_api".to_owned(),
            model,
            purpose,
            idempotency_key: format!("upload:{}", uuid::Uuid::now_v7()),
            request: Value::Object(Default::default()),
            attachments,
        },
    )
    .await;
    match result {
        Ok(document) => {
            if document.duplicate {
                delete_unreferenced_object(&state.pool, store, cleanup_object_key.as_str()).await;
                if let Some(key) = cleanup_analysis_key.as_deref() {
                    delete_unreferenced_object(&state.pool, store, key).await;
                }
                for key in &cleanup_attachment_keys { delete_unreferenced_object(&state.pool, store, key).await; }
            }
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
            (
                if document.duplicate {
                    StatusCode::OK
                } else {
                    StatusCode::ACCEPTED
                },
                Json(response),
            )
                .into_response()
        }
        Err(error) => {
            tracing::error!(error = ?error, "Document registration failed");
            delete_unreferenced_object(&state.pool, store, cleanup_object_key.as_str()).await;
            if let Some(key) = cleanup_analysis_key.as_deref() {
                delete_unreferenced_object(&state.pool, store, key).await;
            }
            for key in &cleanup_attachment_keys { delete_unreferenced_object(&state.pool, store, key).await; }
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

fn valid_analysis_transform(value: &Value) -> bool {
    let Some(values) = value.as_array() else {
        return false;
    };
    if values.len() != 9
        || values
            .iter()
            .any(|value| !value.as_f64().is_some_and(f64::is_finite))
    {
        return false;
    }
    let values = values
        .iter()
        .map(|value| value.as_f64().unwrap_or_default())
        .collect::<Vec<_>>();
    if values[6] != 0.0
        || values[7] != 0.0
        || values[8] != 1.0
        || (values[0] * values[4] - values[1] * values[3]).abs() < 1e-12
    {
        return false;
    }
    [[0.0, 0.0], [1.0, 0.0], [0.0, 1.0], [1.0, 1.0]]
        .iter()
        .all(|[x, y]| {
            let mapped_x = values[0] * *x + values[1] * *y + values[2];
            let mapped_y = values[3] * *x + values[4] * *y + values[5];
            (0.0..=1.0).contains(&mapped_x) && (0.0..=1.0).contains(&mapped_y)
        })
}

async fn delete_unreferenced_object(pool: &PgPool, store: &ObjectStore, object_key: &str) {
    use horse_postgres_common::schema::public::source_documents;
    let Ok(mut connection) = pool.get().await else {
        return;
    };
    let referenced = source_documents::table
        .filter(
            source_documents::object_key
                .eq(object_key)
                .or(source_documents::analysis_object_key.eq(Some(object_key.to_owned()))),
        )
        .select(source_documents::id)
        .first::<uuid::Uuid>(&mut *connection)
        .await
        .optional();
    match referenced {
        Ok(None) => {
            if let Err(error) = store.delete(object_key).await {
                tracing::warn!(error = ?error, "Unreferenced document object cleanup failed");
            }
        }
        Ok(Some(_)) | Err(_) => {}
    }
}

async fn list_documents(
    State(state): State<ApiState>,
    headers: HeaderMap,
    axum::extract::Query(query): axum::extract::Query<std::collections::HashMap<String, String>>,
) -> Response {
    let cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    let principal = match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => principal,
        Ok(None) => return StatusCode::UNAUTHORIZED.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let household_id = match query
        .get("householdId")
        .and_then(|value| decode_household_id(value))
    {
        Some(id) => id,
        None => return (StatusCode::NOT_FOUND, "household not found").into_response(),
    };
    let first = match query.get("first") {
        None => 50,
        Some(value) if value.is_empty() => 50,
        Some(value) => match value.parse::<i64>() {
            Ok(value) if (1..=100).contains(&value) => value,
            _ => {
                return (StatusCode::BAD_REQUEST, "first must be between 1 and 100")
                    .into_response();
            }
        },
    };
    let source_kind = query.get("sourceKind").filter(|value| !value.is_empty());
    if source_kind.is_some_and(|value| {
        !matches!(
            value.as_str(),
            "receipt" | "order" | "email" | "card" | "bank"
        )
    }) {
        return (StatusCode::BAD_REQUEST, "invalid source kind").into_response();
    }
    let from = match parse_document_date(query.get("from"), false) {
        Ok(value) => value,
        Err(_) => return (StatusCode::BAD_REQUEST, "invalid from date").into_response(),
    };
    let to = match parse_document_date(query.get("to"), true) {
        Ok(value) => value,
        Err(_) => return (StatusCode::BAD_REQUEST, "invalid to date").into_response(),
    };
    if from.zip(to).is_some_and(|(from, to)| from >= to) {
        return (StatusCode::BAD_REQUEST, "invalid date range").into_response();
    }
    let confirmed = match query
        .get("confirmed")
        .filter(|value| !value.is_empty())
        .map(String::as_str)
    {
        Some("true") => Some(true),
        Some("false") => Some(false),
        Some(_) => return (StatusCode::BAD_REQUEST, "invalid confirmation filter").into_response(),
        None => None,
    };
    let store_filter = query
        .get("store")
        .map(|value| value.trim())
        .unwrap_or_default()
        .to_owned();
    let filter_identity = document_filter_identity(
        household_id,
        &store_filter,
        source_kind.map(String::as_str).unwrap_or_default(),
        from,
        to,
        confirmed,
    );
    let cursor = match query.get("after").filter(|value| !value.is_empty()) {
        Some(value) => match decode_document_cursor(value, &filter_identity) {
            Some(cursor) => Some(cursor),
            None => return (StatusCode::BAD_REQUEST, "invalid cursor").into_response(),
        },
        None => None,
    };
    use horse_postgres_common::schema::public::{
        correction_evidence_links, evidence_links, extraction_jobs, household_members,
        source_documents,
    };
    let mut pooled = match state.pool.get().await {
        Ok(pooled) => pooled,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
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
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    }
    let mut documents_query = source_documents::table
        .filter(source_documents::household_id.eq(household_id))
        .into_boxed();
    if let Some(source_kind) = source_kind {
        documents_query = documents_query.filter(source_documents::source_kind.eq(source_kind));
    }
    if let Some(from) = from {
        documents_query = documents_query.filter(source_documents::created_at.ge(from));
    }
    if let Some(to) = to {
        documents_query = documents_query.filter(source_documents::created_at.lt(to));
    }
    if let Some((cursor_created_at, cursor_id)) = cursor {
        documents_query = documents_query.filter(
            source_documents::created_at
                .lt(cursor_created_at)
                .or(source_documents::created_at
                    .eq(cursor_created_at)
                    .and(source_documents::id.lt(cursor_id))),
        );
    }
    if let Some(confirmed) = confirmed {
        use horse_postgres_common::schema::public::source_confirmations;
        let has_confirmation = diesel::dsl::exists(
            source_confirmations::table
                .filter(source_confirmations::household_id.eq(household_id))
                .filter(source_confirmations::source_document_id.eq(source_documents::id))
                .filter(source_confirmations::status.eq("confirmed")),
        );
        documents_query = if confirmed {
            documents_query.filter(has_confirmation)
        } else {
            documents_query.filter(diesel::dsl::not(has_confirmation))
        };
    }
    let documents = documents_query
        .order_by(source_documents::created_at.desc())
        .then_order_by(source_documents::id.desc())
        .limit(if store_filter.is_empty() {
            first + 1
        } else {
            5001
        })
        .select((
            source_documents::id,
            source_documents::sha256,
            source_documents::mime_type,
            source_documents::source_kind,
            source_documents::original_filename,
            source_documents::created_at,
        ))
        .load::<(
            uuid::Uuid,
            String,
            String,
            String,
            String,
            chrono::DateTime<chrono::Utc>,
        )>(&mut *pooled)
        .await;
    let documents = match documents {
        Ok(documents) => documents,
        Err(error) => {
            tracing::error!(error = ?error, "Document list lookup failed");
            return StatusCode::SERVICE_UNAVAILABLE.into_response();
        }
    };
    let mut results = Vec::new();
    for (id, sha256, mime_type, source_kind, name, created_at) in documents {
        let latest = extraction_jobs::table
            .filter(extraction_jobs::household_id.eq(household_id))
            .filter(extraction_jobs::source_document_id.eq(id))
            .order_by(extraction_jobs::version.desc())
            .select((
                extraction_jobs::id,
                extraction_jobs::status,
                extraction_jobs::model,
                extraction_jobs::error_code,
                extraction_jobs::result,
                extraction_jobs::request,
                extraction_jobs::interaction_reply_preview,
            ))
            .first::<(
                uuid::Uuid,
                String,
                String,
                Option<String>,
                Option<Value>,
                Value,
                String,
            )>(&mut *pooled)
            .await
            .optional();
        let (job_id, status, model, error_code, mut result, request, interaction_reply_preview) =
            match latest {
                Ok(Some(job)) => job,
                Ok(None) => (
                    uuid::Uuid::nil(),
                    "queued".to_owned(),
                    String::new(),
                    None,
                    None,
                    Value::Null,
                    String::new(),
                ),
                Err(error) => {
                    tracing::error!(error = ?error, "Document extraction status lookup failed");
                    return StatusCode::SERVICE_UNAVAILABLE.into_response();
                }
            };
        if result.is_none() {
            result = extraction_jobs::table
                .filter(extraction_jobs::household_id.eq(household_id))
                .filter(extraction_jobs::source_document_id.eq(id))
                .filter(extraction_jobs::status.eq("succeeded"))
                .order_by(extraction_jobs::version.desc())
                .select(extraction_jobs::result)
                .first::<Option<Value>>(&mut *pooled)
                .await
                .optional()
                .unwrap_or(None)
                .flatten();
        }
        let has_evidence = diesel::select(
            diesel::dsl::exists(
                evidence_links::table
                    .filter(evidence_links::household_id.eq(household_id))
                    .filter(evidence_links::source_document_id.eq(id)),
            )
            .or(diesel::dsl::exists(
                correction_evidence_links::table
                    .filter(correction_evidence_links::household_id.eq(household_id))
                    .filter(correction_evidence_links::source_document_id.eq(id)),
            )),
        )
        .get_result::<bool>(&mut *pooled)
        .await
        .unwrap_or(false);
        let (confirmations, attachments, attachment_parent) =
            match document_relationships(&mut pooled, household_id, id).await {
                Ok(relationships) => relationships,
                Err(error) => {
                    tracing::error!(error = ?error, "Document relationships lookup failed");
                    return StatusCode::SERVICE_UNAVAILABLE.into_response();
                }
            };
        let model = request
            .get("model")
            .and_then(Value::as_str)
            .filter(|value| !value.is_empty())
            .unwrap_or(&model);
        let pending_interaction =
            pending_document_interaction(&job_id, &status, &request, &interaction_reply_preview);
        if !store_filter.is_empty() {
            let needle = store_filter.to_lowercase();
            let receipt_match = result
                .as_ref()
                .and_then(|value| value.pointer("/receipt/merchant"))
                .and_then(Value::as_str)
                .is_some_and(|merchant| merchant.to_lowercase().contains(&needle));
            let statement_match = result
                .as_ref()
                .and_then(|value| value.get("statements"))
                .and_then(Value::as_array)
                .is_some_and(|entries| {
                    entries.iter().any(|entry| {
                        entry
                            .get("merchant")
                            .and_then(Value::as_str)
                            .is_some_and(|merchant| merchant.to_lowercase().contains(&needle))
                    })
                });
            if !receipt_match && !statement_match {
                continue;
            }
        }
        results.push(json!({
            "id": id,
            "householdId": household_id,
            "name": name,
            "sha256": sha256,
            "mimeType": mime_type,
            "sourceKind": source_kind,
            "status": status,
            "model": model,
            "pendingInteraction": pending_interaction,
            "errorCode": error_code,
            "jobId": if job_id.is_nil() { None } else { Some(job_id) },
            "result": result,
            "createdAt": created_at,
            "confirmations": confirmations,
            "hasEvidence": has_evidence,
            "attachments": attachments,
            "attachmentParent": attachment_parent
        }));
        if results.len() > first as usize {
            break;
        }
    }
    let next_cursor = if results.len() > first as usize {
        results.truncate(first as usize);
        results.last().and_then(|last| {
            let id = last
                .get("id")?
                .as_str()
                .and_then(|value| uuid::Uuid::parse_str(value).ok())?;
            let created_at = last
                .get("createdAt")?
                .as_str()
                .and_then(|value| chrono::DateTime::parse_from_rfc3339(value).ok())?
                .with_timezone(&chrono::Utc);
            Some(encode_document_cursor(id, created_at, &filter_identity))
        })
    } else {
        None
    };
    Json(json!({"documents": results, "nextCursor": next_cursor})).into_response()
}

fn parse_document_date(
    value: Option<&String>,
    exclusive_end: bool,
) -> Result<Option<chrono::DateTime<chrono::Utc>>, ()> {
    let Some(value) = value.filter(|value| !value.is_empty()) else {
        return Ok(None);
    };
    let date = chrono::NaiveDate::parse_from_str(value, "%Y-%m-%d").map_err(|_| ())?;
    let date = if exclusive_end {
        date.succ_opt().ok_or(())?
    } else {
        date
    };
    Ok(Some(
        date.and_hms_opt(0, 0, 0).ok_or(())?.and_utc() - chrono::Duration::hours(9),
    ))
}

fn document_filter_identity(
    household_id: uuid::Uuid,
    store: &str,
    source_kind: &str,
    from: Option<chrono::DateTime<chrono::Utc>>,
    to: Option<chrono::DateTime<chrono::Utc>>,
    confirmed: Option<bool>,
) -> String {
    use sha2::{Digest, Sha256};
    let parts = [
        household_id.to_string(),
        store.to_owned(),
        source_kind.to_owned(),
        from.map(|date| date.to_rfc3339()).unwrap_or_default(),
        to.map(|date| date.to_rfc3339()).unwrap_or_default(),
        confirmed.map(|value| value.to_string()).unwrap_or_default(),
    ];
    let hash = Sha256::digest(parts.join("\0").as_bytes());
    hex::encode(hash)
}

fn encode_document_cursor(
    id: uuid::Uuid,
    created_at: chrono::DateTime<chrono::Utc>,
    filter_hash: &str,
) -> String {
    use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
    URL_SAFE_NO_PAD.encode(format!("{id}|{}|{filter_hash}", created_at.to_rfc3339()))
}

fn decode_document_cursor(
    value: &str,
    expected_filter_hash: &str,
) -> Option<(chrono::DateTime<chrono::Utc>, uuid::Uuid)> {
    use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
    let decoded = URL_SAFE_NO_PAD.decode(value).ok()?;
    let text = std::str::from_utf8(&decoded).ok()?;
    let mut parts = text.split('|');
    let id = uuid::Uuid::parse_str(parts.next()?).ok()?;
    let created_at = chrono::DateTime::parse_from_rfc3339(parts.next()?)
        .ok()?
        .with_timezone(&chrono::Utc);
    if parts.next()? != expected_filter_hash || parts.next().is_some() {
        return None;
    }
    Some((created_at, id))
}

async fn document_relationships(
    connection: &mut diesel_async::AsyncPgConnection,
    household_id: uuid::Uuid,
    document_id: uuid::Uuid,
) -> Result<(Vec<Value>, Vec<Value>, Option<Value>), diesel::result::Error> {
    use horse_postgres_common::schema::public::{
        extraction_jobs, source_confirmations, source_document_attachments, source_documents,
    };
    let confirmation_rows = source_confirmations::table
        .filter(source_confirmations::household_id.eq(household_id))
        .filter(source_confirmations::source_document_id.eq(document_id))
        .order_by(source_confirmations::source_reference.asc())
        .select((
            source_confirmations::source_reference,
            source_confirmations::purchase_id,
            source_confirmations::correction_id,
            source_confirmations::account_movement_id,
            source_confirmations::account_income_id,
            source_confirmations::transfer_id,
        ))
        .load::<(
            String,
            Option<uuid::Uuid>,
            Option<uuid::Uuid>,
            Option<uuid::Uuid>,
            Option<uuid::Uuid>,
            Option<uuid::Uuid>,
        )>(connection)
        .await?;
    let confirmations = confirmation_rows.into_iter().map(|(source_reference, purchase_id, correction_id, account_movement_id, account_income_id, transfer_id)| {
        let mut confirmation = json!({"sourceReference": source_reference, "purchaseId": purchase_id.map(|id| id.to_string()).unwrap_or_default()});
        if let Some(id) = correction_id { confirmation["correctionId"] = json!(id); }
        if let Some(id) = account_movement_id { confirmation["accountMovementId"] = json!(id); }
        if let Some(id) = account_income_id { confirmation["accountIncomeId"] = json!(id); }
        if let Some(id) = transfer_id { confirmation["transferId"] = json!(id); }
        confirmation
    }).collect::<Vec<_>>();
    let children = source_document_attachments::table
        .filter(source_document_attachments::household_id.eq(household_id))
        .filter(source_document_attachments::parent_document_id.eq(document_id))
        .order_by(source_document_attachments::attachment_index.asc())
        .select((
            source_document_attachments::child_document_id,
            source_document_attachments::filename,
            source_document_attachments::mime_type,
        ))
        .load::<(uuid::Uuid, String, String)>(connection)
        .await?;
    let mut attachments = Vec::with_capacity(children.len());
    for (child_id, filename, mime_type) in children {
        let child_name = source_documents::table
            .filter(source_documents::household_id.eq(household_id))
            .filter(source_documents::id.eq(child_id))
            .select(source_documents::original_filename)
            .first::<String>(connection)
            .await?;
        let latest_job = extraction_jobs::table
            .filter(extraction_jobs::household_id.eq(household_id))
            .filter(extraction_jobs::source_document_id.eq(child_id))
            .order_by(extraction_jobs::version.desc())
            .select((extraction_jobs::id, extraction_jobs::status))
            .first::<(uuid::Uuid, String)>(connection)
            .await
            .optional()?;
        attachments.push(json!({
            "id": child_id,
            "name": child_name,
            "filename": filename,
            "mimeType": mime_type,
            "status": latest_job.as_ref().map(|job| job.1.as_str()),
            "jobId": latest_job.map(|job| job.0)
        }));
    }
    let parent_relation = source_document_attachments::table
        .filter(source_document_attachments::household_id.eq(household_id))
        .filter(source_document_attachments::child_document_id.eq(document_id))
        .order_by(source_document_attachments::parent_document_id.asc())
        .order_by(source_document_attachments::attachment_index.asc())
        .select((
            source_document_attachments::parent_document_id,
            source_document_attachments::filename,
            source_document_attachments::attachment_index,
        ))
        .first::<(uuid::Uuid, String, i32)>(connection)
        .await
        .optional()?;
    let attachment_parent = if let Some((parent_id, filename, index)) = parent_relation {
        let name = source_documents::table
            .filter(source_documents::household_id.eq(household_id))
            .filter(source_documents::id.eq(parent_id))
            .select(source_documents::original_filename)
            .first::<String>(connection)
            .await?;
        Some(json!({"id": parent_id, "name": name, "filename": filename, "index": index}))
    } else {
        None
    };
    Ok((confirmations, attachments, attachment_parent))
}

fn pending_document_interaction(
    job_id: &uuid::Uuid,
    status: &str,
    request: &Value,
    preview: &str,
) -> Option<Value> {
    if !matches!(status, "queued" | "running" | "failed")
        || request.get("purpose").and_then(Value::as_str) != Some("interactive")
    {
        return None;
    }
    let message = request
        .get("clarification")
        .and_then(Value::as_str)
        .filter(|value| !value.is_empty())?;
    let mut interaction = json!({"jobId": job_id, "status": status, "message": message});
    if !preview.is_empty() {
        interaction["replyPreview"] = json!(preview);
    }
    for (key, value_key) in [("model", "model"), ("baseJobId", "baseJobId")] {
        if let Some(value) = request
            .get(value_key)
            .and_then(Value::as_str)
            .filter(|value| !value.is_empty())
        {
            interaction[key] = json!(value);
        }
    }
    Some(interaction)
}

async fn read_document(
    State(state): State<ApiState>,
    Path(raw_id): Path<String>,
    headers: HeaderMap,
) -> Response {
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
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    use horse_postgres_common::schema::public::{
        correction_evidence_links, evidence_links, extraction_jobs, household_members,
        source_documents,
    };
    let mut pooled = match state.pool.get().await {
        Ok(pooled) => pooled,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let document = source_documents::table
        .inner_join(
            household_members::table
                .on(household_members::household_id.eq(source_documents::household_id)),
        )
        .filter(source_documents::id.eq(document_id))
        .filter(household_members::member_id.eq(principal.member_id))
        .select((
            source_documents::household_id,
            source_documents::sha256,
            source_documents::mime_type,
            source_documents::source_kind,
            source_documents::original_filename,
            source_documents::created_at,
        ))
        .first::<(
            uuid::Uuid,
            String,
            String,
            String,
            String,
            chrono::DateTime<chrono::Utc>,
        )>(&mut *pooled)
        .await;
    let (household_id, sha256, mime_type, source_kind, name, created_at) = match document {
        Ok(document) => document,
        Err(diesel::result::Error::NotFound) => return StatusCode::NOT_FOUND.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let latest = extraction_jobs::table
        .filter(extraction_jobs::household_id.eq(household_id))
        .filter(extraction_jobs::source_document_id.eq(document_id))
        .order_by(extraction_jobs::version.desc())
        .select((
            extraction_jobs::id,
            extraction_jobs::status,
            extraction_jobs::model,
            extraction_jobs::error_code,
            extraction_jobs::result,
            extraction_jobs::request,
            extraction_jobs::interaction_reply_preview,
        ))
        .first::<(
            uuid::Uuid,
            String,
            String,
            Option<String>,
            Option<Value>,
            Value,
            String,
        )>(&mut *pooled)
        .await;
    let (job_id, status, model, error_code, mut result, request, interaction_reply_preview) =
        match latest {
            Ok(job) => job,
            Err(diesel::result::Error::NotFound) => (
                uuid::Uuid::nil(),
                "queued".to_owned(),
                String::new(),
                None,
                None,
                Value::Null,
                String::new(),
            ),
            Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
        };
    if result.is_none() {
        result = extraction_jobs::table
            .filter(extraction_jobs::household_id.eq(household_id))
            .filter(extraction_jobs::source_document_id.eq(document_id))
            .filter(extraction_jobs::status.eq("succeeded"))
            .order_by(extraction_jobs::version.desc())
            .select(extraction_jobs::result)
            .first::<Option<Value>>(&mut *pooled)
            .await
            .optional()
            .unwrap_or(None)
            .flatten();
    }
    let has_evidence = diesel::select(
        diesel::dsl::exists(
            evidence_links::table
                .filter(evidence_links::household_id.eq(household_id))
                .filter(evidence_links::source_document_id.eq(document_id)),
        )
        .or(diesel::dsl::exists(
            correction_evidence_links::table
                .filter(correction_evidence_links::household_id.eq(household_id))
                .filter(correction_evidence_links::source_document_id.eq(document_id)),
        )),
    )
    .get_result::<bool>(&mut *pooled)
    .await
    .unwrap_or(false);
    let (confirmations, attachments, attachment_parent) =
        match document_relationships(&mut pooled, household_id, document_id).await {
            Ok(relationships) => relationships,
            Err(error) => {
                tracing::error!(error = ?error, "Document relationships lookup failed");
                return StatusCode::SERVICE_UNAVAILABLE.into_response();
            }
        };
    let model = request
        .get("model")
        .and_then(Value::as_str)
        .filter(|value| !value.is_empty())
        .unwrap_or(&model);
    let pending_interaction =
        pending_document_interaction(&job_id, &status, &request, &interaction_reply_preview);
    Json(json!({
        "id": document_id,
        "householdId": household_id,
        "name": name,
        "sha256": sha256,
        "mimeType": mime_type,
        "sourceKind": source_kind,
        "status": status,
        "model": model,
        "pendingInteraction": pending_interaction,
        "errorCode": error_code,
        "jobId": if job_id.is_nil() { None } else { Some(job_id) },
        "result": result,
        "createdAt": created_at,
        "confirmations": confirmations,
        "hasEvidence": has_evidence,
        "attachments": attachments,
        "attachmentParent": attachment_parent
    }))
    .into_response()
}

async fn delete_document(
    State(state): State<ApiState>,
    Path(raw_id): Path<String>,
    headers: HeaderMap,
) -> Response {
    if !same_origin(&headers, &state.config) {
        return (StatusCode::FORBIDDEN, "request origin rejected").into_response();
    }
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
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    use horse_postgres_common::schema::public::{
        evidence_links, extraction_jobs, household_members, source_confirmations,
        source_document_attachments, source_documents,
    };
    let mut pooled = match state.pool.get().await {
        Ok(pooled) => pooled,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let pending = (&mut *pooled)
        .transaction::<(String, Option<String>), diesel::result::Error, _>(async |conn| {
            let document = source_documents::table
                .filter(source_documents::id.eq(document_id))
                .for_update()
                .select((
                    source_documents::household_id,
                    source_documents::object_key,
                    source_documents::analysis_object_key,
                    source_documents::metadata,
                ))
                .first::<(uuid::Uuid, String, Option<String>, Value)>(conn)
                .await?;
            let household_id = document.0;
            let member_exists = diesel::select(diesel::dsl::exists(
                household_members::table
                    .filter(household_members::household_id.eq(household_id))
                    .filter(household_members::member_id.eq(principal.member_id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            if !member_exists {
                return Err(diesel::result::Error::NotFound);
            }
            let status = extraction_jobs::table
                .filter(extraction_jobs::household_id.eq(household_id))
                .filter(extraction_jobs::source_document_id.eq(document_id))
                .order_by(extraction_jobs::version.desc())
                .select(extraction_jobs::status)
                .first::<String>(conn)
                .await?;
            if status != "succeeded" && status != "failed" {
                return Err(diesel::result::Error::RollbackTransaction);
            }
            let has_evidence = diesel::select(diesel::dsl::exists(
                evidence_links::table
                    .filter(evidence_links::household_id.eq(household_id))
                    .filter(evidence_links::source_document_id.eq(document_id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            let has_confirmation = diesel::select(diesel::dsl::exists(
                source_confirmations::table
                    .filter(source_confirmations::household_id.eq(household_id))
                    .filter(source_confirmations::source_document_id.eq(document_id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            let has_attachments = diesel::select(diesel::dsl::exists(
                source_document_attachments::table.filter(
                    source_document_attachments::parent_document_id
                        .eq(document_id)
                        .or(source_document_attachments::child_document_id.eq(document_id)),
                ),
            ))
            .get_result::<bool>(conn)
            .await?;
            if has_evidence || has_confirmation || has_attachments {
                return Err(diesel::result::Error::RollbackTransaction);
            }
            let mut metadata = document.3;
            let Some(metadata) = metadata.as_object_mut() else {
                return Err(diesel::result::Error::RollbackTransaction);
            };
            metadata.insert("deletionPending".to_owned(), Value::Bool(true));
            diesel::update(source_documents::table.filter(source_documents::id.eq(document_id)))
                .set(source_documents::metadata.eq(Value::Object(metadata.clone())))
                .execute(conn)
                .await?;
            Ok((document.1, document.2))
        })
        .await;
    let (object_key, analysis_key) = match pending {
        Ok(keys) => keys,
        Err(diesel::result::Error::NotFound) => return StatusCode::NOT_FOUND.into_response(),
        Err(diesel::result::Error::RollbackTransaction) => {
            return (StatusCode::CONFLICT, "document cannot be deleted").into_response();
        }
        Err(error) => {
            tracing::error!(error = ?error, "Document deletion validation failed");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };
    drop(pooled);
    let mut keys = vec![object_key];
    if let Some(key) = analysis_key {
        if !keys.contains(&key) {
            keys.push(key);
        }
    }
    for key in &keys {
        let mut connection = match state.pool.get().await {
            Ok(connection) => connection,
            Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
        };
        let referenced = source_documents::table
            .filter(source_documents::id.ne(document_id))
            .filter(
                source_documents::object_key
                    .eq(key)
                    .or(source_documents::analysis_object_key.eq(Some(key.as_str()))),
            )
            .select(source_documents::id)
            .first::<uuid::Uuid>(&mut *connection)
            .await
            .optional();
        match referenced {
            Ok(Some(_)) => continue,
            Ok(None) => {}
            Err(error) => {
                tracing::error!(error = ?error, "Document object reference lookup failed");
                return StatusCode::SERVICE_UNAVAILABLE.into_response();
            }
        }
        drop(connection);
        if let Err(error) = store.delete(key).await {
            tracing::error!(error = ?error, "Document object deletion failed");
            return StatusCode::BAD_GATEWAY.into_response();
        }
    }
    let mut connection = match state.pool.get().await {
        Ok(connection) => connection,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let deleted = (&mut *connection)
        .transaction::<usize, diesel::result::Error, _>(async |conn| {
            let metadata = source_documents::table
                .filter(source_documents::id.eq(document_id))
                .for_update()
                .select(source_documents::metadata)
                .first::<Value>(conn)
                .await?;
            if metadata.get("deletionPending") != Some(&Value::Bool(true)) {
                return Err(diesel::result::Error::RollbackTransaction);
            }
            let has_evidence = diesel::select(diesel::dsl::exists(
                evidence_links::table.filter(evidence_links::source_document_id.eq(document_id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            let has_confirmation = diesel::select(diesel::dsl::exists(
                source_confirmations::table
                    .filter(source_confirmations::source_document_id.eq(document_id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            let has_attachments = diesel::select(diesel::dsl::exists(
                source_document_attachments::table.filter(
                    source_document_attachments::parent_document_id
                        .eq(document_id)
                        .or(source_document_attachments::child_document_id.eq(document_id)),
                ),
            ))
            .get_result::<bool>(conn)
            .await?;
            if has_evidence || has_confirmation || has_attachments {
                return Err(diesel::result::Error::RollbackTransaction);
            }
            diesel::delete(source_documents::table.filter(source_documents::id.eq(document_id)))
                .execute(conn)
                .await
        })
        .await;
    match deleted {
        Ok(1) => StatusCode::NO_CONTENT.into_response(),
        Err(diesel::result::Error::RollbackTransaction) => (
            StatusCode::CONFLICT,
            "document deletion is no longer allowed",
        )
            .into_response(),
        Err(diesel::result::Error::NotFound) => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => (StatusCode::CONFLICT, "document deletion is not pending").into_response(),
        Err(error) => {
            tracing::error!(error = ?error, "Document deletion failed");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

fn decode_household_id(value: &str) -> Option<uuid::Uuid> {
    if let Ok(id) = uuid::Uuid::parse_str(value) {
        return Some(id);
    }
    use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
    let decoded = URL_SAFE_NO_PAD.decode(value).ok()?;
    let separator = decoded.iter().position(|byte| *byte == b':')?;
    let (type_name, payload) = decoded.split_at(separator);
    let payload = &payload[1..];
    if type_name != b"Household" || payload.len() != 16 {
        return None;
    }
    uuid::Uuid::from_slice(payload).ok()
}

fn document_mime(filename: &str, declared: &str, data: &[u8]) -> Option<&'static str> {
    let lower = filename.to_ascii_lowercase();
    if declared == "message/rfc822" && (lower.ends_with(".eml") || lower.ends_with(".rfc822")) {
        Some("message/rfc822")
    } else if declared == "text/csv" && lower.ends_with(".csv") {
        Some("text/csv")
    } else if data.starts_with(b"%PDF-")
        && (declared == "application/pdf" || lower.ends_with(".pdf"))
    {
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
            household_members::table
                .on(household_members::household_id.eq(source_documents::household_id)),
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
        let valid = ["top", "left", "bottom", "right"].iter().all(|key| {
            crop.get(key)
                .and_then(Value::as_f64)
                .is_some_and(f64::is_finite)
        });
        let page = crop.get("page").and_then(Value::as_u64).unwrap_or(0);
        let top = crop.get("top").and_then(Value::as_f64).unwrap_or(0.0);
        let left = crop.get("left").and_then(Value::as_f64).unwrap_or(0.0);
        let bottom = crop.get("bottom").and_then(Value::as_f64).unwrap_or(0.0);
        let right = crop.get("right").and_then(Value::as_f64).unwrap_or(0.0);
        if !valid
            || page > u64::from(u32::MAX)
            || top < 0.0
            || left < 0.0
            || bottom > 1.0
            || right > 1.0
            || top >= bottom
            || left >= right
        {
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
            household_members::table
                .on(household_members::household_id.eq(source_documents::household_id)),
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
    request.insert(
        "purpose".to_owned(),
        Value::String("interactive".to_owned()),
    );
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
        Ok(job_id) => (
            StatusCode::ACCEPTED,
            Json(json!({"jobId": job_id, "status": "queued"})),
        )
            .into_response(),
        Err(error) => {
            tracing::error!(error = ?error, "Document retry registration failed");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

async fn document_extractions(
    State(state): State<ApiState>,
    Path(raw_id): Path<String>,
    headers: HeaderMap,
) -> Response {
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
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    use horse_postgres_common::schema::public::{
        extraction_jobs, household_members, source_documents,
    };
    let mut pooled = match state.pool.get().await {
        Ok(pooled) => pooled,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let household_id = source_documents::table
        .inner_join(
            household_members::table
                .on(household_members::household_id.eq(source_documents::household_id)),
        )
        .filter(source_documents::id.eq(document_id))
        .filter(household_members::member_id.eq(principal.member_id))
        .select(source_documents::household_id)
        .first::<uuid::Uuid>(&mut *pooled)
        .await;
    let household_id = match household_id {
        Ok(id) => id,
        Err(diesel::result::Error::NotFound) => return StatusCode::NOT_FOUND.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let jobs = extraction_jobs::table
        .filter(extraction_jobs::household_id.eq(household_id))
        .filter(extraction_jobs::source_document_id.eq(document_id))
        .filter(extraction_jobs::status.eq("succeeded"))
        .order_by(extraction_jobs::version.desc())
        .select((
            extraction_jobs::id,
            extraction_jobs::version,
            extraction_jobs::model,
            extraction_jobs::request,
            extraction_jobs::result,
            extraction_jobs::created_at,
        ))
        .load::<(
            uuid::Uuid,
            i32,
            String,
            Value,
            Option<Value>,
            chrono::DateTime<chrono::Utc>,
        )>(&mut *pooled)
        .await;
    match jobs {
        Ok(jobs) => {
            let versions = jobs
                .into_iter()
                .filter_map(|(id, version, model, request, result, created_at)| {
                    let result = result?;
                    let conversation_turns = result
                        .get("conversation")
                        .and_then(Value::as_array)
                        .map(|conversation| conversation.len() / 2)
                        .unwrap_or_default();
                    let model = request
                        .get("model")
                        .and_then(Value::as_str)
                        .filter(|model| !model.is_empty())
                        .unwrap_or(&model);
                    Some(json!({
                        "id": id,
                        "version": version,
                        "model": model,
                        "createdAt": created_at,
                        "conversationTurns": conversation_turns
                    }))
                })
                .collect::<Vec<_>>();
            Json(json!({"versions": versions})).into_response()
        }
        Err(error) => {
            tracing::error!(error = ?error, "Document extraction history lookup failed");
            StatusCode::SERVICE_UNAVAILABLE.into_response()
        }
    }
}

async fn document_extraction(
    State(state): State<ApiState>,
    Path((raw_id, raw_version_id)): Path<(String, String)>,
    headers: HeaderMap,
) -> Response {
    let (Ok(document_id), Ok(version_id)) = (
        uuid::Uuid::parse_str(&raw_id),
        uuid::Uuid::parse_str(&raw_version_id),
    ) else {
        return StatusCode::NOT_FOUND.into_response();
    };
    let cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    let principal = match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => principal,
        Ok(None) => return StatusCode::UNAUTHORIZED.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    use horse_postgres_common::schema::public::{
        extraction_jobs, household_members, source_documents,
    };
    let mut pooled = match state.pool.get().await {
        Ok(pooled) => pooled,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let household_id = source_documents::table
        .inner_join(
            household_members::table
                .on(household_members::household_id.eq(source_documents::household_id)),
        )
        .filter(source_documents::id.eq(document_id))
        .filter(household_members::member_id.eq(principal.member_id))
        .select(source_documents::household_id)
        .first::<uuid::Uuid>(&mut *pooled)
        .await;
    let Ok(household_id) = household_id else {
        return StatusCode::NOT_FOUND.into_response();
    };
    let job = extraction_jobs::table
        .filter(extraction_jobs::household_id.eq(household_id))
        .filter(extraction_jobs::source_document_id.eq(document_id))
        .filter(extraction_jobs::id.eq(version_id))
        .filter(extraction_jobs::status.eq("succeeded"))
        .select((
            extraction_jobs::id,
            extraction_jobs::version,
            extraction_jobs::model,
            extraction_jobs::request,
            extraction_jobs::result,
            extraction_jobs::created_at,
        ))
        .first::<(
            uuid::Uuid,
            i32,
            String,
            Value,
            Option<Value>,
            chrono::DateTime<chrono::Utc>,
        )>(&mut *pooled)
        .await;
    match job {
        Ok((id, version, model, request, Some(result), created_at)) => {
            let conversation_turns = result
                .get("conversation")
                .and_then(Value::as_array)
                .map(|conversation| conversation.len() / 2)
                .unwrap_or_default();
            let model = request
                .get("model")
                .and_then(Value::as_str)
                .filter(|model| !model.is_empty())
                .unwrap_or(&model);
            Json(json!({
                "id": id,
                "version": version,
                "model": model,
                "createdAt": created_at,
                "conversationTurns": conversation_turns,
                "result": result
            }))
            .into_response()
        }
        Ok(_) | Err(diesel::result::Error::NotFound) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!(error = ?error, "Document extraction lookup failed");
            StatusCode::SERVICE_UNAVAILABLE.into_response()
        }
    }
}

async fn interact_with_document(
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
    let message = input
        .get("message")
        .and_then(Value::as_str)
        .unwrap_or_default();
    let requested_model = input
        .get("model")
        .and_then(Value::as_str)
        .unwrap_or_default();
    if message.trim().is_empty() || message.len() > 8_000 || !matches!(requested_model, "" | "pro")
    {
        return (
            StatusCode::BAD_REQUEST,
            "invalid document interaction input",
        )
            .into_response();
    }
    let base_job_id = match input.get("baseJobId").and_then(Value::as_str) {
        Some(value) => match uuid::Uuid::parse_str(value) {
            Ok(id) => Some(id),
            Err(_) => {
                return (
                    StatusCode::BAD_REQUEST,
                    "invalid document interaction input",
                )
                    .into_response();
            }
        },
        None => None,
    };
    let cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    let principal = match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => principal,
        Ok(None) => return StatusCode::UNAUTHORIZED.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    use horse_postgres_common::schema::public::{
        extraction_jobs, household_members, source_documents,
    };
    let mut pooled = match state.pool.get().await {
        Ok(pooled) => pooled,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let household_id = source_documents::table
        .inner_join(
            household_members::table
                .on(household_members::household_id.eq(source_documents::household_id)),
        )
        .filter(source_documents::id.eq(document_id))
        .filter(household_members::member_id.eq(principal.member_id))
        .select(source_documents::household_id)
        .first::<uuid::Uuid>(&mut *pooled)
        .await;
    let household_id = match household_id {
        Ok(id) => id,
        Err(diesel::result::Error::NotFound) => return StatusCode::NOT_FOUND.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let latest_status = extraction_jobs::table
        .filter(extraction_jobs::household_id.eq(household_id))
        .filter(extraction_jobs::source_document_id.eq(document_id))
        .order_by(extraction_jobs::version.desc())
        .select(extraction_jobs::status)
        .first::<String>(&mut *pooled)
        .await;
    match latest_status {
        Ok(status) if status == "queued" || status == "running" => {
            return (
                StatusCode::CONFLICT,
                "document extraction is already running",
            )
                .into_response();
        }
        Ok(_) => {}
        Err(diesel::result::Error::NotFound) => {
            return (
                StatusCode::CONFLICT,
                "document interaction requires an existing extraction",
            )
                .into_response();
        }
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    }
    let base_job_query = extraction_jobs::table
        .filter(extraction_jobs::household_id.eq(household_id))
        .filter(extraction_jobs::source_document_id.eq(document_id))
        .filter(extraction_jobs::status.eq("succeeded"))
        .into_boxed::<diesel::pg::Pg>();
    let base_job_query = if let Some(id) = base_job_id {
        base_job_query.filter(extraction_jobs::id.eq(id))
    } else {
        base_job_query
    };
    let base_job = base_job_query
        .order_by(extraction_jobs::version.desc())
        .select((
            extraction_jobs::id,
            extraction_jobs::model,
            extraction_jobs::request,
            extraction_jobs::result,
        ))
        .first::<(uuid::Uuid, String, Value, Option<Value>)>(&mut *pooled)
        .await;
    let (base_job_id, stored_model, base_request, extraction) = match base_job {
        Ok((id, model, request, Some(extraction))) => (id, model, request, extraction),
        Ok(_) | Err(diesel::result::Error::NotFound) => {
            return (
                StatusCode::CONFLICT,
                "document interaction requires a successful extraction",
            )
                .into_response();
        }
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let Some(receipt_or_statement) = extraction
        .get("receipt")
        .or_else(|| extraction.get("statements"))
    else {
        return (
            StatusCode::CONFLICT,
            "document interaction requires receipt or statement data",
        )
            .into_response();
    };
    if receipt_or_statement.is_null() || receipt_or_statement.as_array().is_some_and(Vec::is_empty)
    {
        return (
            StatusCode::CONFLICT,
            "document interaction requires receipt or statement data",
        )
            .into_response();
    }
    let conversation = extraction
        .get("conversation")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    if conversation.len() > 38 {
        return (
            StatusCode::CONFLICT,
            "document conversation reached its turn limit",
        )
            .into_response();
    }
    let conversation_id = extraction
        .get("conversationId")
        .and_then(Value::as_str)
        .map(str::to_owned)
        .unwrap_or_else(|| uuid::Uuid::now_v7().to_string());
    let model = if requested_model == "pro" {
        "pro"
    } else {
        base_request
            .get("model")
            .and_then(Value::as_str)
            .filter(|value| *value == "pro")
            .unwrap_or(if stored_model == "pro" { "pro" } else { "" })
    };
    drop(pooled);
    let mut current_extraction = serde_json::Map::new();
    for key in ["receipt", "statements", "issues"] {
        if let Some(value) = extraction.get(key) {
            current_extraction.insert(key.to_owned(), value.clone());
        }
    }
    let mut request = serde_json::Map::new();
    request.insert(
        "purpose".to_owned(),
        Value::String("interactive".to_owned()),
    );
    request.insert(
        "clarification".to_owned(),
        Value::String(message.to_owned()),
    );
    request.insert(
        "baseJobId".to_owned(),
        Value::String(base_job_id.to_string()),
    );
    request.insert("conversationId".to_owned(), Value::String(conversation_id));
    request.insert("conversation".to_owned(), Value::Array(conversation));
    request.insert(
        "currentExtraction".to_owned(),
        Value::Object(current_extraction),
    );
    if !model.is_empty() {
        request.insert("model".to_owned(), Value::String(model.to_owned()));
    }
    let job = evidence_postgres::document_registration::enqueue_extraction_job(
        &state.pool,
        household_id,
        document_id,
        principal.member_id,
        "gemini_developer_api",
        model,
        &format!("interaction:{}", uuid::Uuid::now_v7()),
        Value::Object(request),
    )
    .await;
    match job {
        Ok(job_id) => (
            StatusCode::ACCEPTED,
            Json(json!({"jobId": job_id, "status": "queued"})),
        )
            .into_response(),
        Err(error) => {
            tracing::error!(error = ?error, "Document interaction registration failed");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

async fn document_interaction_events(
    State(state): State<ApiState>,
    Path((raw_document_id, raw_job_id)): Path<(String, String)>,
    headers: HeaderMap,
) -> Response {
    let (Ok(document_id), Ok(job_id)) = (
        uuid::Uuid::parse_str(&raw_document_id),
        uuid::Uuid::parse_str(&raw_job_id),
    ) else {
        return StatusCode::NOT_FOUND.into_response();
    };
    let cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    let principal = match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => principal,
        Ok(None) => return StatusCode::UNAUTHORIZED.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    use horse_postgres_common::schema::public::{
        extraction_jobs, household_members, source_documents,
    };
    let mut connection = match state.pool.get().await {
        Ok(connection) => connection,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let household_id = source_documents::table
        .inner_join(
            household_members::table
                .on(household_members::household_id.eq(source_documents::household_id)),
        )
        .filter(source_documents::id.eq(document_id))
        .filter(household_members::member_id.eq(principal.member_id))
        .select(source_documents::household_id)
        .first::<uuid::Uuid>(&mut *connection)
        .await;
    let household_id = match household_id {
        Ok(id) => id,
        Err(diesel::result::Error::NotFound) => return StatusCode::NOT_FOUND.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let job = extraction_jobs::table
        .filter(extraction_jobs::id.eq(job_id))
        .filter(extraction_jobs::household_id.eq(household_id))
        .filter(extraction_jobs::source_document_id.eq(document_id))
        .select((
            extraction_jobs::status,
            extraction_jobs::interaction_reply_preview,
            extraction_jobs::request,
        ))
        .first::<(String, String, Value)>(&mut *connection)
        .await;
    let (status, _, request) = match job {
        Ok(job) => job,
        Err(diesel::result::Error::NotFound) => return StatusCode::NOT_FOUND.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    if !matches!(
        status.as_str(),
        "queued" | "running" | "succeeded" | "failed"
    ) {
        return StatusCode::NOT_FOUND.into_response();
    }
    if request.get("purpose").and_then(Value::as_str) != Some("interactive")
        || request
            .get("clarification")
            .and_then(Value::as_str)
            .is_none_or(str::is_empty)
    {
        return StatusCode::NOT_FOUND.into_response();
    }
    let stream = stream::unfold(
        (
            state.pool,
            household_id,
            principal.member_id,
            document_id,
            job_id,
            None,
            false,
        ),
        |(pool, household_id, member_id, document_id, job_id, previous, terminal)| async move {
            if terminal {
                return None;
            }
            let mut connection = pool.get().await.ok()?;
            let member_exists = diesel::select(diesel::dsl::exists(
                household_members::table
                    .filter(household_members::household_id.eq(household_id))
                    .filter(household_members::member_id.eq(member_id)),
            ))
            .get_result::<bool>(&mut *connection)
            .await
            .ok()?;
            if !member_exists {
                return None;
            }
            let current = extraction_jobs::table
                .filter(extraction_jobs::id.eq(job_id))
                .filter(extraction_jobs::household_id.eq(household_id))
                .filter(extraction_jobs::source_document_id.eq(document_id))
                .select((
                    extraction_jobs::status,
                    extraction_jobs::interaction_reply_preview,
                ))
                .first::<(String, String)>(&mut *connection)
                .await
                .ok()?;
            drop(connection);
            let is_terminal = current.0 != "queued" && current.0 != "running";
            if previous.as_ref() == Some(&current) {
                tokio::time::sleep(std::time::Duration::from_secs(1)).await;
                return Some((
                    Ok(Event::default().comment("keep-alive")),
                    (
                        pool,
                        household_id,
                        member_id,
                        document_id,
                        job_id,
                        previous,
                        is_terminal,
                    ),
                ));
            }
            let event = Event::default()
                .json_data(json!({"status": current.0, "reply": if is_terminal { "" } else { &current.1 }}))
                .unwrap_or_else(|_| Event::default().comment("event encoding failed"));
            if !is_terminal {
                tokio::time::sleep(std::time::Duration::from_secs(1)).await;
            }
            Some((
                Ok(event),
                (
                    pool,
                    household_id,
                    member_id,
                    document_id,
                    job_id,
                    Some(current),
                    is_terminal,
                ),
            ))
        },
    );
    let stream: std::pin::Pin<Box<dyn Stream<Item = Result<Event, Infallible>> + Send>> =
        Box::pin(stream);
    Sse::new(stream)
        .keep_alive(
            axum::response::sse::KeepAlive::new().interval(std::time::Duration::from_secs(15)),
        )
        .into_response()
}

async fn read_purchase_evidence(
    State(state): State<ApiState>,
    Path(raw_purchase_id): Path<String>,
    headers: HeaderMap,
) -> Response {
    let Some(purchase_id) = decode_relay_uuid(&raw_purchase_id, "Purchase") else {
        return StatusCode::NOT_FOUND.into_response();
    };
    let cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    let principal = match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => principal,
        Ok(None) => return StatusCode::UNAUTHORIZED.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    use horse_postgres_common::schema::public::{
        evidence_links, household_members, purchases, source_documents,
    };
    let mut pooled = match state.pool.get().await {
        Ok(pooled) => pooled,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let household_id = purchases::table
        .inner_join(
            household_members::table
                .on(household_members::household_id.eq(purchases::household_id)),
        )
        .filter(purchases::id.eq(purchase_id))
        .filter(household_members::member_id.eq(principal.member_id))
        .select(purchases::household_id)
        .first::<uuid::Uuid>(&mut *pooled)
        .await;
    let household_id = match household_id {
        Ok(id) => id,
        Err(diesel::result::Error::NotFound) => return StatusCode::NOT_FOUND.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let rows = evidence_links::table
        .inner_join(
            source_documents::table.on(source_documents::id.eq(evidence_links::source_document_id)),
        )
        .filter(evidence_links::household_id.eq(household_id))
        .filter(evidence_links::entity_kind.eq("purchase"))
        .filter(evidence_links::entity_id.eq(purchase_id))
        .order_by(evidence_links::created_at.asc())
        .then_order_by(evidence_links::source_document_id.asc())
        .then_order_by(evidence_links::item_id.asc())
        .select((
            source_documents::id,
            evidence_links::item_id,
            source_documents::source_kind,
            source_documents::mime_type,
            source_documents::sha256,
            evidence_links::evidence_region,
            evidence_links::observation,
            evidence_links::created_at,
        ))
        .load::<(
            uuid::Uuid,
            Option<uuid::Uuid>,
            String,
            String,
            String,
            Option<Value>,
            Value,
            chrono::DateTime<chrono::Utc>,
        )>(&mut *pooled)
        .await;
    match rows {
        Ok(rows) => {
            let evidence = rows
                .into_iter()
                .map(
                    |(
                        document_id,
                        item_id,
                        source_kind,
                        mime_type,
                        sha256,
                        region,
                        observation,
                        created_at,
                    )| {
                        let mut transform = json!([1, 0, 0, 0, 1, 0, 0, 0, 1]);
                        if let Some(stored) = observation.get("transform") {
                            transform = stored.clone();
                        }
                        json!({
                            "documentId": document_id,
                            "itemId": item_id.map(|id| encode_relay_uuid("PurchaseItem", id)),
                            "sourceKind": source_kind,
                            "mimeType": mime_type,
                            "sha256": sha256,
                            "region": region,
                            "transform": transform,
                            "observation": observation,
                            "createdAt": created_at
                        })
                    },
                )
                .collect::<Vec<_>>();
            Json(json!({"evidence": evidence})).into_response()
        }
        Err(error) => {
            tracing::error!(error = ?error, "Purchase evidence lookup failed");
            StatusCode::SERVICE_UNAVAILABLE.into_response()
        }
    }
}

fn decode_relay_uuid(value: &str, expected_type: &str) -> Option<uuid::Uuid> {
    if let Ok(id) = uuid::Uuid::parse_str(value) {
        return Some(id);
    }
    use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
    let bytes = URL_SAFE_NO_PAD.decode(value).ok()?;
    let separator = bytes.iter().position(|byte| *byte == b':')?;
    let (type_name, payload) = bytes.split_at(separator);
    if type_name != expected_type.as_bytes() || payload.len() != 17 || payload[0] != b':' {
        return None;
    }
    uuid::Uuid::from_slice(&payload[1..]).ok()
}

fn encode_relay_uuid(type_name: &str, id: uuid::Uuid) -> String {
    use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
    let mut bytes = type_name.as_bytes().to_vec();
    bytes.push(b':');
    bytes.extend_from_slice(id.as_bytes());
    URL_SAFE_NO_PAD.encode(bytes)
}

async fn confirm_document_evidence(
    State(state): State<ApiState>,
    Path(raw_document_id): Path<String>,
    headers: HeaderMap,
    Json(input): Json<Value>,
) -> Response {
    if !same_origin(&headers, &state.config) {
        return (StatusCode::FORBIDDEN, "request origin rejected").into_response();
    }
    let Some(document_id) = uuid::Uuid::parse_str(&raw_document_id).ok() else {
        return StatusCode::NOT_FOUND.into_response();
    };
    let source = json!({
        "documentId": document_id,
        "sourceReference": input.get("sourceReference").cloned().unwrap_or_else(|| Value::String(String::new())),
        "itemId": input.get("itemId"),
        "observationJobId": input.get("observationJobId"),
        "sourceItemIndex": input.get("sourceItemIndex")
    });
    let purchase_id = input
        .get("purchaseId")
        .and_then(Value::as_str)
        .and_then(|value| decode_relay_uuid(value, "Purchase"));
    let Some(purchase_id) = purchase_id else {
        return (StatusCode::BAD_REQUEST, "invalid evidence confirmation").into_response();
    };
    let cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    let principal = match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => principal,
        Ok(None) => return StatusCode::UNAUTHORIZED.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    confirm_purchase_sources(&state, purchase_id, principal.member_id, vec![source]).await
}

#[derive(Clone)]
struct ReconciliationObservation {
    document_id: uuid::Uuid,
    source_reference: String,
    hash: String,
    source_kind: String,
    transaction_kind: String,
    order_id: String,
    transaction_id: String,
    merchant: String,
    occurred_at: Option<chrono::DateTime<chrono::Utc>>,
    amount: i64,
    card_last4: String,
}

#[derive(serde::Serialize)]
#[serde(rename_all = "camelCase")]
struct ReconciliationMatch {
    document_id: uuid::Uuid,
    source_reference: String,
    state: String,
    reasons: Vec<String>,
}

async fn document_matches(
    State(state): State<ApiState>,
    Path(raw_id): Path<String>,
    headers: HeaderMap,
    axum::extract::Query(query): axum::extract::Query<std::collections::HashMap<String, String>>,
) -> Response {
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
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    use horse_postgres_common::schema::public::{
        household_members, source_documents, source_observations,
    };
    let mut pooled = match state.pool.get().await {
        Ok(pooled) => pooled,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let household_id = source_documents::table
        .inner_join(
            household_members::table
                .on(household_members::household_id.eq(source_documents::household_id)),
        )
        .filter(source_documents::id.eq(document_id))
        .filter(household_members::member_id.eq(principal.member_id))
        .select(source_documents::household_id)
        .first::<uuid::Uuid>(&mut *pooled)
        .await;
    let household_id = match household_id {
        Ok(id) => id,
        Err(diesel::result::Error::NotFound) => return StatusCode::NOT_FOUND.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let mut incoming_query = source_observations::table
        .filter(source_observations::household_id.eq(household_id))
        .filter(source_observations::source_document_id.eq(document_id))
        .into_boxed();
    if let Some(source_reference) = query.get("sourceReference") {
        incoming_query =
            incoming_query.filter(source_observations::source_reference.eq(source_reference));
    }
    let incoming_rows = incoming_query
        .order_by(source_observations::source_reference.asc())
        .limit(101)
        .select((
            source_observations::source_document_id,
            source_observations::source_reference,
            source_observations::source_hash,
            source_observations::source_kind,
            source_observations::transaction_kind,
            source_observations::external_order_id,
            source_observations::external_transaction_id,
            source_observations::merchant,
            source_observations::occurred_at,
            source_observations::amount_yen,
            source_observations::card_last4,
        ))
        .load::<(
            uuid::Uuid,
            String,
            String,
            String,
            String,
            String,
            String,
            String,
            Option<chrono::DateTime<chrono::Utc>>,
            Option<i64>,
            String,
        )>(&mut *pooled)
        .await;
    let incoming_rows = match incoming_rows {
        Ok(rows) => rows,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let mut truncated = incoming_rows.len() > 100;
    let incoming = incoming_rows
        .into_iter()
        .take(100)
        .map(|row| reconciliation_observation(row))
        .collect::<Vec<_>>();
    if incoming.is_empty() {
        return Json(json!({"observations": [], "truncated": truncated})).into_response();
    }
    let existing_rows = source_observations::table
        .filter(source_observations::household_id.eq(household_id))
        .order_by(source_observations::source_document_id.asc())
        .then_order_by(source_observations::source_reference.asc())
        .limit(5001)
        .select((
            source_observations::source_document_id,
            source_observations::source_reference,
            source_observations::source_hash,
            source_observations::source_kind,
            source_observations::transaction_kind,
            source_observations::external_order_id,
            source_observations::external_transaction_id,
            source_observations::merchant,
            source_observations::occurred_at,
            source_observations::amount_yen,
            source_observations::card_last4,
        ))
        .load::<(
            uuid::Uuid,
            String,
            String,
            String,
            String,
            String,
            String,
            String,
            Option<chrono::DateTime<chrono::Utc>>,
            Option<i64>,
            String,
        )>(&mut *pooled)
        .await;
    let existing_rows = match existing_rows {
        Ok(rows) => rows,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    truncated |= existing_rows.len() > 5000;
    let existing = existing_rows
        .into_iter()
        .take(5000)
        .map(|row| reconciliation_observation(row))
        .collect::<Vec<_>>();
    let observations = incoming
        .iter()
        .map(|candidate| {
            let mut matches = reconcile_observation(candidate, &existing);
            if matches.len() > 20 {
                matches.truncate(20);
                truncated = true;
            }
            json!({"sourceReference": candidate.source_reference, "matches": matches})
        })
        .collect::<Vec<_>>();
    Json(json!({"observations": observations, "truncated": truncated})).into_response()
}

fn reconciliation_observation(
    row: (
        uuid::Uuid,
        String,
        String,
        String,
        String,
        String,
        String,
        String,
        Option<chrono::DateTime<chrono::Utc>>,
        Option<i64>,
        String,
    ),
) -> ReconciliationObservation {
    let (
        document_id,
        source_reference,
        hash,
        source_kind,
        transaction_kind,
        order_id,
        transaction_id,
        merchant,
        occurred_at,
        amount,
        card_last4,
    ) = row;
    ReconciliationObservation {
        document_id,
        source_reference,
        hash,
        source_kind,
        transaction_kind,
        order_id,
        transaction_id,
        merchant,
        occurred_at,
        amount: amount.unwrap_or_default(),
        card_last4,
    }
}

fn reconcile_observation(
    incoming: &ReconciliationObservation,
    existing: &[ReconciliationObservation],
) -> Vec<ReconciliationMatch> {
    let mut matches = Vec::new();
    for old in existing {
        if incoming.document_id == old.document_id {
            continue;
        }
        let mut candidate = ReconciliationMatch {
            document_id: old.document_id,
            source_reference: old.source_reference.clone(),
            state: "POSSIBLE".to_owned(),
            reasons: Vec::new(),
        };
        if !incoming.hash.is_empty() && incoming.hash == old.hash {
            candidate.state = "DUPLICATE".to_owned();
            candidate.reasons.push("同じ原本".to_owned());
            matches.push(candidate);
            continue;
        }
        if !reconcilable_pair(incoming, old) {
            continue;
        }
        let order_match = !incoming.order_id.is_empty() && incoming.order_id == old.order_id;
        let amount_match = reconciliation_amounts_equal(incoming, old);
        let incoming_merchant = normalize_reconciliation_merchant(&incoming.merchant);
        let old_merchant = normalize_reconciliation_merchant(&old.merchant);
        let merchant_match = !incoming_merchant.is_empty() && incoming_merchant == old_merchant;
        let merchant_conflict =
            !incoming_merchant.is_empty() && !old_merchant.is_empty() && !merchant_match;
        let near = incoming
            .occurred_at
            .zip(old.occurred_at)
            .is_some_and(|(left, right)| left.signed_duration_since(right).num_hours().abs() <= 72);
        let card_match = !incoming.card_last4.is_empty() && incoming.card_last4 == old.card_last4;
        if order_match {
            candidate.reasons.push("同じ注文番号".to_owned());
            if !amount_match {
                candidate
                    .reasons
                    .push("分割発送・分割決済または金額相違".to_owned());
            }
        } else if !amount_match || !merchant_match || !near {
            continue;
        }
        if amount_match {
            candidate.reasons.push("金額一致".to_owned());
        }
        if merchant_match {
            candidate.reasons.push("店舗一致".to_owned());
        }
        if merchant_conflict {
            candidate.reasons.push("店舗名相違".to_owned());
        }
        if near {
            candidate.reasons.push("購入日時が近い".to_owned());
        }
        if card_match {
            candidate.reasons.push("カード末尾一致".to_owned());
        }
        if !incoming.card_last4.is_empty() && !old.card_last4.is_empty() && !card_match {
            candidate.state = "CONFLICT".to_owned();
            candidate.reasons.push("カード末尾相違".to_owned());
        }
        matches.push(candidate);
    }
    matches
}

fn reconcilable_pair(left: &ReconciliationObservation, right: &ReconciliationObservation) -> bool {
    if left.source_kind != "bank" && right.source_kind != "bank" {
        return true;
    }
    (left.source_kind == "bank"
        && left.transaction_kind == "PURCHASE_PAYMENT"
        && right.transaction_kind == "RECEIPT")
        || (right.source_kind == "bank"
            && right.transaction_kind == "PURCHASE_PAYMENT"
            && left.transaction_kind == "RECEIPT")
}

fn reconciliation_amounts_equal(
    left: &ReconciliationObservation,
    right: &ReconciliationObservation,
) -> bool {
    if left.source_kind == "bank"
        && left.transaction_kind == "PURCHASE_PAYMENT"
        && right.transaction_kind == "RECEIPT"
    {
        return left.amount < 0 && right.amount > 0 && left.amount == -right.amount;
    }
    if right.source_kind == "bank"
        && right.transaction_kind == "PURCHASE_PAYMENT"
        && left.transaction_kind == "RECEIPT"
    {
        return right.amount < 0 && left.amount > 0 && right.amount == -left.amount;
    }
    left.amount == right.amount
}

fn normalize_reconciliation_merchant(value: &str) -> String {
    value
        .chars()
        .filter(|character| !character.is_whitespace())
        .flat_map(char::to_lowercase)
        .collect()
}

async fn document_preview(
    State(state): State<ApiState>,
    Path(raw_id): Path<String>,
    headers: HeaderMap,
    RawQuery(raw_query): RawQuery,
) -> Response {
    let pages = raw_query
        .as_deref()
        .map(|query| {
            url::form_urlencoded::parse(query.as_bytes())
                .filter(|(key, _)| key == "page")
                .map(|(_, value)| value.into_owned())
                .collect::<Vec<_>>()
        })
        .unwrap_or_default();
    if pages.len() > 1 {
        return (StatusCode::BAD_REQUEST, "invalid page").into_response();
    }
    let page = match pages.first() {
        Some(value) => match value.parse::<u32>() {
            Ok(page) => page,
            Err(_) => return (StatusCode::BAD_REQUEST, "invalid page").into_response(),
        },
        None => 0,
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
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    if state.config.ocr_endpoint.is_empty() || state.config.object_store.is_none() {
        return StatusCode::SERVICE_UNAVAILABLE.into_response();
    }
    use horse_postgres_common::schema::public::{household_members, source_documents};
    let mut pooled = match state.pool.get().await {
        Ok(pooled) => pooled,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let document = source_documents::table
        .inner_join(
            household_members::table
                .on(household_members::household_id.eq(source_documents::household_id)),
        )
        .filter(source_documents::id.eq(document_id))
        .filter(household_members::member_id.eq(principal.member_id))
        .select((source_documents::mime_type, source_documents::object_key))
        .first::<(String, String)>(&mut *pooled)
        .await;
    let (mime_type, object_key) = match document {
        Ok(document) => document,
        Err(diesel::result::Error::NotFound) => return StatusCode::NOT_FOUND.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let is_pdf = mime_type == "application/pdf";
    if !matches!(
        mime_type.as_str(),
        "image/jpeg" | "image/png" | "image/webp" | "application/pdf"
    ) || (!is_pdf && page != 0)
    {
        return (StatusCode::BAD_REQUEST, "invalid page or document type").into_response();
    }
    drop(pooled);
    let store = state
        .config
        .object_store
        .as_ref()
        .expect("object store checked above");
    let original = match store.get(&object_key, &mime_type).await {
        Ok(document) if !document.data.is_empty() && document.data.len() <= (20 << 20) => document,
        Ok(_) => {
            return (StatusCode::BAD_GATEWAY, "document preview source invalid").into_response();
        }
        Err(error) => {
            tracing::error!(error = ?error, "Document preview source lookup failed");
            return (
                StatusCode::BAD_GATEWAY,
                "document preview source unavailable",
            )
                .into_response();
        }
    };
    let mut endpoint = match Url::parse(&state.config.ocr_endpoint) {
        Ok(endpoint) => endpoint,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    if let Ok(mut segments) = endpoint.path_segments_mut() {
        segments.pop_if_empty().push("crop");
    } else {
        return StatusCode::SERVICE_UNAVAILABLE.into_response();
    }
    let part = match reqwest::multipart::Part::bytes(original.data)
        .file_name("original")
        .mime_str(&mime_type)
    {
        Ok(part) => part,
        Err(_) => return StatusCode::INTERNAL_SERVER_ERROR.into_response(),
    };
    let form = reqwest::multipart::Form::new()
        .part("image", part)
        .text("page", page.to_string())
        .text("top", "0")
        .text("left", "0")
        .text("bottom", "1")
        .text("right", "1");
    let client = match reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(45))
        .build()
    {
        Ok(client) => client,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let response = match client.post(endpoint).multipart(form).send().await {
        Ok(response) => response,
        Err(error) => {
            tracing::error!(error = ?error, "Document preview crop request failed");
            return (
                StatusCode::BAD_GATEWAY,
                "document preview generation failed",
            )
                .into_response();
        }
    };
    if response.status() == StatusCode::BAD_REQUEST {
        let body = response.bytes().await.unwrap_or_default();
        if serde_json::from_slice::<Value>(&body)
            .ok()
            .and_then(|value| {
                value
                    .get("detail")
                    .and_then(Value::as_str)
                    .map(str::to_owned)
            })
            .is_some_and(|detail| detail.contains("page is out of range"))
        {
            return (StatusCode::BAD_REQUEST, "invalid page").into_response();
        }
        return (
            StatusCode::BAD_GATEWAY,
            "document preview generation failed",
        )
            .into_response();
    }
    if response.status() != StatusCode::OK
        || response
            .headers()
            .get(header::CONTENT_TYPE)
            .and_then(|value| value.to_str().ok())
            != Some("image/png")
    {
        return (
            StatusCode::BAD_GATEWAY,
            "document preview generation failed",
        )
            .into_response();
    }
    let transform = match response
        .headers()
        .get("x-coordinate-transform")
        .and_then(|value| value.to_str().ok())
        .and_then(|value| serde_json::from_str::<Value>(value).ok())
    {
        Some(Value::Array(values))
            if values.len() == 9
                && values
                    .iter()
                    .all(|value| value.as_f64().is_some_and(f64::is_finite))
                && values[8].as_f64().is_some_and(|value| value != 0.0) =>
        {
            Value::Array(values)
        }
        _ => {
            return (
                StatusCode::BAD_GATEWAY,
                "document preview transform invalid",
            )
                .into_response();
        }
    };
    let mut data = Vec::new();
    let mut response = response;
    while let Some(chunk) = match response.chunk().await {
        Ok(chunk) => chunk,
        Err(_) => {
            return (StatusCode::BAD_GATEWAY, "document preview response invalid").into_response();
        }
    } {
        if data.len() + chunk.len() > (20 << 20) {
            return (
                StatusCode::BAD_GATEWAY,
                "document preview response too large",
            )
                .into_response();
        }
        data.extend_from_slice(&chunk);
    }
    if data.len() < 24 || &data[..8] != b"\x89PNG\r\n\x1a\n" || &data[12..16] != b"IHDR" {
        return (StatusCode::BAD_GATEWAY, "document preview image invalid").into_response();
    }
    let width = u32::from_be_bytes(data[16..20].try_into().unwrap_or_default());
    let height = u32::from_be_bytes(data[20..24].try_into().unwrap_or_default());
    if width == 0 || height == 0 || u64::from(width) * u64::from(height) > 40_000_000 {
        return (StatusCode::BAD_GATEWAY, "document preview image invalid").into_response();
    }
    let mut response = Response::new(Body::from(data));
    *response.status_mut() = StatusCode::OK;
    response
        .headers_mut()
        .insert(header::CONTENT_TYPE, HeaderValue::from_static("image/png"));
    response.headers_mut().insert(
        header::CONTENT_DISPOSITION,
        HeaderValue::from_static("inline"),
    );
    response.headers_mut().insert(
        header::CACHE_CONTROL,
        HeaderValue::from_static("private, no-store"),
    );
    response.headers_mut().insert(
        "x-content-type-options",
        HeaderValue::from_static("nosniff"),
    );
    response.headers_mut().insert(
        "x-original-content-type",
        HeaderValue::from_str(&mime_type)
            .unwrap_or_else(|_| HeaderValue::from_static("application/octet-stream")),
    );
    response.headers_mut().insert(
        "x-source-page",
        HeaderValue::from_str(&page.to_string()).unwrap_or_else(|_| HeaderValue::from_static("0")),
    );
    response.headers_mut().insert(
        "x-coordinate-transform",
        HeaderValue::from_str(&transform.to_string())
            .unwrap_or_else(|_| HeaderValue::from_static("[1,0,0,0,1,0,0,0,1]")),
    );
    response
}

async fn link_purchase_evidence(
    State(state): State<ApiState>,
    Path(raw_purchase_id): Path<String>,
    headers: HeaderMap,
    Json(input): Json<Value>,
) -> Response {
    if !same_origin(&headers, &state.config) {
        return (StatusCode::FORBIDDEN, "request origin rejected").into_response();
    }
    let Some(purchase_id) = decode_relay_uuid(&raw_purchase_id, "Purchase") else {
        return StatusCode::NOT_FOUND.into_response();
    };
    let Some(sources) = input.get("sources").and_then(Value::as_array) else {
        return (StatusCode::BAD_REQUEST, "invalid evidence sources").into_response();
    };
    if sources.is_empty() || sources.len() > 100 {
        return (StatusCode::BAD_REQUEST, "invalid evidence sources").into_response();
    }
    let cookie = headers
        .get(header::COOKIE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    let principal = match resolve_cookie(&state.pool, cookie).await {
        Ok(Some(principal)) => principal,
        Ok(None) => return StatusCode::UNAUTHORIZED.into_response(),
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    confirm_purchase_sources(&state, purchase_id, principal.member_id, sources.clone()).await
}

async fn confirm_purchase_sources(
    state: &ApiState,
    purchase_id: uuid::Uuid,
    member_id: uuid::Uuid,
    sources: Vec<Value>,
) -> Response {
    use horse_postgres_common::schema::public::{
        evidence_links, extraction_jobs, household_events, household_members, purchase_items,
        purchases, source_confirmations, source_documents, source_observations,
    };
    let mut pooled = match state.pool.get().await {
        Ok(pooled) => pooled,
        Err(_) => return StatusCode::SERVICE_UNAVAILABLE.into_response(),
    };
    let result = (&mut *pooled)
        .transaction::<i64, diesel::result::Error, _>(async |conn| {
            let (household_id, revision) = purchases::table
                .filter(purchases::id.eq(purchase_id))
                .for_update()
                .select((purchases::household_id, purchases::revision))
                .first::<(uuid::Uuid, i64)>(conn)
                .await?;
            let member_exists = diesel::select(diesel::dsl::exists(
                household_members::table
                    .filter(household_members::household_id.eq(household_id))
                    .filter(household_members::member_id.eq(member_id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            if !member_exists {
                return Err(diesel::result::Error::NotFound);
            }
            for source in &sources {
                let document_id = source
                    .get("documentId")
                    .and_then(Value::as_str)
                    .and_then(|value| uuid::Uuid::parse_str(value).ok())
                    .ok_or(diesel::result::Error::NotFound)?;
                let source_reference = source
                    .get("sourceReference")
                    .and_then(Value::as_str)
                    .unwrap_or_default();
                if source_reference.len() > 256 {
                    return Err(diesel::result::Error::RollbackTransaction);
                }
                let source_household = source_documents::table
                    .filter(source_documents::id.eq(document_id))
                    .select(source_documents::household_id)
                    .first::<uuid::Uuid>(conn)
                    .await?;
                if source_household != household_id {
                    return Err(diesel::result::Error::NotFound);
                }
                let item_id = match source.get("itemId").and_then(Value::as_str) {
                    Some(value) => Some(
                        decode_relay_uuid(value, "PurchaseItem")
                            .ok_or(diesel::result::Error::RollbackTransaction)?,
                    ),
                    None => None,
                };
                if let Some(item_id) = item_id {
                    let exists = diesel::select(diesel::dsl::exists(
                        purchase_items::table
                            .filter(purchase_items::id.eq(item_id))
                            .filter(purchase_items::purchase_id.eq(purchase_id))
                            .filter(purchase_items::household_id.eq(household_id)),
                    ))
                    .get_result::<bool>(conn)
                    .await?;
                    if !exists {
                        return Err(diesel::result::Error::NotFound);
                    }
                }
                let observation_id = source_observations::table
                    .filter(source_observations::household_id.eq(household_id))
                    .filter(source_observations::source_document_id.eq(document_id))
                    .filter(source_observations::source_reference.eq(source_reference))
                    .order_by(source_observations::created_at.desc())
                    .select(source_observations::id)
                    .first::<uuid::Uuid>(conn)
                    .await
                    .optional()?;
                let observation_job_id = source
                    .get("observationJobId")
                    .and_then(Value::as_str)
                    .and_then(|value| uuid::Uuid::parse_str(value).ok());
                let item_index = source
                    .get("sourceItemIndex")
                    .and_then(Value::as_u64)
                    .and_then(|value| usize::try_from(value).ok());
                if observation_job_id.is_some() != item_index.is_some() {
                    return Err(diesel::result::Error::RollbackTransaction);
                }
                let mut observation = json!({});
                let mut region = None;
                if let (Some(job_id), Some(index)) = (observation_job_id, item_index) {
                    let result = extraction_jobs::table
                        .filter(extraction_jobs::id.eq(job_id))
                        .filter(extraction_jobs::household_id.eq(household_id))
                        .filter(extraction_jobs::source_document_id.eq(document_id))
                        .filter(extraction_jobs::status.eq("succeeded"))
                        .select(extraction_jobs::result)
                        .first::<Option<Value>>(conn)
                        .await?
                        .ok_or(diesel::result::Error::NotFound)?;
                    let item = result
                        .pointer(&format!("/receipt/items/{index}"))
                        .cloned()
                        .ok_or(diesel::result::Error::RollbackTransaction)?;
                    region = item.get("region").cloned();
                    observation = json!({
                        "jobId": job_id,
                        "itemIndex": index,
                        "item": item,
                        "transform": result.get("transform"),
                        "transformPage": result.get("transformPage")
                    });
                }
                let active_confirmation = source_confirmations::table
                    .filter(source_confirmations::household_id.eq(household_id))
                    .filter(source_confirmations::source_document_id.eq(document_id))
                    .filter(source_confirmations::source_reference.eq(source_reference))
                    .filter(source_confirmations::status.eq("confirmed"))
                    .select(source_confirmations::entity_id)
                    .first::<uuid::Uuid>(conn)
                    .await
                    .optional()?;
                if active_confirmation
                    .is_some_and(|confirmed_purchase| confirmed_purchase != purchase_id)
                {
                    return Err(diesel::result::Error::RollbackTransaction);
                }
                let confirmation_id = uuid::Uuid::now_v7();
                diesel::insert_into(source_confirmations::table)
                    .values((
                        source_confirmations::id.eq(confirmation_id),
                        source_confirmations::household_id.eq(household_id),
                        source_confirmations::source_document_id.eq(document_id),
                        source_confirmations::observation_id.eq(observation_id),
                        source_confirmations::source_reference.eq(source_reference),
                        source_confirmations::entity_kind.eq("purchase"),
                        source_confirmations::entity_id.eq(purchase_id),
                        source_confirmations::status.eq("confirmed"),
                        source_confirmations::idempotency_key
                            .eq(format!("http:{}", uuid::Uuid::now_v7())),
                        source_confirmations::changed_by.eq(member_id),
                        source_confirmations::changed_at.eq(chrono::Utc::now()),
                    ))
                    .on_conflict_do_nothing()
                    .execute(conn)
                    .await?;
                diesel::insert_into(evidence_links::table)
                    .values((
                        evidence_links::id.eq(uuid::Uuid::now_v7()),
                        evidence_links::household_id.eq(household_id),
                        evidence_links::source_document_id.eq(document_id),
                        evidence_links::observation_id.eq(observation_id),
                        evidence_links::source_reference.eq(source_reference),
                        evidence_links::entity_kind.eq("purchase"),
                        evidence_links::entity_id.eq(purchase_id),
                        evidence_links::item_id.eq(item_id),
                        evidence_links::evidence_region.eq(region),
                        evidence_links::observation.eq(observation),
                        evidence_links::created_by.eq(member_id),
                        evidence_links::created_at.eq(chrono::Utc::now()),
                    ))
                    .on_conflict_do_nothing()
                    .execute(conn)
                    .await?;
            }
            diesel::insert_into(household_events::table)
                .values((
                    household_events::household_id.eq(household_id),
                    household_events::event_type.eq("evidence.group_linked"),
                    household_events::entity_kind.eq("purchase"),
                    household_events::entity_id.eq(purchase_id),
                    household_events::revision.eq(revision),
                    household_events::changed_fields.eq(vec![Some("evidence".to_owned())]),
                    household_events::payload.eq(json!({"sourceCount": sources.len()})),
                    household_events::actor_id.eq(member_id),
                    household_events::created_at.eq(chrono::Utc::now()),
                ))
                .execute(conn)
                .await?;
            Ok(revision)
        })
        .await;
    match result {
        Ok(_) => Json(json!({"linked": true, "sourceCount": sources.len()})).into_response(),
        Err(diesel::result::Error::NotFound) => StatusCode::NOT_FOUND.into_response(),
        Err(diesel::result::Error::RollbackTransaction) => (
            StatusCode::CONFLICT,
            "evidence source conflicts with an existing link",
        )
            .into_response(),
        Err(error) => {
            tracing::error!(error = ?error, "Purchase evidence confirmation failed");
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
    let mut cursor = match query.get("after") {
        Some(value) if !value.is_empty() => match value.parse::<i64>() {
            Ok(cursor) => cursor,
            Err(_) => return Err((StatusCode::BAD_REQUEST, "invalid event cursor").into_response()),
        },
        _ => 0,
    };
    if let Some(last_event_id) = headers
        .get("last-event-id")
        .and_then(|value| value.to_str().ok())
    {
        cursor = match last_event_id.parse::<i64>() {
            Ok(cursor) => cursor,
            Err(_) => return Err((StatusCode::BAD_REQUEST, "invalid event cursor").into_response()),
        };
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
                    return Some((
                        Ok(Event::default().comment("keepalive")),
                        (pool, household_id, cursor),
                    ));
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
                .load::<(
                    i64,
                    String,
                    uuid::Uuid,
                    i64,
                    Vec<Option<String>>,
                    chrono::DateTime<chrono::Utc>,
                )>(&mut *connection)
                .await;
            let events = match events {
                Ok(events) => events,
                Err(error) => {
                    tracing::error!(error = ?error, "Event stream polling failed");
                    return None;
                }
            };
            if let Some((
                event_cursor,
                event_type,
                entity_id,
                revision,
                changed_fields,
                created_at,
            )) = events.first()
            {
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
                    .json_data(event)
                    .unwrap_or_else(|_| Event::default().comment("event encoding failed"));
                return Some((Ok(stream_event), (pool, household_id, *event_cursor)));
            }
            drop(connection);
            tokio::time::sleep(std::time::Duration::from_secs(1)).await;
            Some((
                Ok(Event::default().comment("keepalive")),
                (pool, household_id, cursor),
            ))
        },
    );
    let stream: std::pin::Pin<Box<dyn Stream<Item = Result<Event, Infallible>> + Send>> =
        Box::pin(stream);
    Ok(Sse::new(stream)
        .keep_alive(
            axum::response::sse::KeepAlive::new().interval(std::time::Duration::from_secs(15)),
        )
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
