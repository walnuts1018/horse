use std::{env, error::Error, fmt, time::Duration};

use async_imap::{Client, types::Fetch};
use chrono::{DateTime, Duration as ChronoDuration, Utc};
use diesel::{BoolExpressionMethods, ExpressionMethods, OptionalExtension, QueryDsl};
use diesel_async::{AsyncConnection, RunQueryDsl};
use futures_util::TryStreamExt;
use mail_parser::{HeaderName, HeaderValue, MessageParser};
use sha2::{Digest, Sha256};
use tokio::{net::TcpStream, time::timeout};
use tokio_rustls::{
    TlsConnector,
    rustls::{ClientConfig, RootCertStore, pki_types::ServerName},
};
use uuid::Uuid;
use webpki_roots::TLS_SERVER_ROOTS;

use evidence_application::{RegisterDocumentAttachment, RegisterDocumentCommand};
use evidence_postgres::{PgPool, document_registration::register_document_in_transaction};
use horse_object_storage::ObjectStore;
use horse_postgres_common::schema::public;
use serde_json::json;

const DEFAULT_MAX_MESSAGE_BYTES: usize = 10 << 20;
const MAX_MESSAGE_BYTES: usize = 25 << 20;
const DEFAULT_MAX_MESSAGES: usize = 100;
const MAX_MESSAGES: usize = 1000;
const MAX_BATCH_BYTES: usize = 50 << 20;
const MAX_DOCUMENT_BYTES: usize = 20 << 20;
const DEFAULT_TIMEOUT: Duration = Duration::from_secs(30);
const MAILBOX_LEASE_DURATION: ChronoDuration = ChronoDuration::minutes(5);

#[derive(Clone)]
pub struct MailboxConfig {
    pub address: String,
    pub server_name: String,
    pub mailbox: String,
    pub username: String,
    pub password: String,
    pub recipient_base: String,
    pub poll_interval: Duration,
    max_message_bytes: usize,
    max_messages: usize,
}

#[derive(Clone, Copy, Debug, Default)]
pub struct MailboxCursor {
    pub uid_validity: u32,
    pub uid: u32,
}

pub struct MailboxMessage {
    pub uid: u32,
    pub delivered_to: String,
    pub rfc822: Vec<u8>,
}

struct MailboxLease {
    token: Uuid,
    cursor: MailboxCursor,
}

struct PreparedMessage {
    command: RegisterDocumentCommand,
    token_hash: [u8; 32],
}

struct RejectedMessage {
    household_id: Uuid,
    created_by: Uuid,
    uid: u32,
    token_hash: [u8; 32],
}

