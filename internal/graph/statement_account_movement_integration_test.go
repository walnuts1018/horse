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
	testStatementTransferThirdOccurredAt  = "2026-10-03T12:00:00Z"
	testBankStatementSourceKind           = "bank"
	testStatementRow3Reference            = "row:3"
	testStatementRow2Reference            = "row:2"
	testStatementRow4Reference            = "row:4"
	testStatementRow5Reference            = "row:5"
	testStatementRow7Reference            = "row:7"
	testStatementSalaryDescription        = "Salary"
	testAccountCardBillDescription        = "Credit card bill"
	testOperatingAccountName              = "Operating"
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
	fromAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: testOperatingAccountName, Kind: model.AccountKindBank})
	if err != nil {
		t.Fatal(err)
	}
	toAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Reserve", Kind: model.AccountKindBank})
	if err != nil {
		t.Fatal(err)
	}
	cardAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Credit card", Kind: model.AccountKindCard})
	if err != nil {
		t.Fatal(err)
	}

	documentID := uuid.NewV7().String()
	documentUUID, householdUUID := generatedUUID(documentID), generatedUUID(household.ID)
	hash, objectKey, mimeType, sourceKind := "hash-"+documentID, household.ID+"/originals/"+documentID, testApplicationPDF, testBankStatementSourceKind
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
		{Row: 2, SourceReference: testStatementRow2Reference, Kind: "ACCOUNT_TRANSFER", OccurredAt: testStatementTransferFirstOccurredAt, Merchant: testAccountTransferDescription, Amount: -250, Region: &region},
		{Row: 3, SourceReference: testStatementRow3Reference, Kind: "ACCOUNT_TRANSFER", OccurredAt: testStatementTransferSecondOccurredAt, Merchant: testAccountTransferDescription, Amount: -300, Region: &region},
		{Row: 4, SourceReference: testStatementRow4Reference, Kind: "CARD_BILL_PAYMENT", OccurredAt: testStatementTransferSecondOccurredAt, Merchant: testAccountCardBillDescription, Amount: -800, Region: &region},
		{Row: 5, SourceReference: testStatementRow5Reference, Kind: "CARD_BILL_PAYMENT", OccurredAt: testStatementTransferSecondOccurredAt, Merchant: testAccountCardBillDescription, Amount: -500, Region: &region},
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
	sourceDocumentID, sourceReference, sourceAccountID := documentID, testStatementRow2Reference, fromAccount.ID
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
	badAmount.SourceReference = new(testStatementRow3Reference)
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
	wrongSourceKind.SourceReference = new(testStatementRow2Reference)
	wrongSourceKind.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordAccountMovement(ctx, wrongSourceKind); !errors.Is(err, ErrBadInput) {
		t.Fatalf("non-bank statement source was not rejected: %v", err)
	}

	cardBillReference := testStatementRow4Reference
	cardBillInput := model.RecordAccountMovementInput{
		HouseholdID: household.ID, Kind: model.AccountMovementKindCardBillPayment,
		FromAccountID: fromAccount.ID, ToAccountID: cardAccount.ID, AmountYen: 800,
		OccurredAt: testStatementTransferSecondOccurredAt, Description: testAccountCardBillDescription, IdempotencyKey: uuid.NewV7().String(),
		SourceDocumentID: &sourceDocumentID, SourceReference: &cardBillReference, SourceAccountID: &sourceAccountID,
	}
	badCardBillAmount := cardBillInput
	badCardBillAmount.AmountYen = 801
	badCardBillAmount.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordAccountMovement(ctx, badCardBillAmount); !errors.Is(err, ErrBadInput) {
		t.Fatalf("card bill amount mismatch was not rejected: %v", err)
	}
	wrongCardBillAccount := cardBillInput
	wrongCardBillAccount.SourceAccountID = &toAccount.ID
	wrongCardBillAccount.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordAccountMovement(ctx, wrongCardBillAccount); !errors.Is(err, ErrBadInput) {
		t.Fatalf("card bill statement account mismatch was not rejected: %v", err)
	}
	wrongCardBillRow := cardBillInput
	wrongCardBillRow.SourceReference = new(testStatementRow3Reference)
	wrongCardBillRow.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordAccountMovement(ctx, wrongCardBillRow); !errors.Is(err, ErrBadInput) {
		t.Fatalf("non-card-bill statement row was not rejected: %v", err)
	}
	existingMovementInput := badAmount
	existingMovementInput.AmountYen = 300
	existingMovementInput.SourceDocumentID = nil
	existingMovementInput.SourceReference = nil
	existingMovementInput.SourceAccountID = nil
	existingMovementInput.IdempotencyKey = uuid.NewV7().String()
	existingMovement, err := resolver.recordAccountMovement(ctx, existingMovementInput)
	if err != nil {
		t.Fatal(err)
	}
	linkInput := model.LinkStatementAccountMovementInput{
		HouseholdID: household.ID, AccountMovementID: existingMovement.ID, SourceDocumentID: documentID,
		SourceReference: testStatementRow3Reference, SourceAccountID: fromAccount.ID,
	}
	wrongLinkAccount := linkInput
	wrongLinkAccount.SourceAccountID = toAccount.ID
	if _, err := resolver.linkStatementAccountMovement(ctx, wrongLinkAccount); !errors.Is(err, ErrBadInput) {
		t.Fatalf("statement bank account mismatch was not rejected: %v", err)
	}
	wrongLinkMovement := linkInput
	wrongLinkMovement.AccountMovementID = created.ID
	if _, err := resolver.linkStatementAccountMovement(ctx, wrongLinkMovement); !errors.Is(err, ErrBadInput) {
		t.Fatalf("movement amount mismatch was not rejected: %v", err)
	}
	linkedMovement, err := resolver.linkStatementAccountMovement(ctx, linkInput)
	if err != nil || linkedMovement.ID != existingMovement.ID {
		t.Fatalf("existing movement was not linked: result=%#v error=%v", linkedMovement, err)
	}
	if _, err := resolver.linkStatementAccountMovement(ctx, linkInput); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate movement link was not rejected: %v", err)
	}
	existingCardBillInput := cardBillInput
	existingCardBillInput.AmountYen = 500
	existingCardBillInput.SourceDocumentID = nil
	existingCardBillInput.SourceReference = nil
	existingCardBillInput.SourceAccountID = nil
	existingCardBillInput.IdempotencyKey = uuid.NewV7().String()
	existingCardBill, err := resolver.recordAccountMovement(ctx, existingCardBillInput)
	if err != nil {
		t.Fatal(err)
	}
	cardBillLink := model.LinkStatementAccountMovementInput{
		HouseholdID: household.ID, AccountMovementID: existingCardBill.ID, SourceDocumentID: documentID,
		SourceReference: "row:5", SourceAccountID: fromAccount.ID,
	}
	wrongCardBillLink := cardBillLink
	wrongCardBillLink.AccountMovementID = existingMovement.ID
	if _, err := resolver.linkStatementAccountMovement(ctx, wrongCardBillLink); !errors.Is(err, ErrBadInput) {
		t.Fatalf("card bill movement kind mismatch was not rejected: %v", err)
	}
	linkedCardBill, err := resolver.linkStatementAccountMovement(ctx, cardBillLink)
	if err != nil || linkedCardBill.ID != existingCardBill.ID {
		t.Fatalf("existing card bill movement was not linked: result=%#v error=%v", linkedCardBill, err)
	}
	if _, err := resolver.linkStatementAccountMovement(ctx, cardBillLink); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate card bill link was not rejected: %v", err)
	}
	cardBillMovement, err := resolver.recordAccountMovement(ctx, cardBillInput)
	if err != nil {
		t.Fatal(err)
	}
	cardBillReplay, err := resolver.recordAccountMovement(ctx, cardBillInput)
	if err != nil || cardBillReplay.ID != cardBillMovement.ID {
		t.Fatalf("same card bill statement request did not return the existing movement: created=%#v replayed=%#v error=%v", cardBillMovement, cardBillReplay, err)
	}
	duplicateCardBill := cardBillInput
	duplicateCardBill.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordAccountMovement(ctx, duplicateCardBill); !errors.Is(err, ErrConflict) {
		t.Fatalf("same card bill row with a different key was not rejected: %v", err)
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
	if err != nil || len(movements) != 4 {
		t.Fatalf("failed or replayed requests created extra movements: movements=%#v error=%v", movements, err)
	}
	confirmations, err := models.DocumentConfirmations.Query(sm.Where(models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(confirmations) != 4 {
		t.Fatalf("confirmed statement rows were not linked to exactly four movements: confirmations=%#v error=%v", confirmations, err)
	}
	for _, confirmation := range confirmations {
		if confirmation.PurchaseID.Valid || confirmation.CorrectionID.Valid || !confirmation.AccountMovementID.Valid {
			t.Fatalf("statement row was not linked to an account movement: %#v", confirmation)
		}
		if confirmation.SourceReference == testStatementRow2Reference && confirmation.AccountMovementID.V != generatedUUID(created.ID) || confirmation.SourceReference == testStatementRow3Reference && confirmation.AccountMovementID.V != generatedUUID(existingMovement.ID) || confirmation.SourceReference == cardBillReference && confirmation.AccountMovementID.V != generatedUUID(cardBillMovement.ID) || confirmation.SourceReference == testStatementRow5Reference && confirmation.AccountMovementID.V != generatedUUID(existingCardBill.ID) {
			t.Fatalf("statement row was linked to the wrong movement: %#v", confirmation)
		}
	}
	cardBillJournalID := movements[0].JournalID
	for _, movement := range movements {
		if movement.ID == generatedUUID(cardBillMovement.ID) {
			cardBillJournalID = movement.JournalID
		}
	}
	journalEntries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(cardBillJournalID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(journalEntries) != 2 || journalEntries[0].DebitYen+journalEntries[1].DebitYen != 800 || journalEntries[0].CreditYen+journalEntries[1].CreditYen != 800 {
		t.Fatalf("card bill payment did not create both balanced entries: entries=%#v error=%v", journalEntries, err)
	}
	for _, entry := range journalEntries {
		if entry.DebitYen == 800 && (!entry.AccountID.Valid || entry.AccountID.V != generatedUUID(cardAccount.ID)) || entry.CreditYen == 800 && (!entry.AccountID.Valid || entry.AccountID.V != generatedUUID(fromAccount.ID)) {
			t.Fatalf("card bill payment did not debit the card and credit the bank: %#v", journalEntries)
		}
	}
	assertHouseholdJournalsBalance(t, ctx, db, householdUUID)
}
