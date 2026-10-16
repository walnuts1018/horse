use async_trait::async_trait;
use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StatementRefundCorrection {
    pub correction_id: Uuid,
    pub amount_yen: i64,
    pub occurred_at: DateTime<Utc>,
    pub merchant: String,
    pub source_filename: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StatementRefundCorrectionConnection {
    pub edges: Vec<StatementRefundCorrection>,
    pub has_next_page: bool,
}

#[derive(Debug)]
pub enum StatementRefundCorrectionQueryError {
    ResourceUnavailable,
    InvalidInput,
    SourceDocumentNotFound,
    StatementNotFound,
    Infrastructure(String),
}

#[async_trait]
pub trait StatementRefundCorrectionQuery: Send + Sync {
    async fn load(
        &self,
        actor_id: Uuid,
        household_id: Uuid,
        source_document_id: Uuid,
        source_reference: String,
        first: Option<i32>,
        after: Option<Uuid>,
    ) -> Result<StatementRefundCorrectionConnection, StatementRefundCorrectionQueryError>;
}
