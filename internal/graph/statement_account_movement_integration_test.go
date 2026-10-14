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
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const (
	testStatementTransferFirstOccurredAt  = "2026-10-02T12:00:00Z"
	testStatementTransferSecondOccurredAt = "2026-10-02T12:01:00Z"
)

func TestStatementAccountTransferConfirmationIsAtomicAndIdempotent(t *testing.T) {
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
	subject, memberName := "statement-transfer:"+memberID, "Statement transfer"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: memberName})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Statement account transfer"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(household.ID))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	fromAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Operating", Kind: model.AccountKindBank})
	if err != nil {
		t.Fatal(err)
	}
	toAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Reserve", Kind: model.AccountKindBank})
	if err != nil {
		t.Fatal(err)
	}

	documentID := uuid.NewV7().String()
	documentUUID, householdUUID := generatedUUID(documentID), generatedUUID(household.ID)
	hash, objectKey, mimeType, sourceKind := "hash-"+documentID, household.ID+"/originals/"+documentID, testApplicationPDF, "bank"
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
		ID: &documentUUID, HouseholdID: &householdUUID, Sha256: &hash, ObjectKey: &objectKey,
		MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberUUID,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	cardDocumentID := uuid.NewV7().String()
	cardDocumentUUID := generatedUUID(cardDocumentID)
	cardSourceKind := testCardSourceKind
	cardHash, cardObjectKey := "hash-"+cardDocumentID, household.ID+"/originals/"+cardDocumentID
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
		ID: &cardDocumentUUID, HouseholdID: &householdUUID, Sha256: &cardHash, ObjectKey: &cardObjectKey,
		MimeType: &mimeType, SourceKind: &cardSourceKind, CreatedBy: &memberUUID,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	region := ingestion.Region{Page: 0, Top: 0.2, Left: 0.1, Bottom: 0.3, Right: 0.8}
	verified, err := stdjson.Marshal(ingestion.Verification{Statements: []ingestion.StatementEntry{
		{Row: 2, SourceReference: "row:2", Kind: "ACCOUNT_TRANSFER", OccurredAt: testStatementTransferFirstOccurredAt, Merchant: testAccountTransferDescription, Amount: -250, Region: &region},
		{Row: 3, SourceReference: "row:3", Kind: "ACCOUNT_TRANSFER", OccurredAt: testStatementTransferSecondOccurredAt, Merchant: testAccountTransferDescription, Amount: -300, Region: &region},
	}})
	if err != nil {
		t.Fatal(err)
	}
	jobID, status := generatedUUID(uuid.NewV7().String()), extractionJobSucceededStatus
	version := int32(1)
	result := sql.Null[types.JSON[stdjson.RawMessage]]{V: types.JSON[stdjson.RawMessage]{Val: verified}, Valid: true}
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
		ID: &jobID, HouseholdID: &householdUUID, SourceDocumentID: &documentUUID, Version: &version, Status: &status, Result: &result,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	sourceDocumentID, sourceReference, sourceAccountID := documentID, "row:2", fromAccount.ID
	input := model.RecordAccountMovementInput{
		HouseholdID: household.ID, Kind: model.AccountMovementKindTransfer,
		FromAccountID: fromAccount.ID, ToAccountID: toAccount.ID, AmountYen: 250,
		OccurredAt: testStatementTransferFirstOccurredAt, Description: testAccountTransferDescription, IdempotencyKey: uuid.NewV7().String(),
		SourceDocumentID: &sourceDocumentID, SourceReference: &sourceReference, SourceAccountID: &sourceAccountID,
	}
	created, err := resolver.recordAccountMovement(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := resolver.recordAccountMovement(ctx, input)
	if err != nil || replayed.ID != created.ID {
		t.Fatalf("same statement request did not return the existing movement: created=%#v replayed=%#v error=%v", created, replayed, err)
	}
	duplicate := input
	duplicate.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordAccountMovement(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("same statement row with a different key was not rejected: %v", err)
	}
	badAmount := input
	badAmount.SourceReference = new("row:3")
	badAmount.AmountYen = 301
	badAmount.OccurredAt = testStatementTransferSecondOccurredAt
	badAmount.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordAccountMovement(ctx, badAmount); !errors.Is(err, ErrBadInput) {
		t.Fatalf("statement amount mismatch was not rejected: %v", err)
	}
	wrongDirection := badAmount
	wrongDirection.AmountYen = 300
	wrongDirection.FromAccountID, wrongDirection.ToAccountID = toAccount.ID, fromAccount.ID
	wrongDirection.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordAccountMovement(ctx, wrongDirection); !errors.Is(err, ErrBadInput) {
		t.Fatalf("statement direction mismatch was not rejected: %v", err)
	}
	wrongSourceKind := input
	wrongSourceKind.SourceDocumentID = &cardDocumentID
	wrongSourceKind.SourceReference = new("row:2")
	wrongSourceKind.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordAccountMovement(ctx, wrongSourceKind); !errors.Is(err, ErrBadInput) {
		t.Fatalf("non-bank statement source was not rejected: %v", err)
	}
	foreignHousehold, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Other household"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(foreignHousehold.ID))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	foreignDocumentID := uuid.NewV7().String()
	foreignDocumentUUID, foreignHouseholdUUID := generatedUUID(foreignDocumentID), generatedUUID(foreignHousehold.ID)
	foreignHash, foreignObjectKey := "hash-"+foreignDocumentID, foreignHousehold.ID+"/originals/"+foreignDocumentID
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
		ID: &foreignDocumentUUID, HouseholdID: &foreignHouseholdUUID, Sha256: &foreignHash, ObjectKey: &foreignObjectKey,
		MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberUUID,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	wrongHousehold := input
	wrongHousehold.SourceDocumentID = &foreignDocumentID
	wrongHousehold.SourceReference = new("row:2")
	wrongHousehold.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordAccountMovement(ctx, wrongHousehold); !errors.Is(err, ErrNotFound) {
		t.Fatalf("statement document from another household was not rejected: %v", err)
	}

	movements, err := models.AccountMovements.Query(sm.Where(models.AccountMovements.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(movements) != 1 || movements[0].ID != generatedUUID(created.ID) {
		t.Fatalf("failed or replayed requests created extra movements: movements=%#v error=%v", movements, err)
	}
	confirmations, err := models.DocumentConfirmations.Query(sm.Where(models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(confirmations) != 1 || confirmations[0].PurchaseID.Valid || confirmations[0].CorrectionID.Valid || !confirmations[0].AccountMovementID.Valid || confirmations[0].AccountMovementID.V != generatedUUID(created.ID) {
		t.Fatalf("statement row was not linked to exactly one movement: confirmations=%#v error=%v", confirmations, err)
	}
	journalEntries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(movements[0].JournalID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(journalEntries) != 2 || journalEntries[0].DebitYen+journalEntries[1].DebitYen != 250 || journalEntries[0].CreditYen+journalEntries[1].CreditYen != 250 {
		t.Fatalf("statement transfer did not create both balanced entries: entries=%#v error=%v", journalEntries, err)
	}
}
