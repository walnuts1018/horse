use std::{collections::HashMap, env, error::Error, fmt};

use aes_gcm::{
    Aes256Gcm, KeyInit, Nonce,
    aead::{Aead, Generate},
};
use aws_config::{BehaviorVersion, meta::region::RegionProviderChain};
use aws_credential_types::Credentials;
use aws_sdk_s3::Client;
use base64::{Engine as _, engine::general_purpose::STANDARD};
use hmac::{Hmac, Mac};
use sha2::{Digest, Sha256};
use uuid::Uuid;

const MAX_DOCUMENT_BYTES: usize = 20 << 20;
const EMAIL_V2_PREFIX: &str = "app-aesgcm-v2/";
const EMAIL_V1_PREFIX: &str = "app-aesgcm-v1/";

pub struct Document {
    pub mime_type: String,
    pub data: Vec<u8>,
}

pub struct ObjectStore {
    client: Client,
    bucket: String,
    email_keys: HashMap<String, Vec<u8>>,
    active_email_key_id: Option<String>,
}

struct EmailKeyring {
    keys: HashMap<String, Vec<u8>>,
    active_key_id: Option<String>,
}

#[derive(Debug)]
pub struct StoreError(&'static str);

impl fmt::Display for StoreError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.0)
    }
}

impl Error for StoreError {}

impl ObjectStore {
    pub async fn from_env() -> Result<Self, Box<dyn Error + Send + Sync>> {
        let endpoint = required_env("S3_ENDPOINT")?;
        let url = reqwest::Url::parse(&endpoint)?;
        if !matches!(url.scheme(), "http" | "https")
            || url.host_str().is_none()
            || !matches!(url.path(), "" | "/")
            || url.query().is_some()
            || url.username() != ""
            || url.password().is_some()
        {
            return Err(StoreError("invalid S3 endpoint").into());
        }
        let bucket = required_env("S3_BUCKET")?;
        let access_key = env::var("S3_ACCESS_KEY_ID").unwrap_or_default();
        let secret_key = env::var("S3_SECRET_ACCESS_KEY").unwrap_or_default();
        if access_key.is_empty() != secret_key.is_empty() {
            return Err(StoreError("incomplete S3 credentials").into());
        }
        let region = RegionProviderChain::default_provider().or_else("us-east-1");
        let config_builder = aws_config::defaults(BehaviorVersion::latest()).region(region);
        let config = if access_key.is_empty() {
            config_builder.load().await
        } else {
            config_builder
                .credentials_provider(Credentials::new(
                    access_key,
                    secret_key,
                    None,
                    None,
                    "horse-worker-static",
                ))
                .load()
                .await
        };
        let s3_config = aws_sdk_s3::config::Builder::from(&config)
            .endpoint_url(endpoint)
            .force_path_style(true)
            .build();
        let client = Client::from_conf(s3_config);
        let email_keyring =
            parse_email_keys(&env::var("EMAIL_BODY_ENCRYPTION_KEYS").unwrap_or_default())?;
        Ok(Self {
            client,
            bucket,
            email_keys: email_keyring.keys,
            active_email_key_id: email_keyring.active_key_id,
        })
    }