pub async fn poll_database(
    pool: &PgPool,
    store: &ObjectStore,
    config: &MailboxConfig,
) -> Result<MailboxBatch, MailboxError> {
    let Some(lease) = acquire_mailbox_lease(pool).await? else {
        return Ok(MailboxBatch {
            messages: Vec::new(),
            rejected_uids: Vec::new(),
            next: MailboxCursor::default(),
        });
    };
    let work = async {
        let mut batch = poll(config, lease.cursor).await?;
        let mut prepared = Vec::new();
        let mut rejected = Vec::new();
        for message in &batch.messages {
            let Some(token_hash) =
                forwarding_token_hash(&message.delivered_to, &config.recipient_base)
            else {
                continue;
            };
            let Some((household_id, created_by)) = active_forwarding(pool, &token_hash).await?
            else {
                continue;
            };
            let parsed_email = match crate::email::parse_email(&message.rfc822) {
                Ok(parsed_email) => parsed_email,
                Err(_) => {
                    if message.rfc822.len() > MAX_DOCUMENT_BYTES
                        && message.rfc822.len() <= MAX_MESSAGE_BYTES
                    {
                        rejected.push(RejectedMessage {
                            household_id,
                            created_by,
                            uid: message.uid,
                            token_hash,
                        });
                    }
                    continue;
                }
            };
            let (object_key, sha256) = store
                .put(household_id, "message/rfc822", &message.rfc822)
                .await
                .map_err(|_| MailboxError("mailbox message storage failed"))?;
            let mut attachment_commands = Vec::with_capacity(parsed_email.attachments.len());
            for (index, attachment) in parsed_email.attachments.iter().enumerate() {
                let (attachment_key, attachment_hash) = store
                    .put(household_id, &attachment.mime_type, &attachment.data)
                    .await
                    .map_err(|_| MailboxError("mailbox attachment storage failed"))?;
                attachment_commands.push(RegisterDocumentAttachment {
                    source_kind: "receipt".to_owned(),
                    original_filename: attachment.filename.clone(),
                    mime_type: attachment.mime_type.clone(),
                    sha256: attachment_hash,
                    object_key: attachment_key,
                    metadata: json!({}),
                    provider: "gemini_developer_api".to_owned(),
                    model: "free".to_owned(),
                    purpose: "interactive".to_owned(),
                    idempotency_key: format!(
                        "mailbox:{}:{}:attachment:{index}",
                        batch.next.uid_validity, message.uid
                    ),
                    request: json!({"purpose": "interactive", "model": "free"}),
                });
            }
            prepared.push(PreparedMessage {
                command: RegisterDocumentCommand {
                    household_id,
                    created_by,
                    source_kind: "email".to_owned(),
                    original_filename: String::new(),
                    mime_type: "message/rfc822".to_owned(),
                    sha256,
                    object_key,
                    analysis_object_key: None,
                    analysis_mime_type: None,
                    analysis_transform: None,
                    metadata: json!({}),
                    provider: "gemini_developer_api".to_owned(),
                    model: "free".to_owned(),
                    purpose: "interactive".to_owned(),
                    idempotency_key: format!("mailbox:{}:{}", batch.next.uid_validity, message.uid),
                    request: json!({"purpose": "interactive", "model": "free"}),
                    attachments: attachment_commands,
                },
                token_hash,
            });
        }
        batch
            .rejected_uids
            .extend(rejected.iter().map(|message| message.uid));
        persist_mailbox_batch(pool, lease.token, batch, prepared, rejected).await
    };
    tokio::pin!(work);
    let mut heartbeat = tokio::time::interval(Duration::from_secs(30));
    heartbeat.tick().await;
    let result = loop {
        tokio::select! {
            biased;
            result = &mut work => break result,
            _ = heartbeat.tick() => {
                if !renew_mailbox_lease(pool, lease.token).await {
                    break Err(MailboxError("mailbox lease ownership was lost"));
                }
            }
        }
    };

    if result.is_err() {
        release_mailbox_lease(pool, lease.token).await;
    }
    result
}

async fn renew_mailbox_lease(pool: &PgPool, token: Uuid) -> bool {
    use public::mailbox_cursors::dsl as cursors;
    let Ok(mut pooled) = pool.get().await else {
        return false;
    };
    let now = Utc::now();
    diesel::update(
        cursors::mailbox_cursors
            .filter(cursors::mailbox.eq("forwarding"))
            .filter(cursors::lease_token.eq(Some(token))),
    )
    .set((
        cursors::lease_expires_at.eq(Some(now + MAILBOX_LEASE_DURATION)),
        cursors::updated_at.eq(now),
    ))
    .execute(&mut *pooled)
    .await
    .is_ok_and(|updated| updated == 1)
}

async fn acquire_mailbox_lease(pool: &PgPool) -> Result<Option<MailboxLease>, MailboxError> {
    use public::mailbox_cursors::dsl as cursors;
    let mut pooled = pool
        .get()
        .await
        .map_err(|_| MailboxError("mailbox database connection failed"))?;
    let now = Utc::now();
    let token = Uuid::now_v7();
    let claimed = diesel::update(
        cursors::mailbox_cursors
            .filter(cursors::mailbox.eq("forwarding"))
            .filter(
                cursors::lease_expires_at
                    .is_null()
                    .or(cursors::lease_expires_at.lt(now)),
            ),
    )
    .set((
        cursors::lease_token.eq(Some(token)),
        cursors::lease_expires_at.eq(Some(now + MAILBOX_LEASE_DURATION)),
        cursors::updated_at.eq(now),
    ))
    .returning((cursors::uid_validity, cursors::uid))
    .get_result::<(i64, i64)>(&mut *pooled)
    .await
    .optional()
    .map_err(|_| MailboxError("mailbox cursor lease failed"))?;
    let Some((uid_validity, uid)) = claimed else {
        return Ok(None);
    };
    Ok(Some(MailboxLease {
        token,
        cursor: MailboxCursor {
            uid_validity: u32::try_from(uid_validity)
                .map_err(|_| MailboxError("mailbox cursor is invalid"))?,
            uid: u32::try_from(uid).map_err(|_| MailboxError("mailbox cursor is invalid"))?,
        },
    }))
}

