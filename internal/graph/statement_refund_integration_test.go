package graph

import (
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"os"
	"testing"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
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

func TestCorrectPurchaseFromStatementKeepsRowEvidenceAtomic(t *testing.T) {
	t.Parallel()
	const (
		originalShop = "Original shop"
		refundRow    = "row:2"
		excessRow    = "row:3"
	)
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
	subject := "statement-refund:" + memberID
	name := "Statement refund"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: name})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Statement refund"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(household.ID))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	pool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Refund pool", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	account, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Refund card", Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := resolver.createAndPostPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-10-01T12:00:00Z", Merchant: originalShop,
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 1000}},
		Items:              []*model.PurchaseItemInput{{Name: "Original item", AmountYen: 1000, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	documentID := uuid.NewV7().String()
	documentUUID := generatedUUID(documentID)
	householdUUID := generatedUUID(household.ID)
	hash, objectKey, mimeType, sourceKind := "hash-"+documentID, household.ID+"/originals/"+documentID, testApplicationPDF, testCardSourceKind
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &documentUUID, HouseholdID: &householdUUID, Sha256: &hash, ObjectKey: &objectKey, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	region := ingestion.Region{Page: 0, Top: 0.2, Left: 0.1, Bottom: 0.3, Right: 0.8}
	verified, err := stdjson.Marshal(ingestion.Verification{Statements: []ingestion.StatementEntry{
		{Row: 2, SourceReference: refundRow, Kind: "REFUND", OccurredAt: testStatementTransferFirstOccurredAt, Merchant: originalShop, Amount: -250, Region: &region},
		{Row: 3, SourceReference: excessRow, Kind: "REFUND", OccurredAt: testStatementTransferSecondOccurredAt, Merchant: originalShop, Amount: -900, Region: &region},
		{Row: 4, SourceReference: testStatementRow4Reference, Kind: "CARD_PURCHASE", OccurredAt: "2026-10-02T12:02:00Z", Merchant: originalShop, Amount: -100, Region: &region},
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
	input := model.CorrectPurchaseFromStatementInput{
		HouseholdID: household.ID, SourceDocumentID: documentID, SourceReference: refundRow,
		Target: &model.CorrectPurchaseTargetInput{Item: &model.PurchaseCorrectionItemTargetInput{PurchaseID: purchase.ID, ItemID: purchase.Items[0].ID}},
		Reason: "Card refund", IdempotencyKey: uuid.NewV7().String(),
	}
	corrected, err := resolver.correctPurchaseFromStatement(ctx, input)
	if err != nil || corrected.ID != purchase.ID {
		t.Fatalf("statement refund did not correct its selected purchase: purchase=%#v error=%v", corrected, err)
	}
	replay, err := resolver.correctPurchaseFromStatement(ctx, input)
	if err != nil || replay.ID != purchase.ID {
		t.Fatalf("same idempotency key did not return the purchase: purchase=%#v error=%v", replay, err)
	}
	if _, err := resolver.correctPurchaseFromStatement(ctx, model.CorrectPurchaseFromStatementInput{
		HouseholdID: household.ID, SourceDocumentID: documentID, SourceReference: refundRow,
		Target: input.Target, Reason: input.Reason, IdempotencyKey: uuid.NewV7().String(),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("same statement row was confirmed with another key: %v", err)
	}
	itemUUID := generatedUUID(purchase.Items[0].ID)
	corrections, err := models.Corrections.Query(sm.Where(psql.And(models.Corrections.Columns.PurchaseID.EQ(psql.Arg(generatedUUID(purchase.ID))), models.Corrections.Columns.ItemID.EQ(psql.Arg(sql.Null[gofrsuuid.UUID]{V: itemUUID, Valid: true}))))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(corrections) != 1 || corrections[0].AmountDeltaYen != -250 {
		t.Fatalf("refund correction amount or target is wrong: corrections=%#v error=%v", corrections, err)
	}
	confirmations, err := models.DocumentConfirmations.Query(sm.Where(psql.And(models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)), models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(refundRow))))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(confirmations) != 1 || confirmations[0].PurchaseID.Valid || !confirmations[0].CorrectionID.Valid || confirmations[0].CorrectionID.V != corrections[0].ID {
		t.Fatalf("statement row was not confirmed against its correction: confirmations=%#v error=%v", confirmations, err)
	}
	evidence, err := models.CorrectionEvidenceLinks.Query(sm.Where(models.CorrectionEvidenceLinks.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(evidence) != 1 || evidence[0].CorrectionID != corrections[0].ID || !evidence[0].EvidenceRegion.Valid {
		t.Fatalf("refund did not retain its source region: evidence=%#v error=%v", evidence, err)
	}
	var observation struct {
		JobID     string                   `json:"jobId"`
		Statement ingestion.StatementEntry `json:"statement"`
	}
	if err := stdjson.Unmarshal(evidence[0].Observation.Val, &observation); err != nil || observation.JobID != jobID.String() || observation.Statement.SourceReference != refundRow || observation.Statement.Amount != -250 {
		t.Fatalf("refund did not retain its source observation: observation=%#v error=%v", observation, err)
	}
	if _, err := resolver.correctPurchaseFromStatement(ctx, model.CorrectPurchaseFromStatementInput{
		HouseholdID: household.ID, SourceDocumentID: documentID, SourceReference: excessRow,
		Target: input.Target, Reason: input.Reason, IdempotencyKey: uuid.NewV7().String(),
	}); !errors.Is(err, ErrBadInput) {
		t.Fatalf("refund exceeding the item's remaining balance returned %v", err)
	}
	unconfirmed, err := models.DocumentConfirmations.Query(sm.Where(psql.And(models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)), models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(excessRow))))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(unconfirmed) != 0 {
		t.Fatalf("failed refund left a confirmed source row: confirmations=%#v error=%v", unconfirmed, err)
	}
	if _, err := resolver.correctPurchaseFromStatement(ctx, model.CorrectPurchaseFromStatementInput{
		HouseholdID: household.ID, SourceDocumentID: documentID, SourceReference: "row:4",
		Target: input.Target, Reason: input.Reason, IdempotencyKey: uuid.NewV7().String(),
	}); !errors.Is(err, ErrBadInput) {
		t.Fatalf("negative purchase row was accepted as a refund: %v", err)
	}
	otherHousehold, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Other statement household"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(generatedUUID(otherHousehold.ID))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	if _, err := resolver.correctPurchaseFromStatement(ctx, model.CorrectPurchaseFromStatementInput{
		HouseholdID: otherHousehold.ID, SourceDocumentID: documentID, SourceReference: "row:3",
		Target: input.Target, Reason: input.Reason, IdempotencyKey: uuid.NewV7().String(),
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("refund accepted a document from another household: %v", err)
	}
}
