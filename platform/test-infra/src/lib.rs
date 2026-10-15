use std::{error::Error, path::PathBuf, process::Command};

use testcontainers_modules::{
    postgres::Postgres,
    testcontainers::{ContainerAsync, ImageExt, runners::AsyncRunner},
};

const POSTGRES_DATABASE: &str = "horse_test";
const POSTGRES_USER: &str = "horse_test";
const POSTGRES_PASSWORD: &str = "horse_test_local_only";

pub struct TestPostgres {
    container: ContainerAsync<Postgres>,
    connection_url: String,
}

impl TestPostgres {
    pub async fn start() -> Result<Self, Box<dyn Error + Send + Sync>> {
        let container = Postgres::default()
            .with_tag("18")
            .with_db_name(POSTGRES_DATABASE)
            .with_user(POSTGRES_USER)
            .with_password(POSTGRES_PASSWORD)
            .start()
            .await?;
        let host = container.get_host().await?;
        let port = container.get_host_port_ipv4(5432).await?;
        let connection_url = format!(
            "postgres://{POSTGRES_USER}:{POSTGRES_PASSWORD}@{host}:{port}/{POSTGRES_DATABASE}?sslmode=disable"
        );

        Ok(Self {
            container,
            connection_url,
        })
    }

    pub fn connection_url(&self) -> &str {
        &self.connection_url
    }

    pub async fn terminate(self) -> Result<(), Box<dyn Error + Send + Sync>> {
        self.container.rm().await?;
        Ok(())
    }
}

pub fn apply_migrations(database_url: &str) -> Result<(), Box<dyn Error + Send + Sync>> {
    let workspace_root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../..");
    let output = Command::new("mise")
        .args(["run", "rust:db:migrate"])
        .current_dir(workspace_root)
        .env("RUST_DATABASE_URL", database_url)
        .output()?;
    if !output.status.success() {
        return Err(format!(
            "apply Rust Atlas migrations failed: {}{}",
            String::from_utf8_lossy(&output.stdout),
            String::from_utf8_lossy(&output.stderr)
        )
        .into());
    }
    Ok(())
}
