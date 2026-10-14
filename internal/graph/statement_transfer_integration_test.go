package graph

import (
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"os"
	"testing"

	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	transferapp "github.com/walnuts1018/horse/internal/application/transfer"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestStatementLoanConfirmationIsAtomicAndIdempotent(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	memberID := uuid.NewV7().String()
	memberUUID := generatedUUID(memberID)
	subject, memberName := "statement-loan:"+memberID, "Statement lender"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: memberName})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Statement loan"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	borrowerID := uuid.NewV7().String()
	borrowerUUID := generatedUUID(borrowerID)
	borrowerSubject, borrowerName := "statement-borrower:"+borrowerID, "Statement borrower"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &borrowerUUID, OidcSubject: &borrowerSubject, DisplayName: &borrowerName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(borrowerUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &borrowerUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	pool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Shared", Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	account, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Bank", Kind: model.AccountKindBank, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	documentID := uuid.NewV7().String()
	documentUUID := generatedUUID(documentID)
	hash, objectKey, mimeType, sourceKind := "hash-"+documentID, household.ID+"/originals/"+documentID, testApplicationPDF, "bank"
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &documentUUID, HouseholdID: &householdUUID, Sha256: &hash, ObjectKey: &objectKey, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	verified, err := stdjson.Marshal(ingestion.Verification{Statements: []ingestion.StatementEntry{
		{Row: 2, SourceReference: "row:2", Kind: "LOAN", OccurredAt: "2026-10-02T12:00:00Z", Merchant: "貸付", Amount: -250},
		{Row: 3, SourceReference: "row:3", Kind: "REPAYMENT", OccurredAt: "2026-10-03T12:00:00Z", Merchant: "返済", Amount: -100},
		{Row: 4, SourceReference: "row:4", Kind: "LOAN", OccurredAt: "2026-10-04T12:00:00Z", Merchant: "誤分類", Amount: -90},
		{Row: 5, SourceReference: "row:5", Kind: "LOAN", OccurredAt: "2026-10-05T12:00:00Z", Merchant: "日付確認", Amount: -90},
	}})
	if err != nil {
		t.Fatal(err)
	}
	jobID, status := generatedUUID(uuid.NewV7().String()), extractionJobSucceededStatus
	version := int32(1)
	result := sql.Null[types.JSON[stdjson.RawMessage]]{V: types.JSON[stdjson.RawMessage]{Val: verified}, Valid: true}
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{ID: &jobID, HouseholdID: &householdUUID, SourceDocumentID: &documentUUID, Version: &version, Status: &status, Result: &result}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	input := model.RecordTransferInput{HouseholdID: household.ID, FromMemberID: memberID, ToMemberID: borrowerID, Kind: model.TransferInputKindLoan, PaymentAccountID: &account.ID, AmountYen: 250, OccurredAt: "2026-10-02T12:00:00Z", Description: "貸付", IdempotencyKey: uuid.NewV7().String(), SourceDocumentID: &documentID, SourceReference: new("row:2"), SourceAccountID: &account.ID}
	created, err := resolver.recordTransfer(ctx, input)
	if err != nil || created.TransferID == nil {
		t.Fatalf("statement loan failed: result=%#v error=%v", created, err)
	}
	replayed, err := resolver.recordTransfer(ctx, input)
	if err != nil || replayed.ID != created.ID {
		t.Fatalf("statement loan replay did not return existing ledger entry: result=%#v error=%v", replayed, err)
	}
	duplicate := input
	duplicate.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordTransfer(ctx, duplicate); !errors.Is(err, transferapp.ErrConflict) {
		t.Fatalf("statement row was confirmed twice: %v", err)
	}
	badAmount := input
	badAmount.AmountYen = 251
	badAmount.SourceReference = new("row:3")
	badAmount.OccurredAt = "2026-10-03T12:00:00Z"
	badAmount.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordTransfer(ctx, badAmount); !errors.Is(err, transferapp.ErrBadInput) {
		t.Fatalf("statement amount mismatch was accepted: %v", err)
	}
	badKind := input
	badKind.SourceReference = new("row:4")
	badKind.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordTransfer(ctx, badKind); !errors.Is(err, transferapp.ErrBadInput) {
		t.Fatalf("statement kind mismatch was accepted: %v", err)
	}
	badDate := input
	badDate.SourceReference = new("row:5")
	badDate.OccurredAt = "2026-10-05T12:01:00Z"
	badDate.AmountYen = 90
	badDate.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordTransfer(ctx, badDate); !errors.Is(err, transferapp.ErrBadInput) {
		t.Fatalf("statement date mismatch was accepted: %v", err)
	}
	otherAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Other bank", Kind: model.AccountKindBank, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	badAccount := input
	badAccount.SourceReference = new("row:5")
	badAccount.PaymentAccountID = &otherAccount.ID
	badAccount.AmountYen = 90
	badAccount.OccurredAt = "2026-10-05T12:00:00Z"
	badAccount.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordTransfer(ctx, badAccount); !errors.Is(err, transferapp.ErrBadInput) {
		t.Fatalf("statement selected-account mismatch was accepted: %v", err)
	}
	foreignHousehold, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Foreign statement household"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(foreignHousehold.ID))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	foreignDocumentID := uuid.NewV7().String()
	foreignDocumentUUID, foreignHouseholdUUID := generatedUUID(foreignDocumentID), generatedUUID(foreignHousehold.ID)
	foreignHash, foreignObjectKey := "hash-"+foreignDocumentID, foreignHousehold.ID+"/originals/"+foreignDocumentID
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &foreignDocumentUUID, HouseholdID: &foreignHouseholdUUID, Sha256: &foreignHash, ObjectKey: &foreignObjectKey, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	foreignDocumentInput := input
	foreignDocumentInput.SourceDocumentID = &foreignDocumentID
	foreignDocumentInput.SourceReference = new("row:2")
	foreignDocumentInput.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordTransfer(ctx, foreignDocumentInput); !errors.Is(err, transferapp.ErrNotFound) {
		t.Fatalf("statement from another household was accepted: %v", err)
	}
	loan, err := resolver.recordTransfer(ctx, model.RecordTransferInput{HouseholdID: household.ID, FromMemberID: memberID, ToMemberID: borrowerID, Kind: model.TransferInputKindLoan, AmountYen: 100, OccurredAt: "2026-09-01T12:00:00Z", Description: "既存貸付", IdempotencyKey: uuid.NewV7().String()})
	if err != nil || loan.TransferID == nil {
		t.Fatalf("repayment fixture loan failed: result=%#v error=%v", loan, err)
	}
	repayment := model.RecordRepaymentInput{HouseholdID: household.ID, FromMemberID: borrowerID, ToMemberID: memberID, LoanID: *loan.TransferID, PaymentAccountID: &account.ID, AmountYen: 100, OccurredAt: "2026-10-03T12:00:00Z", Description: "返済", IdempotencyKey: uuid.NewV7().String(), SourceDocumentID: &documentID, SourceReference: new("row:3"), SourceAccountID: &account.ID}
	repaid, err := resolver.recordRepayment(ctx, repayment)
	if err != nil || repaid.TransferID == nil {
		t.Fatalf("statement repayment failed: result=%#v error=%v", repaid, err)
	}
	confirmation, err := models.DocumentConfirmations.Query(sm.Where(psql.And(models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)), models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg("row:3"))))).One(ctx, bobpgx.NewPool(db))
	if err != nil || !confirmation.TransferID.Valid || confirmation.TransferID.V != generatedUUID(*repaid.TransferID) {
		t.Fatalf("repayment confirmation points to wrong transfer: confirmation=%#v error=%v", confirmation, err)
	}
	transfers, err := models.Transfers.Query(sm.Where(models.Transfers.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(transfers) != 3 {
		t.Fatalf("failed statement requests created transfer records: transfers=%#v error=%v", transfers, err)
	}
}
