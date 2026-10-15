//! Gemini Developer APIのInteractions APIに接続するHTTP境界。
//!
//! 接続先とAPIキーの送信方法は固定し、Vertex AIや任意のHTTP接続先を受け付けない。
//! 入力DTOと応答DTOはドメイン型から分離し、機密内容をDebug出力しない。

use std::{fmt, pin::Pin, time::Duration};

use base64::{Engine as _, engine::general_purpose::STANDARD};
use eventsource_stream::{EventStreamError, Eventsource};
use futures_util::{Stream, StreamExt};
use reqwest::{Client as HttpClient, StatusCode, header, header::HeaderValue};
use serde::{Deserialize, Serialize, Serializer};

const API_ROOT: &str = "https://generativelanguage.googleapis.com/v1beta";

/// Gemini Developer APIクライアント。
pub struct GeminiDeveloperApi {
    api_key: ApiKey,
    http: HttpClient,
}

impl fmt::Debug for GeminiDeveloperApi {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("GeminiDeveloperApi")
            .field("api_key", &"[REDACTED]")
            .finish_non_exhaustive()
    }
}

struct ApiKey(HeaderValue);

impl fmt::Debug for ApiKey {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("[REDACTED]")
    }
}

impl GeminiDeveloperApi {
    /// APIキーを保持するクライアントを作成する。
    pub fn new(api_key: impl Into<String>) -> Result<Self, Error> {
        let api_key = api_key.into();
        if api_key.trim().is_empty() {
            return Err(Error::InvalidConfiguration);
        }
        let mut api_key =
            HeaderValue::from_str(&api_key).map_err(|_| Error::InvalidConfiguration)?;
        api_key.set_sensitive(true);

        let http = HttpClient::builder()
            .https_only(true)
            .build()
            .map_err(|_| Error::InvalidConfiguration)?;

        Ok(Self {
            api_key: ApiKey(api_key),
            http,
        })
    }

    /// 会話入力を含むInteractionを作成する。モデルIDは呼び出し側が選択する。
    pub async fn create(
        &self,
        model: &str,
        input: InteractionsInput,
    ) -> Result<Interaction, Error> {
        let request = CreateInteractionRequest {
            model,
            input,
            store: false,
            stream: false,
            system_instruction: None,
            response_format: None,
            generation_config: None,
        };
        let response = self
            .http
            .post(format!("{API_ROOT}/interactions"))
            .header("x-goog-api-key", self.api_key.0.clone())
            .json(&request)
            .send()
            .await
            .map_err(classify_transport)?;

        decode_response(response).await
    }

    /// Interactionを作成し、SSEイベントを到着順に返す。
    pub async fn create_stream(
        &self,
        model: &str,
        input: InteractionsInput,
    ) -> Result<InteractionEventStream, Error> {
        let request = CreateInteractionRequest {
            model,
            input,
            store: false,
            stream: true,
            system_instruction: None,
            response_format: None,
            generation_config: None,
        };
        let response = self
            .http
            .post(format!("{API_ROOT}/interactions"))
            .header("x-goog-api-key", self.api_key.0.clone())
            .json(&request)
            .send()
            .await
            .map_err(classify_transport)?;

        event_stream(response).await
    }

    /// JSONスキーマを指定したストリーミングInteractionを作成する。
    pub async fn create_stream_with_schema(
        &self,
        model: &str,
        input: InteractionsInput,
        system_instruction: &str,
        response_schema: serde_json::Value,
        thinking_level: &str,
        max_output_tokens: u32,
    ) -> Result<InteractionEventStream, Error> {
        let request = CreateInteractionRequest {
            model,
            input,
            store: false,
            stream: true,
            system_instruction: Some(system_instruction),
            response_format: Some(serde_json::json!({
                "mime_type": "application/json",
                "schema": response_schema,
            })),
            generation_config: Some(serde_json::json!({
                "thinking_level": thinking_level.to_ascii_uppercase(),
                "max_output_tokens": max_output_tokens,
            })),
        };
        let response = self
            .http
            .post(format!("{API_ROOT}/interactions"))
            .header("x-goog-api-key", self.api_key.0.clone())
            .json(&request)
            .send()
            .await
            .map_err(classify_transport)?;

        event_stream(response).await
    }

