use chrono::Utc;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncPgConnection, RunQueryDsl, pooled_connection::bb8::Pool};
use horse_postgres::schema::public::{members, oidc_sessions};
use sha2::{Digest, Sha256};
use uuid::Uuid;

mod oidc;

pub use oidc::{AuthConfig, AuthService, Cookie, LoginStart, OidcError, OidcResult};

pub const SESSION_COOKIE_NAME: &str = "horse_session";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Principal {
    pub member_id: Uuid,
    pub subject: String,
    pub display_name: String,
}

#[derive(Debug)]
pub enum ResolveError {
    Pool(String),
    Database(DieselError),
}

pub async fn resolve_cookie(
    pool: &Pool<AsyncPgConnection>,
    cookie_header: &str,
) -> Result<Option<Principal>, ResolveError> {
    let Some(token) = cookie_value(cookie_header, SESSION_COOKIE_NAME) else {
        return Ok(None);
    };
    resolve_session(pool, token).await
}

pub async fn resolve_session(
    pool: &Pool<AsyncPgConnection>,
    token: &str,
) -> Result<Option<Principal>, ResolveError> {
    if token.is_empty() {
        return Ok(None);
    }
    let token_hash = Sha256::digest(token.as_bytes()).to_vec();
    let mut conn = pool
        .get()
        .await
        .map_err(|error| ResolveError::Pool(error.to_string()))?;
    let result = oidc_sessions::table
        .inner_join(members::table.on(members::id.eq(oidc_sessions::member_id)))
        .filter(oidc_sessions::token_hash.eq(token_hash))
        .filter(oidc_sessions::expiry.gt(Utc::now()))
        .select((members::id, members::oidc_subject, members::display_name))
        .first::<(Uuid, String, String)>(&mut conn)
        .await
        .optional()
        .map_err(ResolveError::Database)?;
    Ok(result.map(|(member_id, subject, display_name)| Principal {
        member_id,
        subject,
        display_name,
    }))
}

pub async fn revoke_cookie(
    pool: &Pool<AsyncPgConnection>,
    cookie_header: &str,
) -> Result<(), ResolveError> {
    let Some(token) = cookie_value(cookie_header, SESSION_COOKIE_NAME) else {
        return Ok(());
    };
    if token.is_empty() {
        return Ok(());
    }

    let token_hash = Sha256::digest(token.as_bytes()).to_vec();
    let mut conn = pool
        .get()
        .await
        .map_err(|error| ResolveError::Pool(error.to_string()))?;
    diesel::delete(oidc_sessions::table.filter(oidc_sessions::token_hash.eq(token_hash)))
        .execute(&mut conn)
        .await
        .map_err(ResolveError::Database)?;
    Ok(())
}

fn cookie_value<'a>(header: &'a str, name: &str) -> Option<&'a str> {
    header.split(';').find_map(|part| {
        let (key, value) = part.trim().split_once('=')?;
        (key.trim() == name).then_some(value.trim())
    })
}
