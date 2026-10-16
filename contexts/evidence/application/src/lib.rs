#![allow(async_fn_in_trait)]

pub mod ingestion_job;
pub mod observation_node_batch_query;

use horse_household_change::UiChangeDescriptor;
use serde_json::Value;
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq)]
pub struct EvidenceLink {
    pub created_at: chrono::DateTime<chrono::Utc>,
    pub household_id: Uuid,
    pub source_document_id: Uuid,
    pub entity_kind: &'static str,
    pub entity_id: Uuid,
    pub item_id: Option<Uuid>,
    pub evidence_region: Option<Value>,
    pub observation: Value,
    pub actor_id: Uuid,
    pub source_reference: String,
    pub ignore_duplicate: bool,
}

pub fn document_registered(job_id: Uuid) -> UiChangeDescriptor {
    UiChangeDescriptor::new("receipt.document.registered", "source_document", ["status"])
        .with_payload(serde_json::json!({"jobId": job_id.to_string()}))
}

pub fn extraction_started(job_id: Uuid) -> UiChangeDescriptor {
    UiChangeDescriptor::new("receipt.extraction.started", "source_document", ["status"])
        .with_payload(serde_json::json!({"jobId": job_id.to_string()}))
}

pub fn extraction_updated() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "receipt.extraction.updated",
        "source_document",
        ["status", "result"],
    )
}

pub fn mailbox_message_rejected() -> UiChangeDescriptor {
    UiChangeDescriptor::new(
        "receipt.mailbox_message.rejected",
        "mailbox_message",
        ["status"],
    )
    .with_payload(serde_json::json!({"status": "rejected", "reason": "invalid_email_size"}))
}

pub fn evidence_group_linked(source_count: usize) -> UiChangeDescriptor {
    UiChangeDescriptor::new("evidence.group_linked", "purchase", ["evidence"])
        .with_payload(serde_json::json!({"sourceCount": source_count}))
}

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
