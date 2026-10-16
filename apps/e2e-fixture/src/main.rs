use std::{env, error::Error};

use chrono::{Duration, Utc};
use diesel::ExpressionMethods;
use diesel_async::RunQueryDsl;
use horse_postgres_common::{
    connect,
    schema::public::{members, oidc_sessions},
};
use sha2::{Digest, Sha256};
use uuid::Uuid;

const SESSION_TOKEN: &str = "horse-e2e-session-token";

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error + Send + Sync>> {
    let database_url = env::var("DATABASE_URL")?;
    let pool = connect(database_url).await?;
    let member_id = Uuid::from_u128(0xe2e);
    let expiry = Utc::now() + Duration::hours(1);
    let mut connection = pool.get().await?;
    diesel::insert_into(members::table)
        .values((
            members::id.eq(member_id),
            members::oidc_subject.eq("horse-e2e"),
            members::display_name.eq("E2E Tester"),
        ))
        .on_conflict_do_nothing()
        .execute(&mut connection)
        .await?;
    diesel::insert_into(oidc_sessions::table)
        .values((
            oidc_sessions::token_hash.eq(Sha256::digest(SESSION_TOKEN.as_bytes()).to_vec()),
            oidc_sessions::member_id.eq(member_id),
            oidc_sessions::expiry.eq(expiry),
        ))
        .on_conflict(oidc_sessions::token_hash)
        .do_update()
        .set(oidc_sessions::expiry.eq(expiry))
        .execute(&mut connection)
        .await?;
    println!("{SESSION_TOKEN}");
    Ok(())
}
