use bookkeeping_domain::{
    AccountingPeriod, MemberId, PositiveYen, YearMonth,
    record_transfer::{RecordTransferCommand, TransferBase, decide_record_transfer},
};
use bookkeeping_postgres::{
    evidence_port::{
        EvidenceLinkQuery, EvidenceLinkRecord, EvidenceOperations, ExtractionQuery,
        InsertConfirmation, SourceConfirmation, SourceDocumentMetadata, SourceDocumentQuery,
        SourceReferenceQuery,
    },
    record_transfer::{PersistRecordTransfer, persist_record_transfer},
    schema::public,
    settlement_preview::{
        FundingRuleRow, FundingScheduleRow, FundingShareRow, HouseholdFundingScheduleRow,
        PolicySettlementData,
    },
};
use chrono::{NaiveDate, TimeZone, Utc};
use diesel::{ExpressionMethods, QueryDsl, insert_into};
use diesel_async::RunQueryDsl;
use horse_postgres_common::{AsyncPgConnection, connect};
use horse_test_infra::{TestPostgres, apply_migrations};
use uuid::Uuid;

struct EmptyEvidence;

#[async_trait::async_trait]
impl EvidenceOperations for EmptyEvidence {
    async fn source_kind(
        &self,
        _: &mut AsyncPgConnection,
        _: SourceDocumentQuery,
    ) -> Result<Option<String>, diesel::result::Error> {
        Ok(None)
    }
    async fn latest_confirmation_status(
        &self,
        _: &mut AsyncPgConnection,
        _: SourceReferenceQuery<'_>,
    ) -> Result<Option<String>, diesel::result::Error> {
        Ok(None)
    }
    async fn has_confirmation_status(
        &self,
        _: &mut AsyncPgConnection,
        _: SourceReferenceQuery<'_>,
        _: &str,
    ) -> Result<bool, diesel::result::Error> {
        Ok(false)
    }
    async fn latest_confirmation(
        &self,
        _: &mut AsyncPgConnection,
        _: SourceReferenceQuery<'_>,
    ) -> Result<Option<SourceConfirmation>, diesel::result::Error> {
        Ok(None)
    }
    async fn latest_extraction(
        &self,
        _: &mut AsyncPgConnection,
        _: ExtractionQuery,
    ) -> Result<Option<serde_json::Value>, diesel::result::Error> {
        Ok(None)
    }
    async fn latest_extraction_with_id(
        &self,
        _: &mut AsyncPgConnection,
        _: ExtractionQuery,
    ) -> Result<Option<(Uuid, serde_json::Value)>, diesel::result::Error> {
        Ok(None)
    }
    async fn source_document_metadata(
        &self,
        _: &mut AsyncPgConnection,
        _: SourceDocumentQuery,
    ) -> Result<Option<SourceDocumentMetadata>, diesel::result::Error> {
        Ok(None)
    }
    async fn evidence_link(
        &self,
        _: &mut AsyncPgConnection,
        _: EvidenceLinkQuery<'_>,
    ) -> Result<Option<EvidenceLinkRecord>, diesel::result::Error> {
        Ok(None)
    }
    async fn insert_confirmation(
        &self,
        _: &mut AsyncPgConnection,
        _: InsertConfirmation,
    ) -> Result<(), diesel::result::Error> {
        Ok(())
    }
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

#[tokio::test]
async fn direct_advance_persists_bilateral_settlement_adjustments() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();
    let household_id = Uuid::now_v7();
    let from_member = Uuid::now_v7();
    let to_member = Uuid::now_v7();
    {
        let mut conn = pool.get().await.unwrap();
        insert_into(public::households::table)
            .values((
                public::households::id.eq(household_id),
                public::households::name.eq("Direct advance"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        for (id, subject) in [(from_member, "sender"), (to_member, "recipient")] {
            insert_into(public::members::table)
                .values((
                    public::members::id.eq(id),
                    public::members::oidc_subject.eq(format!("direct-advance-{subject}-{id}")),
                    public::members::display_name.eq(subject),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
            insert_into(public::household_members::table)
                .values((
                    public::household_members::household_id.eq(household_id),
                    public::household_members::member_id.eq(id),
                    public::household_members::role.eq("OWNER"),
                ))
                .execute(&mut *conn)
                .await
                .unwrap();
        }
    }

    let sender = MemberId::new(from_member.to_string()).unwrap();
    let recipient = MemberId::new(to_member.to_string()).unwrap();
    let period = AccountingPeriod::open(YearMonth::new(2026, 10).unwrap(), 1)
        .unwrap()
        .into_open()
        .unwrap();
    let decision = decide_record_transfer(RecordTransferCommand::Advance(TransferBase {
        actor_id: sender.clone(),
        from_member_id: sender,
        to_member_id: recipient,
        amount: PositiveYen::new(1_250).unwrap(),
        description: "Direct reimbursement".to_owned(),
        accounting_period: period,
        payment_account: None,
        destination_account: None,
    }))
    .unwrap();
    let occurred_at = Utc.with_ymd_and_hms(2026, 10, 16, 3, 0, 0).unwrap();
    let persisted = persist_record_transfer(
        &pool,
        PersistRecordTransfer {
            household_id: household_id.to_string(),
            actor_id: from_member.to_string(),
            occurred_at,
            idempotency_key: "direct-advance-settlement".to_owned(),
            request_fingerprint: [7; 32],
            source_document_id: None,
            source_reference: None,
            source_account_id: None,
        },
        decision,
        &EmptyEvidence,
        &mut EmptyPolicy,
    )
    .await
    .unwrap();

    let mut conn = pool.get().await.unwrap();
    let mut adjustments = public::settlement_adjustments::table
        .filter(public::settlement_adjustments::household_id.eq(household_id))
        .filter(
            public::settlement_adjustments::source_id
                .eq(Uuid::parse_str(&persisted.transfer_id).unwrap()),
        )
        .select((
            public::settlement_adjustments::member_id,
            public::settlement_adjustments::amount_yen,
        ))
        .load::<(Uuid, i64)>(&mut *conn)
        .await
        .unwrap();
    adjustments.sort_by_key(|(member_id, _)| *member_id);
    let mut expected = vec![(from_member, -1_250), (to_member, 1_250)];
    expected.sort_by_key(|(member_id, _)| *member_id);
    assert_eq!(adjustments, expected);
}
