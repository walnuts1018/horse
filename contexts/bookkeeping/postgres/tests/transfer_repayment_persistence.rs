use bookkeeping_domain::{
    AccountingPeriod, MemberId, PositiveYen, YearMonth, Yen,
    record_repayment::{
        Loan as RepaymentLoan, LoanId as RepaymentLoanId, RecordRepaymentCommand, RepaymentSource,
        decide_record_repayment,
    },
    record_transfer::{RecordTransferCommand, TransferBase, decide_record_transfer},
};
use bookkeeping_postgres::{
    evidence_port::{
        EvidenceLinkQuery, EvidenceLinkRecord, EvidenceOperations, ExtractionQuery,
        InsertConfirmation, SourceConfirmation, SourceDocumentMetadata, SourceDocumentQuery,
        SourceReferenceQuery,
    },
    record_repayment::{PersistRecordRepayment, persist_record_repayment},
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
async fn transfer_and_repayment_persist_journals_and_idempotency_keys() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();
    let household_id = Uuid::now_v7();
    let lender_id = Uuid::now_v7();
    let borrower_id = Uuid::now_v7();
    let occurred_at = Utc.with_ymd_and_hms(2026, 10, 16, 3, 0, 0).unwrap();
    {
        let mut conn = pool.get().await.unwrap();
        use public::{household_members, households, members};
        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Transfer repayment persistence"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        for (member_id, subject) in [(lender_id, "lender"), (borrower_id, "borrower")] {
            insert_into(members::table)
                .values((
                    members::id.eq(member_id),
                    members::oidc_subject.eq(format!("transfer-repayment-{subject}-{member_id}")),
                    members::display_name.eq(subject),
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
        }
    }

    let period = AccountingPeriod::open(YearMonth::new(2026, 10).unwrap(), 1)
        .unwrap()
        .into_open()
        .unwrap();
    let lender = MemberId::new(lender_id.to_string()).unwrap();
    let borrower = MemberId::new(borrower_id.to_string()).unwrap();
    let loan_decision = decide_record_transfer(RecordTransferCommand::Loan(TransferBase {
        actor_id: lender.clone(),
        from_member_id: lender.clone(),
        to_member_id: borrower.clone(),
        amount: PositiveYen::new(20_000).unwrap(),
        description: "Loan".to_owned(),
        accounting_period: period.clone(),
        payment_account: None,
        destination_account: None,
    }))
    .unwrap();
    let loan_key = "transfer-loan-persistence";
    let loan = persist_record_transfer(
        &pool,
        PersistRecordTransfer {
            household_id: household_id.to_string(),
            actor_id: lender_id.to_string(),
            occurred_at,
            idempotency_key: loan_key.to_owned(),
            request_fingerprint: [1; 32],
            source_document_id: None,
            source_reference: None,
            source_account_id: None,
        },
        loan_decision,
        &EmptyEvidence,
        &mut EmptyPolicy,
    )
    .await
    .unwrap();

    let repayment_decision = decide_record_repayment(RecordRepaymentCommand {
        loan: RepaymentLoan {
            id: RepaymentLoanId::new(loan.transfer_id.clone()).unwrap(),
            lender: lender.clone(),
            borrower: borrower.clone(),
            principal: PositiveYen::new(20_000).unwrap(),
        },
        repaid_so_far: Yen::new(0).unwrap(),
        from_member: borrower.clone(),
        to_member: lender.clone(),
        amount: PositiveYen::new(5_000).unwrap(),
        accounting_period: period,
        source: None::<RepaymentSource>,
        destination_account: None,
        description: "Repayment".to_owned(),
    })
    .unwrap();
    let repayment_key = "transfer-repayment-persistence";
    let repayment = persist_record_repayment(
        &pool,
        PersistRecordRepayment {
            household_id: household_id.to_string(),
            actor_id: borrower_id.to_string(),
            occurred_at,
            idempotency_key: repayment_key.to_owned(),
            request_fingerprint: [2; 32],
            source_document_id: None,
            source_reference: None,
            source_account_id: None,
        },
        repayment_decision,
        &EmptyEvidence,
    )
    .await
    .unwrap();

    let mut conn = pool.get().await.unwrap();
    let journal_count = public::journal_entries::table
        .filter(public::journal_entries::household_id.eq(household_id))
        .filter(public::journal_entries::journal_id.eq(Uuid::parse_str(&loan.journal_id).unwrap()))
        .select(diesel::dsl::count_star())
        .first::<i64>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(journal_count, 2);
    let repayment_journal_count = public::journal_entries::table
        .filter(public::journal_entries::household_id.eq(household_id))
        .filter(
            public::journal_entries::journal_id.eq(Uuid::parse_str(&repayment.journal_id).unwrap()),
        )
        .select(diesel::dsl::count_star())
        .first::<i64>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(repayment_journal_count, 2);
    let loan_journal_key = public::journals::table
        .filter(public::journals::id.eq(Uuid::parse_str(&loan.journal_id).unwrap()))
        .select(public::journals::idempotency_key)
        .first::<String>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(loan_journal_key, loan_key);
    let repayment_journal_key = public::journals::table
        .filter(public::journals::id.eq(Uuid::parse_str(&repayment.journal_id).unwrap()))
        .select(public::journals::idempotency_key)
        .first::<String>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(repayment_journal_key, repayment_key);
}