async fn persist_mailbox_batch(
    pool: &PgPool,
    lease_token: Uuid,
    batch: MailboxBatch,
    prepared: Vec<PreparedMessage>,
    rejected: Vec<RejectedMessage>,
) -> Result<MailboxBatch, MailboxError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|_| MailboxError("mailbox database connection failed"))?;
    (*pooled)
        .transaction::<MailboxBatch, MailboxError, _>(async move |conn| {
            use public::{household_events, mailbox_cursors::dsl as cursors};
            for message in rejected {
                let Some((household_id, created_by)) =
                    active_forwarding_in_transaction(conn, &message.token_hash).await?
                else {
                    return Err(MailboxError("mailbox forwarding was revoked"));
                };
                if household_id != message.household_id || created_by != message.created_by {
                    return Err(MailboxError("mailbox forwarding changed during polling"));
                }
                diesel::insert_into(household_events::table)
                    .values((
                        household_events::household_id.eq(message.household_id),
                        household_events::event_type.eq("receipt.mailbox_message.rejected"),
                        household_events::entity_kind.eq("mailbox_message"),
                        household_events::entity_id.eq(Uuid::now_v7()),
                        household_events::revision.eq(1_i64),
                        household_events::changed_fields.eq(vec![Some("status".to_owned())]),
                        household_events::payload
                            .eq(json!({"status": "rejected", "reason": "invalid_email_size"})),
                        household_events::actor_id.eq(message.created_by),
                        household_events::created_at.eq(Utc::now()),
                    ))
                    .execute(conn)
                    .await
                    .map_err(|_| MailboxError("mailbox rejection event failed"))?;
            }
            for message in prepared {
                let Some((household_id, created_by)) =
                    active_forwarding_in_transaction(conn, &message.token_hash).await?
                else {
                    return Err(MailboxError("mailbox forwarding was revoked"));
                };
                if household_id != message.command.household_id
                    || created_by != message.command.created_by
                {
                    return Err(MailboxError("mailbox forwarding changed during polling"));
                }
                register_document_in_transaction(conn, message.command)
                    .await
                    .map_err(|_| MailboxError("mailbox document registration failed"))?;
            }
            let updated = diesel::update(
                cursors::mailbox_cursors
                    .filter(cursors::mailbox.eq("forwarding"))
                    .filter(cursors::lease_token.eq(Some(lease_token))),
            )
            .set((
                cursors::uid_validity.eq(i64::from(batch.next.uid_validity)),
                cursors::uid.eq(i64::from(batch.next.uid)),
                cursors::lease_token.eq(None::<Uuid>),
                cursors::lease_expires_at.eq(None::<DateTime<Utc>>),
                cursors::updated_at.eq(Utc::now()),
            ))
            .execute(conn)
            .await
            .map_err(|_| MailboxError("mailbox cursor update failed"))?;
            if updated != 1 {
                return Err(MailboxError("mailbox lease ownership was lost"));
            }
            Ok(batch)
        })
        .await
}

async fn release_mailbox_lease(pool: &PgPool, token: Uuid) {
    use public::mailbox_cursors::dsl as cursors;
    let Ok(mut pooled) = pool.get().await else {
        return;
    };
    if diesel::update(
        cursors::mailbox_cursors
            .filter(cursors::mailbox.eq("forwarding"))
            .filter(cursors::lease_token.eq(Some(token))),
    )
    .set((
        cursors::lease_token.eq(None::<Uuid>),
        cursors::lease_expires_at.eq(None::<DateTime<Utc>>),
        cursors::updated_at.eq(Utc::now()),
    ))
    .execute(&mut *pooled)
    .await
    .is_err()
    {
        tracing::warn!("mailbox lease release failed");
    }
}

pub async fn run(
    pool: &PgPool,
    store: &ObjectStore,
    config: &MailboxConfig,
    mut shutdown: tokio::sync::watch::Receiver<bool>,
) {
    loop {
        if *shutdown.borrow() {
            return;
        }
        match poll_database(pool, store, config).await {
            Ok(batch) => {
                if !batch.rejected_uids.is_empty() {
                    tracing::warn!(
                        count = batch.rejected_uids.len(),
                        "mailbox messages were rejected"
                    );
                }
            }
            Err(error) => tracing::warn!(error = %error, "mailbox poll failed"),
        }
        tokio::select! {
            _ = tokio::time::sleep(config.poll_interval) => {},
            changed = shutdown.changed() => {
                if changed.is_err() || *shutdown.borrow() {
                    return;
                }
            }
        }
    }
}

