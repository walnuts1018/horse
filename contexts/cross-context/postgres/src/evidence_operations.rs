use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, dsl::now};
use diesel_async::{AsyncPgConnection, RunQueryDsl};
use uuid::Uuid;

use bookkeeping_postgres::evidence_port::{
    EvidenceLinkQuery, EvidenceLinkRecord, EvidenceOperations, ExtractionQuery, InsertConfirmation,
    SourceConfirmation, SourceDocumentMetadata, SourceDocumentQuery, SourceReferenceQuery,
};

pub struct EvidencePostgresOperations;

#[async_trait::async_trait]
impl EvidenceOperations for EvidencePostgresOperations {
    async fn source_kind(
        &self,
        conn: &mut AsyncPgConnection,
        query: SourceDocumentQuery,
    ) -> Result<Option<String>, diesel::result::Error> {
        if query.lock {
            crate::schema::public::source_documents::table
                .filter(
                    crate::schema::public::source_documents::household_id.eq(query.household_id),
                )
                .filter(crate::schema::public::source_documents::id.eq(query.document_id))
                .for_update()
                .select(crate::schema::public::source_documents::source_kind)
                .first::<String>(conn)
                .await
                .optional()
        } else {
            crate::schema::public::source_documents::table
                .filter(
                    crate::schema::public::source_documents::household_id.eq(query.household_id),
                )
                .filter(crate::schema::public::source_documents::id.eq(query.document_id))
                .select(crate::schema::public::source_documents::source_kind)
                .first::<String>(conn)
                .await
                .optional()
        }
    }

    async fn latest_confirmation_status(
        &self,
        conn: &mut AsyncPgConnection,
        query: SourceReferenceQuery<'_>,
    ) -> Result<Option<String>, diesel::result::Error> {
        crate::schema::public::source_confirmations::table
            .filter(
                crate::schema::public::source_confirmations::household_id.eq(query.household_id),
            )
            .filter(
                crate::schema::public::source_confirmations::source_document_id
                    .eq(query.document_id),
            )
            .filter(
                crate::schema::public::source_confirmations::source_reference
                    .eq(query.source_reference),
            )
            .order_by(crate::schema::public::source_confirmations::changed_at.desc())
            .select(crate::schema::public::source_confirmations::status)
            .first::<String>(conn)
            .await
            .optional()
    }

    async fn has_confirmation_status(
        &self,
        conn: &mut AsyncPgConnection,
        query: SourceReferenceQuery<'_>,
        status: &str,
    ) -> Result<bool, diesel::result::Error> {
        crate::schema::public::source_confirmations::table
            .filter(
                crate::schema::public::source_confirmations::household_id.eq(query.household_id),
            )
            .filter(
                crate::schema::public::source_confirmations::source_document_id
                    .eq(query.document_id),
            )
            .filter(
                crate::schema::public::source_confirmations::source_reference
                    .eq(query.source_reference),
            )
            .filter(crate::schema::public::source_confirmations::status.eq(status))
            .select(crate::schema::public::source_confirmations::id)
            .first::<Uuid>(conn)
            .await
            .optional()
            .map(|row| row.is_some())
    }

    async fn latest_confirmation(
        &self,
        conn: &mut AsyncPgConnection,
        query: SourceReferenceQuery<'_>,
    ) -> Result<Option<SourceConfirmation>, diesel::result::Error> {
        crate::schema::public::source_confirmations::table
            .filter(
                crate::schema::public::source_confirmations::household_id.eq(query.household_id),
            )
            .filter(
                crate::schema::public::source_confirmations::source_document_id
                    .eq(query.document_id),
            )
            .filter(
                crate::schema::public::source_confirmations::source_reference
                    .eq(query.source_reference),
            )
            .order_by(crate::schema::public::source_confirmations::changed_at.desc())
            .select((
                crate::schema::public::source_confirmations::status,
                crate::schema::public::source_confirmations::entity_kind,
                crate::schema::public::source_confirmations::entity_id,
            ))
            .first::<(String, String, Uuid)>(conn)
            .await
            .optional()
            .map(|row| {
                row.map(|(status, entity_kind, entity_id)| SourceConfirmation {
                    status,
                    entity_kind,
                    entity_id,
                })
            })
    }

    async fn latest_extraction(
        &self,
        conn: &mut AsyncPgConnection,
        query: ExtractionQuery,
    ) -> Result<Option<serde_json::Value>, diesel::result::Error> {
        crate::schema::public::extraction_jobs::table
            .filter(crate::schema::public::extraction_jobs::household_id.eq(query.household_id))
            .filter(
                crate::schema::public::extraction_jobs::source_document_id.eq(query.document_id),
            )
            .filter(crate::schema::public::extraction_jobs::status.eq("succeeded"))
            .order_by(crate::schema::public::extraction_jobs::version.desc())
            .select(crate::schema::public::extraction_jobs::result)
            .first::<Option<serde_json::Value>>(conn)
            .await
            .optional()
            .map(Option::flatten)
    }

