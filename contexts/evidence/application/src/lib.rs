#![allow(async_fn_in_trait)]

pub mod ingestion_job;

use serde_json::Value;
use uuid::Uuid;

#[derive(Debug, Clone)]
pub struct RegisterDocumentCommand {
    pub household_id: Uuid,
    pub created_by: Uuid,
    pub source_kind: String,
    pub original_filename: String,
    pub mime_type: String,
    pub sha256: String,
    pub object_key: String,
    pub analysis_object_key: Option<String>,
    pub analysis_mime_type: Option<String>,
    pub analysis_transform: Option<Value>,
    pub metadata: Value,
    pub provider: String,
    pub model: String,
    pub purpose: String,
    pub idempotency_key: String,
    pub request: Value,
    pub attachments: Vec<RegisterDocumentAttachment>,
}

#[derive(Debug, Clone)]
pub struct RegisterDocumentAttachment {
    pub source_kind: String,
    pub original_filename: String,
    pub mime_type: String,
    pub sha256: String,
    pub object_key: String,
    pub metadata: Value,
    pub provider: String,
    pub model: String,
    pub purpose: String,
    pub idempotency_key: String,
    pub request: Value,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RegisteredDocument {
    pub document_id: Uuid,
    pub job_id: Option<Uuid>,
    pub duplicate: bool,
    pub attachment_ids: Vec<Uuid>,
    pub attachment_job_ids: Vec<Uuid>,
}
