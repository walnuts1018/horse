use std::time::Duration;
use std::{error::Error, fmt};

use chrono::{DateTime, Duration as ChronoDuration, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncPgConnection, RunQueryDsl, pooled_connection::bb8::Pool};
use horse_postgres_common::schema::public::{members, oidc_sessions, oidc_states};
use openidconnect::{
    AuthorizationCode, ClientId, ClientSecret, CsrfToken, EndpointMaybeSet, EndpointNotSet,
    EndpointSet, IssuerUrl, Nonce, OAuth2TokenResponse, PkceCodeChallenge, RedirectUrl, Scope,
    TokenResponse, TokenUrl,
    core::{CoreAuthenticationFlow, CoreClient, CoreProviderMetadata},
    reqwest,
};
use sha2::{Digest, Sha256};
use subtle::ConstantTimeEq;
use url::Url;
use uuid::Uuid;

use crate::SESSION_COOKIE_NAME;

const STATE_COOKIE_NAME: &str = "horse_oidc_state";
const STATE_LIFETIME: ChronoDuration = ChronoDuration::minutes(10);
const SESSION_LIFETIME: ChronoDuration = ChronoDuration::hours(24);

type OidcClient = CoreClient<
    EndpointSet,
    EndpointNotSet,
    EndpointNotSet,
    EndpointNotSet,
    EndpointSet,
    EndpointMaybeSet,
>;

#[derive(Clone)]
pub struct AuthConfig {
    pub issuer_url: String,
    pub client_id: String,
    pub client_secret: String,
    pub redirect_url: String,
    pub public_origin: String,
    pub frontend_origin: String,
}

#[derive(Clone)]
pub struct AuthService {
    pool: Pool<AsyncPgConnection>,
    client: OidcClient,
    http_client: reqwest::Client,
    config: AuthConfig,
    secure_cookie: bool,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Cookie {
    pub name: &'static str,
    pub value: String,
    pub path: &'static str,
    pub expires_at: Option<DateTime<Utc>>,
    pub max_age_seconds: Option<i64>,
    pub http_only: bool,
    pub secure: bool,
    pub same_site_lax: bool,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct LoginStart {
    pub authorization_url: String,
    pub state_cookie: Cookie,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct OidcResult {
    pub redirect_to: String,
    pub session_cookie: Cookie,
}

#[derive(Debug)]
pub enum OidcError {
    IncompleteConfiguration(Vec<&'static str>),
    Provider(String),
    Database(DieselError),
    Pool(String),
    InvalidResponse,
    InvalidState,
    AuthenticationFailed,
    SessionExpired,
}

impl fmt::Display for OidcError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "{self:?}")
    }
}

impl Error for OidcError {}

impl AuthService {
    pub async fn new(
        pool: Pool<AsyncPgConnection>,
        config: AuthConfig,
    ) -> Result<Option<Self>, OidcError> {
        let mut missing = Vec::new();
        if config.issuer_url.is_empty() {
            missing.push("issuer URL");
        }
        if config.client_id.is_empty() {
            missing.push("client ID");
        }
        if config.redirect_url.is_empty() {
            missing.push("redirect URL");
        }
        if missing.len() == 3 {
            return Ok(None);
        }
        if !missing.is_empty() {
            return Err(OidcError::IncompleteConfiguration(missing));
        }

        let http_client = reqwest::ClientBuilder::new()
            .redirect(reqwest::redirect::Policy::none())
            .timeout(Duration::from_secs(10))
            .build()
            .map_err(|error| OidcError::Provider(error.to_string()))?;
        let issuer = IssuerUrl::new(config.issuer_url.clone())
            .map_err(|error| OidcError::Provider(error.to_string()))?;
        let provider = CoreProviderMetadata::discover_async(issuer, &http_client)
            .await
            .map_err(|error| OidcError::Provider(error.to_string()))?;
        let token_url = provider
            .token_endpoint()
            .cloned()
            .ok_or_else(|| OidcError::Provider("provider has no token endpoint".to_owned()))?;
        let client = CoreClient::from_provider_metadata(
            provider,
            ClientId::new(config.client_id.clone()),
            Some(ClientSecret::new(config.client_secret.clone())),
        )
        .set_token_uri(
            TokenUrl::new(token_url.to_string())
                .map_err(|error| OidcError::Provider(error.to_string()))?,
        )
        .set_redirect_uri(
            RedirectUrl::new(config.redirect_url.clone())
                .map_err(|error| OidcError::Provider(error.to_string()))?,
        );
        let secure_cookie =
            Url::parse(&config.public_origin).is_ok_and(|origin| origin.scheme() == "https");

        Ok(Some(Self {
            pool,
            client,
            http_client,
            config,
            secure_cookie,
        }))
    }