    /// 既存InteractionのSSEを、指定したevent IDの次から再開する。
    pub async fn resume_stream(
        &self,
        interaction_id: &str,
        last_event_id: &str,
    ) -> Result<InteractionEventStream, Error> {
        if last_event_id.is_empty() {
            return Err(Error::InvalidConfiguration);
        }

        let response = self
            .http
            .get(format!(
                "{API_ROOT}/interactions/{}",
                encode_path_segment(interaction_id)
            ))
            .query(&[("stream", "true"), ("last_event_id", last_event_id)])
            .header("x-goog-api-key", self.api_key.0.clone())
            .send()
            .await
            .map_err(classify_transport)?;

        event_stream(response).await
    }

    /// IDを指定してInteractionを取得する。
    pub async fn get(&self, interaction_id: &str) -> Result<Interaction, Error> {
        let response = self
            .http
            .get(format!(
                "{API_ROOT}/interactions/{}",
                encode_path_segment(interaction_id)
            ))
            .header("x-goog-api-key", self.api_key.0.clone())
            .send()
            .await
            .map_err(classify_transport)?;

        decode_response(response).await
    }
}

#[derive(Clone, Serialize)]
struct CreateInteractionRequest<'a> {
    model: &'a str,
    input: InteractionsInput,
    store: bool,
    stream: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    system_instruction: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    response_format: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    generation_config: Option<serde_json::Value>,
}

/// Interactions APIが受け付けるテキスト、content block、複数turnの履歴。
///
/// `History`は`user_input`と`model_output`のstep列として送られる。
/// APIへの保存は作成時に常に無効化される。
#[derive(Serialize, Clone)]
#[serde(untagged)]
pub enum InteractionsInput {
    Text(String),
    Content(Vec<InputContent>),
    History(Vec<HistoryStep>),
}

/// Interactions APIの会話履歴step。
#[derive(Serialize, Clone)]
pub struct HistoryStep {
    #[serde(rename = "type")]
    role: HistoryRole,
    content: Vec<InputContent>,
}

impl HistoryStep {
    pub fn user(content: Vec<InputContent>) -> Self {
        Self {
            role: HistoryRole::UserInput,
            content,
        }
    }

    pub fn model(content: Vec<InputContent>) -> Self {
        Self {
            role: HistoryRole::ModelOutput,
            content,
        }
    }
}

#[derive(Serialize, Clone)]
enum HistoryRole {
    #[serde(rename = "user_input")]
    UserInput,
    #[serde(rename = "model_output")]
    ModelOutput,
}

/// Interactions APIへ送信するテキスト、画像、またはPDF。
///
/// メディアはインラインbase64として送信するため、任意URIや外部ファイル取得を許可しない。
#[derive(Clone)]
pub enum InputContent {
    Text(String),
    Image { bytes: Vec<u8>, mime_type: String },
    Document { bytes: Vec<u8>, mime_type: String },
}

impl InputContent {
    pub fn text(text: impl Into<String>) -> Self {
        Self::Text(text.into())
    }

    pub fn image(bytes: impl Into<Vec<u8>>, mime_type: impl Into<String>) -> Self {
        Self::Image {
            bytes: bytes.into(),
            mime_type: mime_type.into(),
        }
    }

    pub fn pdf(bytes: impl Into<Vec<u8>>) -> Self {
        Self::Document {
            bytes: bytes.into(),
            mime_type: "application/pdf".to_owned(),
        }
    }
}

impl Serialize for InputContent {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        use serde::ser::SerializeStruct;

        match self {
            Self::Text(text) => {
                let mut content = serializer.serialize_struct("TextContent", 2)?;
                content.serialize_field("type", "text")?;
                content.serialize_field("text", text)?;
                content.end()
            }
            Self::Image { bytes, mime_type } => {
                let mut content = serializer.serialize_struct("ImageContent", 3)?;
                content.serialize_field("type", "image")?;
                content.serialize_field("data", &Base64(bytes))?;
                content.serialize_field("mime_type", mime_type)?;
                content.end()
            }
            Self::Document { bytes, mime_type } => {
                let mut content = serializer.serialize_struct("DocumentContent", 3)?;
                content.serialize_field("type", "document")?;
                content.serialize_field("data", &Base64(bytes))?;
                content.serialize_field("mime_type", mime_type)?;
                content.end()
            }
        }
    }
}

