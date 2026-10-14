package graph

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const testReceiptSourceKind = "receipt"

const extractionJobSucceededStatus = "succeeded"

const testSplitBreadItemName = "Bread"

func TestCreateAndPostPurchaseIsAtomicAndIdempotent(t *testing.T) {
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
	t.Cleanup(db.Close)
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	memberID := uuid.NewV7().String()
	memberUUID := generatedUUID(memberID)
	subject, memberName := "create-and-post:"+memberID, "Create and post integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: memberName})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Create and post integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	pool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Purchase pool", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	account, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Purchase card", Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}

	successDocument := createAndPostSourceDocument(t, ctx, db, householdUUID, memberUUID, "atomic-success")
	successDocumentID := successDocument.ID.String()
	extractionID, extractionVersion, extractionStatus := generatedUUID(uuid.NewV7().String()), int32(1), extractionJobSucceededStatus
	verification := ingestion.Verification{
		Receipt: ingestion.Receipt{Total: 120, Items: []ingestion.Item{{
			PrintedName: "Atomic printed item", Name: testItemName, PrintedAmount: 120, GrossAmount: 120,
			Region: &ingestion.Region{Page: 1, Top: 0.1, Left: 0.2, Bottom: 0.3, Right: 0.4},
		}}},
		Transform: [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}, TransformPage: 1, Balanced: true,
	}
	verificationJSON, err := json.Marshal(verification)
	if err != nil {
		t.Fatal(err)
	}
	extractionResult := sql.Null[types.JSON[json.RawMessage]]{V: types.JSON[json.RawMessage]{Val: verificationJSON}, Valid: true}
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
		ID: &extractionID, HouseholdID: &householdUUID, SourceDocumentID: &successDocument.ID,
		Version: &extractionVersion, Status: &extractionStatus, Result: &extractionResult,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	input := model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: testPurchaseTime, Merchant: "Atomic purchase",
		SourceDocumentID:   &successDocumentID,
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 120}},
		Items:              []*model.PurchaseItemInput{{Name: testItemName, AmountYen: 120}},
	}
	createdPurchase, err := resolver.createAndPostPurchase(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if createdPurchase.Status != model.PurchaseStatusPosted || createdPurchase.Revision != 2 {
		t.Fatalf("purchase state = %s at revision %d, want POSTED at revision 2", createdPurchase.Status, createdPurchase.Revision)
	}
	purchaseUUID := generatedUUID(createdPurchase.ID)
	assertSourceDocumentLinked(t, ctx, db, householdUUID, successDocument.ID, purchaseUUID)
	assertReceiptItemEvidenceLinked(t, ctx, db, householdUUID, successDocument.ID, purchaseUUID, extractionID, testItemName)
	retried, err := resolver.createAndPostPurchase(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if retried.ID != createdPurchase.ID || retried.Status != model.PurchaseStatusPosted || retried.Revision != 2 {
		t.Fatalf("idempotent retry returned a different purchase: %#v", retried)
	}

	splitDocument := createAndPostSourceDocument(t, ctx, db, householdUUID, memberUUID, "atomic-split")
	splitDocumentID := splitDocument.ID.String()
	splitExtractionID, splitVersion, splitStatus := generatedUUID(uuid.NewV7().String()), int32(1), extractionJobSucceededStatus
	splitVerification := ingestion.Verification{
		Receipt: ingestion.Receipt{Total: 120, Items: []ingestion.Item{{
			PrintedName: "Printed bundle", Name: "Printed bundle", PrintedAmount: 120, GrossAmount: 120,
		}}},
		Transform: [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}, TransformPage: 1,
	}
	splitVerificationJSON, err := json.Marshal(splitVerification)
	if err != nil {
		t.Fatal(err)
	}
	splitResult := sql.Null[types.JSON[json.RawMessage]]{V: types.JSON[json.RawMessage]{Val: splitVerificationJSON}, Valid: true}
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
		ID: &splitExtractionID, HouseholdID: &householdUUID, SourceDocumentID: &splitDocument.ID,
		Version: &splitVersion, Status: &splitStatus, Result: &splitResult,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	splitInput := model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: testPurchaseTime, Merchant: "Split purchase",
		SourceDocumentID:   &splitDocumentID,
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 120}},
		Items:              []*model.PurchaseItemInput{{Name: "Milk", AmountYen: 45}, {Name: testSplitBreadItemName, AmountYen: 75}},
	}
	splitPurchase, err := resolver.createAndPostPurchase(ctx, splitInput)
	if err != nil {
		t.Fatal(err)
	}
	assertReceiptSplitEvidence(t, ctx, db, householdUUID, splitDocument.ID, generatedUUID(splitPurchase.ID), splitExtractionID)

	keyRows, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(input.IdempotencyKey)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(keyRows) != 2 {
		t.Fatalf("idempotency records = %d, want one create and one post record", len(keyRows))
	}
	journals, err := models.Journals.Query(sm.Where(psql.And(
		models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Journals.Columns.SourceKind.EQ(psql.Arg("PURCHASE")),
		models.Journals.Columns.SourceID.EQ(psql.Arg(createdPurchase.ID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	ledgers, err := models.LedgerEntries.Query(sm.Where(psql.And(
		models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.LedgerEntries.Columns.SourcePurchaseID.EQ(psql.Arg(sql.Null[gofrsuuid.UUID]{V: purchaseUUID, Valid: true})),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(journals) != 1 || len(ledgers) != 1 {
		t.Fatalf("purchase journals=%d ledgers=%d, want exactly one of each", len(journals), len(ledgers))
	}
	assertHouseholdJournalsBalance(t, ctx, db, householdUUID)

	document := createAndPostSourceDocument(t, ctx, db, householdUUID, memberUUID, "atomic-rollback")
	documentID := document.ID.String()
	failedInput := model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-11-13T10:00:00+09:00", Merchant: "Atomic rollback",
		SourceDocumentID: &documentID, Items: []*model.PurchaseItemInput{{Name: testItemName, AmountYen: 0}},
	}
	if _, err := resolver.createAndPostPurchase(ctx, failedInput); !errors.Is(err, purchase.ErrBadInput) {
		t.Fatalf("zero-total purchase error = %v, want bad input", err)
	}
	assertSourceDocumentUnlinked(t, ctx, db, householdUUID, document.ID)
	assertPurchaseAbsent(t, ctx, db, householdUUID, "Atomic rollback")
	failedKeys, err := models.IdempotencyKeys.Query(sm.Where(psql.And(
		models.IdempotencyKeys.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.IdempotencyKeys.Columns.Key.EQ(psql.Arg(failedInput.IdempotencyKey)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(failedKeys) != 0 {
		t.Fatalf("failed create-and-post left %d idempotency records", len(failedKeys))
	}

	closedPeriodDocument := createAndPostSourceDocument(t, ctx, db, householdUUID, memberUUID, "closed-period")
	if _, err := resolver.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: "2026-12", ExpectedRevision: 0}); err != nil {
		t.Fatal(err)
	}
	closedPeriodDocumentID := closedPeriodDocument.ID.String()
	closedInput := model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-12-13T10:00:00+09:00", Merchant: "Closed period",
		SourceDocumentID: &closedPeriodDocumentID, Items: []*model.PurchaseItemInput{{Name: testItemName, AmountYen: 100}},
	}
	if _, err := resolver.createAndPostPurchase(ctx, closedInput); !errors.Is(err, purchase.ErrConflict) {
		t.Fatalf("closed period error = %v, want conflict", err)
	}
	assertSourceDocumentUnlinked(t, ctx, db, householdUUID, closedPeriodDocument.ID)
	assertPurchaseAbsent(t, ctx, db, householdUUID, "Closed period")
}

func createAndPostSourceDocument(t *testing.T, ctx context.Context, db *pgxpool.Pool, householdID, memberID gofrsuuid.UUID, key string) *models.SourceDocument {
	t.Helper()
	id, digest, objectKey, mimeType, sourceKind := generatedUUID(uuid.NewV7().String()), key, "test/"+key, "application/x-test", testReceiptSourceKind
	metadata := types.JSON[json.RawMessage]{Val: json.RawMessage(`{}`)}
	returnValue, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
		ID: &id, HouseholdID: &householdID, Sha256: &digest, ObjectKey: &objectKey, MimeType: &mimeType,
		SourceKind: &sourceKind, Metadata: &metadata, CreatedBy: &memberID,
	}).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	return returnValue
}

func assertSourceDocumentUnlinked(t *testing.T, ctx context.Context, db *pgxpool.Pool, householdID gofrsuuid.UUID, documentID gofrsuuid.UUID) {
	t.Helper()
	confirmations, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	evidenceLinks, err := models.EvidenceLinks.Query(sm.Where(psql.And(
		models.EvidenceLinks.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.EvidenceLinks.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(confirmations) != 0 || len(evidenceLinks) != 0 {
		t.Fatalf("document retained %d confirmations and %d evidence links", len(confirmations), len(evidenceLinks))
	}
}

func assertSourceDocumentLinked(t *testing.T, ctx context.Context, db *pgxpool.Pool, householdID, documentID, purchaseID gofrsuuid.UUID) {
	t.Helper()
	confirmations, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
		models.DocumentConfirmations.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	evidenceLinks, err := models.EvidenceLinks.Query(sm.Where(psql.And(
		models.EvidenceLinks.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.EvidenceLinks.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
		models.EvidenceLinks.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(confirmations) != 1 || len(evidenceLinks) != 1 || !evidenceLinks[0].ItemID.Valid {
		t.Fatalf("posted purchase has %d confirmations and %d evidence links, want one confirmation and one item evidence link", len(confirmations), len(evidenceLinks))
	}
}

func assertReceiptItemEvidenceLinked(t *testing.T, ctx context.Context, db *pgxpool.Pool, householdID, documentID, purchaseID, extractionID gofrsuuid.UUID, itemName string) {
	t.Helper()
	executor := bobpgx.NewPool(db)
	evidenceLinks, err := models.EvidenceLinks.Query(sm.Where(psql.And(
		models.EvidenceLinks.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.EvidenceLinks.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
		models.EvidenceLinks.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
	))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidenceLinks) != 1 {
		t.Fatalf("receipt evidence links = %d, want one item link without a generic link", len(evidenceLinks))
	}
	link := evidenceLinks[0]
	if !link.ItemID.Valid || !link.EvidenceRegion.Valid {
		t.Fatalf("receipt evidence is missing item id or region: %#v", link)
	}
	items, err := models.PurchaseItems.Query(sm.Where(psql.And(
		models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
		models.PurchaseItems.Columns.Name.EQ(psql.Arg(itemName)),
	))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != link.ItemID.V {
		t.Fatalf("evidence item id = %v, purchase item ids = %#v", link.ItemID, items)
	}
	var observation map[string]json.RawMessage
	if err := json.Unmarshal(link.Observation.Val, &observation); err != nil {
		t.Fatal(err)
	}
	var jobID string
	if err := json.Unmarshal(observation["jobId"], &jobID); err != nil {
		t.Fatal(err)
	}
	var index int
	if err := json.Unmarshal(observation["itemIndex"], &index); err != nil {
		t.Fatal(err)
	}
	var observedItem ingestion.Item
	if err := json.Unmarshal(observation["item"], &observedItem); err != nil {
		t.Fatal(err)
	}
	var receiptTotal, confirmedTotal int64
	if err := json.Unmarshal(observation["receiptTotal"], &receiptTotal); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(observation["confirmedTotal"], &confirmedTotal); err != nil {
		t.Fatal(err)
	}
	if jobID != extractionID.String() || index != 0 || observedItem.PrintedName != "Atomic printed item" || observedItem.Name != itemName {
		t.Fatalf("unexpected receipt evidence observation: %#v", observation)
	}
	if receiptTotal != 120 || confirmedTotal != 120 {
		t.Fatalf("receipt total=%d confirmed total=%d, want both 120", receiptTotal, confirmedTotal)
	}
	var confirmedItem struct {
		PurchaseItemID string `json:"purchaseItemId"`
		Name           string `json:"name"`
		AmountYen      int64  `json:"amountYen"`
	}
	if err := json.Unmarshal(observation["confirmedItem"], &confirmedItem); err != nil {
		t.Fatal(err)
	}
	if confirmedItem.Name != itemName || confirmedItem.AmountYen != 120 {
		t.Fatalf("unexpected confirmed receipt item: %#v", confirmedItem)
	}
}

func assertReceiptSplitEvidence(t *testing.T, ctx context.Context, db *pgxpool.Pool, householdID, documentID, purchaseID, extractionID gofrsuuid.UUID) {
	t.Helper()
	evidenceLinks, err := models.EvidenceLinks.Query(sm.Where(psql.And(
		models.EvidenceLinks.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.EvidenceLinks.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
		models.EvidenceLinks.Columns.PurchaseID.EQ(psql.Arg(purchaseID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(evidenceLinks) != 1 || evidenceLinks[0].ItemID.Valid {
		t.Fatalf("split receipt evidence links = %#v, want one document-level link", evidenceLinks)
	}
	var observation struct {
		JobID          string `json:"jobId"`
		ConfirmedTotal int64  `json:"confirmedTotal"`
		Receipt        struct {
			Total int64 `json:"total"`
			Items []any `json:"items"`
		} `json:"receipt"`
		ConfirmedItems []struct {
			PurchaseItemID string `json:"purchaseItemId"`
			Name           string `json:"name"`
			AmountYen      int64  `json:"amountYen"`
		} `json:"confirmedItems"`
	}
	if err := json.Unmarshal(evidenceLinks[0].Observation.Val, &observation); err != nil {
		t.Fatal(err)
	}
	if observation.JobID != extractionID.String() || observation.Receipt.Total != 120 || observation.ConfirmedTotal != 120 || len(observation.Receipt.Items) != 1 {
		t.Fatalf("original receipt evidence = %#v", observation)
	}
	if len(observation.ConfirmedItems) != 2 || observation.ConfirmedItems[0].Name != "Milk" || observation.ConfirmedItems[0].AmountYen != 45 || observation.ConfirmedItems[1].Name != testSplitBreadItemName || observation.ConfirmedItems[1].AmountYen != 75 {
		t.Fatalf("confirmed split evidence = %#v", observation.ConfirmedItems)
	}
}

func assertPurchaseAbsent(t *testing.T, ctx context.Context, db *pgxpool.Pool, householdID gofrsuuid.UUID, merchant string) {
	t.Helper()
	purchases, err := models.Purchases.Query(sm.Where(psql.And(
		models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Purchases.Columns.Merchant.EQ(psql.Arg(merchant)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(purchases) != 0 {
		t.Fatalf("failed create-and-post left %d purchases for %q", len(purchases), merchant)
	}
}
