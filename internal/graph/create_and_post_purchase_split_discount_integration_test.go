package graph

import (
	"context"
	"database/sql"
	"encoding/json"
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

func TestCreateAndPostPurchaseConfirmsDiscountedReceiptAcrossPaymentsAndPools(t *testing.T) {
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
	subject, memberName := "receipt-split:"+memberID, "Receipt split integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &memberName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: memberName})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "Receipt split integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	poolA, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Groceries A", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	poolB, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Groceries B", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	for _, poolID := range []string{poolA.ID, poolB.ID} {
		if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{
			HouseholdID: household.ID, PoolID: poolID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	accountA, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Card A", Kind: model.AccountKindCard, PoolID: &poolA.ID})
	if err != nil {
		t.Fatal(err)
	}
	accountB, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Card B", Kind: model.AccountKindCard, PoolID: &poolB.ID})
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := ingestion.Verify(ingestion.Receipt{
		Merchant: "Synthetic market", PurchasedAt: "2026-10-15T10:00:00+09:00", DatePrecision: "DATE_TIME", Currency: "JPY", Total: 281, Discount: -9,
		Items: []ingestion.Item{
			{PrintedName: "Printed rice", Name: "Rice", PrintedAmount: 100, Discount: -10, PriceBasis: "EXEMPT", Confidence: 1},
			{PrintedName: "Printed tea", Name: "Tea", PrintedAmount: 200, PriceBasis: "EXEMPT", Confidence: 1},
		},
		Payments: []ingestion.Payment{{Method: "card", Amount: 141}, {Method: "card", Amount: 140}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Balanced || receipt.ComputedTotal != 281 || receipt.Receipt.Items[0].GrossAmount != 87 || receipt.Receipt.Items[1].GrossAmount != 194 {
		t.Fatalf("receipt verification = %#v, want balanced discounted total 281 with item amounts 87 and 194", receipt)
	}

	document := createAndPostSourceDocument(t, ctx, db, householdUUID, memberUUID, "split-discount")
	extractionID, version, status := generatedUUID(uuid.NewV7().String()), int32(1), extractionJobSucceededStatus
	verificationJSON, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	result := sql.Null[types.JSON[json.RawMessage]]{V: types.JSON[json.RawMessage]{Val: verificationJSON}, Valid: true}
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
		ID: &extractionID, HouseholdID: &householdUUID, SourceDocumentID: &document.ID,
		Version: &version, Status: &status, Result: &result,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	documentID := document.ID.String()
	purchase, err := resolver.createAndPostPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-10-15T01:00:00Z", Merchant: receipt.Receipt.Merchant,
		SourceDocumentID: &documentID,
		PaymentAllocations: []*model.PurchasePaymentInput{
			{PaymentAccountID: accountA.ID, AmountYen: 141},
			{PaymentAccountID: accountB.ID, AmountYen: 140},
		},
		Items: []*model.PurchaseItemInput{
			{Name: "Rice", AmountYen: 87, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 2}, {PoolID: poolB.ID, Weight: 1}}},
			{Name: "Tea", AmountYen: 194, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 1}, {PoolID: poolB.ID, Weight: 2}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if purchase.Status != model.PurchaseStatusPosted || purchase.TotalYen != 281 || len(purchase.Items) != 2 || len(purchase.PaymentAllocations) != 2 {
		t.Fatalf("created purchase = %#v, want posted total 281 with two items and two payments", purchase)
	}

	purchaseUUID := generatedUUID(purchase.ID)
	payments, err := models.PurchasePayments.Query(
		sm.Where(models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID))),
		sm.OrderBy(models.PurchasePayments.Columns.LineNo),
	).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(payments) != 2 || payments[0].AccountID != generatedUUID(accountA.ID) || payments[0].AmountYen != 141 || payments[1].AccountID != generatedUUID(accountB.ID) || payments[1].AmountYen != 140 {
		t.Fatalf("persisted split payments = %#v, want 141 and 140 on separate accounts", payments)
	}

	items, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	allocations, err := models.ItemAllocations.Query(
		sm.Where(models.ItemAllocations.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID))),
		sm.OrderBy(models.ItemAllocations.Columns.PoolID),
	).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || len(allocations) != 4 {
		t.Fatalf("persisted purchase items=%d allocations=%d, want two items with two pools each", len(items), len(allocations))
	}
	itemNames := make(map[gofrsuuid.UUID]string, len(items))
	for _, item := range items {
		itemNames[item.ID] = item.Name
	}
	allocatedByPool := make(map[string]int64, 2)
	allocatedByItem := make(map[string]map[string]int64, 2)
	for _, allocation := range allocations {
		poolID := allocation.PoolID.String()
		itemName := itemNames[allocation.ItemID]
		allocatedByPool[poolID] += allocation.AmountYen
		if allocatedByItem[itemName] == nil {
			allocatedByItem[itemName] = make(map[string]int64, 2)
		}
		allocatedByItem[itemName][poolID] = allocation.AmountYen
	}
	if allocatedByItem["Rice"][poolA.ID] != 58 || allocatedByItem["Rice"][poolB.ID] != 29 ||
		allocatedByItem["Tea"][poolA.ID] != 65 || allocatedByItem["Tea"][poolB.ID] != 129 ||
		allocatedByPool[poolA.ID] != 123 || allocatedByPool[poolB.ID] != 158 {
		t.Fatalf("item allocations = %#v by pool %#v, want Rice 58/29 and Tea 65/129", allocatedByItem, allocatedByPool)
	}

	journals, err := models.Journals.Query(sm.Where(psql.And(
		models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Journals.Columns.SourceKind.EQ(psql.Arg("PURCHASE")),
		models.Journals.Columns.SourceID.EQ(psql.Arg(purchaseUUID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(journals) != 1 {
		t.Fatalf("purchase journals = %d, want one", len(journals))
	}
	entries, err := models.JournalEntries.Query(
		sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(journals[0].ID))),
		sm.OrderBy(models.JournalEntries.Columns.LineNo),
	).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	var debits, credits int64
	debitsByPool := make(map[string]int64, 2)
	creditsByAccount := make(map[string]int64, 2)
	for _, entry := range entries {
		debits += entry.DebitYen
		credits += entry.CreditYen
		if entry.DebitYen > 0 && entry.PoolID.Valid {
			debitsByPool[entry.PoolID.V.String()] += entry.DebitYen
		}
		if entry.CreditYen > 0 && entry.AccountID.Valid {
			creditsByAccount[entry.AccountID.V.String()] += entry.CreditYen
		}
	}
	if debits != 281 || credits != 281 || len(entries) != 4 ||
		debitsByPool[poolA.ID] != 123 || debitsByPool[poolB.ID] != 158 ||
		creditsByAccount[accountA.ID] != 141 || creditsByAccount[accountB.ID] != 140 {
		t.Fatalf("purchase journal entries=%#v debit=%d credit=%d, want balanced pool debits 123/158 and account credits 141/140", entries, debits, credits)
	}

	confirmations, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)),
		models.DocumentConfirmations.Columns.PurchaseID.EQ(psql.Arg(sql.Null[gofrsuuid.UUID]{V: purchaseUUID, Valid: true})),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	evidenceLinks, err := models.EvidenceLinks.Query(sm.Where(psql.And(
		models.EvidenceLinks.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.EvidenceLinks.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)),
		models.EvidenceLinks.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(confirmations) != 1 || len(evidenceLinks) != 2 || !evidenceLinks[0].ItemID.Valid || !evidenceLinks[1].ItemID.Valid {
		t.Fatalf("receipt confirmation=%#v evidence links=%#v, want one confirmation and two item links", confirmations, evidenceLinks)
	}
	for _, link := range evidenceLinks {
		var observation struct {
			Item struct {
				GrossAmount int64 `json:"grossAmount"`
			} `json:"item"`
			ReceiptTotal   int64 `json:"receiptTotal"`
			ConfirmedTotal int64 `json:"confirmedTotal"`
			ConfirmedItem  struct {
				Name      string `json:"name"`
				AmountYen int64  `json:"amountYen"`
			} `json:"confirmedItem"`
		}
		if err := json.Unmarshal(link.Observation.Val, &observation); err != nil {
			t.Fatal(err)
		}
		if itemNames[link.ItemID.V] != observation.ConfirmedItem.Name || observation.Item.GrossAmount != observation.ConfirmedItem.AmountYen ||
			observation.ReceiptTotal != 281 || observation.ConfirmedTotal != 281 {
			t.Fatalf("receipt evidence did not preserve discounted item confirmation: %#v", observation)
		}
	}
	assertHouseholdJournalsBalance(t, ctx, db, householdUUID)
}