struct Base64<'a>(&'a [u8]);

impl Serialize for Base64<'_> {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        serializer.serialize_str(&STANDARD.encode(self.0))
    }
}

/// Interactions APIから返る応答DTO。
///
/// `input`と`steps`はAPIの将来追加フィールドを保持する。機密内容の誤出力を避けるため
/// この型は`Debug`を実装しない。
#[derive(Deserialize)]
pub struct Interaction {
    pub id: String,
    pub model: Option<String>,
    pub status: Option<String>,
    pub input: Option<serde_json::Value>,
    #[serde(default)]
    pub steps: Vec<serde_json::Value>,
    pub usage: Option<serde_json::Value>,
}

/// Interaction SSEから得た一つのイベント。
///
/// payloadにはモデル出力や入力由来の内容が含まれるため`Debug`を実装しない。
pub struct InteractionEvent {
    pub event_type: String,
    pub event_id: Option<String>,
    pub payload: serde_json::Value,
}

/// InteractionのSSEイベントを逐次読むストリーム。
pub type InteractionEventStream =
    Pin<Box<dyn Stream<Item = Result<InteractionEvent, Error>> + Send>>;

/// 機密の入力値やAPIキーを含まないクライアントエラー。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Error {
    InvalidConfiguration,
    InvalidResponse,
    RateLimited { retry_after: Option<Duration> },
    UsageQuotaExceeded { retry_after: Option<Duration> },
    SpendCapExceeded,
    TemporaryFailure,
    RemoteApiFailure,
    Api { status: u16 },
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidConfiguration => f.write_str("Gemini API client configuration is invalid"),
            Self::InvalidResponse => f.write_str("Gemini API returned an invalid response"),
            Self::RateLimited { .. } => f.write_str("Gemini API rate limit was reached"),
            Self::UsageQuotaExceeded { .. } => f.write_str("Gemini API usage quota was exceeded"),
            Self::SpendCapExceeded => f.write_str("Gemini API project spend cap was reached"),
            Self::TemporaryFailure => f.write_str("Gemini API is temporarily unavailable"),
            Self::RemoteApiFailure => f.write_str("Gemini API reported a stream failure"),
            Self::Api { status } => write!(f, "Gemini API request failed with status {status}"),
        }
    }
}

impl std::error::Error for Error {}

async fn decode_response(response: reqwest::Response) -> Result<Interaction, Error> {
    let status = response.status();
    if !status.is_success() {
        return Err(classify_status(status, response).await);
    }
    response.json().await.map_err(|_| Error::InvalidResponse)
}