    pub async fn put(
        &self,
        household_id: Uuid,
        mime_type: &str,
        data: &[u8],
    ) -> Result<(String, String), StoreError> {
        if data.is_empty() || data.len() > MAX_DOCUMENT_BYTES {
            return Err(StoreError("invalid document size"));
        }
        let hash = hex::encode(Sha256::digest(data));
        let mut object_key = format!("{household_id}/originals/{hash}");
        let mut stored_data = data.to_vec();
        let metadata = if mime_type == "message/rfc822" {
            let key_id = self
                .active_email_key_id
                .as_deref()
                .ok_or(StoreError("email encryption key is unavailable"))?;
            let key = self
                .email_keys
                .get(key_id)
                .ok_or(StoreError("email encryption key is unavailable"))?;
            let fingerprint = email_fingerprint(key, data)?;
            object_key =
                format!("{household_id}/originals/{EMAIL_V2_PREFIX}{key_id}/{fingerprint}");
            stored_data = encrypt_email_body(key, &object_key, data)?;
            [("hmac-sha256", fingerprint)]
        } else {
            [("sha256", hash.clone())]
        };
        let result = self
            .client
            .put_object()
            .bucket(&self.bucket)
            .key(&object_key)
            .content_type(mime_type)
            .content_length(stored_data.len() as i64)
            .metadata(metadata[0].0, metadata[0].1.clone())
            .if_none_match("*")
            .body(aws_sdk_s3::primitives::ByteStream::from(stored_data))
            .send()
            .await;
        if result.is_err() {
            let existing = self.get(&object_key, mime_type).await;
            if !existing.is_ok_and(|document| document.data.as_slice() == data) {
                return Err(StoreError("store original document failed"));
            }
        }
        Ok((object_key, hash))
    }

    pub async fn delete(&self, object_key: &str) -> Result<(), StoreError> {
        if object_key.is_empty() {
            return Err(StoreError("invalid original document key"));
        }
        self.client
            .delete_object()
            .bucket(&self.bucket)
            .key(object_key)
            .send()
            .await
            .map_err(|_| StoreError("delete original document failed"))?;
        Ok(())
    }

    pub async fn get(&self, object_key: &str, mime_type: &str) -> Result<Document, StoreError> {
        let object = self
            .client
            .get_object()
            .bucket(&self.bucket)
            .key(object_key)
            .send()
            .await
            .map_err(|_| StoreError("load original document failed"))?;
        let encrypted = object_key.contains("/originals/app-aesgcm-v")
            || object_key.contains("/originals/sse-v1/");
        let max_stored = MAX_DOCUMENT_BYTES + if encrypted { 28 } else { 0 };
        let content_length = object.content_length().unwrap_or_default();
        if content_length <= 0 || content_length as usize > max_stored {
            return Err(StoreError("invalid original document"));
        }
        let mut data = Vec::with_capacity(content_length as usize);
        let mut body = object.body;
        while let Some(chunk) = body.next().await {
            let chunk = chunk.map_err(|_| StoreError("read original document failed"))?;
            if data.len().saturating_add(chunk.len()) > max_stored {
                return Err(StoreError("original document exceeds limit"));
            }
            data.extend_from_slice(&chunk);
        }
        if data.is_empty() {
            return Err(StoreError("original document exceeds limit"));
        }
        if let Some(original_key) = object_key.split_once("/originals/").map(|(_, key)| key) {
            if let Some(suffix) = original_key.strip_prefix(EMAIL_V2_PREFIX) {
                let (key_id, fingerprint) = suffix
                    .split_once('/')
                    .ok_or(StoreError("decrypt original document failed"))?;
                let key = self
                    .email_keys
                    .get(key_id)
                    .ok_or(StoreError("decrypt original document failed"))?;
                data = decrypt_email_body(key, object_key, &data)?;
                if email_fingerprint(key, &data)? != fingerprint {
                    return Err(StoreError("original document fingerprint mismatch"));
                }
            } else if let Some(hash_path) = original_key.strip_prefix(EMAIL_V1_PREFIX) {
                let mut decrypted = None;
                for key in self.email_keys.values() {
                    if let Ok(plaintext) = decrypt_email_body(key, object_key, &data) {
                        decrypted = Some(plaintext);
                        break;
                    }
                }
                data = decrypted.ok_or(StoreError("decrypt original document failed"))?;
                verify_object_hash(hash_path, &data)?;
            } else if let Some(key) = original_key.strip_prefix("sse-v1/") {
                verify_object_hash(key, &data)?;
            } else {
                verify_object_hash(original_key, &data)?;
            }
        }
        if data.is_empty() || data.len() > MAX_DOCUMENT_BYTES {
            return Err(StoreError("invalid original document"));
        }
        Ok(Document {
            mime_type: mime_type.to_owned(),
            data,
        })
    }
}

