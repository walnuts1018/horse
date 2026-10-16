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
use chrono::{Datelike, FixedOffset, NaiveDate, TimeZone, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, insert_into};
use diesel_async::{AsyncPgConnection, RunQueryDsl};
use horse_postgres_common::connect;
use uuid::Uuid;

struct TestEvidence {
    fail_after_confirmation_insert: bool,
}

struct PersonalPoolPolicy {
    member_id: Uuid,
    pool_id: Uuid,
}

#[async_trait::async_trait]
impl PolicySettlementData for PersonalPoolPolicy {
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
        pool_ids: &[Uuid],
        versions: &[i32],
    ) -> Result<Vec<FundingRuleRow>, diesel::result::Error> {
        Ok((pool_ids.contains(&self.pool_id) && versions.contains(&1))
            .then_some((self.pool_id, 1, Some(self.member_id), None))
            .into_iter()
            .collect())
    }
    async fn rule_owner(
        &mut self,
        _: &mut AsyncPgConnection,
        _: Uuid,
        pool_id: Uuid,
        _: i32,
    ) -> Result<Option<Uuid>, diesel::result::Error> {
        Ok((pool_id == self.pool_id).then_some(self.member_id))
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
    use diesel::{dsl::count_star, insert_into};
    use horse_test_infra::{TestPostgres, apply_migrations};

    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();
    let household_id = Uuid::now_v7();
    let member_id = Uuid::now_v7();
    let pool_id = Uuid::now_v7();
    let account_id = Uuid::now_v7();
    let purchase_id = Uuid::now_v7();
    let item_id = Uuid::now_v7();
    let document_id = Uuid::now_v7();
    let over_refund_document_id = Uuid::now_v7();
    let now = Utc::now();
    let japan_offset = FixedOffset::east_opt(9 * 60 * 60).unwrap();
    let current_period_start = now
        .with_timezone(&japan_offset)
        .date_naive()
        .with_day(1)
        .unwrap();
    let original_period_start = current_period_start
        .pred_opt()
        .unwrap()
        .with_day(1)
        .unwrap();
    let occurred_at = japan_offset
        .from_local_datetime(&original_period_start.and_hms_opt(12, 0, 0).unwrap())
        .single()
        .unwrap()
        .with_timezone(&Utc);
    let statement_occurred_at = now.to_rfc3339();
    let mut conn = pool.get().await.unwrap();
    use public::{
        accounts, household_members, households, members, pool_funding_rules, pools,
        purchase_allocations, purchase_items, purchase_payments, purchases,
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
    insert_into(public::accounting_periods::table)
        .values((
            public::accounting_periods::household_id.eq(household_id),
            public::accounting_periods::period_start.eq(original_period_start),
            public::accounting_periods::status.eq("CLOSED"),
            public::accounting_periods::revision.eq(1_i64),
            public::accounting_periods::closed_by.eq(Some(member_id)),
            public::accounting_periods::closed_at.eq(Some(now)),
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
    insert_into(pool_funding_rules::table)
        .values((
            pool_funding_rules::household_id.eq(household_id),
            pool_funding_rules::pool_id.eq(pool_id),
            pool_funding_rules::version.eq(1_i32),
            pool_funding_rules::owner_member_id.eq(Some(member_id)),
            pool_funding_rules::settlement_policy.eq(None::<String>),
            pool_funding_rules::created_by.eq(member_id),
        ))
        .execute(&mut *conn)
        .await
        .unwrap();
    diesel::update(pools::table.filter(pools::id.eq(pool_id)))
        .set(pools::current_funding_version.eq(Some(1)))
        .execute(&mut *conn)
        .await
        .unwrap();
    insert_into(accounts::table)
        .values((
            accounts::id.eq(account_id),
            accounts::household_id.eq(household_id),
            accounts::pool_id.eq(Some(pool_id)),
            accounts::name.eq("Personal card"),
            accounts::kind.eq("CARD"),
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
    insert_into(purchase_payments::table)
        .values((
            purchase_payments::household_id.eq(household_id),
            purchase_payments::purchase_id.eq(purchase_id),
            purchase_payments::line_no.eq(1_i32),
            purchase_payments::account_id.eq(account_id),
            purchase_payments::amount_yen.eq(100_i64),
        ))
        .execute(&mut *conn)
        .await
        .unwrap();
    insert_into(public::purchase_member_burdens::table)
        .values((
            public::purchase_member_burdens::household_id.eq(household_id),
            public::purchase_member_burdens::purchase_id.eq(purchase_id),
            public::purchase_member_burdens::item_id.eq(item_id),
            public::purchase_member_burdens::pool_id.eq(pool_id),
            public::purchase_member_burdens::member_id.eq(member_id),
            public::purchase_member_burdens::amount_yen.eq(100_i64),
            public::purchase_member_burdens::funding_rule_version.eq(1_i32),
            public::purchase_member_burdens::funding_weight.eq(1_i64),
            public::purchase_member_burdens::settlement_policy.eq(None::<String>),
            public::purchase_member_burdens::snapshot_source.eq("POSTED"),
        ))
        .execute(&mut *conn)
        .await
        .unwrap();
    insert_into(public::purchase_payment_funding_snapshots::table)
        .values((
            public::purchase_payment_funding_snapshots::household_id.eq(household_id),
            public::purchase_payment_funding_snapshots::purchase_id.eq(purchase_id),
            public::purchase_payment_funding_snapshots::payment_line_no.eq(1_i32),
            public::purchase_payment_funding_snapshots::account_id.eq(account_id),
            public::purchase_payment_funding_snapshots::pool_id.eq(pool_id),
            public::purchase_payment_funding_snapshots::member_id.eq(member_id),
            public::purchase_payment_funding_snapshots::funding_rule_version.eq(1_i32),
            public::purchase_payment_funding_snapshots::funding_weight.eq(1_i64),
            public::purchase_payment_funding_snapshots::settlement_policy.eq(None::<String>),
            public::purchase_payment_funding_snapshots::snapshot_source.eq("POSTED"),
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
                    "occurredAt": statement_occurred_at,
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

    let mut policy = PersonalPoolPolicy { member_id, pool_id };
    let fail_after_confirmation = correct_purchase(
        &pool,
        refund_request(
            household_id,
            member_id,
            purchase_id,
            RefundSource {
                document_id,
                row: 1,
            },
            -60,
            "card-refund",
            [4; 32],
        ),
        &TestEvidence {
            fail_after_confirmation_insert: true,
        },
        &mut policy,
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
            RefundSource {
                document_id,
                row: 1,
            },
            -60,
            "card-refund",
            [4; 32],
        ),
        &TestEvidence {
            fail_after_confirmation_insert: false,
        },
        &mut policy,
    )
    .await
    .unwrap();
    let replay = correct_purchase(
        &pool,
        refund_request(
            household_id,
            member_id,
            purchase_id,
            RefundSource {
                document_id,
                row: 1,
            },
            -60,
            "card-refund",
            [4; 32],
        ),
        &TestEvidence {
            fail_after_confirmation_insert: false,
        },
        &mut policy,
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
            RefundSource {
                document_id,
                row: 1,
            },
            -60,
            "duplicate-source",
            [6; 32],
        ),
        &TestEvidence {
            fail_after_confirmation_insert: false,
        },
        &mut policy,
    )
    .await;
    assert!(duplicate_source.is_err());

    let over_refund = correct_purchase(
        &pool,
        refund_request(
            household_id,
            member_id,
            purchase_id,
            RefundSource {
                document_id: over_refund_document_id,
                row: 2,
            },
            -41,
            "over-refund",
            [5; 32],
        ),
        &TestEvidence {
            fail_after_confirmation_insert: false,
        },
        &mut policy,
    )
    .await;
    assert!(over_refund.is_err());

    let mut conn = pool.get().await.unwrap();
    let effective_period = public::purchase_corrections::table
        .filter(public::purchase_corrections::household_id.eq(household_id))
        .filter(
            public::purchase_corrections::id.eq(Uuid::parse_str(&refund.correction_id).unwrap()),
        )
        .select(public::purchase_corrections::effective_period_start)
        .first::<NaiveDate>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(effective_period, current_period_start);
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
    let journal_id = Uuid::parse_str(&refund.journal_id).unwrap();
    let journal_idempotency_key = public::journals::table
        .filter(public::journals::household_id.eq(household_id))
        .filter(public::journals::id.eq(journal_id))
        .select(public::journals::idempotency_key)
        .first::<String>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(journal_idempotency_key, "card-refund");
    let entries = public::journal_entries::table
        .filter(public::journal_entries::household_id.eq(household_id))
        .filter(public::journal_entries::journal_id.eq(journal_id))
        .select((
            public::journal_entries::debit_yen,
            public::journal_entries::credit_yen,
        ))
        .load::<(i64, i64)>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(entries.iter().map(|entry| entry.0).sum::<i64>(), 60);
    assert_eq!(entries.iter().map(|entry| entry.1).sum::<i64>(), 60);
}

struct RefundSource {
    document_id: Uuid,
    row: i64,
}

fn refund_request(
    household_id: Uuid,
    member_id: Uuid,
    purchase_id: Uuid,
    source: RefundSource,
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
        source_document_id: Some(source.document_id.to_string()),
        source_reference: Some(format!("row:{}", source.row)),
    }
}
