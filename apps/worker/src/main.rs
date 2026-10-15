use std::{net::SocketAddr, sync::Arc};

use axum::{Router, extract::State, http::StatusCode, routing::get};
use horse_worker_runtime::Worker;
use tokio::net::TcpListener;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let log_filter =
        tracing_subscriber::EnvFilter::from_default_env().add_directive("async_imap=off".parse()?);
    tracing_subscriber::fmt()
        .json()
        .with_env_filter(log_filter)
        .init();

    let listener = TcpListener::bind(SocketAddr::from(([0, 0, 0, 0], 8081))).await?;
    let worker = Arc::new(Worker::from_env().await?);
    let (shutdown_tx, shutdown_rx) = tokio::sync::watch::channel(false);
    let worker_task = {
        let worker = Arc::clone(&worker);
        tokio::spawn(async move { worker.run(shutdown_rx).await })
    };

    let app = Router::new()
        .route("/healthz", get(|| async { StatusCode::OK }))
        .route("/readyz", get(readiness))
        .with_state(worker);
    let server_result = axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal(shutdown_tx.clone()))
        .await;
    let _ = shutdown_tx.send(true);
    worker_task.await??;
    server_result?;
    Ok(())
}

async fn readiness(State(worker): State<Arc<Worker>>) -> StatusCode {
    if worker.is_ready().await {
        StatusCode::OK
    } else {
        StatusCode::SERVICE_UNAVAILABLE
    }
}

async fn shutdown_signal(shutdown: tokio::sync::watch::Sender<bool>) {
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
    let _ = shutdown.send(true);
}