async fn active_forwarding(
    pool: &PgPool,
    token_hash: &[u8; 32],
) -> Result<Option<(Uuid, Uuid)>, MailboxError> {
    use public::household_mail_forwardings::dsl as forwardings;
    let mut pooled = pool
        .get()
        .await
        .map_err(|_| MailboxError("mailbox database connection failed"))?;
    forwardings::household_mail_forwardings
        .filter(forwardings::token_hash.eq(token_hash.to_vec()))
        .filter(forwardings::revoked_at.is_null())
        .select((forwardings::household_id, forwardings::created_by))
        .first::<(Uuid, Uuid)>(&mut *pooled)
        .await
        .optional()
        .map_err(|_| MailboxError("mailbox forwarding lookup failed"))
}

async fn active_forwarding_in_transaction(
    conn: &mut diesel_async::AsyncPgConnection,
    token_hash: &[u8; 32],
) -> Result<Option<(Uuid, Uuid)>, MailboxError> {
    use public::household_mail_forwardings::dsl as forwardings;
    forwardings::household_mail_forwardings
        .filter(forwardings::token_hash.eq(token_hash.to_vec()))
        .filter(forwardings::revoked_at.is_null())
        .for_share()
        .select((forwardings::household_id, forwardings::created_by))
        .first::<(Uuid, Uuid)>(conn)
        .await
        .optional()
        .map_err(|_| MailboxError("mailbox forwarding lookup failed"))
}

pub struct MailboxBatch {
    pub messages: Vec<MailboxMessage>,
    pub rejected_uids: Vec<u32>,
    pub next: MailboxCursor,
}

#[derive(Debug)]
pub struct MailboxError(&'static str);

impl fmt::Display for MailboxError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(self.0)
    }
}

impl Error for MailboxError {}

impl From<diesel::result::Error> for MailboxError {
    fn from(_: diesel::result::Error) -> Self {
        Self("mailbox database operation failed")
    }
}

impl MailboxConfig {
    pub fn from_env() -> Result<Option<Self>, MailboxError> {
        let address = env::var("MAILBOX_IMAPS_ADDRESS").unwrap_or_default();
        let server_name = env::var("MAILBOX_IMAPS_SERVER_NAME").unwrap_or_default();
        let mailbox = env::var("MAILBOX_IMAPS_MAILBOX").unwrap_or_default();
        let username = env::var("MAILBOX_IMAPS_USERNAME").unwrap_or_default();
        let password = env::var("MAILBOX_IMAPS_PASSWORD").unwrap_or_default();
        let recipient_base = env::var("MAILBOX_RECIPIENT_BASE").unwrap_or_default();
        if [
            address.as_str(),
            server_name.as_str(),
            mailbox.as_str(),
            username.as_str(),
            password.as_str(),
            recipient_base.as_str(),
        ]
        .iter()
        .any(|value| value.is_empty())
        {
            return Ok(None);
        }

        let (host, port) = address
            .rsplit_once(':')
            .ok_or(MailboxError("mailbox address must include a host and port"))?;
        if host.is_empty() || port.parse::<u16>().is_err() || !valid_plain_address(&recipient_base)
        {
            return Err(MailboxError("mailbox configuration is invalid"));
        }
        let max_message_bytes =
            parse_positive_limit("MAILBOX_MAX_MESSAGE_BYTES").unwrap_or(DEFAULT_MAX_MESSAGE_BYTES);
        let max_messages =
            parse_positive_limit("MAILBOX_MAX_MESSAGES_PER_POLL").unwrap_or(DEFAULT_MAX_MESSAGES);
        if max_message_bytes > MAX_MESSAGE_BYTES || max_messages > MAX_MESSAGES {
            return Err(MailboxError("mailbox poll limit is invalid"));
        }
        if max_message_bytes > MAX_BATCH_BYTES {
            return Err(MailboxError("mailbox message limit exceeds batch limit"));
        }
        let poll_interval = env::var("MAILBOX_POLL_INTERVAL")
            .ok()
            .and_then(|value| humantime::parse_duration(&value).ok())
            .filter(|duration| !duration.is_zero())
            .unwrap_or(Duration::from_secs(60));
        Ok(Some(Self {
            address,
            server_name,
            mailbox,
            username,
            password,
            recipient_base,
            poll_interval,
            max_message_bytes,
            max_messages,
        }))
    }
}