async fn event_stream(response: reqwest::Response) -> Result<InteractionEventStream, Error> {
    let status = response.status();
    if !status.is_success() {
        return Err(classify_status(status, response).await);
    }

    let content_type = response
        .headers()
        .get(header::CONTENT_TYPE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    if !content_type
        .to_ascii_lowercase()
        .starts_with("text/event-stream")
    {
        return Err(Error::InvalidResponse);
    }

    let stream = response
        .bytes_stream()
        .eventsource()
        .map(|event| match event {
            Ok(event) => parse_interaction_event(event),
            Err(EventStreamError::Transport(_)) => Err(Error::TemporaryFailure),
            Err(EventStreamError::Parser(_)) | Err(EventStreamError::Utf8(_)) => {
                Err(Error::InvalidResponse)
            }
        });

    Ok(Box::pin(stream))
}

fn parse_interaction_event(event: eventsource_stream::Event) -> Result<InteractionEvent, Error> {
    let payload: serde_json::Value =
        serde_json::from_str(&event.data).map_err(|_| Error::InvalidResponse)?;
    let event_type = payload
        .get("event_type")
        .and_then(serde_json::Value::as_str)
        .ok_or(Error::InvalidResponse)?
        .to_owned();
    if event_type == "error" {
        return Err(Error::RemoteApiFailure);
    }

    let event_id = payload
        .get("event_id")
        .and_then(serde_json::Value::as_str)
        .map(str::to_owned)
        .or_else(|| (!event.id.is_empty()).then_some(event.id));

    Ok(InteractionEvent {
        event_type,
        event_id,
        payload,
    })
}

async fn classify_status(status: StatusCode, response: reqwest::Response) -> Error {
    let retry_after = response
        .headers()
        .get(header::RETRY_AFTER)
        .and_then(|value| value.to_str().ok())
        .and_then(|value| value.parse::<u64>().ok())
        .map(Duration::from_secs);
    let body = response.bytes().await.unwrap_or_default();
    let provider_error = serde_json::from_slice::<serde_json::Value>(&body).ok();
    if provider_error.as_ref().is_some_and(is_spend_cap_error) {
        return Error::SpendCapExceeded;
    }

    if status == StatusCode::TOO_MANY_REQUESTS {
        let is_quota = provider_error.as_ref().is_some_and(has_quota_reason);
        return if is_quota {
            Error::UsageQuotaExceeded { retry_after }
        } else {
            Error::RateLimited { retry_after }
        };
    }

    if matches!(status.as_u16(), 500 | 502 | 503 | 504) {
        Error::TemporaryFailure
    } else {
        Error::Api {
            status: status.as_u16(),
        }
    }
}

fn is_spend_cap_error(value: &serde_json::Value) -> bool {
    let Some(error) = value.get("error").and_then(serde_json::Value::as_object) else {
        return false;
    };
    let mut classification = String::new();
    if let Some(status) = error.get("status").and_then(serde_json::Value::as_str) {
        classification.push_str(status);
        classification.push(' ');
    }
    if let Some(message) = error.get("message").and_then(serde_json::Value::as_str) {
        classification.push_str(message);
        classification.push(' ');
    }
    if let Some(details) = error.get("details").and_then(serde_json::Value::as_array) {
        for detail in details {
            if let Some(reason) = detail.get("reason").and_then(serde_json::Value::as_str) {
                classification.push_str(reason);
                classification.push(' ');
            }
        }
    }
    let normalized = classification.replace(['_', '-'], " ").to_ascii_lowercase();
    normalized.contains("spend cap")
        || normalized.contains("spending cap")
        || normalized.contains("spending limit")
        || normalized.contains("billing limit")
        || normalized.contains("budget exceeded")
}

fn has_quota_reason(value: &serde_json::Value) -> bool {
    let Some(object) = value.as_object() else {
        return false;
    };

    let message = object
        .get("error")
        .and_then(serde_json::Value::as_object)
        .and_then(|error| error.get("message"))
        .and_then(serde_json::Value::as_str)
        .unwrap_or_default();
    let reasons = object
        .get("error")
        .and_then(serde_json::Value::as_object)
        .and_then(|error| error.get("details"))
        .and_then(serde_json::Value::as_array)
        .into_iter()
        .flatten()
        .filter_map(|detail| detail.get("reason").and_then(serde_json::Value::as_str))
        .collect::<Vec<_>>();

    let reason_is_quota = reasons.iter().any(|reason| {
        let normalized = reason.to_ascii_uppercase();
        normalized.contains("QUOTA_EXCEEDED") || normalized.contains("DAILY_LIMIT_EXCEEDED")
    });
    let normalized_message = message.to_ascii_lowercase();
    let message_is_quota = (normalized_message.contains("quota exceeded")
        || normalized_message.contains("exceeded your current quota"))
        && !normalized_message.contains("per minute")
        && !normalized_message.contains("per second");

    reason_is_quota || message_is_quota
}

fn classify_transport(_error: reqwest::Error) -> Error {
    Error::TemporaryFailure
}

fn encode_path_segment(segment: &str) -> String {
    let mut encoded = String::with_capacity(segment.len());
    for byte in segment.bytes() {
        if byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.' | b'~') {
            encoded.push(byte as char);
        } else {
            use fmt::Write as _;
            let _ = write!(encoded, "%{byte:02X}");
        }
    }
    encoded
}
