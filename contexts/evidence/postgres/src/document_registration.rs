use chrono::Utc;
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::PgPool;
use evidence_application::{RegisterDocumentCommand, RegisteredDocument};

#[derive(Debug)]
pub enum RegisterDocumentError {
    Pool(String),
    Database(DieselError),
    InvalidInput(&'static str),
}

impl From<DieselError> for RegisterDocumentError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn register_document(
    pool: &PgPool,
    command: RegisterDocumentCommand,
) -> Result<RegisteredDocument, RegisterDocumentError> {
    validate(&command)?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| RegisterDocumentError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<RegisteredDocument, RegisterDocumentError, _>(async move |conn| {
            use horse_postgres_common::schema::public::{
                extraction_jobs, household_events, source_document_attachments, source_documents,
            };
            let now = Utc::now();
            let candidate_id = Uuid::now_v7();
            let model = if command.model == "free" {
                String::new()
            } else {
                command.model.clone()
            };
            let mut request = command.request.clone();
            if let Some(object) = request.as_object_mut() {
                object.entry("purpose").or_insert_with(|| command.purpose.clone().into());
                if !model.is_empty() {
                    object.entry("model").or_insert_with(|| model.clone().into());
                }
            }
            let inserted_id = diesel::insert_into(source_documents::table)
                .values((
                    source_documents::id.eq(candidate_id),
                    source_documents::household_id.eq(command.household_id),
                    source_documents::sha256.eq(&command.sha256),
                    source_documents::object_key.eq(&command.object_key),
                    source_documents::mime_type.eq(&command.mime_type),
                    source_documents::source_kind.eq(&command.source_kind),
                    source_documents::original_filename.eq(&command.original_filename),
                    source_documents::metadata.eq(&command.metadata),
                    source_documents::created_by.eq(command.created_by),
                    source_documents::created_at.eq(now),
                ))
                .on_conflict((source_documents::household_id, source_documents::sha256))
                .do_nothing()
                .returning(source_documents::id)
                .get_result::<Uuid>(conn)
                .await
                .optional()?;
            let duplicate = inserted_id.is_none();
            let document_id = if let Some(id) = inserted_id {
                id
            } else {
                source_documents::table
                    .filter(source_documents::household_id.eq(command.household_id))
                    .filter(source_documents::sha256.eq(&command.sha256))
                    .select(source_documents::id)
                    .first::<Uuid>(conn)
                    .await?
            };

            let existing_job = extraction_jobs::table
                .filter(extraction_jobs::household_id.eq(command.household_id))
                .filter(extraction_jobs::source_document_id.eq(document_id))
                .order_by(extraction_jobs::version.desc())
                .select(extraction_jobs::id)
                .first::<Uuid>(conn)
                .await
                .optional()?;
            let job_id = if let Some(id) = existing_job {
                Some(id)
            } else if duplicate {
                None
            } else {
                let id = Uuid::now_v7();
                diesel::insert_into(extraction_jobs::table)
                    .values((
                        extraction_jobs::id.eq(id),
                        extraction_jobs::household_id.eq(command.household_id),
                        extraction_jobs::source_document_id.eq(document_id),
                        extraction_jobs::version.eq(1),
                        extraction_jobs::status.eq("queued"),
                        extraction_jobs::provider.eq(&command.provider),
                        extraction_jobs::model.eq(&model),
                        extraction_jobs::idempotency_key.eq(&command.idempotency_key),
                        extraction_jobs::request.eq(&request),
                        extraction_jobs::attempt_count.eq(0),
                        extraction_jobs::failure_count.eq(0),
                        extraction_jobs::max_attempts.eq(5),
                        extraction_jobs::available_at.eq(now),
                        extraction_jobs::created_at.eq(now),
                        extraction_jobs::updated_at.eq(now),
                    ))
                    .execute(conn)
                    .await?;
                diesel::insert_into(household_events::table)
                    .values((
                        household_events::household_id.eq(command.household_id),
                        household_events::event_type.eq("receipt.document.registered"),
                        household_events::entity_kind.eq("source_document"),
                        household_events::entity_id.eq(document_id),
                        household_events::revision.eq(1_i64),
                        household_events::changed_fields
                            .eq(vec![Some("status".to_owned())]),
                        household_events::payload.eq(serde_json::json!({"jobId": id})),
                        household_events::actor_id.eq(command.created_by),
                        household_events::created_at.eq(now),
                    ))
                    .execute(conn)
                    .await?;
                Some(id)
            };
            let mut attachment_ids = Vec::with_capacity(command.attachments.len());
            let mut attachment_job_ids = Vec::new();
            for (index, attachment) in command.attachments.iter().enumerate() {
                let candidate_id = Uuid::now_v7();
                let inserted_id = diesel::insert_into(source_documents::table)
                    .values((
                        source_documents::id.eq(candidate_id),
                        source_documents::household_id.eq(command.household_id),
                        source_documents::sha256.eq(&attachment.sha256),
                        source_documents::object_key.eq(&attachment.object_key),
                        source_documents::mime_type.eq(&attachment.mime_type),
                        source_documents::source_kind.eq(&attachment.source_kind),
                        source_documents::original_filename.eq(&attachment.original_filename),
                        source_documents::metadata.eq(&attachment.metadata),
                        source_documents::created_by.eq(command.created_by),
                        source_documents::created_at.eq(now),
                    ))
                    .on_conflict((source_documents::household_id, source_documents::sha256))
                    .do_nothing()
                    .returning(source_documents::id)
                    .get_result::<Uuid>(conn)
                    .await
                    .optional()?;
                let new_attachment = inserted_id.is_some();
                let attachment_id = if let Some(id) = inserted_id {
                    id
                } else {
                    source_documents::table
                        .filter(source_documents::household_id.eq(command.household_id))
                        .filter(source_documents::sha256.eq(&attachment.sha256))
                        .select(source_documents::id)
                        .first::<Uuid>(conn)
                        .await?
                };
                diesel::insert_into(source_document_attachments::table)
                    .values((
                        source_document_attachments::household_id.eq(command.household_id),
                        source_document_attachments::parent_document_id.eq(document_id),
                        source_document_attachments::child_document_id.eq(attachment_id),
                        source_document_attachments::attachment_index.eq(index as i32),
                        source_document_attachments::filename.eq(&attachment.original_filename),
                        source_document_attachments::mime_type.eq(&attachment.mime_type),
                        source_document_attachments::created_at.eq(now),
                    ))
                    .on_conflict((
                        source_document_attachments::parent_document_id,
                        source_document_attachments::attachment_index,
                    ))
                    .do_nothing()
                    .execute(conn)
                    .await?;
                let existing_job = extraction_jobs::table
                    .filter(extraction_jobs::household_id.eq(command.household_id))
                    .filter(extraction_jobs::source_document_id.eq(attachment_id))
                    .order_by(extraction_jobs::version.desc())
                    .select(extraction_jobs::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?;
                if let Some(id) = existing_job {
                    attachment_job_ids.push(id);
                } else if new_attachment {
                    let id = Uuid::now_v7();
                    let model = if attachment.model == "free" {
                        String::new()
                    } else {
                        attachment.model.clone()
                    };
                    let mut request = attachment.request.clone();
                    if let Some(object) = request.as_object_mut() {
                        object.entry("purpose").or_insert_with(|| attachment.purpose.clone().into());
                        if !model.is_empty() {
                            object.entry("model").or_insert_with(|| model.clone().into());
                        }
                    }
                    diesel::insert_into(extraction_jobs::table)
                        .values((
                            extraction_jobs::id.eq(id),
                            extraction_jobs::household_id.eq(command.household_id),
                            extraction_jobs::source_document_id.eq(attachment_id),
                            extraction_jobs::version.eq(1),
                            extraction_jobs::status.eq("queued"),
                            extraction_jobs::provider.eq(&attachment.provider),
                            extraction_jobs::model.eq(&model),
                            extraction_jobs::idempotency_key.eq(&attachment.idempotency_key),
                            extraction_jobs::request.eq(&request),
                            extraction_jobs::attempt_count.eq(0),
                            extraction_jobs::failure_count.eq(0),
                            extraction_jobs::max_attempts.eq(5),
                            extraction_jobs::available_at.eq(now),
                            extraction_jobs::created_at.eq(now),
                            extraction_jobs::updated_at.eq(now),
                        ))
                        .execute(conn)
                        .await?;
                    attachment_job_ids.push(id);
                }
                attachment_ids.push(attachment_id);
            }
            Ok(RegisteredDocument {
                document_id,
                job_id,
                duplicate,
                attachment_ids,
                attachment_job_ids,
            })
        })
        .await
}

pub async fn enqueue_extraction_job(
    pool: &PgPool,
    household_id: Uuid,
    document_id: Uuid,
    actor_id: Uuid,
    provider: &str,
    model: &str,
    idempotency_key: &str,
    request: serde_json::Value,
) -> Result<Uuid, RegisterDocumentError> {
    if provider.is_empty() || provider.len() > 128 || model.len() > 256 {
        return Err(RegisterDocumentError::InvalidInput("provider/model"));
    }
    if idempotency_key.is_empty() || idempotency_key.len() > 200 {
        return Err(RegisterDocumentError::InvalidInput("idempotency_key"));
    }
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| RegisterDocumentError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<Uuid, RegisterDocumentError, _>(async move |conn| {
            use horse_postgres_common::schema::public::{
                extraction_jobs, household_events, source_documents,
            };
            let now = Utc::now();
            let document_exists = source_documents::table
                .filter(source_documents::household_id.eq(household_id))
                .filter(source_documents::id.eq(document_id))
                .select(source_documents::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .is_some();
            if !document_exists {
                return Err(RegisterDocumentError::InvalidInput("document_id"));
            }
            let existing = extraction_jobs::table
                .filter(extraction_jobs::household_id.eq(household_id))
                .filter(extraction_jobs::idempotency_key.eq(idempotency_key))
                .select(extraction_jobs::id)
                .first::<Uuid>(conn)
                .await
                .optional()?;
            if let Some(id) = existing {
                return Ok(id);
            }
            let version = extraction_jobs::table
                .filter(extraction_jobs::household_id.eq(household_id))
                .filter(extraction_jobs::source_document_id.eq(document_id))
                .select(diesel::dsl::max(extraction_jobs::version))
                .first::<Option<i32>>(conn)
                .await?
                .unwrap_or_default()
                + 1;
            let id = Uuid::now_v7();
            diesel::insert_into(extraction_jobs::table)
                .values((
                    extraction_jobs::id.eq(id),
                    extraction_jobs::household_id.eq(household_id),
                    extraction_jobs::source_document_id.eq(document_id),
                    extraction_jobs::version.eq(version),
                    extraction_jobs::status.eq("queued"),
                    extraction_jobs::provider.eq(provider),
                    extraction_jobs::model.eq(model),
                    extraction_jobs::idempotency_key.eq(idempotency_key),
                    extraction_jobs::request.eq(&request),
                    extraction_jobs::attempt_count.eq(0),
                    extraction_jobs::failure_count.eq(0),
                    extraction_jobs::max_attempts.eq(5),
                    extraction_jobs::available_at.eq(now),
                    extraction_jobs::created_at.eq(now),
                    extraction_jobs::updated_at.eq(now),
                ))
                .execute(conn)
                .await?;
            diesel::insert_into(household_events::table)
                .values((
                    household_events::household_id.eq(household_id),
                    household_events::event_type.eq("receipt.extraction.started"),
                    household_events::entity_kind.eq("source_document"),
                    household_events::entity_id.eq(document_id),
                    household_events::revision.eq(i64::from(version)),
                    household_events::changed_fields.eq(vec![Some("status".to_owned())]),
                    household_events::payload.eq(serde_json::json!({"jobId": id})),
                    household_events::actor_id.eq(actor_id),
                    household_events::created_at.eq(now),
                ))
                .execute(conn)
                .await?;
            Ok(id)
        })
        .await
}

fn validate(command: &RegisterDocumentCommand) -> Result<(), RegisterDocumentError> {
    if command.sha256.len() != 64 || !command.sha256.bytes().all(|byte| byte.is_ascii_hexdigit()) {
        return Err(RegisterDocumentError::InvalidInput("sha256"));
    }
    if command.idempotency_key.is_empty() || command.idempotency_key.len() > 200 {
        return Err(RegisterDocumentError::InvalidInput("idempotency_key"));
    }
    if !matches!(command.purpose.as_str(), "interactive" | "background") {
        return Err(RegisterDocumentError::InvalidInput("purpose"));
    }
    if !matches!(command.source_kind.as_str(), "receipt" | "order" | "email" | "card" | "bank") {
        return Err(RegisterDocumentError::InvalidInput("source_kind"));
    }
    if command.provider.is_empty() || command.provider.len() > 128 || command.model.len() > 256 {
        return Err(RegisterDocumentError::InvalidInput("provider/model"));
    }
    if command.attachments.len() > 100 {
        return Err(RegisterDocumentError::InvalidInput("attachments"));
    }
    for attachment in &command.attachments {
        if attachment.sha256.len() != 64
            || !attachment.sha256.bytes().all(|byte| byte.is_ascii_hexdigit())
            || attachment.idempotency_key.is_empty()
            || attachment.idempotency_key.len() > 200
            || !matches!(attachment.source_kind.as_str(), "receipt" | "order" | "email" | "card" | "bank")
            || !matches!(attachment.purpose.as_str(), "interactive" | "background")
            || !matches!(attachment.mime_type.as_str(), "application/pdf" | "image/jpeg" | "image/png" | "image/webp")
        {
            return Err(RegisterDocumentError::InvalidInput("attachment"));
        }
    }
    Ok(())
}