    async fn latest_extraction_with_id(
        &self,
        conn: &mut AsyncPgConnection,
        query: ExtractionQuery,
    ) -> Result<Option<(Uuid, serde_json::Value)>, diesel::result::Error> {
        crate::schema::public::extraction_jobs::table
            .filter(crate::schema::public::extraction_jobs::household_id.eq(query.household_id))
            .filter(
                crate::schema::public::extraction_jobs::source_document_id.eq(query.document_id),
            )
            .filter(crate::schema::public::extraction_jobs::status.eq("succeeded"))
            .order_by(crate::schema::public::extraction_jobs::version.desc())
            .select((
                crate::schema::public::extraction_jobs::id,
                crate::schema::public::extraction_jobs::result,
            ))
            .first::<(Uuid, Option<serde_json::Value>)>(conn)
            .await
            .optional()
            .map(|row| row.and_then(|(id, result)| result.map(|result| (id, result))))
    }

    async fn source_document_metadata(
        &self,
        conn: &mut AsyncPgConnection,
        query: SourceDocumentQuery,
    ) -> Result<Option<SourceDocumentMetadata>, diesel::result::Error> {
        let row = if query.lock {
            crate::schema::public::source_documents::table
                .filter(
                    crate::schema::public::source_documents::household_id.eq(query.household_id),
                )
                .filter(crate::schema::public::source_documents::id.eq(query.document_id))
                .for_update()
                .select((
                    crate::schema::public::source_documents::source_kind,
                    crate::schema::public::source_documents::sha256,
                ))
                .first::<(String, String)>(conn)
                .await
                .optional()?
        } else {
            crate::schema::public::source_documents::table
                .filter(
                    crate::schema::public::source_documents::household_id.eq(query.household_id),
                )
                .filter(crate::schema::public::source_documents::id.eq(query.document_id))
                .select((
                    crate::schema::public::source_documents::source_kind,
                    crate::schema::public::source_documents::sha256,
                ))
                .first::<(String, String)>(conn)
                .await
                .optional()?
        };
        Ok(row.map(|(source_kind, sha256)| SourceDocumentMetadata {
            source_kind,
            sha256,
        }))
    }

    async fn evidence_link(
        &self,
        conn: &mut AsyncPgConnection,
        query: EvidenceLinkQuery<'_>,
    ) -> Result<Option<EvidenceLinkRecord>, diesel::result::Error> {
        let row = if query.lock {
            crate::schema::public::evidence_links::table
                .filter(crate::schema::public::evidence_links::household_id.eq(query.household_id))
                .filter(crate::schema::public::evidence_links::entity_kind.eq(query.entity_kind))
                .filter(crate::schema::public::evidence_links::entity_id.eq(query.entity_id))
                .for_update()
                .select((
                    crate::schema::public::evidence_links::source_document_id,
                    crate::schema::public::evidence_links::source_reference,
                    crate::schema::public::evidence_links::evidence_region,
                    crate::schema::public::evidence_links::observation,
                ))
                .first::<(Uuid, String, Option<serde_json::Value>, serde_json::Value)>(conn)
                .await
                .optional()?
        } else {
            crate::schema::public::evidence_links::table
                .filter(crate::schema::public::evidence_links::household_id.eq(query.household_id))
                .filter(crate::schema::public::evidence_links::entity_kind.eq(query.entity_kind))
                .filter(crate::schema::public::evidence_links::entity_id.eq(query.entity_id))
                .select((
                    crate::schema::public::evidence_links::source_document_id,
                    crate::schema::public::evidence_links::source_reference,
                    crate::schema::public::evidence_links::evidence_region,
                    crate::schema::public::evidence_links::observation,
                ))
                .first::<(Uuid, String, Option<serde_json::Value>, serde_json::Value)>(conn)
                .await
                .optional()?
        };
        Ok(row.map(
            |(source_document_id, source_reference, evidence_region, observation)| {
                EvidenceLinkRecord {
                    source_document_id,
                    source_reference,
                    evidence_region,
                    observation,
                }
            },
        ))
    }

    async fn insert_confirmation(
        &self,
        conn: &mut AsyncPgConnection,
        confirmation: InsertConfirmation,
    ) -> Result<(), diesel::result::Error> {
        let values = (
            crate::schema::public::source_confirmations::id.eq(Uuid::now_v7()),
            crate::schema::public::source_confirmations::household_id.eq(confirmation.household_id),
            crate::schema::public::source_confirmations::source_document_id
                .eq(confirmation.document_id),
            crate::schema::public::source_confirmations::observation_id.eq(None::<Uuid>),
            crate::schema::public::source_confirmations::source_reference
                .eq(confirmation.source_reference),
            crate::schema::public::source_confirmations::entity_kind.eq(confirmation.entity_kind),
            crate::schema::public::source_confirmations::entity_id.eq(confirmation.entity_id),
            crate::schema::public::source_confirmations::status.eq("confirmed"),
            crate::schema::public::source_confirmations::idempotency_key
                .eq(confirmation.idempotency_key),
            crate::schema::public::source_confirmations::changed_by.eq(confirmation.changed_by),
        );
        if let Some(changed_at) = confirmation.changed_at {
            diesel::insert_into(crate::schema::public::source_confirmations::table)
                .values((
                    values,
                    crate::schema::public::source_confirmations::changed_at.eq(changed_at),
                ))
                .execute(conn)
                .await?;
        } else {
            diesel::insert_into(crate::schema::public::source_confirmations::table)
                .values((
                    values,
                    crate::schema::public::source_confirmations::changed_at.eq(now),
                ))
                .execute(conn)
                .await?;
        }
        Ok(())
    }
}