pub async fn poll(
    config: &MailboxConfig,
    cursor: MailboxCursor,
) -> Result<MailboxBatch, MailboxError> {
    if cursor.uid_validity == 0 && cursor.uid != 0 {
        return Err(MailboxError("mailbox cursor has no UIDVALIDITY"));
    }
    timeout(DEFAULT_TIMEOUT, poll_with_timeout(config, cursor))
        .await
        .map_err(|_| MailboxError("mailbox poll timed out"))?
}

async fn poll_with_timeout(
    config: &MailboxConfig,
    cursor: MailboxCursor,
) -> Result<MailboxBatch, MailboxError> {
    let socket = TcpStream::connect(&config.address)
        .await
        .map_err(|_| MailboxError("mailbox connection failed"))?;
    let mut roots = RootCertStore::empty();
    roots.extend(TLS_SERVER_ROOTS.iter().cloned());
    let tls_config = ClientConfig::builder()
        .with_root_certificates(roots)
        .with_no_client_auth();
    let server_name = ServerName::try_from(config.server_name.clone())
        .map_err(|_| MailboxError("mailbox server name is invalid"))?;
    let tls = TlsConnector::from(std::sync::Arc::new(tls_config))
        .connect(server_name, socket)
        .await
        .map_err(|_| MailboxError("mailbox TLS connection failed"))?;
    let client = Client::new(tls);
    let mut session = client
        .login(&config.username, &config.password)
        .await
        .map_err(|_| MailboxError("mailbox authentication failed"))?;
    let status = session
        .examine(&config.mailbox)
        .await
        .map_err(|_| MailboxError("mailbox read-only selection failed"))?;
    let uid_validity = status
        .uid_validity
        .filter(|validity| *validity != 0)
        .ok_or(MailboxError("mailbox UIDVALIDITY is unavailable"))?;
    let uid_next = status
        .uid_next
        .filter(|next| *next != 0)
        .ok_or(MailboxError("mailbox UIDNEXT is unavailable"))?;
    let mut next = if uid_validity == cursor.uid_validity {
        cursor
    } else {
        MailboxCursor {
            uid_validity,
            uid: 0,
        }
    };
    if next.uid == u32::MAX {
        return Ok(MailboxBatch {
            messages: Vec::new(),
            rejected_uids: Vec::new(),
            next,
        });
    }
    let first_uid = next.uid + 1;
    let last_uid = uid_next - 1;
    if first_uid > last_uid {
        return Ok(MailboxBatch {
            messages: Vec::new(),
            rejected_uids: Vec::new(),
            next,
        });
    }
    let search_last_uid = first_uid
        .saturating_add(config.max_messages as u32 - 1)
        .min(last_uid);
    let uids = session
        .uid_search(format!("UID {first_uid}:{search_last_uid}"))
        .await
        .map_err(|_| MailboxError("mailbox UID search failed"))?;
    if uids
        .iter()
        .any(|uid| *uid < first_uid || *uid > search_last_uid)
    {
        return Err(MailboxError("mailbox UID search returned an invalid UID"));
    }
    if uids.is_empty() {
        next.uid = search_last_uid;
        return Ok(MailboxBatch {
            messages: Vec::new(),
            rejected_uids: Vec::new(),
            next,
        });
    }

    let mut sorted_uids = uids.into_iter().collect::<Vec<_>>();
    sorted_uids.sort_unstable();
    let mut batch = MailboxBatch {
        messages: Vec::new(),
        rejected_uids: Vec::new(),
        next,
    };
    let mut batch_bytes = 0usize;
    for uid in sorted_uids {
        let size = fetch_size(&mut session, uid).await?;
        if size > config.max_message_bytes {
            batch.rejected_uids.push(uid);
            batch.next.uid = uid;
            continue;
        }
        let remaining_batch_bytes = MAX_BATCH_BYTES.saturating_sub(batch_bytes);
        if size > remaining_batch_bytes {
            break;
        }
        let body_limit = config
            .max_message_bytes
            .min(remaining_batch_bytes)
            .saturating_add(1);
        let Some(raw) = fetch_body(&mut session, uid, body_limit).await? else {
            batch.rejected_uids.push(uid);
            batch.next.uid = uid;
            continue;
        };
        if raw.len() > remaining_batch_bytes && remaining_batch_bytes < config.max_message_bytes {
            break;
        }
        if raw.len() > config.max_message_bytes {
            batch.rejected_uids.push(uid);
            batch.next.uid = uid;
            continue;
        }
        if raw.len() > remaining_batch_bytes {
            if remaining_batch_bytes < config.max_message_bytes {
                break;
            }
            batch.rejected_uids.push(uid);
            batch.next.uid = uid;
            continue;
        }
        let Some(delivered_to) = trusted_delivered_to(&raw, &config.recipient_base) else {
            batch.rejected_uids.push(uid);
            batch.next.uid = uid;
            continue;
        };
        batch_bytes += raw.len();
        batch.next.uid = uid;
        batch.messages.push(MailboxMessage {
            uid,
            delivered_to,
            rfc822: raw,
        });
    }
    let _ = session.logout().await;
    Ok(batch)
}

