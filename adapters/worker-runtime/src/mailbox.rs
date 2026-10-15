use std::{
    env,
    error::Error,
    fmt,
    sync::{Arc, Mutex},
    time::Duration,
};

use async_imap::{Client, types::Fetch};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, dsl::now};
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

pub async fn poll_database(
    pool: &PgPool,
    store: &ObjectStore,
    config: &MailboxConfig,
) -> Result<MailboxBatch, MailboxError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|_| MailboxError("mailbox database connection failed"))?;
    let uploaded_keys = Arc::new(Mutex::new(Vec::new()));
    let transaction_keys = Arc::clone(&uploaded_keys);
    let result = (&mut *pooled)
        .transaction::<MailboxBatch, MailboxError, _>(async |conn| {
            use public::{household_events, mailbox_cursors::dsl as cursors};
            let (uid_validity, uid) = cursors::mailbox_cursors
                .filter(cursors::mailbox.eq("forwarding"))
                .for_update()
                .select((cursors::uid_validity, cursors::uid))
                .first::<(i64, i64)>(conn)
                .await
                .map_err(|_| MailboxError("mailbox cursor load failed"))?;
            let cursor = MailboxCursor {
                uid_validity: u32::try_from(uid_validity)
                    .map_err(|_| MailboxError("mailbox cursor is invalid"))?,
                uid: u32::try_from(uid).map_err(|_| MailboxError("mailbox cursor is invalid"))?,
            };
            let mut batch = poll(config, cursor).await?;
            let mut rejected_uids = Vec::new();
            for message in &batch.messages {
                let Some(token_hash) =
                    forwarding_token_hash(&message.delivered_to, &config.recipient_base)
                else {
                    continue;
                };
                let Some((household_id, created_by)) = active_forwarding(conn, &token_hash).await?
                else {
                    continue;
                };
                let parsed_email = match crate::email::parse_email(&message.rfc822) {
                    Ok(parsed_email) => parsed_email,
                    Err(_) => {
                        if message.rfc822.len() > MAX_DOCUMENT_BYTES
                            && message.rfc822.len() <= MAX_MESSAGE_BYTES
                        {
                            diesel::insert_into(household_events::table)
                                .values((
                                    household_events::household_id.eq(household_id),
                                    household_events::event_type
                                        .eq("receipt.mailbox_message.rejected"),
                                    household_events::entity_kind.eq("mailbox_message"),
                                    household_events::entity_id.eq(Uuid::now_v7()),
                                    household_events::revision.eq(1_i64),
                                    household_events::changed_fields
                                        .eq(vec![Some("status".to_owned())]),
                                    household_events::payload
                                        .eq(json!({"status": "rejected", "reason": "invalid_email_size"})),
                                    household_events::actor_id.eq(created_by),
                                    household_events::created_at.eq(now),
                                ))
                                .execute(conn)
                                .await
                                .map_err(|_| MailboxError("mailbox rejection event failed"))?;
                            rejected_uids.push(message.uid);
                        }
                        continue;
                    }
                };
                let attachments = parsed_email.attachments;
                let (object_key, sha256) = store
                    .put(household_id, "message/rfc822", &message.rfc822)
                    .await
                    .map_err(|_| MailboxError("mailbox message storage failed"))?;
                if let Ok(mut keys) = transaction_keys.lock() {
                    keys.push(object_key.clone());
                }
                let mut attachment_commands = Vec::with_capacity(attachments.len());
                for (index, attachment) in attachments.iter().enumerate() {
                    let (attachment_key, attachment_hash) = store
                        .put(household_id, &attachment.mime_type, &attachment.data)
                        .await
                        .map_err(|_| MailboxError("mailbox attachment storage failed"))?;
                    if let Ok(mut keys) = transaction_keys.lock() {
                        keys.push(attachment_key.clone());
                    }
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
                register_document_in_transaction(
                    conn,
                    RegisterDocumentCommand {
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
                        idempotency_key: format!(
                            "mailbox:{}:{}",
                            batch.next.uid_validity, message.uid
                        ),
                        request: json!({"purpose": "interactive", "model": "free"}),
                        attachments: attachment_commands,
                    },
                )
                .await
                .map_err(|_| MailboxError("mailbox document registration failed"))?;
            }
            batch.rejected_uids.extend(rejected_uids);
            let updated =
                diesel::update(cursors::mailbox_cursors.filter(cursors::mailbox.eq("forwarding")))
                    .set((
                        cursors::uid_validity.eq(i64::from(batch.next.uid_validity)),
                        cursors::uid.eq(i64::from(batch.next.uid)),
                        cursors::updated_at.eq(now),
                    ))
                    .execute(conn)
                    .await
                    .map_err(|_| MailboxError("mailbox cursor update failed"))?;
            if updated != 1 {
                return Err(MailboxError("mailbox cursor update failed"));
            }
            Ok(batch)
        })
        .await;
    if result.is_err() {
        let keys = uploaded_keys
            .lock()
            .map(|mut keys| std::mem::take(&mut *keys))
            .unwrap_or_default();
        for key in keys {
            if object_is_referenced(pool, &key).await {
                continue;
            }
            if store.delete(&key).await.is_err() {
                tracing::warn!("mailbox orphaned object cleanup failed");
            }
        }
    }
    result
}

async fn object_is_referenced(pool: &PgPool, object_key: &str) -> bool {
    use public::source_documents::dsl as documents;
    let Ok(mut pooled) = pool.get().await else {
        return true;
    };
    documents::source_documents
        .filter(documents::object_key.eq(object_key))
        .select(documents::id)
        .first::<Uuid>(&mut *pooled)
        .await
        .optional()
        .map(|document| document.is_some())
        .unwrap_or(true)
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
    T: tokio::io::AsyncRead + tokio::io::AsyncWrite + Unpin + fmt::Debug,
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
    T: tokio::io::AsyncRead + tokio::io::AsyncWrite + Unpin + fmt::Debug,
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
