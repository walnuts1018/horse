use chrono::{DateTime, Duration, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, dsl, prelude::*};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_object_storage::ObjectStore;
use horse_postgres_common::{PgPool, schema::public};
use serde_json::Value;
use std::{
    collections::{HashMap, HashSet},
    future::Future,
};
use uuid::Uuid;

const RETENTION_DAYS: i64 = 90;
const BATCH_SIZE: i64 = 100;

#[derive(Debug)]
struct DocumentKeys {
    id: Uuid,
    household_id: Uuid,
    object_key: String,
    analysis_object_key: Option<String>,
}

struct ConfirmationStatus {
    source_reference: String,
    status: String,
    changed_at: DateTime<Utc>,
    id: Uuid,
}

pub(crate) trait RetentionObjectStore: Sync {
    #[allow(clippy::manual_async_fn)]
    fn delete(
        &self,
        object_key: &str,
    ) -> impl Future<Output = Result<(), Box<dyn std::error::Error + Send + Sync>>> + Send;
}

impl RetentionObjectStore for ObjectStore {
    #[allow(clippy::manual_async_fn)]
    fn delete(
        &self,
        object_key: &str,
    ) -> impl Future<Output = Result<(), Box<dyn std::error::Error + Send + Sync>>> + Send {
        async move {
            ObjectStore::delete(self, object_key)
                .await
                .map_err(|error| Box::new(error) as Box<dyn std::error::Error + Send + Sync>)
        }
    }
}

pub(crate) async fn delete_expired_unconfirmed(
    pool: &PgPool,
    store: &impl RetentionObjectStore,
    now: DateTime<Utc>,
) -> Result<usize, Box<dyn std::error::Error + Send + Sync>> {
    use public::{
        bank_statement_imports, evidence_links, extraction_jobs, source_confirmations,
        source_documents,
    };
    use source_documents::dsl as documents;

    let cutoff = now - Duration::days(RETENTION_DAYS);
    let mut deleted = 0;
    let mut cursor = None;
    loop {
        let mut connection = pool.get().await?;
        let mut query = documents::source_documents
            .filter(documents::created_at.le(cutoff))
            .filter(dsl::not(dsl::exists(
                evidence_links::table
                    .filter(evidence_links::household_id.eq(documents::household_id))
                    .filter(evidence_links::source_document_id.eq(documents::id)),
            )))
            .filter(dsl::not(dsl::exists(
                bank_statement_imports::table
                    .filter(bank_statement_imports::household_id.eq(documents::household_id))
                    .filter(bank_statement_imports::source_document_id.eq(documents::id)),
            )))
            .filter(dsl::not(dsl::exists(
                extraction_jobs::table
                    .filter(extraction_jobs::household_id.eq(documents::household_id))
                    .filter(extraction_jobs::source_document_id.eq(documents::id))
                    .filter(extraction_jobs::status.eq("running"))
                    .filter(extraction_jobs::lease_expires_at.gt(Some(now))),
            )))
            .into_boxed();
        if let Some((created_at, id)) = cursor {
            query = query.filter(diesel::BoolExpressionMethods::or(
                documents::created_at.gt(created_at),
                diesel::BoolExpressionMethods::and(
                    documents::created_at.eq(created_at),
                    documents::id.gt(id),
                ),
            ));
        }
        let rows = query
            .order_by(documents::created_at.asc())
            .then_order_by(documents::id.asc())
            .select((documents::created_at, documents::id))
            .limit(BATCH_SIZE)
            .load::<(DateTime<Utc>, Uuid)>(&mut *connection)
            .await?;
        let candidate_ids = rows.iter().map(|(_, id)| *id).collect::<Vec<_>>();
        let confirmations = source_confirmations::table
            .filter(source_confirmations::source_document_id.eq_any(&candidate_ids))
            .select((
                source_confirmations::source_document_id,
                source_confirmations::source_reference,
                source_confirmations::status,
                source_confirmations::changed_at,
                source_confirmations::id,
            ))
            .load::<(Uuid, String, String, DateTime<Utc>, Uuid)>(&mut *connection)
            .await?;
        let mut confirmations_by_document = HashMap::<Uuid, Vec<ConfirmationStatus>>::new();
        for (document_id, source_reference, status, changed_at, id) in confirmations {
            confirmations_by_document
                .entry(document_id)
                .or_default()
                .push(ConfirmationStatus {
                    source_reference,
                    status,
                    changed_at,
                    id,
                });
        }
        drop(connection);
        let Some(last) = rows.last().copied() else {
            break;
        };
        cursor = Some(last);
        for (_, id) in rows {
            if has_current_confirmation_status(
                confirmations_by_document.remove(&id).unwrap_or_default(),
            ) {
                continue;
            }
            match delete_one(pool, store, id, now).await {
                Ok(true) => deleted += 1,
                Ok(false) => {}
                Err(error) => {
                    tracing::error!(document_id = %id, error = ?error, "Expired document deletion failed")
                }
            }
        }
    }
    Ok(deleted)
}

