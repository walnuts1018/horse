use super::load_document_header;
use chrono::Utc;
use diesel::{ExpressionMethods, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::schema::public::{
    household_members, households, members, source_documents,
};
use uuid::Uuid;

#[tokio::test]
async fn household_member_cannot_read_another_households_document_header() {
    let postgres = horse_test_infra::TestPostgres::start().await.unwrap();
    horse_test_infra::apply_migrations(postgres.connection_url()).unwrap();
    let pool = horse_postgres_common::connect(postgres.connection_url())
        .await
        .unwrap();
    let owner_id = Uuid::now_v7();
    let outsider_id = Uuid::now_v7();
    let household_id = Uuid::now_v7();
    let document_id = Uuid::now_v7();
    let now = Utc::now();
    {
        let mut conn = pool.get().await.unwrap();
        for (id, subject) in [
            (owner_id, "document-access-owner"),
            (outsider_id, "document-access-outsider"),
        ] {
            insert_into(members::table)
                .values((
                    members::id.eq(id),
                    members::oidc_subject.eq(subject),
                    members::display_name.eq(subject),
                ))
                .execute(&mut conn)
                .await
                .unwrap();
        }
        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Document access household"),
                households::created_at.eq(now),
                households::updated_at.eq(now),
                households::revision.eq(1_i64),
                households::default_allocation_version.eq(1_i64),
            ))
            .execute(&mut conn)
            .await
            .unwrap();
        insert_into(household_members::table)
            .values((
                household_members::household_id.eq(household_id),
                household_members::member_id.eq(owner_id),
                household_members::role.eq("OWNER"),
                household_members::joined_at.eq(now),
            ))
            .execute(&mut conn)
            .await
            .unwrap();
        insert_into(source_documents::table)
            .values((
                source_documents::id.eq(document_id),
                source_documents::household_id.eq(household_id),
                source_documents::sha256.eq("a".repeat(64)),
                source_documents::object_key.eq("household/originals/document"),
                source_documents::mime_type.eq("image/jpeg"),
                source_documents::source_kind.eq("receipt"),
                source_documents::original_filename.eq("receipt.jpg"),
                source_documents::metadata.eq(serde_json::json!({})),
                source_documents::created_by.eq(owner_id),
                source_documents::created_at.eq(now),
            ))
            .execute(&mut conn)
            .await
            .unwrap();

        let visible = load_document_header(&mut conn, owner_id, document_id)
            .await
            .unwrap();
        assert!(visible.is_some());
        let hidden = load_document_header(&mut conn, outsider_id, document_id)
            .await
            .unwrap();
        assert!(hidden.is_none());
    }
    postgres.terminate().await.unwrap();
}