    pub async fn login(&self, return_to: Option<&str>) -> Result<LoginStart, OidcError> {
        let now = Utc::now();
        let expires_at = now + STATE_LIFETIME;
        let (challenge, verifier) = PkceCodeChallenge::new_random_sha256();
        let (authorization_url, state, nonce) = self
            .client
            .authorize_url(
                CoreAuthenticationFlow::AuthorizationCode,
                CsrfToken::new_random,
                Nonce::new_random,
            )
            .add_scope(Scope::new("openid".to_owned()))
            .add_scope(Scope::new("profile".to_owned()))
            .add_scope(Scope::new("email".to_owned()))
            .set_pkce_challenge(challenge)
            .url();
        let redirect_to = safe_redirect(return_to.unwrap_or_default());
        let verifier = verifier.secret().to_owned();
        let nonce = nonce.secret().to_owned();
        let state_hash = hash(state.secret());
        let mut conn = self.connection().await?;
        use oidc_states::dsl as states;
        diesel::insert_into(oidc_states::table)
            .values((
                states::state_hash.eq(state_hash),
                states::nonce.eq(nonce),
                states::verifier.eq(verifier),
                states::redirect_to.eq(redirect_to),
                states::expires_at.eq(expires_at),
            ))
            .execute(&mut conn)
            .await
            .map_err(OidcError::Database)?;

        Ok(LoginStart {
            authorization_url: authorization_url.to_string(),
            state_cookie: Cookie {
                name: STATE_COOKIE_NAME,
                value: state.secret().to_owned(),
                path: "/auth/callback",
                expires_at: Some(expires_at),
                max_age_seconds: Some(600),
                http_only: true,
                secure: self.secure_cookie,
                same_site_lax: true,
            },
        })
    }

    pub async fn callback(
        &self,
        state: &str,
        code: &str,
        state_cookie: Option<&str>,
    ) -> Result<OidcResult, OidcError> {
        if state.is_empty() || code.is_empty() {
            return Err(OidcError::InvalidResponse);
        }
        let Some(state_cookie) = state_cookie else {
            return Err(OidcError::InvalidState);
        };
        if state_cookie.len() != state.len()
            || !bool::from(state_cookie.as_bytes().ct_eq(state.as_bytes()))
        {
            return Err(OidcError::InvalidState);
        }

        let now = Utc::now();
        let state_hash = hash(state);
        let mut conn = self.connection().await?;
        use oidc_states::dsl as states;
        let state_record = diesel::delete(
            oidc_states::table
                .filter(states::state_hash.eq(state_hash))
                .filter(states::expires_at.gt(now)),
        )
        .returning((states::nonce, states::verifier, states::redirect_to))
        .get_result::<(String, String, String)>(&mut conn)
        .await
        .optional()
        .map_err(OidcError::Database)?
        .ok_or(OidcError::InvalidState)?;
        let (nonce, verifier, redirect_to) = state_record;

        let token = self
            .client
            .exchange_code(AuthorizationCode::new(code.to_owned()))
            .set_pkce_verifier(openidconnect::PkceCodeVerifier::new(verifier));
        let token = token
            .request_async(&self.http_client)
            .await
            .map_err(|error| OidcError::Provider(error.to_string()))?;
        let id_token = token.id_token().ok_or(OidcError::AuthenticationFailed)?;
        let nonce = Nonce::new(nonce);
        let claims = id_token
            .claims(&self.client.id_token_verifier(), &nonce)
            .map_err(|_| OidcError::AuthenticationFailed)?;
        let subject = claims.subject().as_str().to_owned();
        if subject.is_empty() {
            return Err(OidcError::AuthenticationFailed);
        }
        let display_name = claims
            .name()
            .and_then(|name| name.get(None))
            .map(|name| name.as_str())
            .filter(|name| !name.is_empty())
            .or_else(|| claims.preferred_username().map(|name| name.as_str()))
            .filter(|name| !name.is_empty())
            .or_else(|| claims.email().map(|email| email.as_str()))
            .filter(|name| !name.is_empty())
            .unwrap_or(&subject)
            .to_owned();

        let id_token_expiry = claims.expiration();
        let token_expiry = token
            .expires_in()
            .and_then(|ttl| ChronoDuration::from_std(ttl).ok())
            .map(|ttl| Utc::now() + ttl);
        let mut expiry = std::cmp::min(Utc::now() + SESSION_LIFETIME, id_token_expiry);
        if let Some(token_expiry) = token_expiry {
            expiry = std::cmp::min(expiry, token_expiry);
        }
        if expiry <= Utc::now() {
            return Err(OidcError::SessionExpired);
        }

        let member_id = diesel::insert_into(members::table)
            .values((
                members::id.eq(Uuid::new_v4()),
                members::oidc_subject.eq(&subject),
                members::display_name.eq(&display_name),
            ))
            .on_conflict(members::oidc_subject)
            .do_update()
            .set(members::display_name.eq(diesel::upsert::excluded(members::display_name)))
            .returning(members::id)
            .get_result::<Uuid>(&mut conn)
            .await
            .map_err(OidcError::Database)?;

        let session_token = CsrfToken::new_random().secret().to_owned();
        diesel::insert_into(oidc_sessions::table)
            .values((
                oidc_sessions::token_hash.eq(hash(&session_token)),
                oidc_sessions::member_id.eq(member_id),
                oidc_sessions::expiry.eq(expiry),
            ))
            .execute(&mut conn)
            .await
            .map_err(OidcError::Database)?;

        Ok(OidcResult {
            redirect_to,
            session_cookie: Cookie {
                name: SESSION_COOKIE_NAME,
                value: session_token,
                path: "/",
                expires_at: Some(expiry),
                max_age_seconds: None,
                http_only: true,
                secure: self.secure_cookie,
                same_site_lax: true,
            },
        })
    }