async fn delete_one(
    pool: &PgPool,
    store: &impl RetentionObjectStore,
    document_id: Uuid,
    now: DateTime<Utc>,
) -> Result<bool, Box<dyn std::error::Error + Send + Sync>> {
    use public::{bank_statement_imports, evidence_links, extraction_jobs, source_documents};

    let mut connection = pool.get().await?;
    let keys = (*connection)
        .transaction::<Option<DocumentKeys>, diesel::result::Error, _>(async |conn| {
            let Some((household_id, object_key, analysis_object_key, mut metadata)) =
                source_documents::table
                    .filter(source_documents::id.eq(document_id))
                    .for_update()
                    .select((
                        source_documents::household_id,
                        source_documents::object_key,
                        source_documents::analysis_object_key,
                        source_documents::metadata,
                    ))
                    .first::<(Uuid, String, Option<String>, Value)>(conn)
                    .await
                    .optional()?
            else {
                return Ok(None);
            };

            let latest_job = extraction_jobs::table
                .filter(extraction_jobs::household_id.eq(household_id))
                .filter(extraction_jobs::source_document_id.eq(document_id))
                .order_by(extraction_jobs::version.desc())
                .for_update()
                .select((extraction_jobs::status, extraction_jobs::lease_expires_at))
                .first::<(String, Option<DateTime<Utc>>)>(conn)
                .await
                .optional()?;
            if is_active_job(latest_job.as_ref(), now) {
                return Ok(None);
            }
            let has_evidence = dsl::select(dsl::exists(
                evidence_links::table
                    .filter(evidence_links::household_id.eq(household_id))
                    .filter(evidence_links::source_document_id.eq(document_id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            let has_confirmation =
                has_current_confirmation(conn, household_id, document_id).await?;
            let has_statement_import = dsl::select(dsl::exists(
                bank_statement_imports::table
                    .filter(bank_statement_imports::household_id.eq(household_id))
                    .filter(bank_statement_imports::source_document_id.eq(document_id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            if has_evidence || has_confirmation || has_statement_import {
                return Ok(None);
            }

            if !metadata.is_object() {
                metadata = Value::Object(Default::default());
            }
            metadata
                .as_object_mut()
                .expect("metadata was normalized to an object")
                .insert("deletionPending".to_owned(), Value::Bool(true));
            diesel::update(source_documents::table.filter(source_documents::id.eq(document_id)))
                .set(source_documents::metadata.eq(metadata))
                .execute(conn)
                .await?;
            Ok(Some(DocumentKeys {
                id: document_id,
                household_id,
                object_key,
                analysis_object_key,
            }))
        })
        .await?;
    let Some(keys) = keys else {
        return Ok(false);
    };

    (*connection)
        .transaction::<bool, Box<dyn std::error::Error + Send + Sync>, _>(async |conn| {
            let mut metadata = source_documents::table
                .filter(source_documents::id.eq(keys.id))
                .for_update()
                .select(source_documents::metadata)
                .first::<Value>(conn)
                .await?;
            if metadata.get("deletionPending") != Some(&Value::Bool(true)) {
                return Ok(false);
            }

            let latest_job = extraction_jobs::table
                .filter(extraction_jobs::household_id.eq(keys.household_id))
                .filter(extraction_jobs::source_document_id.eq(keys.id))
                .order_by(extraction_jobs::version.desc())
                .for_update()
                .select((extraction_jobs::status, extraction_jobs::lease_expires_at))
                .first::<(String, Option<DateTime<Utc>>)>(conn)
                .await
                .optional()?;
            if is_active_job(latest_job.as_ref(), now) {
                clear_deletion_pending(conn, keys.id, &mut metadata).await?;
                return Ok(false);
            }
            let has_evidence = dsl::select(dsl::exists(
                evidence_links::table
                    .filter(evidence_links::household_id.eq(keys.household_id))
                    .filter(evidence_links::source_document_id.eq(keys.id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            let has_confirmation =
                has_current_confirmation(conn, keys.household_id, keys.id).await?;
            let has_statement_import = dsl::select(dsl::exists(
                bank_statement_imports::table
                    .filter(bank_statement_imports::household_id.eq(keys.household_id))
                    .filter(bank_statement_imports::source_document_id.eq(keys.id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            if has_evidence || has_confirmation || has_statement_import {
                clear_deletion_pending(conn, keys.id, &mut metadata).await?;
                return Ok(false);
            }

            for object_key in unique_keys(&keys) {
                let referenced = source_documents::table
                    .filter(source_documents::id.ne(keys.id))
                    .filter(
                        source_documents::object_key
                            .eq(object_key.clone())
                            .or(source_documents::analysis_object_key.eq(Some(object_key.clone()))),
                    )
                    .select(source_documents::id)
                    .first::<Uuid>(conn)
                    .await
                    .optional()?
                    .is_some();
                if !referenced {
                    store.delete(&object_key).await?;
                }
            }
            diesel::delete(source_documents::table.filter(source_documents::id.eq(keys.id)))
                .execute(conn)
                .await?;
            Ok(true)
        })
        .await
}

fn unique_keys(document: &DocumentKeys) -> Vec<String> {
    let mut keys = vec![document.object_key.clone()];
    if let Some(key) = &document.analysis_object_key
        && !keys.contains(key)
    {
        keys.push(key.clone());
    }
    keys
}

async fn has_current_confirmation(
    conn: &mut horse_postgres_common::AsyncPgConnection,
    household_id: Uuid,
    document_id: Uuid,
) -> Result<bool, diesel::result::Error> {
    use public::source_confirmations;

    let confirmations = source_confirmations::table
        .filter(source_confirmations::household_id.eq(household_id))
        .filter(source_confirmations::source_document_id.eq(document_id))
        .select((
            source_confirmations::source_reference,
            source_confirmations::status,
            source_confirmations::changed_at,
            source_confirmations::id,
        ))
        .load::<(String, String, DateTime<Utc>, Uuid)>(conn)
        .await?
        .into_iter()
        .map(
            |(source_reference, status, changed_at, id)| ConfirmationStatus {
                source_reference,
                status,
                changed_at,
                id,
            },
        )
        .collect();
    Ok(has_current_confirmation_status(confirmations))
}

fn has_current_confirmation_status(mut confirmations: Vec<ConfirmationStatus>) -> bool {
    confirmations.sort_unstable_by(|left, right| {
        left.source_reference
            .cmp(&right.source_reference)
            .then_with(|| right.changed_at.cmp(&left.changed_at))
            .then_with(|| right.id.cmp(&left.id))
    });

    let mut seen_references = HashSet::new();
    confirmations.into_iter().any(|confirmation| {
        seen_references.insert(confirmation.source_reference) && confirmation.status == "confirmed"
    })
}

fn is_active_job(latest_job: Option<&(String, Option<DateTime<Utc>>)>, now: DateTime<Utc>) -> bool {
    latest_job.is_some_and(|(status, lease_expires_at)| {
        status == "running" && lease_expires_at.is_some_and(|expires_at| expires_at > now)
    })
}

async fn clear_deletion_pending(
    conn: &mut horse_postgres_common::AsyncPgConnection,
    document_id: Uuid,
    metadata: &mut Value,
) -> Result<(), diesel::result::Error> {
    use public::source_documents;

    if let Some(object) = metadata.as_object_mut() {
        object.remove("deletionPending");
    }
    diesel::update(source_documents::table.filter(source_documents::id.eq(document_id)))
        .set(source_documents::metadata.eq(metadata.clone()))
        .execute(conn)
        .await?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use std::{error::Error, sync::Mutex};

    use chrono::Duration;
    use diesel::{ExpressionMethods, QueryDsl, dsl::count_star, insert_into};
    use diesel_async::RunQueryDsl;
    use horse_postgres_common::{connect, schema::public};
    use horse_test_infra::{TestPostgres, apply_migrations};

    use super::{
        ConfirmationStatus, RetentionObjectStore, delete_expired_unconfirmed,
        has_current_confirmation_status,
    };

    #[test]
    fn current_confirmation_uses_latest_event_per_reference_and_breaks_timestamp_ties_by_id() {
        let changed_at = chrono::Utc::now();
        let revoked_id = uuid::Uuid::from_u128(2);
        let confirmed_id = uuid::Uuid::from_u128(1);
        let prior_confirmation = ConfirmationStatus {
            source_reference: "receipt".to_owned(),
            status: "confirmed".to_owned(),
            changed_at,
            id: confirmed_id,
        };
        let latest_revocation = ConfirmationStatus {
            source_reference: "receipt".to_owned(),
            status: "revoked".to_owned(),
            changed_at,
            id: revoked_id,
        };

        assert!(!has_current_confirmation_status(vec![
            prior_confirmation,
            latest_revocation,
        ]));
        assert!(has_current_confirmation_status(vec![
            ConfirmationStatus {
                source_reference: "receipt".to_owned(),
                status: "revoked".to_owned(),
                changed_at,
                id: revoked_id,
            },
            ConfirmationStatus {
                source_reference: "other-row".to_owned(),
                status: "confirmed".to_owned(),
                changed_at,
                id: confirmed_id,
            },
        ]));
    }

    #[derive(Default)]
    struct DeletedObjects(Mutex<Vec<String>>);

    impl RetentionObjectStore for DeletedObjects {
        #[allow(clippy::manual_async_fn)]
        fn delete(
            &self,
            object_key: &str,
        ) -> impl Future<Output = Result<(), Box<dyn Error + Send + Sync>>> + Send {
            let deleted = &self.0;
            let object_key = object_key.to_owned();
            async move {
                deleted.lock().unwrap().push(object_key);
                Ok(())
            }
        }
    }

    #[tokio::test]
    async fn deletes_expired_unconfirmed_documents_and_retains_confirmed_documents() {
        use public::{
            household_members, households, members, purchases, source_confirmations,
            source_documents,
        };

        let postgres = TestPostgres::start().await.unwrap();
        apply_migrations(postgres.connection_url()).unwrap();
        let pool = connect(postgres.connection_url()).await.unwrap();
        let now = chrono::Utc::now();
        let household_id = uuid::Uuid::now_v7();
        let member_id = uuid::Uuid::now_v7();
        let confirmed_purchase_id = uuid::Uuid::now_v7();
        let unconfirmed_document_id = uuid::Uuid::now_v7();
        let confirmed_document_id = uuid::Uuid::now_v7();
        let revoked_document_id = uuid::Uuid::now_v7();
        let old_created_at = now - Duration::days(91);

        {
            let mut conn = pool.get().await.unwrap();
            insert_into(households::table)
                .values((
                    households::id.eq(household_id),
                    households::name.eq("Retention test"),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            insert_into(members::table)
                .values((
                    members::id.eq(member_id),
                    members::oidc_subject.eq("retention-test"),
                    members::display_name.eq("Retention test"),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            insert_into(household_members::table)
                .values((
                    household_members::household_id.eq(household_id),
                    household_members::member_id.eq(member_id),
                    household_members::role.eq("OWNER"),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            for (id, sha256, object_key) in [
                (
                    unconfirmed_document_id,
                    "a".repeat(64),
                    "retention/unconfirmed".to_owned(),
                ),
                (
                    confirmed_document_id,
                    "b".repeat(64),
                    "retention/confirmed".to_owned(),
                ),
                (
                    revoked_document_id,
                    "c".repeat(64),
                    "retention/revoked".to_owned(),
                ),
            ] {
                insert_into(source_documents::table)
                    .values((
                        source_documents::id.eq(id),
                        source_documents::household_id.eq(household_id),
                        source_documents::sha256.eq(sha256),
                        source_documents::object_key.eq(object_key),
                        source_documents::mime_type.eq("image/jpeg"),
                        source_documents::source_kind.eq("receipt"),
                        source_documents::original_filename.eq("receipt.jpg"),
                        source_documents::created_by.eq(member_id),
                        source_documents::created_at.eq(old_created_at),
                    ))
                    .execute(&mut *conn)
                    .await
                    .unwrap();
            }
            insert_into(purchases::table)
                .values((
                    purchases::id.eq(confirmed_purchase_id),
                    purchases::household_id.eq(household_id),
                    purchases::occurred_at.eq(old_created_at),
                    purchases::created_by.eq(member_id),
                    purchases::allocation_rule_version.eq(0),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            insert_into(source_confirmations::table)
                .values((
                    source_confirmations::id.eq(uuid::Uuid::now_v7()),
                    source_confirmations::household_id.eq(household_id),
                    source_confirmations::source_document_id.eq(confirmed_document_id),
                    source_confirmations::source_reference.eq("receipt"),
                    source_confirmations::entity_kind.eq("purchase"),
                    source_confirmations::entity_id.eq(confirmed_purchase_id),
                    source_confirmations::status.eq("confirmed"),
                    source_confirmations::idempotency_key.eq("confirmed-retention-test"),
                    source_confirmations::changed_by.eq(member_id),
                    source_confirmations::changed_at.eq(now - Duration::seconds(2)),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            for (document_id, source_reference, status, idempotency_key, changed_at) in [
                (
                    confirmed_document_id,
                    "receipt",
                    "revoked",
                    "revoked-retention-test",
                    now - Duration::seconds(1),
                ),
                (
                    confirmed_document_id,
                    "other-row",
                    "confirmed",
                    "other-reference-retention-test",
                    now,
                ),
                (
                    revoked_document_id,
                    "receipt",
                    "confirmed",
                    "revoked-only-confirmed-retention-test",
                    now - Duration::seconds(2),
                ),
                (
                    revoked_document_id,
                    "receipt",
                    "revoked",
                    "revoked-only-revoked-retention-test",
                    now - Duration::seconds(1),
                ),
            ] {
                insert_into(source_confirmations::table)
                    .values((
                        source_confirmations::id.eq(uuid::Uuid::now_v7()),
                        source_confirmations::household_id.eq(household_id),
                        source_confirmations::source_document_id.eq(document_id),
                        source_confirmations::source_reference.eq(source_reference),
                        source_confirmations::entity_kind.eq("purchase"),
                        source_confirmations::entity_id.eq(confirmed_purchase_id),
                        source_confirmations::status.eq(status),
                        source_confirmations::idempotency_key.eq(idempotency_key),
                        source_confirmations::changed_by.eq(member_id),
                        source_confirmations::changed_at.eq(changed_at),
                    ))
                    .execute(&mut *conn)
                    .await
                    .unwrap();
            }
        }

        let deleted_objects = DeletedObjects::default();
        let deleted = delete_expired_unconfirmed(&pool, &deleted_objects, now)
            .await
            .unwrap();
        assert_eq!(deleted, 2);
        let mut deleted_keys = deleted_objects.0.lock().unwrap().clone();
        deleted_keys.sort();
        assert_eq!(
            deleted_keys,
            vec![
                "retention/revoked".to_owned(),
                "retention/unconfirmed".to_owned(),
            ]
        );

        let mut conn = pool.get().await.unwrap();
        let remaining_ids = source_documents::table
            .filter(source_documents::household_id.eq(household_id))
            .select(source_documents::id)
            .load::<uuid::Uuid>(&mut *conn)
            .await
            .unwrap();
        assert_eq!(remaining_ids, vec![confirmed_document_id]);
        let retained_confirmation = source_confirmations::table
            .filter(source_confirmations::source_document_id.eq(confirmed_document_id))
            .select(count_star())
            .first::<i64>(&mut *conn)
            .await
            .unwrap();
        assert_eq!(retained_confirmation, 3);
    }

    #[tokio::test]
    async fn expires_email_attachment_documents_and_cascades_attachment_rows() {
        use public::{
            household_members, households, members, purchases, source_confirmations,
            source_document_attachments, source_documents,
        };

        let postgres = TestPostgres::start().await.unwrap();
        apply_migrations(postgres.connection_url()).unwrap();
        let pool = connect(postgres.connection_url()).await.unwrap();
        let now = chrono::Utc::now();
        let old_created_at = now - Duration::days(91);
        let household_id = uuid::Uuid::now_v7();
        let member_id = uuid::Uuid::now_v7();
        let purchase_id = uuid::Uuid::now_v7();
        let expired_parent_id = uuid::Uuid::now_v7();
        let confirmed_child_id = uuid::Uuid::now_v7();
        let expired_parent_with_child_id = uuid::Uuid::now_v7();
        let expired_child_id = uuid::Uuid::now_v7();

        {
            let mut conn = pool.get().await.unwrap();
            insert_into(households::table)
                .values((
                    households::id.eq(household_id),
                    households::name.eq("Retention attachment test"),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            insert_into(members::table)
                .values((
                    members::id.eq(member_id),
                    members::oidc_subject.eq("retention-attachment-test"),
                    members::display_name.eq("Retention attachment test"),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            insert_into(household_members::table)
                .values((
                    household_members::household_id.eq(household_id),
                    household_members::member_id.eq(member_id),
                    household_members::role.eq("OWNER"),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            for (id, index, source_kind, mime_type, object_key) in [
                (
                    expired_parent_id,
                    0,
                    "email",
                    "message/rfc822",
                    "retention/expired-email".to_owned(),
                ),
                (
                    confirmed_child_id,
                    1,
                    "receipt",
                    "image/jpeg",
                    "retention/confirmed-child".to_owned(),
                ),
                (
                    expired_parent_with_child_id,
                    2,
                    "email",
                    "message/rfc822",
                    "retention/expired-email-with-child".to_owned(),
                ),
                (
                    expired_child_id,
                    3,
                    "receipt",
                    "image/jpeg",
                    "retention/expired-child".to_owned(),
                ),
            ] {
                insert_into(source_documents::table)
                    .values((
                        source_documents::id.eq(id),
                        source_documents::household_id.eq(household_id),
                        source_documents::sha256.eq(format!("{index:064x}")),
                        source_documents::object_key.eq(object_key),
                        source_documents::mime_type.eq(mime_type),
                        source_documents::source_kind.eq(source_kind),
                        source_documents::original_filename.eq("attachment"),
                        source_documents::created_by.eq(member_id),
                        source_documents::created_at.eq(old_created_at),
                    ))
                    .execute(&mut *conn)
                    .await
                    .unwrap();
            }
            insert_into(purchases::table)
                .values((
                    purchases::id.eq(purchase_id),
                    purchases::household_id.eq(household_id),
                    purchases::occurred_at.eq(old_created_at),
                    purchases::created_by.eq(member_id),
                    purchases::allocation_rule_version.eq(0),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            insert_into(source_confirmations::table)
                .values((
                    source_confirmations::id.eq(uuid::Uuid::now_v7()),
                    source_confirmations::household_id.eq(household_id),
                    source_confirmations::source_document_id.eq(confirmed_child_id),
                    source_confirmations::source_reference.eq("receipt"),
                    source_confirmations::entity_kind.eq("purchase"),
                    source_confirmations::entity_id.eq(purchase_id),
                    source_confirmations::status.eq("confirmed"),
                    source_confirmations::idempotency_key.eq("confirmed-attachment-test"),
                    source_confirmations::changed_by.eq(member_id),
                    source_confirmations::changed_at.eq(now),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            for (parent_id, child_id, index, filename, mime_type) in [
                (
                    expired_parent_id,
                    confirmed_child_id,
                    0,
                    "confirmed.jpg",
                    "image/jpeg",
                ),
                (
                    expired_parent_with_child_id,
                    expired_child_id,
                    0,
                    "expired.jpg",
                    "image/jpeg",
                ),
            ] {
                insert_into(source_document_attachments::table)
                    .values((
                        source_document_attachments::household_id.eq(household_id),
                        source_document_attachments::parent_document_id.eq(parent_id),
                        source_document_attachments::child_document_id.eq(child_id),
                        source_document_attachments::attachment_index.eq(index),
                        source_document_attachments::filename.eq(filename),
                        source_document_attachments::mime_type.eq(mime_type),
                    ))
                    .execute(&mut *conn)
                    .await
                    .unwrap();
            }
        }

        let deleted_objects = DeletedObjects::default();
        let deleted = delete_expired_unconfirmed(&pool, &deleted_objects, now)
            .await
            .unwrap();
        assert_eq!(deleted, 3);
        let mut deleted_keys = deleted_objects.0.lock().unwrap().clone();
        deleted_keys.sort();
        assert_eq!(
            deleted_keys,
            vec![
                "retention/expired-child".to_owned(),
                "retention/expired-email".to_owned(),
                "retention/expired-email-with-child".to_owned(),
            ]
        );

        let mut conn = pool.get().await.unwrap();
        let remaining_ids = source_documents::table
            .filter(source_documents::household_id.eq(household_id))
            .select(source_documents::id)
            .load::<uuid::Uuid>(&mut *conn)
            .await
            .unwrap();
        assert_eq!(remaining_ids, vec![confirmed_child_id]);
        let remaining_attachments = source_document_attachments::table
            .filter(source_document_attachments::household_id.eq(household_id))
            .select(count_star())
            .first::<i64>(&mut *conn)
            .await
            .unwrap();
        assert_eq!(remaining_attachments, 0);
    }
}
