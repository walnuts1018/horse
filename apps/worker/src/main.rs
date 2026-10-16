use std::{net::SocketAddr, sync::Arc};

use axum::{Router, extract::State, http::StatusCode, routing::get};
use horse_extraction_queue::{ExtractionQueue, next_delivery};
use horse_observability::Telemetry;
use horse_worker_runtime::Worker;
use tokio::net::TcpListener;
use tracing_subscriber::EnvFilter;

const QUEUE_RETRY_INITIAL: std::time::Duration = std::time::Duration::from_secs(5);
const QUEUE_RETRY_MAX: std::time::Duration = std::time::Duration::from_secs(60);

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let log_filter = EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| EnvFilter::new("info"))
        .add_directive("async_imap=off".parse()?);
    let telemetry = Telemetry::init("horse-worker", log_filter)?;

    let listener = TcpListener::bind(SocketAddr::from(([0, 0, 0, 0], 8081))).await?;
    let worker = Arc::new(Worker::from_env().await?);
    let (shutdown_tx, shutdown_rx) = tokio::sync::watch::channel(false);
    let worker_task = {
        let worker = Arc::clone(&worker);
        let shutdown = shutdown_rx.clone();
        tokio::spawn(async move { worker.run(shutdown).await })
    };
    let queue_task = tokio::spawn(run_queue(Arc::clone(&worker), shutdown_rx.clone()));

    let app = Router::new()
        .route("/healthz", get(|| async { StatusCode::OK }))
        .route("/readyz", get(readiness))
        .with_state(worker);
    let server_result = axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal(shutdown_tx.clone()))
        .await;
    let _ = shutdown_tx.send(true);
    let worker_result = worker_task.await?;
    let queue_result = queue_task.await;
    let telemetry_result = telemetry.shutdown();
    server_result?;
    worker_result?;
    queue_result?;
    telemetry_result?;
    Ok(())
}

async fn run_queue(worker: Arc<Worker>, mut shutdown: tokio::sync::watch::Receiver<bool>) {
    let mut retry_delay = QUEUE_RETRY_INITIAL;
    loop {
        if *shutdown.borrow() {
            return;
        }
        let queue_result = tokio::select! {
            changed = shutdown.changed() => {
                if changed.is_err() || *shutdown.borrow() {
                    return;
                }
                continue;
            }
            result = ExtractionQueue::from_env() => result,
        };
        match queue_result {
            Ok(None) => {
                if wait_for_shutdown(&mut shutdown, std::time::Duration::from_secs(10)).await {
                    return;
                }
            }
            Err(error) => {
                tracing::warn!(error = %error, "Extraction queue unavailable; database polling remains active");
                if wait_for_shutdown(&mut shutdown, retry_delay).await {
                    return;
                }
                retry_delay = next_retry_delay(retry_delay);
            }
            Ok(Some(queue)) => {
                let consumer_result = tokio::select! {
                    changed = shutdown.changed() => {
                        if changed.is_err() || *shutdown.borrow() {
                            queue.close().await;
                            return;
                        }
                        continue;
                    }
                    result = queue.consume() => result,
                };
                match consumer_result {
                    Ok(mut consumer) => loop {
                        retry_delay = QUEUE_RETRY_INITIAL;
                        tokio::select! {
                            changed = shutdown.changed() => {
                                if changed.is_err() || *shutdown.borrow() {
                                    break;
                                }
                            }
                            delivery = next_delivery(&mut consumer) => {
                                match delivery {
                                    Some(Ok(delivery)) => {
                                        let job_id = std::str::from_utf8(&delivery.data)
                                            .ok()
                                            .and_then(|value| uuid::Uuid::parse_str(value).ok());
                                        let Some(job_id) = job_id else {
                                            tracing::warn!("Discard extraction queue delivery with invalid job ID");
                                            if let Err(error) = queue.ack(&delivery).await {
                                                tracing::warn!(error = %error, "Acknowledge invalid extraction delivery failed");
                                                break;
                                            }
                                            continue;
                                        };
                                        match worker.run_notified_job(&job_id.to_string()).await {
                                            Ok(_) => {
                                                if let Err(error) = queue.ack(&delivery).await {
                                                    tracing::warn!(error = %error, "Acknowledge extraction delivery failed");
                                                    break;
                                                }
                                            }
                                            Err(_error) => {
                                                let failure_class = "worker_processing_error";
                                                if let Err(error) = queue.ack(&delivery).await {
                                                    tracing::warn!(job_id = %job_id, failure_class, error = %error, "Acknowledge failed extraction delivery failed");
                                                    break;
                                                }
                                                tracing::warn!(job_id = %job_id, failure_class, "Extraction notification failed; database polling will retry");
                                            }
                                        }
                                    }
                                    Some(Err(error)) => {
                                        tracing::warn!(error = %error, "Extraction queue delivery failed");
                                        break;
                                    }
                                    None => {
                                        tracing::warn!("Extraction queue consumer disconnected");
                                        break;
                                    }
                                }
                            }
                        }
                    },
                    Err(error) => {
                        tracing::warn!(error = %error, "Start extraction queue consumer failed")
                    }
                }
                queue.close().await;
                if wait_for_shutdown(&mut shutdown, retry_delay).await {
                    return;
                }
                retry_delay = next_retry_delay(retry_delay);
            }
        }
    }
}

fn next_retry_delay(current: std::time::Duration) -> std::time::Duration {
    current.saturating_mul(2).min(QUEUE_RETRY_MAX)
}

async fn wait_for_shutdown(
    shutdown: &mut tokio::sync::watch::Receiver<bool>,
    delay: std::time::Duration,
) -> bool {
    tokio::select! {
        _ = tokio::time::sleep(delay) => *shutdown.borrow(),
        changed = shutdown.changed() => changed.is_err() || *shutdown.borrow(),
    }
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