fn parse_email_keys(configuration: &str) -> Result<EmailKeyring, StoreError> {
    if configuration.is_empty() {
        return Ok(EmailKeyring {
            keys: HashMap::new(),
            active_key_id: None,
        });
    }
    let mut keys = HashMap::new();
    let mut active_key_id = None;
    for entry in configuration.split(',') {
        let (key_id, encoded_key) = entry
            .split_once(':')
            .ok_or(StoreError("invalid email encryption keyring"))?;
        if !valid_key_id(key_id) || keys.contains_key(key_id) {
            return Err(StoreError("invalid email encryption keyring"));
        }
        let key = STANDARD
            .decode(encoded_key)
            .map_err(|_| StoreError("invalid email encryption keyring"))?;
        if key.len() != 32 {
            return Err(StoreError("invalid email encryption keyring"));
        }
        if active_key_id.is_none() {
            active_key_id = Some(key_id.to_owned());
        }
        keys.insert(key_id.to_owned(), key);
    }
    Ok(EmailKeyring {
        keys,
        active_key_id,
    })
}

fn encrypt_email_body(
    key: &[u8],
    object_key: &str,
    plaintext: &[u8],
) -> Result<Vec<u8>, StoreError> {
    let cipher = Aes256Gcm::new_from_slice(key)
        .map_err(|_| StoreError("encrypt original document failed"))?;
    let nonce = Nonce::generate();
    let encrypted = cipher
        .encrypt(
            &nonce,
            aes_gcm::aead::Payload {
                msg: plaintext,
                aad: object_key.as_bytes(),
            },
        )
        .map_err(|_| StoreError("encrypt original document failed"))?;
    let mut output = Vec::with_capacity(nonce.len() + encrypted.len());
    output.extend_from_slice(&nonce);
    output.extend_from_slice(&encrypted);
    Ok(output)
}

fn email_fingerprint(key: &[u8], plaintext: &[u8]) -> Result<String, StoreError> {
    let mut derivation = <Hmac<Sha256> as hmac::digest::KeyInit>::new_from_slice(key)
        .map_err(|_| StoreError("fingerprint original document failed"))?;
    derivation.update(b"horse/email-object-fingerprint-key/v1");
    let derived = derivation.finalize().into_bytes();
    let mut fingerprint = <Hmac<Sha256> as hmac::digest::KeyInit>::new_from_slice(&derived)
        .map_err(|_| StoreError("fingerprint original document failed"))?;
    fingerprint.update(plaintext);
    Ok(hex::encode(fingerprint.finalize().into_bytes()))
}

fn valid_key_id(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 32
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_'))
}

fn decrypt_email_body(
    key: &[u8],
    object_key: &str,
    ciphertext: &[u8],
) -> Result<Vec<u8>, StoreError> {
    if ciphertext.len() < 28 {
        return Err(StoreError("encrypted email body is truncated"));
    }
    let cipher = Aes256Gcm::new_from_slice(key)
        .map_err(|_| StoreError("decrypt original document failed"))?;
    let nonce = Nonce::try_from(&ciphertext[..12])
        .map_err(|_| StoreError("decrypt original document failed"))?;
    cipher
        .decrypt(
            &nonce,
            aes_gcm::aead::Payload {
                msg: &ciphertext[12..],
                aad: object_key.as_bytes(),
            },
        )
        .map_err(|_| StoreError("decrypt original document failed"))
}

fn verify_object_hash(object_key: &str, data: &[u8]) -> Result<(), StoreError> {
    let digest = object_key.split('/').next().unwrap_or_default();
    if digest.len() == 64 && hex::encode(Sha256::digest(data)) != digest {
        return Err(StoreError("original document hash mismatch"));
    }
    Ok(())
}

fn required_env(name: &str) -> Result<String, Box<dyn Error + Send + Sync>> {
    let value = std::env::var(name)?;
    if value.trim().is_empty() {
        return Err(StoreError("required worker configuration is empty").into());
    }
    Ok(value)
}
