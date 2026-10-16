use bookkeeping_domain::{
    AccountingPeriod, MemberId, PositiveYen, YearMonth, Yen,
    create_account::{CreateAccountCommand, OpeningBalance},
    record_repayment::{
        Loan as RepaymentLoan, LoanId as RepaymentLoanId, PoolKind as RepaymentPoolKind,
        RecordRepaymentCommand, RepaymentSource, decide_record_repayment,
    },
    record_transfer::{RecordTransferCommand, TransferBase, decide_record_transfer},
};
use bookkeeping_postgres::{
    account_balance_analysis::account_balance_analysis_for_period,
    create_account::persist_create_account,
    evidence_port::{
        EvidenceLinkQuery, EvidenceLinkRecord, EvidenceOperations, ExtractionQuery,
        InsertConfirmation, SourceConfirmation, SourceDocumentMetadata, SourceDocumentQuery,
        SourceReferenceQuery,
    },
    loan_balance_query,
    monthly_spending_query::monthly_category_spending,
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
async fn shared_account_repayment_balances_journal_reduces_balance_and_adjusts_lender_settlement() {
    let postgres = TestPostgres::start().await.unwrap();
    apply_migrations(postgres.connection_url()).unwrap();
    let pool = connect(postgres.connection_url()).await.unwrap();
    let household_id = Uuid::now_v7();
    let lender_id = Uuid::now_v7();
    let borrower_id = Uuid::now_v7();
    let pool_id = Uuid::now_v7();
    let purchase_id = Uuid::now_v7();
    let item_id = Uuid::now_v7();
    let occurred_at = Utc.with_ymd_and_hms(2026, 10, 16, 3, 0, 0).unwrap();

    {
        let mut conn = pool.get().await.unwrap();
        use public::{household_members, households, members, pools, purchase_items, purchases};
        insert_into(households::table)
            .values((
                households::id.eq(household_id),
                households::name.eq("Shared account repayment"),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
        for (member_id, subject) in [(lender_id, "lender"), (borrower_id, "borrower")] {
            insert_into(members::table)
                .values((
                    members::id.eq(member_id),
                    members::oidc_subject.eq(format!("shared-repayment-{subject}-{member_id}")),
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
        insert_into(pools::table)
            .values((
                pools::id.eq(pool_id),
                pools::household_id.eq(household_id),
                pools::name.eq("Shared"),
                pools::kind.eq("SHARED"),
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
                purchases::created_by.eq(borrower_id),
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
                purchase_items::amount_yen.eq(1_200_i64),
            ))
            .execute(&mut *conn)
            .await
            .unwrap();
    }

    let account = persist_create_account(
        &pool,
        CreateAccountCommand {
            actor_id: lender_id.to_string(),
            household_id: household_id.to_string(),
            name: "Shared bank".to_owned(),
            kind: "BANK".to_owned(),
            card_last4: None,
            pool_id: Some(pool_id.to_string()),
            opening_balance: Some(OpeningBalance {
                amount_yen: 30_000,
                at: "2026-10-01T00:00:00+09:00".to_owned(),
            }),
        },
    )
    .await
    .unwrap();
    let account_id = Uuid::parse_str(&account.id).unwrap();
    let period = AccountingPeriod::open(YearMonth::new(2026, 10).unwrap(), 1)
        .unwrap()
        .into_open()
        .unwrap();
    let lender = MemberId::new(lender_id.to_string()).unwrap();
    let borrower = MemberId::new(borrower_id.to_string()).unwrap();
    let loan = persist_record_transfer(
        &pool,
        PersistRecordTransfer {
            household_id: household_id.to_string(),
            actor_id: lender_id.to_string(),
            occurred_at,
            idempotency_key: "shared-account-loan".to_owned(),
            request_fingerprint: [21; 32],
            source_document_id: None,
            source_reference: None,
            source_account_id: None,
        },
        decide_record_transfer(RecordTransferCommand::Loan(TransferBase {
            actor_id: lender.clone(),
            from_member_id: lender.clone(),
            to_member_id: borrower.clone(),
            amount: PositiveYen::new(20_000).unwrap(),
            description: "Loan".to_owned(),
            accounting_period: period.clone(),
            payment_account: None,
            destination_account: None,
        }))
        .unwrap(),
        &EmptyEvidence,
        &mut EmptyPolicy,
    )
    .await
    .unwrap();

    let decision = decide_record_repayment(RecordRepaymentCommand {
        loan: RepaymentLoan {
            id: RepaymentLoanId::new(loan.transfer_id.clone()).unwrap(),
            lender,
            borrower,
            principal: PositiveYen::new(20_000).unwrap(),
        },
        repaid_so_far: Yen::new(0).unwrap(),
        from_member: MemberId::new(borrower_id.to_string()).unwrap(),
        to_member: MemberId::new(lender_id.to_string()).unwrap(),
        amount: PositiveYen::new(5_000).unwrap(),
        accounting_period: period,
        source: Some(RepaymentSource {
            account_id: bookkeeping_domain::AccountId::new(account.id.clone()).unwrap(),
            pool_id: bookkeeping_domain::PoolId::new(pool_id.to_string()).unwrap(),
            pool_kind: RepaymentPoolKind::Shared,
        }),
        destination_account: None,
        description: "Shared repayment".to_owned(),
    })
    .unwrap();
    let repayment = persist_record_repayment(
        &pool,
        PersistRecordRepayment {
            household_id: household_id.to_string(),
            actor_id: borrower_id.to_string(),
            occurred_at,
            idempotency_key: "shared-account-repayment".to_owned(),
            request_fingerprint: [22; 32],
            source_document_id: None,
            source_reference: None,
            source_account_id: None,
        },
        decision,
        &EmptyEvidence,
    )
    .await
    .unwrap();

    let repayment_journal_id = Uuid::parse_str(&repayment.journal_id).unwrap();
    let repayment_id = Uuid::parse_str(&repayment.transfer_id).unwrap();
    let mut conn = pool.get().await.unwrap();
    let postings = public::journal_entries::table
        .filter(public::journal_entries::household_id.eq(household_id))
        .filter(public::journal_entries::journal_id.eq(repayment_journal_id))
        .select((
            public::journal_entries::account_id,
            public::journal_entries::debit_yen,
            public::journal_entries::credit_yen,
        ))
        .load::<(Option<Uuid>, i64, i64)>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(postings.iter().map(|row| row.1).sum::<i64>(), 10_000);
    assert_eq!(postings.iter().map(|row| row.2).sum::<i64>(), 10_000);
    assert_eq!(
        postings
            .iter()
            .filter(|row| row.0 == Some(account_id))
            .map(|row| row.2 - row.1)
            .sum::<i64>(),
        -5_000
    );

    let adjustment = public::settlement_adjustments::table
        .filter(public::settlement_adjustments::household_id.eq(household_id))
        .filter(public::settlement_adjustments::source_id.eq(repayment_id))
        .select((
            public::settlement_adjustments::member_id,
            public::settlement_adjustments::amount_yen,
        ))
        .first::<(Uuid, i64)>(&mut *conn)
        .await
        .unwrap();
    assert_eq!(adjustment, (lender_id, 5_000));

    let loan_balances = loan_balance_query::list(&pool, household_id).await.unwrap();
    let loan_balance = loan_balances
        .iter()
        .find(|row| row.id == Uuid::parse_str(&loan.transfer_id).unwrap())
        .unwrap();
    assert_eq!(loan_balance.principal_yen, 20_000);
    assert_eq!(loan_balance.repaid_yen, 5_000);
    assert_eq!(loan_balance.balance_yen, 15_000);

    let balance = account_balance_analysis_for_period(
        &pool,
        household_id,
        account_id,
        Utc.with_ymd_and_hms(2026, 10, 1, 0, 0, 0).unwrap(),
        Utc.with_ymd_and_hms(2026, 11, 1, 0, 0, 0).unwrap(),
        None,
        20,
    )
    .await
    .unwrap();
    assert_eq!(balance.closing_balance_yen, 25_000);

    drop(conn);
    let monthly_spending =
        monthly_category_spending(&pool, lender_id, household_id, "2026-10", "2026-10")
            .await
            .unwrap();
    assert_eq!(monthly_spending.len(), 1);
    assert_eq!(monthly_spending[0].amount_yen, 1_200);
}
