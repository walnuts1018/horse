use bookkeeping_postgres::{
    correct_purchase::{CorrectPurchaseRequest, correct_purchase},
    evidence_port::{
        EvidenceLinkQuery, EvidenceLinkRecord, EvidenceOperations, ExtractionQuery,
        InsertConfirmation, SourceConfirmation, SourceDocumentMetadata, SourceDocumentQuery,
        SourceReferenceQuery,
    },
    schema::public,
    settlement_preview::{
        FundingRuleRow, FundingScheduleRow, FundingShareRow, HouseholdFundingScheduleRow,
        PolicySettlementData,
    },
};
use chrono::{NaiveDate, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, insert_into};
use diesel_async::{AsyncPgConnection, RunQueryDsl};
use horse_postgres_common::connect;
use uuid::Uuid;

struct TestEvidence {
    fail_after_confirmation_insert: bool,
}

struct EmptyPolicy;

#[async_trait::async_trait]
impl PolicySettlementData for EmptyPolicy {
    async fn household_members_for_settlement(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
    ) -> Result<Vec<(Uuid, String, Option<chrono::DateTime<Utc>>)>, diesel::result::Error> {
        Ok(Vec::new())
    }
    async fn household_members_present_after(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
        _: chrono::DateTime<Utc>,
    ) -> Result<Vec<(Uuid, String)>, diesel::result::Error> {
        Ok(Vec::new())
    }
    async fn schedules_for_period(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
        _: NaiveDate,
        _: NaiveDate,
    ) -> Result<Vec<FundingScheduleRow>, diesel::result::Error> {
        Ok(Vec::new())
    }
    async fn schedules_overlapping(
        &mut self,
        _: &mut AsyncPgConnection,
        _: &[Uuid],
        _: NaiveDate,
        _: NaiveDate,
    ) -> Result<Vec<HouseholdFundingScheduleRow>, diesel::result::Error> {
        Ok(Vec::new())
    }
    async fn earliest_schedule_start(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
    ) -> Result<Option<NaiveDate>, diesel::result::Error> {
        Ok(None)
    }
    async fn rules_and_shares(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
    ) -> Result<(Vec<FundingRuleRow>, Vec<FundingShareRow>), diesel::result::Error> {
        Ok((Vec::new(), Vec::new()))
    }
    async fn historical_rules(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
        _: &[Uuid],
        _: &[i32],
    ) -> Result<Vec<FundingRuleRow>, diesel::result::Error> {
        Ok(Vec::new())
    }
    async fn rule_owner(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
        _: Uuid,
        _: i32,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        Ok(None)
    }
}