async fn fetch_size<T>(
    session: &mut async_imap::Session<T>,
    uid: u32,
) -> Result<usize, MailboxError>
where
    T: tokio::io::AsyncRead + tokio::io::AsyncWrite + Unpin + fmt::Debug + Send,
{
    let messages = session
        .uid_fetch(uid.to_string(), "(UID RFC822.SIZE)")
        .await
        .map_err(|_| MailboxError("mailbox message size fetch failed"))?
        .try_collect::<Vec<Fetch>>()
        .await
        .map_err(|_| MailboxError("mailbox message size fetch failed"))?;
    if messages.len() != 1 || messages[0].uid != Some(uid) {
        return Err(MailboxError("mailbox message size response is invalid"));
    }
    messages[0]
        .size
        .map(|size| size as usize)
        .ok_or(MailboxError("mailbox message size is unavailable"))
}

async fn fetch_body<T>(
    session: &mut async_imap::Session<T>,
    uid: u32,
    max_bytes: usize,
) -> Result<Option<Vec<u8>>, MailboxError>
where
    T: tokio::io::AsyncRead + tokio::io::AsyncWrite + Unpin + fmt::Debug + Send,
{
    let messages = session
        .uid_fetch(uid.to_string(), format!("(UID BODY.PEEK[]<0.{max_bytes}>)"))
        .await
        .map_err(|_| MailboxError("mailbox message fetch failed"))?
        .try_collect::<Vec<Fetch>>()
        .await
        .map_err(|_| MailboxError("mailbox message fetch failed"))?;
    if messages.len() != 1 || messages[0].uid != Some(uid) {
        return Err(MailboxError("mailbox message response is invalid"));
    }
    Ok(messages[0].body().map(<[u8]>::to_vec))
}

fn trusted_delivered_to(raw: &[u8], recipient_base: &str) -> Option<String> {
    let message = MessageParser::new().with_minimal_headers().parse(raw)?;
    let values = message.header_values(HeaderName::Other("delivered-to".into()));
    let mut values = values.filter_map(|value| match value {
        HeaderValue::Text(value) => Some(value.trim()),
        _ => None,
    });
    let address = values.next()?;
    if values.next().is_some()
        || !valid_plain_address(address)
        || !has_plus_recipient(address, recipient_base)
    {
        return None;
    }
    Some(address.to_owned())
}

fn valid_plain_address(value: &str) -> bool {
    let Some((local, domain)) = value.split_once('@') else {
        return false;
    };
    value.matches('@').count() == 1
        && !local.is_empty()
        && !domain.is_empty()
        && !value.chars().any(|character| {
            matches!(
                character,
                ' ' | '\t' | '<' | '>' | ',' | ';' | '"' | '\r' | '\n'
            )
        })
        && !local.contains('+')
}

fn has_plus_recipient(address: &str, base: &str) -> bool {
    let Some((local, domain)) = address.split_once('@') else {
        return false;
    };
    let Some((base_local, base_domain)) = base.split_once('@') else {
        return false;
    };
    let Some(tag) = local.strip_prefix(&format!("{base_local}+")) else {
        return false;
    };
    !tag.is_empty() && domain.eq_ignore_ascii_case(base_domain)
}