    pub fn state_cookie_clear(&self) -> Cookie {
        Cookie {
            name: STATE_COOKIE_NAME,
            value: String::new(),
            path: "/auth/callback",
            expires_at: None,
            max_age_seconds: Some(-1),
            http_only: true,
            secure: self.secure_cookie,
            same_site_lax: true,
        }
    }

    pub fn session_cookie_clear(&self) -> Cookie {
        Cookie {
            name: SESSION_COOKIE_NAME,
            value: String::new(),
            path: "/",
            expires_at: None,
            max_age_seconds: Some(-1),
            http_only: true,
            secure: self.secure_cookie,
            same_site_lax: true,
        }
    }

    pub fn accepts_origin(&self, origin: &str) -> bool {
        let Ok(origin) = Url::parse(origin) else {
            return false;
        };
        [&self.config.public_origin, &self.config.frontend_origin]
            .into_iter()
            .filter_map(|expected| Url::parse(expected).ok())
            .any(|expected| {
                expected.scheme().eq_ignore_ascii_case(origin.scheme())
                    && expected.host_str().is_some_and(|host| {
                        origin
                            .host_str()
                            .is_some_and(|actual| host.eq_ignore_ascii_case(actual))
                    })
                    && expected.port_or_known_default() == origin.port_or_known_default()
            })
    }

    async fn connection(
        &self,
    ) -> Result<
        diesel_async::pooled_connection::bb8::PooledConnection<'_, AsyncPgConnection>,
        OidcError,
    > {
        self.pool
            .get()
            .await
            .map_err(|error| OidcError::Pool(error.to_string()))
    }
}

fn hash(value: &str) -> Vec<u8> {
    Sha256::digest(value.as_bytes()).to_vec()
}

fn safe_redirect(value: &str) -> String {
    if value.is_empty()
        || !value.starts_with('/')
        || value.starts_with("//")
        || value.contains('\\')
        || value.chars().any(char::is_control)
    {
        return "/".to_owned();
    }
    let Ok(parsed) = Url::parse(&format!("https://horse.invalid{value}")) else {
        return "/".to_owned();
    };
    if parsed.host_str() != Some("horse.invalid") {
        return "/".to_owned();
    }
    let path = parsed.path();
    let query = parsed
        .query()
        .map(|query| format!("?{query}"))
        .unwrap_or_default();
    format!("{path}{query}")
}
