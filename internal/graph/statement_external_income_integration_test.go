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

func TestStatementExternalIncomeConfirmationIsAtomicAndIdempotent(t *testing.T) {
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
	subject, memberName := "statement-income:"+memberID, "Statement income"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: memberName})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Statement external income"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(household.ID))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	account, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: testOperatingAccountName, Kind: model.AccountKindBank})
	if err != nil {
		t.Fatal(err)
	}
	cashAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Statement income cash", Kind: model.AccountKindCash})
	if err != nil {
		t.Fatal(err)
	}
	otherBank, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Statement income other bank", Kind: model.AccountKindBank})
	if err != nil {
		t.Fatal(err)
	}

	documentID, occurredAt, reference := uuid.NewV7().String(), testStatementTransferFirstOccurredAt, testStatementRow2Reference
	documentUUID, householdUUID := generatedUUID(documentID), generatedUUID(household.ID)
	hash, objectKey, mimeType, sourceKind := "hash-"+documentID, household.ID+"/originals/"+documentID, testApplicationPDF, testBankStatementSourceKind
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
		ID: &documentUUID, HouseholdID: &householdUUID, Sha256: &hash, ObjectKey: &objectKey,
		MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberUUID,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	region := ingestion.Region{Page: 0, Top: 0.2, Left: 0.1, Bottom: 0.3, Right: 0.8}
	verified, err := stdjson.Marshal(ingestion.Verification{Statements: []ingestion.StatementEntry{{
		Row: 2, SourceReference: reference, Kind: "EXTERNAL_INCOME", OccurredAt: occurredAt, Merchant: testStatementSalaryDescription, Amount: 72500, Region: &region,
	}}})
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
	sourceDocumentID := documentID
	sourceReference := reference
	input := model.RecordExternalIncomeInput{
		HouseholdID: household.ID, AccountID: account.ID, AmountYen: 72500, OccurredAt: model.DateTime(occurredAt),
		Description: testStatementSalaryDescription, IdempotencyKey: uuid.NewV7().String(), SourceDocumentID: &sourceDocumentID, SourceReference: &sourceReference,
	}
	wrongAmount := input
	wrongAmount.AmountYen = 72501
	wrongAmount.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordExternalIncome(ctx, wrongAmount); !errors.Is(err, ErrBadInput) {
		t.Fatalf("statement amount mismatch was not rejected: %v", err)
	}
	wrongDate := input
	wrongDate.OccurredAt = testStatementTransferThirdOccurredAt
	wrongDate.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordExternalIncome(ctx, wrongDate); !errors.Is(err, ErrBadInput) {
		t.Fatalf("statement date mismatch was not rejected: %v", err)
	}
	wrongAccount := input
	wrongAccount.AccountID = cashAccount.ID
	wrongAccount.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordExternalIncome(ctx, wrongAccount); !errors.Is(err, ErrBadInput) {
		t.Fatalf("bank statement income was accepted for a cash account: %v", err)
	}
	created, err := resolver.recordExternalIncome(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := resolver.recordExternalIncome(ctx, input)
	if err != nil || replayed.ID != created.ID {
		t.Fatalf("same statement request did not return the existing income: created=%#v replayed=%#v error=%v", created, replayed, err)
	}
	duplicate := input
	duplicate.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordExternalIncome(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("same statement row with a different key was not rejected: %v", err)
	}

	linkDocumentID := uuid.NewV7().String()
	linkDocumentUUID := generatedUUID(linkDocumentID)
	linkHash, linkObjectKey := "hash-"+linkDocumentID, household.ID+"/originals/"+linkDocumentID
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
		ID: &linkDocumentUUID, HouseholdID: &householdUUID, Sha256: &linkHash, ObjectKey: &linkObjectKey,
		MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberUUID,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	linkReference := testStatementRow7Reference
	linkVerified, err := stdjson.Marshal(ingestion.Verification{Statements: []ingestion.StatementEntry{{
		Row: 7, SourceReference: linkReference, Kind: "EXTERNAL_INCOME", OccurredAt: occurredAt, Merchant: testStatementSalaryDescription, Amount: 72500, Region: &region,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	linkJobID, linkVersion := generatedUUID(uuid.NewV7().String()), int32(1)
	linkResult := sql.Null[types.JSON[stdjson.RawMessage]]{V: types.JSON[stdjson.RawMessage]{Val: linkVerified}, Valid: true}
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
		ID: &linkJobID, HouseholdID: &householdUUID, SourceDocumentID: &linkDocumentUUID,
		Version: &linkVersion, Status: &status, Result: &linkResult,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	otherAccountIncome, err := resolver.recordExternalIncome(ctx, model.RecordExternalIncomeInput{
		HouseholdID: household.ID, AccountID: otherBank.ID, AmountYen: 72500, OccurredAt: model.DateTime(occurredAt),
		Description: testStatementSalaryDescription, IdempotencyKey: uuid.NewV7().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	wrongAmountIncome, err := resolver.recordExternalIncome(ctx, model.RecordExternalIncomeInput{
		HouseholdID: household.ID, AccountID: account.ID, AmountYen: 72501, OccurredAt: model.DateTime(occurredAt),
		Description: testStatementSalaryDescription, IdempotencyKey: uuid.NewV7().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	wrongDateIncome, err := resolver.recordExternalIncome(ctx, model.RecordExternalIncomeInput{
		HouseholdID: household.ID, AccountID: account.ID, AmountYen: 72500, OccurredAt: testStatementTransferThirdOccurredAt,
		Description: testStatementSalaryDescription, IdempotencyKey: uuid.NewV7().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	linkInput := model.LinkStatementAccountIncomeInput{
		HouseholdID: household.ID, AccountIncomeID: otherAccountIncome.ID, SourceDocumentID: linkDocumentID,
		SourceReference: linkReference, SourceAccountID: account.ID,
	}
	if _, err := resolver.linkStatementAccountIncome(ctx, linkInput); !errors.Is(err, ErrBadInput) {
		t.Fatalf("income from a different bank account was accepted: %v", err)
	}
	linkInput.AccountIncomeID = wrongAmountIncome.ID
	if _, err := resolver.linkStatementAccountIncome(ctx, linkInput); !errors.Is(err, ErrBadInput) {
		t.Fatalf("income with a different amount was accepted: %v", err)
	}
	linkInput.AccountIncomeID = wrongDateIncome.ID
	if _, err := resolver.linkStatementAccountIncome(ctx, linkInput); !errors.Is(err, ErrBadInput) {
		t.Fatalf("income with a different date was accepted: %v", err)
	}
	linkInput.AccountIncomeID = created.ID
	linked, err := resolver.linkStatementAccountIncome(ctx, linkInput)
	if err != nil || linked.ID != created.ID {
		t.Fatalf("existing income was not linked: income=%#v error=%v", linked, err)
	}
	if _, err := resolver.linkStatementAccountIncome(ctx, linkInput); !errors.Is(err, ErrConflict) {
		t.Fatalf("same statement row was linked twice: %v", err)
	}

	incomes, err := models.AccountIncomes.Query(sm.Where(models.AccountIncomes.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(incomes) != 4 {
		t.Fatalf("failed or replayed requests created extra incomes: incomes=%#v error=%v", incomes, err)
	}
	incomeUUID := generatedUUID(created.ID)
	confirmations, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(reference)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(confirmations) != 1 || !confirmations[0].AccountIncomeID.Valid || confirmations[0].AccountIncomeID.V != incomeUUID {
		t.Fatalf("statement row was not linked to its income: confirmations=%#v error=%v", confirmations, err)
	}
	linkedConfirmations, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(linkDocumentUUID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(linkReference)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(linkedConfirmations) != 1 || !linkedConfirmations[0].AccountIncomeID.Valid || linkedConfirmations[0].AccountIncomeID.V != incomeUUID {
		t.Fatalf("source row was not linked to the existing income: confirmations=%#v error=%v", linkedConfirmations, err)
	}
	entries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(incomes[0].JournalID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(entries) != 2 || entries[0].DebitYen+entries[1].DebitYen != entries[0].CreditYen+entries[1].CreditYen {
		t.Fatalf("income did not create exactly two balanced journal entries: entries=%#v error=%v", entries, err)
	}
	assertHouseholdJournalsBalance(t, ctx, db, householdUUID)
}
