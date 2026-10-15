use axum::{extract::Request, middleware::Next, response::Response};
use horse_observability::set_parent_from_headers;
use tracing::{Instrument, field::Empty};

pub async fn trace_request(request: Request, next: Next) -> Response {
    if matches!(request.uri().path(), "/healthz" | "/readyz") {
        return next.run(request).await;
    }

    let route = request
        .extensions()
        .get::<axum::extract::MatchedPath>()
        .map(axum::extract::MatchedPath::as_str)
        .unwrap_or_else(|| request.uri().path());
    let span = tracing::info_span!(
        "http.server.request",
        otel.kind = "server",
        http.request.method = %request.method(),
        http.route = %route,
        http.response.status_code = Empty,
        otel.status_code = Empty,
    );
    set_parent_from_headers(&span, request.headers());
    let response = next.run(request).instrument(span.clone()).await;
    span.record("http.response.status_code", response.status().as_u16());
    if response.status().is_server_error() {
        span.record("otel.status_code", "ERROR");
    }
    response
}
