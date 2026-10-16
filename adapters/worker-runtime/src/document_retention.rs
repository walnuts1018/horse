use chrono::{DateTime, Duration, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, dsl, prelude::*};
use diesel_async::{AsyncConnection, RunQueryDsl};
use horse_object_storage::ObjectStore;
use horse_postgres_common::{PgPool, schema::public};
use serde_json::Value;
use std::future::Future;
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
        source_document_attachments, source_documents,
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
                source_confirmations::table
                    .filter(source_confirmations::household_id.eq(documents::household_id))
                    .filter(source_confirmations::source_document_id.eq(documents::id)),
            )))
            .filter(dsl::not(dsl::exists(
                source_document_attachments::table.filter(
                    source_document_attachments::parent_document_id
                        .eq(documents::id)
                        .or(source_document_attachments::child_document_id.eq(documents::id)),
                ),
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
        drop(connection);
        let Some(last) = rows.last().copied() else {
            break;
        };
        cursor = Some(last);
        for (_, id) in rows {
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
    use public::{
        bank_statement_imports, evidence_links, extraction_jobs, source_confirmations,
        source_document_attachments, source_documents,
    };

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
            let has_confirmation = dsl::select(dsl::exists(
                source_confirmations::table
                    .filter(source_confirmations::household_id.eq(household_id))
                    .filter(source_confirmations::source_document_id.eq(document_id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            let has_attachment = dsl::select(dsl::exists(
                source_document_attachments::table.filter(
                    source_document_attachments::parent_document_id
                        .eq(document_id)
                        .or(source_document_attachments::child_document_id.eq(document_id)),
                ),
            ))
            .get_result::<bool>(conn)
            .await?;
            let has_statement_import = dsl::select(dsl::exists(
                bank_statement_imports::table
                    .filter(bank_statement_imports::household_id.eq(household_id))
                    .filter(bank_statement_imports::source_document_id.eq(document_id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            if has_evidence || has_confirmation || has_attachment || has_statement_import {
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
            let has_confirmation = dsl::select(dsl::exists(
                source_confirmations::table
                    .filter(source_confirmations::household_id.eq(keys.household_id))
                    .filter(source_confirmations::source_document_id.eq(keys.id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            let has_attachment = dsl::select(dsl::exists(
                source_document_attachments::table.filter(
                    source_document_attachments::parent_document_id
                        .eq(keys.id)
                        .or(source_document_attachments::child_document_id.eq(keys.id)),
                ),
            ))
            .get_result::<bool>(conn)
            .await?;
            let has_statement_import = dsl::select(dsl::exists(
                bank_statement_imports::table
                    .filter(bank_statement_imports::household_id.eq(keys.household_id))
                    .filter(bank_statement_imports::source_document_id.eq(keys.id)),
            ))
            .get_result::<bool>(conn)
            .await?;
            if has_evidence || has_confirmation || has_attachment || has_statement_import {
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

    use super::{RetentionObjectStore, delete_expired_unconfirmed};

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
                    source_confirmations::changed_at.eq(now),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
        }

        let deleted_objects = DeletedObjects::default();
        let deleted = delete_expired_unconfirmed(&pool, &deleted_objects, now)
            .await
            .unwrap();
        assert_eq!(deleted, 1);
        assert_eq!(
            *deleted_objects.0.lock().unwrap(),
            vec!["retention/unconfirmed".to_owned()]
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
        assert_eq!(retained_confirmation, 1);
    }
}
