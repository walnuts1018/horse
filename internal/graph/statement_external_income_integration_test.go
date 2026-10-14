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

	documentID, occurredAt, reference := uuid.NewV7().String(), testStatementTransferFirstOccurredAt, testStatementRow2Reference
	documentUUID, householdUUID := generatedUUID(documentID), generatedUUID(household.ID)
	hash, objectKey, mimeType, sourceKind := "hash-"+documentID, household.ID+"/originals/"+documentID, testApplicationPDF, "bank"
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
		ID: &documentUUID, HouseholdID: &householdUUID, Sha256: &hash, ObjectKey: &objectKey,
		MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberUUID,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	region := ingestion.Region{Page: 0, Top: 0.2, Left: 0.1, Bottom: 0.3, Right: 0.8}
	verified, err := stdjson.Marshal(ingestion.Verification{Statements: []ingestion.StatementEntry{{
		Row: 2, SourceReference: reference, Kind: "EXTERNAL_INCOME", OccurredAt: occurredAt, Merchant: "Salary", Amount: 72500, Region: &region,
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
		Description: "Salary", IdempotencyKey: uuid.NewV7().String(), SourceDocumentID: &sourceDocumentID, SourceReference: &sourceReference,
	}
	wrongAmount := input
	wrongAmount.AmountYen = 72501
	wrongAmount.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordExternalIncome(ctx, wrongAmount); !errors.Is(err, ErrBadInput) {
		t.Fatalf("statement amount mismatch was not rejected: %v", err)
	}
	wrongDate := input
	wrongDate.OccurredAt = "2026-10-03T12:00:00Z"
	wrongDate.IdempotencyKey = uuid.NewV7().String()
	if _, err := resolver.recordExternalIncome(ctx, wrongDate); !errors.Is(err, ErrBadInput) {
		t.Fatalf("statement date mismatch was not rejected: %v", err)
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

	incomes, err := models.AccountIncomes.Query(sm.Where(models.AccountIncomes.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(incomes) != 1 {
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
	entries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(incomes[0].JournalID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(entries) != 2 || entries[0].DebitYen+entries[1].DebitYen != entries[0].CreditYen+entries[1].CreditYen {
		t.Fatalf("income did not create exactly two balanced journal entries: entries=%#v error=%v", entries, err)
	}
}