fn forwarding_token_hash(address: &str, base: &str) -> Option<[u8; 32]> {
    let (local, domain) = address.split_once('@')?;
    let (base_local, base_domain) = base.split_once('@')?;
    let tag = local.strip_prefix(&format!("{base_local}+"))?;
    if !domain.eq_ignore_ascii_case(base_domain) || tag.len() != 52 {
        return None;
    }
    let mut decoded = Vec::with_capacity(32);
    let mut accumulator = 0_u32;
    let mut bits = 0_u8;
    for character in tag.bytes() {
        let value = match character {
            b'a'..=b'z' => character - b'a',
            b'2'..=b'7' => character - b'2' + 26,
            _ => return None,
        };
        accumulator = (accumulator << 5) | u32::from(value);
        bits += 5;
        if bits >= 8 {
            bits -= 8;
            decoded.push(((accumulator >> bits) & 0xff) as u8);
        }
    }
    if decoded.len() != 32 || bits != 4 || accumulator & 0x0f != 0 {
        return None;
    }
    Some(Sha256::digest(decoded).into())
}

fn parse_positive_limit(name: &str) -> Option<usize> {
    env::var(name)
        .ok()?
        .parse::<usize>()
        .ok()
        .filter(|value| *value > 0)
}

#[cfg(test)]
mod tests {
    use chrono::{DateTime, Utc};
    use diesel::{ExpressionMethods, QueryDsl};
    use diesel_async::RunQueryDsl;

    use super::{acquire_mailbox_lease, release_mailbox_lease};
    use horse_postgres_common::{connect, schema::public::mailbox_cursors::dsl as cursors};

    #[tokio::test]
    async fn forwarding_cursor_lease_is_exclusive_and_reacquirable() {
        let Ok(database_url) = std::env::var("HORSE_TEST_DATABASE_URL") else {
            eprintln!("HORSE_TEST_DATABASE_URL is unset; skipping the PostgreSQL lease check");
            return;
        };
        let pool = connect(database_url)
            .await
            .expect("test database connection should succeed");
        let mut connection = pool
            .get()
            .await
            .expect("test database connection should be available");
        let saved_cursor = cursors::mailbox_cursors
            .filter(cursors::mailbox.eq("forwarding"))
            .select((
                cursors::uid_validity,
                cursors::uid,
                cursors::lease_token,
                cursors::lease_expires_at,
                cursors::updated_at,
            ))
            .first::<(
                i64,
                i64,
                Option<uuid::Uuid>,
                Option<DateTime<Utc>>,
                DateTime<Utc>,
            )>(&mut *connection)
            .await
            .expect("forwarding cursor should exist after migrations");
        diesel::update(cursors::mailbox_cursors.filter(cursors::mailbox.eq("forwarding")))
            .set((
                cursors::uid_validity.eq(0_i64),
                cursors::uid.eq(0_i64),
                cursors::lease_token.eq(None::<uuid::Uuid>),
                cursors::lease_expires_at.eq(None::<DateTime<Utc>>),
                cursors::updated_at.eq(Utc::now()),
            ))
            .execute(&mut *connection)
            .await
            .expect("forwarding cursor should be reset for the concurrency check");
        drop(connection);

        let check = async {
            let (first, second) =
                tokio::join!(acquire_mailbox_lease(&pool), acquire_mailbox_lease(&pool));
            let owned_token = match (first, second) {
                (Ok(Some(lease)), Ok(None)) | (Ok(None), Ok(Some(lease))) => Some(lease.token),
                (Ok(Some(first)), Ok(Some(second))) => {
                    release_mailbox_lease(&pool, first.token).await;
                    release_mailbox_lease(&pool, second.token).await;
                    None
                }
                _ => None,
            };
            let Some(owned_token) = owned_token else {
                return false;
            };
            release_mailbox_lease(&pool, owned_token).await;
            let Ok(Some(reacquired)) = acquire_mailbox_lease(&pool).await else {
                return false;
            };
            release_mailbox_lease(&pool, reacquired.token).await;
            true
        }
        .await;

        let mut connection = pool
            .get()
            .await
            .expect("test database connection should be available for restoration");
        diesel::update(cursors::mailbox_cursors.filter(cursors::mailbox.eq("forwarding")))
            .set((
                cursors::uid_validity.eq(saved_cursor.0),
                cursors::uid.eq(saved_cursor.1),
                cursors::lease_token.eq(saved_cursor.2),
                cursors::lease_expires_at.eq(saved_cursor.3),
                cursors::updated_at.eq(saved_cursor.4),
            ))
            .execute(&mut *connection)
            .await
            .expect("forwarding cursor state should be restored");

        assert!(
            check,
            "concurrent lease acquisition must have exactly one owner and permit reacquisition after release"
        );
    }
}
