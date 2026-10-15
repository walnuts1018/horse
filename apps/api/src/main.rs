use std::{env, error::Error, net::SocketAddr, sync::Arc};

use horse_auth::{AuthConfig, AuthService};
use horse_graphql_api::{Config as GraphqlConfig, schema};
use horse_http_api::{Config, router};
use horse_object_storage::ObjectStore;
use horse_postgres_common::connect;
use tokio::net::TcpListener;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    tracing_subscriber::fmt()
        .json()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .init();

    let database_url = env::var("DATABASE_URL")?;
    let port = env::var("PORT")
        .ok()
        .and_then(|value| value.parse::<u16>().ok())
        .unwrap_or(8080);
    let pool = connect(database_url).await?;
    let public_origin = env::var("PUBLIC_ORIGIN").unwrap_or_default();
    let frontend_origin = env::var("FRONTEND_ORIGIN").unwrap_or_default();
    let auth = AuthService::new(
        pool.clone(),
        AuthConfig {
            issuer_url: env::var("OIDC_ISSUER_URL").unwrap_or_default(),
            client_id: env::var("OIDC_CLIENT_ID").unwrap_or_default(),
            client_secret: env::var("OIDC_CLIENT_SECRET").unwrap_or_default(),
            redirect_url: env::var("OIDC_REDIRECT_URL").unwrap_or_default(),
            public_origin: public_origin.clone(),
            frontend_origin: frontend_origin.clone(),
        },
    )
    .await?;
    let mailbox_domain = env::var("MAIL_FORWARDING_MAILBOX_DOMAIN").unwrap_or_default();
    let local_part_prefix =
        env::var("MAIL_FORWARDING_ADDRESS_LOCAL_PART_PREFIX").unwrap_or_default();
    let recipient_base = env::var("MAILBOX_RECIPIENT_BASE").unwrap_or_default();
    let (recipient_local_part, recipient_domain) =
        recipient_base.split_once('@').unwrap_or_default();
    let mailbox_enabled = [
        "MAILBOX_IMAPS_ADDRESS",
        "MAILBOX_IMAPS_SERVER_NAME",
        "MAILBOX_IMAPS_MAILBOX",
        "MAILBOX_IMAPS_USERNAME",
        "MAILBOX_IMAPS_PASSWORD",
    ]
    .iter()
    .all(|key| env::var(key).is_ok_and(|value| !value.is_empty()))
        && !mailbox_domain.is_empty()
        && local_part_prefix.ends_with('+')
        && recipient_local_part == local_part_prefix.trim_end_matches('+')
        && recipient_domain.eq_ignore_ascii_case(&mailbox_domain);
    let graphql_schema = schema(
        pool.clone(),
        GraphqlConfig {
            mail_forwarding_enabled: mailbox_enabled,
            mailbox_domain,
            local_part_prefix,
        },
    );
    let object_store = match ObjectStore::from_env().await {
        Ok(store) => Some(Arc::new(store)),
        Err(error) => {
            tracing::warn!(error = %error, "Object storage is unavailable; document HTTP routes are disabled");
            None
        }
    };
    let listener = TcpListener::bind(SocketAddr::from(([0, 0, 0, 0], port))).await?;
    axum::serve(
        listener,
        router(
            pool,
            Config {
                public_origin,
                frontend_origin,
                ocr_endpoint: std::env::var("OCR_URL").unwrap_or_default(),
                auth,
                object_store,
            },
            graphql_schema,
        ),
    )
    .with_graceful_shutdown(shutdown_signal())
    .await?;
    Ok(())
}

async fn shutdown_signal() {
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
}
