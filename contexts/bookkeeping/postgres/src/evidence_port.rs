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

#[derive(Debug, Clone, Copy)]
pub struct SourceDocumentQuery {
    pub household_id: Uuid,
    pub document_id: Uuid,
    pub lock: bool,
}

#[derive(Debug, Clone, Copy)]
pub struct ExtractionQuery {
    pub household_id: Uuid,
    pub document_id: Uuid,
}

#[derive(Debug, Clone, Copy)]
pub struct SourceReferenceQuery<'a> {
    pub household_id: Uuid,
    pub document_id: Uuid,
    pub source_reference: &'a str,
}

#[derive(Debug, Clone, Copy)]
pub struct EvidenceLinkQuery<'a> {
    pub household_id: Uuid,
    pub entity_kind: &'a str,
    pub entity_id: Uuid,
    pub lock: bool,
}

#[derive(Debug, Clone)]
pub struct InsertConfirmation {
    pub household_id: Uuid,
    pub document_id: Uuid,
    pub source_reference: String,
    pub entity_kind: String,
    pub entity_id: Uuid,
    pub changed_by: Uuid,
    pub idempotency_key: String,
    pub changed_at: Option<DateTime<Utc>>,
}

#[async_trait::async_trait]
pub trait EvidenceOperations: Sync {
    async fn source_kind(
        &self,
        conn: &mut AsyncPgConnection,
        query: SourceDocumentQuery,
    ) -> Result<Option<String>, diesel::result::Error>;

    async fn latest_confirmation_status(
        &self,
        conn: &mut AsyncPgConnection,
        query: SourceReferenceQuery<'_>,
    ) -> Result<Option<String>, diesel::result::Error>;

    async fn has_confirmation_status(
        &self,
        conn: &mut AsyncPgConnection,
        query: SourceReferenceQuery<'_>,
        status: &str,
    ) -> Result<bool, diesel::result::Error>;

    async fn latest_confirmation(
        &self,
        conn: &mut AsyncPgConnection,
        query: SourceReferenceQuery<'_>,
    ) -> Result<Option<SourceConfirmation>, diesel::result::Error>;

    async fn latest_extraction(
        &self,
        conn: &mut AsyncPgConnection,
        query: ExtractionQuery,
    ) -> Result<Option<serde_json::Value>, diesel::result::Error>;

    async fn latest_extraction_with_id(
        &self,
        conn: &mut AsyncPgConnection,
        query: ExtractionQuery,
    ) -> Result<Option<(Uuid, serde_json::Value)>, diesel::result::Error>;

    async fn source_document_metadata(
        &self,
        conn: &mut AsyncPgConnection,
        query: SourceDocumentQuery,
    ) -> Result<Option<SourceDocumentMetadata>, diesel::result::Error>;

    async fn evidence_link(
        &self,
        conn: &mut AsyncPgConnection,
        query: EvidenceLinkQuery<'_>,
    ) -> Result<Option<EvidenceLinkRecord>, diesel::result::Error>;

    async fn insert_confirmation(
        &self,
        conn: &mut AsyncPgConnection,
        confirmation: InsertConfirmation,
    ) -> Result<(), diesel::result::Error>;
}
