use chrono::{DateTime, Utc};
use diesel_async::AsyncPgConnection;
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SourceConfirmation {
    pub status: String,
    pub entity_kind: String,
    pub entity_id: Uuid,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SourceDocumentMetadata {
    pub source_kind: String,
    pub sha256: String,
}

#[derive(Debug, Clone, PartialEq)]
pub struct EvidenceLinkRecord {
    pub source_document_id: Uuid,
    pub source_reference: String,
    pub evidence_region: Option<serde_json::Value>,
    pub observation: serde_json::Value,
}

#[async_trait::async_trait]
pub trait EvidenceOperations: Sync {
    async fn source_kind(
        &self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        document_id: Uuid,
        lock: bool,
    ) -> Result<Option<String>, diesel::result::Error>;

    async fn latest_confirmation_status(
        &self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        document_id: Uuid,
        source_reference: &str,
    ) -> Result<Option<String>, diesel::result::Error>;

    async fn has_confirmation_status(
        &self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        document_id: Uuid,
        source_reference: &str,
        status: &str,
    ) -> Result<bool, diesel::result::Error>;

    async fn latest_confirmation(
        &self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        document_id: Uuid,
        source_reference: &str,
    ) -> Result<Option<SourceConfirmation>, diesel::result::Error>;

    async fn latest_extraction(
        &self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        document_id: Uuid,
    ) -> Result<Option<serde_json::Value>, diesel::result::Error>;

    async fn latest_extraction_with_id(
        &self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        document_id: Uuid,
    ) -> Result<Option<(Uuid, serde_json::Value)>, diesel::result::Error>;

    async fn source_document_metadata(
        &self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        document_id: Uuid,
        lock: bool,
    ) -> Result<Option<SourceDocumentMetadata>, diesel::result::Error>;

    async fn evidence_link(
        &self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        entity_kind: &str,
        entity_id: Uuid,
        lock: bool,
    ) -> Result<Option<EvidenceLinkRecord>, diesel::result::Error>;

    async fn insert_confirmation(
        &self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        document_id: Uuid,
        source_reference: &str,
        entity_kind: &str,
        entity_id: Uuid,
        changed_by: Uuid,
        idempotency_key: String,
        changed_at: Option<DateTime<Utc>>,
    ) -> Result<(), diesel::result::Error>;
}