#[async_trait::async_trait]
impl EvidenceOperations for TestEvidence {
    async fn source_kind(
        &self,
        conn: &mut AsyncPgConnection,
        query: SourceDocumentQuery,
    ) -> Result<Option<String>, diesel::result::Error> {
        public::source_documents::table
            .filter(public::source_documents::household_id.eq(query.household_id))
            .filter(public::source_documents::id.eq(query.document_id))
            .select(public::source_documents::source_kind)
            .first::<String>(conn)
            .await
            .optional()
    }
    async fn latest_confirmation_status(
        &self,
        conn: &mut AsyncPgConnection,
        query: SourceReferenceQuery<'_>,
    ) -> Result<Option<String>, diesel::result::Error> {
        public::source_confirmations::table
            .filter(public::source_confirmations::household_id.eq(query.household_id))
            .filter(public::source_confirmations::source_document_id.eq(query.document_id))
            .filter(public::source_confirmations::source_reference.eq(query.source_reference))
            .order(public::source_confirmations::changed_at.desc())
            .select(public::source_confirmations::status)
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
        public::source_confirmations::table
            .filter(public::source_confirmations::household_id.eq(query.household_id))
            .filter(public::source_confirmations::source_document_id.eq(query.document_id))
            .filter(public::source_confirmations::source_reference.eq(query.source_reference))
            .filter(public::source_confirmations::status.eq(status))
            .select(public::source_confirmations::id)
            .first::<Uuid>(conn)
            .await
            .optional()
            .map(|id| id.is_some())
    }
    async fn latest_confirmation(
        &self,
        conn: &mut AsyncPgConnection,
        query: SourceReferenceQuery<'_>,
    ) -> Result<Option<SourceConfirmation>, diesel::result::Error> {
        public::source_confirmations::table
            .filter(public::source_confirmations::household_id.eq(query.household_id))
            .filter(public::source_confirmations::source_document_id.eq(query.document_id))
            .filter(public::source_confirmations::source_reference.eq(query.source_reference))
            .order(public::source_confirmations::changed_at.desc())
            .select((
                public::source_confirmations::status,
                public::source_confirmations::entity_kind,
                public::source_confirmations::entity_id,
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
        self.latest_extraction_with_id(conn, query)
            .await
            .map(|row| row.map(|(_, value)| value))
    }
    async fn latest_extraction_with_id(
        &self,
        conn: &mut AsyncPgConnection,
        query: ExtractionQuery,
    ) -> Result<Option<(Uuid, serde_json::Value)>, diesel::result::Error> {
        public::extraction_jobs::table
            .filter(public::extraction_jobs::household_id.eq(query.household_id))
            .filter(public::extraction_jobs::source_document_id.eq(query.document_id))
            .filter(public::extraction_jobs::status.eq("succeeded"))
            .order(public::extraction_jobs::version.desc())
            .select((public::extraction_jobs::id, public::extraction_jobs::result))
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
        public::source_documents::table
            .filter(public::source_documents::household_id.eq(query.household_id))
            .filter(public::source_documents::id.eq(query.document_id))
            .select((
                public::source_documents::source_kind,
                public::source_documents::sha256,
            ))
            .first::<(String, String)>(conn)
            .await
            .optional()
            .map(|row| {
                row.map(|(source_kind, sha256)| SourceDocumentMetadata {
                    source_kind,
                    sha256,
                })
            })
    }
    async fn evidence_link(
        &self,
        conn: &mut AsyncPgConnection,
        query: EvidenceLinkQuery<'_>,
    ) -> Result<Option<EvidenceLinkRecord>, diesel::result::Error> {
        public::evidence_links::table
            .filter(public::evidence_links::household_id.eq(query.household_id))
            .filter(public::evidence_links::entity_kind.eq(query.entity_kind))
            .filter(public::evidence_links::entity_id.eq(query.entity_id))
            .select((
                public::evidence_links::source_document_id,
                public::evidence_links::source_reference,
                public::evidence_links::evidence_region,
                public::evidence_links::observation,
            ))
            .first::<(Uuid, String, Option<serde_json::Value>, serde_json::Value)>(conn)
            .await
            .optional()
            .map(|row| {
                row.map(
                    |(source_document_id, source_reference, evidence_region, observation)| {
                        EvidenceLinkRecord {
                            source_document_id,
                            source_reference,
                            evidence_region,
                            observation,
                        }
                    },
                )
            })
    }
    async fn insert_confirmation(
        &self,
        conn: &mut AsyncPgConnection,
        confirmation: InsertConfirmation,
    ) -> Result<(), diesel::result::Error> {
        insert_into(public::source_confirmations::table)
            .values((
                public::source_confirmations::id.eq(Uuid::now_v7()),
                public::source_confirmations::household_id.eq(confirmation.household_id),
                public::source_confirmations::source_document_id.eq(confirmation.document_id),
                public::source_confirmations::observation_id.eq(None::<Uuid>),
                public::source_confirmations::source_reference.eq(confirmation.source_reference),
                public::source_confirmations::entity_kind.eq(confirmation.entity_kind),
                public::source_confirmations::entity_id.eq(confirmation.entity_id),
                public::source_confirmations::status.eq("confirmed"),
                public::source_confirmations::idempotency_key.eq(confirmation.idempotency_key),
                public::source_confirmations::changed_by.eq(confirmation.changed_by),
                public::source_confirmations::changed_at
                    .eq(confirmation.changed_at.unwrap_or_else(chrono::Utc::now)),
                public::source_confirmations::purchase_id.eq(None::<Uuid>),
                public::source_confirmations::correction_id.eq(Some(confirmation.entity_id)),
                public::source_confirmations::transfer_id.eq(None::<Uuid>),
                public::source_confirmations::account_income_id.eq(None::<Uuid>),
                public::source_confirmations::account_movement_id.eq(None::<Uuid>),
            ))
            .execute(conn)
            .await?;
        if self.fail_after_confirmation_insert {
            Err(diesel::result::Error::RollbackTransaction)
        } else {
            Ok(())
        }
    }
}

#[tokio::test]
async fn card_refund_correction_is_atomic_idempotent_and_bounded_by_remaining_amount() {
    use chrono::TimeZone;
    use diesel::{dsl::count_star, insert_into};
    use horse_test_infra::{TestPostgres, apply_migrations};

    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();
    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    let pool_id = Uuid::now_v7();
    let purchase_id = Uuid::now_v7();
    let item_id = Uuid::now_v7();
    let document_id = Uuid::now_v7();
    let over_refund_document_id = Uuid::now_v7();
    let occurred_at = Utc.with_ymd_and_hms(2026, 10, 10, 3, 0, 0).unwrap();
    let mut conn = pool.get().await.unwrap();
    use public::{
        household_members, households, members, pools, purchase_allocations, purchase_items,
        purchases,
    };
    insert_into(households::table)
        .values((
            households::id.eq(household_id),
            households::name.eq("Refund"),
        ))
        .execute(&mut *conn)
        .await
        .unwrap();
    insert_into(members::table)
        .values((
            members::id.eq(member_id),
            members::oidc_subject.eq(format!("refund-{member_id}")),
            members::display_name.eq("Refund"),
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
    insert_into(pools::table)
        .values((
            pools::id.eq(pool_id),
            pools::household_id.eq(household_id),
            pools::name.eq("Personal"),
            pools::kind.eq("PERSONAL"),
            pools::current_funding_version.eq(None::<i32>),
        ))
        .execute(&mut *conn)
        .await
        .unwrap();
    insert_into(purchases::table)
        .values((
            purchases::id.eq(purchase_id),
            purchases::household_id.eq(household_id),
            purchases::occurred_at.eq(occurred_at),
            purchases::merchant.eq("Market"),
            purchases::status.eq("POSTED"),
            purchases::revision.eq(1_i64),
            purchases::created_by.eq(member_id),
            purchases::allocation_rule_version.eq(1_i64),
        ))
        .execute(&mut *conn)
        .await
        .unwrap();
    insert_into(purchase_items::table)
        .values((
            purchase_items::id.eq(item_id),
            purchase_items::household_id.eq(household_id),
            purchase_items::purchase_id.eq(purchase_id),
            purchase_items::name.eq("Groceries"),
            purchase_items::amount_yen.eq(100_i64),
        ))
        .execute(&mut *conn)
        .await
        .unwrap();
    insert_into(purchase_allocations::table)
        .values((
            purchase_allocations::household_id.eq(household_id),
            purchase_allocations::purchase_id.eq(purchase_id),
            purchase_allocations::item_id.eq(item_id),
            purchase_allocations::pool_id.eq(pool_id),
            purchase_allocations::amount_yen.eq(100_i64),
            purchase_allocations::weight.eq(1_i64),
            purchase_allocations::rule_version.eq(1_i64),
            purchase_allocations::rule_id.eq(None::<Uuid>),
        ))
        .execute(&mut *conn)
        .await
        .unwrap();
    for (source_document_id, sha, row, amount) in [
        (document_id, "a".repeat(64), 1_i64, -60_i64),
        (over_refund_document_id, "b".repeat(64), 2_i64, -41_i64),
    ] {
        insert_into(public::source_documents::table)
            .values((
                public::source_documents::id.eq(source_document_id),
                public::source_documents::household_id.eq(household_id),
                public::source_documents::sha256.eq(sha),
                public::source_documents::object_key.eq(format!("refund/{source_document_id}")),
                public::source_documents::mime_type.eq("text/csv"),
                public::source_documents::source_kind.eq("card"),
                public::source_documents::original_filename.eq("statement.csv"),
                public::source_documents::metadata.eq(serde_json::json!({})),
                public::source_documents::created_by.eq(member_id),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        insert_into(public::extraction_jobs::table)
            .values((
                public::extraction_jobs::id.eq(Uuid::now_v7()),
                public::extraction_jobs::household_id.eq(household_id),
                public::extraction_jobs::source_document_id.eq(source_document_id),
                public::extraction_jobs::version.eq(1_i32),
                public::extraction_jobs::status.eq("succeeded"),
                public::extraction_jobs::provider.eq("test"),
                public::extraction_jobs::model.eq("test"),
                public::extraction_jobs::idempotency_key
                    .eq(format!("refund-extraction-{source_document_id}")),
                public::extraction_jobs::request.eq(serde_json::json!({})),
                public::extraction_jobs::result.eq(Some(serde_json::json!({"statements": [{
                    "row": row,
                    "sourceReference": format!("row:{row}"),
                    "kind": "REFUND",
                    "occurredAt": "2026-10-15T03:00:00+00:00",
                    "merchant": "Market",
                    "amount": amount,
                    "region": {"page": 0, "top": 0.1, "left": 0.1, "bottom": 0.2, "right": 0.8}
                }]}))),
                public::extraction_jobs::attempt_count.eq(1_i32),
                public::extraction_jobs::max_attempts.eq(5_i32),
                public::extraction_jobs::finished_at.eq(Some(Utc::now())),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
    }
    drop(conn);

    let fail_after_confirmation = correct_purchase(
        &pool,
        refund_request(
            household_id,
            member_id,
            purchase_id,
            document_id,
            1,
            -60,
            "card-refund",
            [4; 32],
        ),
        &TestEvidence {
            fail_after_confirmation_insert: true,
        },
        &mut EmptyPolicy,
    )
    .await;
    assert!(fail_after_confirmation.is_err());

    let mut conn = pool.get().await.unwrap();
    assert_eq!(
        public::purchase_corrections::table
            .filter(public::purchase_corrections::household_id.eq(household_id))
            .select(count_star())
            .first::<i64>(&mut *conn)
            .await
            .unwrap(),
        0
    );
    assert_eq!(
        public::journals::table
            .filter(public::journals::household_id.eq(household_id))
            .select(count_star())
            .first::<i64>(&mut *conn)
            .await
            .unwrap(),
        0
    );
    assert_eq!(
        public::idempotency_records::table
            .filter(public::idempotency_records::household_id.eq(household_id))
            .filter(public::idempotency_records::key.eq("card-refund"))
            .select(count_star())
            .first::<i64>(&mut *conn)
            .await
            .unwrap(),
        0
    );
    assert_eq!(
        public::source_confirmations::table
            .filter(public::source_confirmations::household_id.eq(household_id))
            .select(count_star())
            .first::<i64>(&mut *conn)
            .await
            .unwrap(),
        0
    );
    drop(conn);

    let refund = correct_purchase(
        &pool,
        refund_request(
            household_id,
            member_id,
            purchase_id,
            document_id,
            1,
            -60,
            "card-refund",
            [4; 32],
        ),
        &TestEvidence {
            fail_after_confirmation_insert: false,
        },
        &mut EmptyPolicy,
    )
    .await
    .unwrap();
    let replay = correct_purchase(
        &pool,
        refund_request(
            household_id,
            member_id,
            purchase_id,
            document_id,
            1,
            -60,
            "card-refund",
            [4; 32],
        ),
        &TestEvidence {
            fail_after_confirmation_insert: false,
        },
        &mut EmptyPolicy,
    )
    .await
    .unwrap();
    assert_eq!(replay, refund);

    let duplicate_source = correct_purchase(
        &pool,
        refund_request(
            household_id,
            member_id,
            purchase_id,
            document_id,
            1,
            -60,
            "duplicate-source",
            [6; 32],
        ),
        &TestEvidence {
            fail_after_confirmation_insert: false,
        },
        &mut EmptyPolicy,
    )
    .await;
    assert!(duplicate_source.is_err());

    let over_refund = correct_purchase(
        &pool,
        refund_request(
            household_id,
            member_id,
            purchase_id,
            over_refund_document_id,
            2,
            -41,
            "over-refund",
            [5; 32],
        ),
        &TestEvidence {
            fail_after_confirmation_insert: false,
        },
        &mut EmptyPolicy,
    )
    .await;
    assert!(over_refund.is_err());

    let mut conn = pool.get().await.unwrap();
    assert_eq!(
        public::purchase_corrections::table
            .filter(public::purchase_corrections::household_id.eq(household_id))
            .select(count_star())
            .first::<i64>(&mut *conn)
            .await
            .unwrap(),
        1
    );
    assert_eq!(
        public::source_confirmations::table
            .filter(public::source_confirmations::household_id.eq(household_id))
            .select(count_star())
            .first::<i64>(&mut *conn)
            .await
            .unwrap(),
        1
    );
    assert_eq!(
        public::idempotency_records::table
            .filter(public::idempotency_records::household_id.eq(household_id))
            .filter(public::idempotency_records::key.eq("over-refund"))
            .select(count_star())
            .first::<i64>(&mut *conn)
            .await
            .unwrap(),
        0
    );
    assert_eq!(
        public::idempotency_records::table
            .filter(public::idempotency_records::household_id.eq(household_id))
            .filter(public::idempotency_records::key.eq("duplicate-source"))
            .select(count_star())
            .first::<i64>(&mut *conn)
            .await
            .unwrap(),
        0
    );
    assert_eq!(
        public::source_confirmations::table
            .filter(public::source_confirmations::household_id.eq(household_id))
            .filter(public::source_confirmations::source_document_id.eq(over_refund_document_id))
            .select(count_star())
            .first::<i64>(&mut *conn)
            .await
            .unwrap(),
        0
    );
    assert_eq!(
        public::evidence_links::table
            .filter(public::evidence_links::household_id.eq(household_id))
            .select(count_star())
            .first::<i64>(&mut *conn)
            .await
            .unwrap(),
        1
    );
    let linked = public::evidence_links::table
        .filter(public::evidence_links::household_id.eq(household_id))
        .select((
            public::evidence_links::source_document_id,
            public::evidence_links::source_reference,
            public::evidence_links::correction_id,
            public::evidence_links::observation,
        ))
        .first::<(Uuid, String, Option<Uuid>, serde_json::Value)>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(linked.0, document_id);
    assert_eq!(linked.1, "row:1");
    assert_eq!(
        linked.2,
        Some(Uuid::parse_str(&refund.correction_id).unwrap())
    );
    assert_eq!(linked.3["statement"]["amount"], -60);
    let balanced = public::journal_entries::table
        .filter(public::journal_entries::household_id.eq(household_id))
        .filter(
            public::journal_entries::journal_id.eq(Uuid::parse_str(&refund.journal_id).unwrap()),
        )
        .select((
            diesel::dsl::sum(public::journal_entries::debit_yen),
            diesel::dsl::sum(public::journal_entries::credit_yen),
        ))
        .first::<(Option<i64>, Option<i64>)>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(balanced, (Some(60), Some(60)));
}

fn refund_request(
    household_id: Uuid,
    member_id: Uuid,
    purchase_id: Uuid,
    document_id: Uuid,
    row: i64,
    amount_delta_yen: i64,
    key: &str,
    fingerprint: [u8; 32],
) -> CorrectPurchaseRequest {
    CorrectPurchaseRequest {
        household_id: household_id.to_string(),
        actor_id: member_id.to_string(),
        purchase_id: purchase_id.to_string(),
        item_id: None,
        amount_delta_yen: Some(amount_delta_yen),
        reason: "Card refund".into(),
        allocations: Vec::new(),
        payments: Vec::new(),
        idempotency_key: key.into(),
        request_fingerprint: fingerprint,
        source_document_id: Some(document_id.to_string()),
        source_reference: Some(format!("row:{row}")),
    }
}
