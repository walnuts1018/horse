package graph

import (
	"context"
	"errors"
	"os"
	"testing"

	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func TestAccountingFlowUsesScheduleAndActualFundingSeparately(t *testing.T) {
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	memberA, memberB := uuid.NewV7().String(), uuid.NewV7().String()
	for id, name := range map[string]string{memberA: "A", memberB: "B"} {
		if _, err := db.Exec(ctx, `INSERT INTO members(id,oidc_subject,display_name) VALUES ($1,$2,$3)`, id, "integration:"+id, name); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM members WHERE id=$1`, memberA)
		_, _ = db.Exec(context.Background(), `DELETE FROM members WHERE id=$1`, memberB)
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "integration:" + memberA, DisplayName: "A"})
	apiResolver := &Resolver{DB: db}
	resolver := apiResolver.Mutation()
	household, err := resolver.CreateHousehold(ctx, model.CreateHouseholdInput{Name: "integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdID := household.ID
	defer func() { _, _ = db.Exec(context.Background(), `DELETE FROM households WHERE id=$1`, householdID) }()
	if _, err := db.Exec(ctx, `INSERT INTO household_members(household_id,member_id) VALUES ($1,$2)`, householdID, memberB); err != nil {
		t.Fatal(err)
	}
	poolA, err := resolver.CreatePool(ctx, model.CreatePoolInput{HouseholdID: householdID, Name: "A", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	poolB, err := resolver.CreatePool(ctx, model.CreatePoolInput{HouseholdID: householdID, Name: "B", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	paymentAccount, err := resolver.CreateAccount(ctx, model.CreateAccountInput{HouseholdID: householdID, Name: "Card", Kind: model.AccountKindCard, PoolID: &poolA.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.SetDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: householdID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 3}, {PoolID: poolB.ID, Weight: 2}}}); err != nil {
		t.Fatal(err)
	}
	for _, schedule := range []model.SetFundingScheduleInput{
		{HouseholdID: householdID, MemberID: memberA, PoolID: poolA.ID, AmountYen: 6000, Cadence: "MONTHLY", StartsOn: "2026-01-01"},
		{HouseholdID: householdID, MemberID: memberB, PoolID: poolB.ID, AmountYen: 4000, Cadence: "MONTHLY", StartsOn: "2026-01-01"},
	} {
		if _, err := resolver.SetFundingSchedule(ctx, schedule); err != nil {
			t.Fatal(err)
		}
	}
	category, err := resolver.CreateCategory(ctx, model.CreateCategoryInput{HouseholdID: householdID, Name: "groceries"})
	if err != nil {
		t.Fatal(err)
	}
	category, err = resolver.SetCategoryArchived(ctx, category.ID, true)
	if err != nil || !category.Archived {
		t.Fatalf("category archival failed: category=%#v error=%v", category, err)
	}
	category, err = resolver.SetCategoryArchived(ctx, category.ID, false)
	if err != nil || category.Archived {
		t.Fatalf("category restoration failed: category=%#v error=%v", category, err)
	}
	storeRuleMerchant := "  MARKET  "
	categoryRule, err := resolver.SetAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, CategoryID: &category.ID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 1}, {PoolID: poolB.ID, Weight: 3}}})
	if err != nil || categoryRule.Scope != model.AllocationRuleScopeCategory {
		t.Fatalf("category allocation rule was not saved: rule=%#v error=%v", categoryRule, err)
	}
	storeRule, err := resolver.SetAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, Merchant: &storeRuleMerchant, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 4}, {PoolID: poolB.ID, Weight: 1}}})
	if err != nil || storeRule.Scope != model.AllocationRuleScopeMerchant || *storeRule.Merchant != "market" {
		t.Fatalf("merchant allocation rule was not normalized: rule=%#v error=%v", storeRule, err)
	}
	productConcept, err := resolver.CreateProductConcept(ctx, model.CreateProductConceptInput{HouseholdID: householdID, Name: "Yogurt", CategoryID: &category.ID})
	if err != nil {
		t.Fatal(err)
	}
	productVariant, err := resolver.CreateProductVariant(ctx, model.CreateProductVariantInput{HouseholdID: householdID, ConceptID: productConcept.ID, Name: "Plain yogurt 400g", Brand: new("Private Brand"), PackageSize: new("400g")})
	if err != nil {
		t.Fatal(err)
	}
	productConceptRule, err := resolver.SetAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, ProductConceptID: &productConcept.ID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 1}, {PoolID: poolB.ID, Weight: 4}}})
	if err != nil || productConceptRule.Scope != model.AllocationRuleScopeProduct {
		t.Fatalf("product concept rule was not saved: rule=%#v error=%v", productConceptRule, err)
	}
	conceptPurchase, err := resolver.CreatePurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T11:59:00Z", Merchant: "market", Items: []*model.PurchaseItemInput{{Name: "YOG-PB 400", AmountYen: 500, ProductVariantID: &productVariant.ID}}})
	if err != nil || conceptPurchase.Items[0].CategoryID == nil || *conceptPurchase.Items[0].CategoryID != category.ID || conceptPurchase.Items[0].Allocations[0].AmountYen == nil || *conceptPurchase.Items[0].Allocations[0].AmountYen != 100 {
		t.Fatalf("linked product should inherit its category and concept rule: purchase=%#v error=%v", conceptPurchase, err)
	}
	productVariantRule, err := resolver.SetAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, ProductVariantID: &productVariant.ID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 4}, {PoolID: poolB.ID, Weight: 1}}})
	if err != nil || productVariantRule.Scope != model.AllocationRuleScopeProductVariant {
		t.Fatalf("product variant rule was not saved: rule=%#v error=%v", productVariantRule, err)
	}
	variantPurchase, err := resolver.CreatePurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T11:58:00Z", Merchant: "market", Items: []*model.PurchaseItemInput{{Name: "YOG-PB 400", AmountYen: 600, ProductVariantID: &productVariant.ID}}})
	if err != nil || variantPurchase.Items[0].Allocations[0].AmountYen == nil || *variantPurchase.Items[0].Allocations[0].AmountYen != 480 {
		t.Fatalf("product variant rule should precede concept/category/merchant rules: purchase=%#v error=%v", variantPurchase, err)
	}
	correctedPurchase, err := resolver.CreatePurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T11:57:00Z", Merchant: "market", Items: []*model.PurchaseItemInput{{Name: "YOG-PB 400", AmountYen: 350}}})
	if err != nil || correctedPurchase.Items[0].ProductVariantID != nil {
		t.Fatalf("unmatched product should remain unnormalized until the user selects a candidate: purchase=%#v error=%v", correctedPurchase, err)
	}
	learnedItem, err := resolver.UpdatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: correctedPurchase.Items[0].ID, BaseRevision: correctedPurchase.Items[0].Revision, ProductVariantID: &productVariant.ID, ProductLearning: new(model.ProductLearningScopeMerchantAlias), ChangedFields: []string{"productVariantId"}})
	if err != nil || learnedItem.ProductVariantID == nil || *learnedItem.ProductVariantID != productVariant.ID {
		t.Fatalf("merchant alias correction did not link the canonical variant: item=%#v error=%v", learnedItem, err)
	}
	if learnedItem.Allocations[0].AmountYen == nil || *learnedItem.Allocations[0].AmountYen != 280 {
		t.Fatalf("linking a product should apply its higher-priority rule: allocations=%#v", learnedItem.Allocations)
	}
	market := " MARKET "
	suggestions, err := apiResolver.Query().ProductSuggestions(ctx, householdID, "YOG-PB", &market, nil)
	if err != nil || len(suggestions) == 0 || suggestions[0].Variant.ID != productVariant.ID || suggestions[0].MatchedName != "YOG-PB 400" {
		t.Fatalf("learned store alias was not suggested first: suggestions=%#v error=%v", suggestions, err)
	}
	canonicalName := "Plain yogurt 400g unsweetened"
	learnedItem, err = resolver.UpdatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: learnedItem.ID, BaseRevision: learnedItem.Revision, ProductVariantID: &productVariant.ID, ProductLearning: new(model.ProductLearningScopeVariantName), VariantName: &canonicalName, ChangedFields: []string{"productVariantId"}})
	if err != nil {
		t.Fatalf("normalized product name correction failed: %v", err)
	}
	suggestions, err = apiResolver.Query().ProductSuggestions(ctx, householdID, "unsweetened", nil, nil)
	if err != nil || len(suggestions) == 0 || suggestions[0].Variant.Name != canonicalName {
		t.Fatalf("corrected canonical variant name was not searchable: suggestions=%#v error=%v", suggestions, err)
	}
	var correctionCount int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM product_corrections WHERE item_id=$1`, learnedItem.ID).Scan(&correctionCount); err != nil || correctionCount != 2 {
		t.Fatalf("product correction learning was not recorded: count=%d error=%v", correctionCount, err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM purchases WHERE id IN ($1,$2,$3)`, conceptPurchase.ID, variantPurchase.ID, correctedPurchase.ID); err != nil {
		t.Fatal(err)
	}
	rules, err := apiResolver.Query().AllocationRules(ctx, householdID)
	if err != nil || len(rules) != 4 {
		t.Fatalf("allocation rule query failed: rules=%#v error=%v", rules, err)
	}
	purchase, err := resolver.CreatePurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T12:00:00Z", Merchant: "market",
		Items: []*model.PurchaseItemInput{{Name: "yogurt", AmountYen: 500, CategoryID: &category.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if purchase.Items[0].Allocations[0].AmountYen == nil || *purchase.Items[0].Allocations[0].AmountYen != 125 || purchase.Items[0].Allocations[1].AmountYen == nil || *purchase.Items[0].Allocations[1].AmountYen != 375 {
		t.Fatalf("category rule should precede merchant and household defaults: %#v", purchase.Items[0].Allocations)
	}
	var appliedRuleID *string
	var appliedRuleRevision int64
	if err := db.QueryRow(ctx, `SELECT rule_id::text, rule_version FROM item_allocations WHERE item_id=$1 ORDER BY pool_id LIMIT 1`, purchase.Items[0].ID).Scan(&appliedRuleID, &appliedRuleRevision); err != nil || appliedRuleID == nil || *appliedRuleID != categoryRule.ID || appliedRuleRevision != 1 {
		t.Fatalf("category rule snapshot metadata mismatch: id=%v revision=%d error=%v", appliedRuleID, appliedRuleRevision, err)
	}
	updatedCategoryRule, err := resolver.SetAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, CategoryID: &category.ID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 3}, {PoolID: poolB.ID, Weight: 2}}})
	if err != nil || updatedCategoryRule.Revision != categoryRule.Revision+1 {
		t.Fatalf("category rule revision did not advance: rule=%#v error=%v", updatedCategoryRule, err)
	}
	if err := db.QueryRow(ctx, `SELECT rule_id::text, rule_version FROM item_allocations WHERE item_id=$1 ORDER BY pool_id LIMIT 1`, purchase.Items[0].ID).Scan(&appliedRuleID, &appliedRuleRevision); err != nil || appliedRuleID == nil || *appliedRuleID != categoryRule.ID || appliedRuleRevision != 1 {
		t.Fatalf("changing a rule rewrote the stored snapshot metadata: id=%v revision=%d error=%v", appliedRuleID, appliedRuleRevision, err)
	}
	merchantOnlyPurchase, err := resolver.CreatePurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T12:01:00Z", Merchant: "Market", Items: []*model.PurchaseItemInput{{Name: "merchant rule item", AmountYen: 500}}})
	if err != nil || merchantOnlyPurchase.Items[0].Allocations[0].AmountYen == nil || *merchantOnlyPurchase.Items[0].Allocations[0].AmountYen != 400 || merchantOnlyPurchase.Items[0].Allocations[1].AmountYen == nil || *merchantOnlyPurchase.Items[0].Allocations[1].AmountYen != 100 {
		t.Fatalf("merchant rule should apply when no category rule matches: purchase=%#v error=%v", merchantOnlyPurchase, err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM purchases WHERE id=$1`, merchantOnlyPurchase.ID); err != nil {
		t.Fatal(err)
	}
	if purchase.Items[0].Allocations[0].AmountYen == nil || *purchase.Items[0].Allocations[0].AmountYen != 125 || purchase.Items[0].Allocations[1].AmountYen == nil || *purchase.Items[0].Allocations[1].AmountYen != 375 {
		t.Fatalf("updating the category rule changed an existing purchase snapshot: %#v", purchase.Items[0].Allocations)
	}
	changedAmount := model.Long(500)
	changedAllocations := []*model.AllocationInput{{PoolID: poolA.ID, Weight: 3}, {PoolID: poolB.ID, Weight: 2}}
	updatedItem, err := resolver.UpdatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: purchase.Items[0].ID, BaseRevision: purchase.Items[0].Revision, AmountYen: &changedAmount, Allocations: changedAllocations, ChangedFields: []string{"amountYen", "allocations"}})
	if err != nil || updatedItem.AmountYen != 500 {
		t.Fatalf("amount and allocation update failed: item=%#v error=%v", updatedItem, err)
	}
	var isOverride bool
	if err := db.QueryRow(ctx, `SELECT allocation_override FROM purchase_items WHERE id=$1`, updatedItem.ID).Scan(&isOverride); err != nil || !isOverride {
		t.Fatalf("explicit item weights were not marked as an override: value=%v error=%v", isOverride, err)
	}
	invalidAmount := model.Long(900)
	invalidAllocations := []*model.AllocationInput{{PoolID: uuid.NewV7().String(), Weight: 1}}
	_, err = resolver.UpdatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: updatedItem.ID, BaseRevision: updatedItem.Revision, AmountYen: &invalidAmount, Allocations: invalidAllocations, ChangedFields: []string{"amountYen", "allocations"}})
	if err == nil {
		t.Fatal("invalid allocation update should fail")
	}
	var persistedAmount int64
	var allocationTotal int64
	if err := db.QueryRow(ctx, `SELECT i.amount_yen, sum(a.amount_yen) FROM purchase_items i JOIN item_allocations a ON a.item_id=i.id WHERE i.id=$1 GROUP BY i.amount_yen`, updatedItem.ID).Scan(&persistedAmount, &allocationTotal); err != nil || persistedAmount != 500 || allocationTotal != 500 {
		t.Fatalf("failed update was not rolled back atomically: amount=%d allocated=%d error=%v", persistedAmount, allocationTotal, err)
	}
	merchant := "market changed"
	purchase, err = resolver.UpdatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: purchase.ID, BaseRevision: purchase.Revision, Patch: &model.PurchasePatch{Merchant: &merchant}, ChangedFields: []string{"merchant"}})
	if err != nil {
		t.Fatal(err)
	}
	occurredAt := "2026-04-13T12:00:00Z"
	purchase, err = resolver.UpdatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: purchase.ID, BaseRevision: 1, Patch: &model.PurchasePatch{OccurredAt: &occurredAt}, ChangedFields: []string{"occurredAt"}})
	if err != nil {
		t.Fatalf("disjoint stale fields should merge: %v", err)
	}
	if _, err := resolver.UpdatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: purchase.ID, BaseRevision: 1, Patch: &model.PurchasePatch{Merchant: &merchant}, ChangedFields: []string{"merchant"}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("same stale field should conflict, got %v", err)
	}
	if len(purchase.Items) != 1 || purchase.Items[0].Allocations[0].AmountYen == nil || *purchase.Items[0].Allocations[0].AmountYen != 300 || *purchase.Items[0].Allocations[1].AmountYen != 200 {
		t.Fatalf("unexpected allocation snapshot: %#v", purchase.Items)
	}
	documentID := uuid.NewV7().String()
	if _, err := db.Exec(ctx, `INSERT INTO source_documents(id,household_id,sha256,object_key,mime_type,source_kind,created_by) VALUES ($1,$2,$3,$4,'image/jpeg','receipt',$5)`, documentID, householdID, "hash-"+documentID, householdID+"/originals/"+documentID, memberA); err != nil {
		t.Fatal(err)
	}
	fromSource := model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, SourceDocumentID: &documentID, OccurredAt: "2026-04-20T12:00:00Z", Merchant: "source", Items: []*model.PurchaseItemInput{{Name: "milk", AmountYen: 250}}}
	confirmed, err := resolver.CreatePurchase(ctx, fromSource)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Items[0].Allocations[0].AmountYen == nil || *confirmed.Items[0].Allocations[0].AmountYen != 150 || confirmed.Items[0].Allocations[1].AmountYen == nil || *confirmed.Items[0].Allocations[1].AmountYen != 100 {
		t.Fatalf("global default allocation was not snapshotted: %#v", confirmed.Items[0].Allocations)
	}
	if _, err := resolver.SetDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: householdID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 2}, {PoolID: poolB.ID, Weight: 3}}}); err != nil {
		t.Fatal(err)
	}
	confirmed, err = loadPurchase(ctx, db, confirmed.ID)
	if err != nil || confirmed.Items[0].Allocations[0].AmountYen == nil || *confirmed.Items[0].Allocations[0].AmountYen != 150 || confirmed.Items[0].Allocations[1].AmountYen == nil || *confirmed.Items[0].Allocations[1].AmountYen != 100 {
		t.Fatalf("changing the global rule changed a prior purchase snapshot: purchase=%#v error=%v", confirmed, err)
	}
	fromSource.IdempotencyKey = uuid.NewV7().String()
	fromSource.Items = []*model.PurchaseItemInput{{Name: "duplicate", AmountYen: 999}}
	replayed, err := resolver.CreatePurchase(ctx, fromSource)
	if err != nil || replayed.ID != confirmed.ID {
		t.Fatalf("source confirmation should be idempotent: purchase=%#v error=%v", replayed, err)
	}
	rowReference := "transaction:card-2"
	rowInput := model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, SourceDocumentID: &documentID, SourceReference: &rowReference, OccurredAt: "2026-04-20T12:01:00Z", Merchant: "statement row", Items: []*model.PurchaseItemInput{{Name: "statement row", AmountYen: 1}}}
	rowPurchase, err := resolver.CreatePurchase(ctx, rowInput)
	if err != nil || rowPurchase.ID == confirmed.ID {
		t.Fatalf("a distinct statement row should confirm independently: purchase=%#v error=%v", rowPurchase, err)
	}
	rowInput.IdempotencyKey = uuid.NewV7().String()
	rowReplay, err := resolver.CreatePurchase(ctx, rowInput)
	if err != nil || rowReplay.ID != rowPurchase.ID {
		t.Fatalf("statement row confirmation should be idempotent: purchase=%#v error=%v", rowReplay, err)
	}
	if _, err := resolver.PostPurchase(ctx, confirmed.ID, confirmed.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM document_confirmations WHERE source_document_id=$1 AND source_reference=$2`, documentID, rowReference); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM evidence_links WHERE purchase_id=$1`, rowPurchase.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `DELETE FROM purchases WHERE id=$1`, rowPurchase.ID); err != nil {
		t.Fatal(err)
	}
	var confirmedPurchases int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM purchases WHERE household_id=$1`, householdID).Scan(&confirmedPurchases); err != nil || confirmedPurchases != 2 {
		t.Fatalf("source replay created a duplicate purchase: count=%d error=%v", confirmedPurchases, err)
	}
	if _, err := resolver.RecordFunding(ctx, model.RecordFundingInput{HouseholdID: householdID, MemberID: memberA, PoolID: poolA.ID, AmountYen: 6000, OccurredAt: "2026-04-15T10:00:00Z", IdempotencyKey: uuid.NewV7().String()}); err != nil {
		t.Fatal(err)
	}
	postedPurchase, err := resolver.PostPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String())
	if err != nil {
		t.Fatal(err)
	}
	postedMerchant := "edited after posting"
	postedOccurredAt := "2026-04-14T12:00:00Z"
	postedPaymentAccountID := paymentAccount.ID
	postedPurchase, err = resolver.UpdatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: postedPurchase.ID, BaseRevision: postedPurchase.Revision, Patch: &model.PurchasePatch{Merchant: &postedMerchant, OccurredAt: &postedOccurredAt, PaymentAccountID: &postedPaymentAccountID}, ChangedFields: []string{"merchant", "occurredAt", "paymentAccountId"}})
	if err != nil {
		t.Fatalf("open-period metadata update should allow a posted purchase: %v", err)
	}
	postedAmount := model.Long(700)
	postedAllocations := []*model.AllocationInput{{PoolID: poolA.ID, Weight: 3}, {PoolID: poolB.ID, Weight: 2}}
	postedItem, err := resolver.UpdatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: postedPurchase.Items[0].ID, BaseRevision: postedPurchase.Items[0].Revision, AmountYen: &postedAmount, Allocations: postedAllocations, ChangedFields: []string{"amountYen", "allocations"}})
	if err != nil || postedItem.AmountYen != 700 {
		t.Fatalf("open-period amount/allocation update should allow a posted purchase: item=%#v error=%v", postedItem, err)
	}
	postedItem, err = resolver.UpdatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: postedItem.ID, BaseRevision: postedItem.Revision, ChangedFields: []string{"categoryId"}})
	if err != nil || postedItem.CategoryID != nil {
		t.Fatalf("explicit null category should clear the category: item=%#v error=%v", postedItem, err)
	}
	var categoryIsNull bool
	var uncategorizedAllocationTotal int64
	if err := db.QueryRow(ctx, `SELECT i.category_id IS NULL,sum(a.amount_yen) FROM purchase_items i JOIN item_allocations a ON a.item_id=i.id WHERE i.id=$1 GROUP BY i.category_id`, postedItem.ID).Scan(&categoryIsNull, &uncategorizedAllocationTotal); err != nil || !categoryIsNull || uncategorizedAllocationTotal != 700 {
		t.Fatalf("category clearing changed the allocation total: categoryIsNull=%v allocated=%d error=%v", categoryIsNull, uncategorizedAllocationTotal, err)
	}
	postedItem, err = resolver.UpdatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: postedItem.ID, BaseRevision: postedItem.Revision, CategoryID: &category.ID, ChangedFields: []string{"categoryId"}})
	if err != nil || postedItem.CategoryID == nil || *postedItem.CategoryID != category.ID {
		t.Fatalf("category restoration should succeed after clearing: item=%#v error=%v", postedItem, err)
	}
	var ledgerDescription string
	var ledgerAmount int64
	var ledgerOccurredAt string
	if err := db.QueryRow(ctx, `SELECT description,amount_yen,to_char(occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"') FROM ledger_entries WHERE source_purchase_id=$1`, postedPurchase.ID).Scan(&ledgerDescription, &ledgerAmount, &ledgerOccurredAt); err != nil || ledgerDescription != postedMerchant || ledgerAmount != 700 || ledgerOccurredAt != postedOccurredAt {
		t.Fatalf("posted purchase ledger was not updated atomically: description=%q amount=%d occurredAt=%q error=%v", ledgerDescription, ledgerAmount, ledgerOccurredAt, err)
	}
	var journalDebits, journalCredits int64
	if err := db.QueryRow(ctx, `SELECT sum(debit_yen),sum(credit_yen) FROM journal_entries je JOIN journals j ON j.id=je.journal_id WHERE j.source_id=$1`, postedPurchase.ID).Scan(&journalDebits, &journalCredits); err != nil || journalDebits != 700 || journalCredits != 700 {
		t.Fatalf("posted purchase journal was not updated atomically: debit=%d credit=%d error=%v", journalDebits, journalCredits, err)
	}
	var journalAccountID string
	if err := db.QueryRow(ctx, `SELECT je.account_id::text FROM journal_entries je JOIN journals j ON j.id=je.journal_id WHERE j.source_id=$1 AND je.credit_yen>0`, postedPurchase.ID).Scan(&journalAccountID); err != nil || journalAccountID != paymentAccount.ID {
		t.Fatalf("posted purchase payment account was not updated: account=%q error=%v", journalAccountID, err)
	}
	postedPurchase, err = resolver.UpdatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: postedPurchase.ID, BaseRevision: postedPurchase.Revision, Patch: &model.PurchasePatch{}, ChangedFields: []string{"paymentAccountId"}})
	if err != nil || postedPurchase.PaymentAccountID != nil {
		t.Fatalf("explicit null payment account should clear the account: purchase=%#v error=%v", postedPurchase, err)
	}
	var accountIsNull bool
	if err := db.QueryRow(ctx, `SELECT account_id IS NULL FROM journal_entries je JOIN journals j ON j.id=je.journal_id WHERE j.source_id=$1 AND je.credit_yen>0`, postedPurchase.ID).Scan(&accountIsNull); err != nil || !accountIsNull {
		t.Fatalf("cleared payment account remained in the posted journal: accountIsNull=%v error=%v", accountIsNull, err)
	}
	settlement, err := resolver.ClosePeriod(ctx, model.ClosePeriodInput{HouseholdID: householdID, Period: "2026-04", ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(settlement.Lines) != 2 {
		t.Fatalf("expected two settlement lines, got %#v", settlement.Lines)
	}
	if _, err := resolver.UpdatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: postedPurchase.ID, BaseRevision: postedPurchase.Revision, Patch: &model.PurchasePatch{Merchant: &postedMerchant}, ChangedFields: []string{"merchant"}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("closed purchase edits must require a correction: %v", err)
	}
	byMember := map[string]*model.SettlementLine{}
	for _, line := range settlement.Lines {
		byMember[line.MemberID] = line
	}
	if byMember[memberA].ScheduledYen != 6000 || byMember[memberA].PaidYen != 6000 || byMember[memberA].PriorBalanceYen != 18000 || byMember[memberA].AmountDueYen != 18000 {
		t.Fatalf("member A schedule/payment and three prior unpaid months mismatch: %#v", byMember[memberA])
	}
	if byMember[memberB].ScheduledYen != 4000 || byMember[memberB].PaidYen != 0 || byMember[memberB].PriorBalanceYen != 12000 || byMember[memberB].AmountDueYen != 16000 {
		t.Fatalf("member B missed payments were not carried through the prior open months: %#v", byMember[memberB])
	}
	spending, err := apiResolver.Query().MonthlyCategorySpending(ctx, householdID, "2026-04", "2026-04")
	if err != nil || len(spending) != 2 {
		t.Fatalf("monthly category spending query failed: rows=%#v error=%v", spending, err)
	}
	if spending[0].CategoryID == nil || *spending[0].CategoryID != category.ID || spending[0].AmountYen != 700 {
		t.Fatalf("category spending mismatch: %#v", spending[0])
	}
}

func TestSettlementUsesPoolFundingSnapshotAndCarriesClosedBalanceOnce(t *testing.T) {
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	memberA, memberB := uuid.NewV7().String(), uuid.NewV7().String()
	for id, name := range map[string]string{memberA: "A", memberB: "B"} {
		if _, err := db.Exec(ctx, `INSERT INTO members(id,oidc_subject,display_name) VALUES ($1,$2,$3)`, id, "settlement-integration:"+id, name); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM members WHERE id=$1`, memberA)
		_, _ = db.Exec(context.Background(), `DELETE FROM members WHERE id=$1`, memberB)
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "settlement-integration:" + memberA, DisplayName: "A"})
	apiResolver := &Resolver{DB: db}
	mutation := apiResolver.Mutation()
	household, err := mutation.CreateHousehold(ctx, model.CreateHouseholdInput{Name: "settlement snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.Exec(context.Background(), `DELETE FROM households WHERE id=$1`, household.ID) }()
	if _, err := db.Exec(ctx, `INSERT INTO household_members(household_id,member_id) VALUES($1,$2)`, household.ID, memberB); err != nil {
		t.Fatal(err)
	}
	personalA, err := mutation.CreatePool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "A wallet", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	personalB, err := mutation.CreatePool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "B wallet", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := mutation.CreatePool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Shared", Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	for _, configuration := range []model.SetPoolFundingRuleInput{
		{HouseholdID: household.ID, PoolID: personalA.ID, OwnerMemberID: &memberA, FundingShares: []*model.PoolFundingShareInput{}, ExpectedVersion: 1},
		{HouseholdID: household.ID, PoolID: personalB.ID, OwnerMemberID: &memberB, FundingShares: []*model.PoolFundingShareInput{}, ExpectedVersion: 1},
		{HouseholdID: household.ID, PoolID: shared.ID, FundingShares: []*model.PoolFundingShareInput{{MemberID: memberA, Weight: 60}, {MemberID: memberB, Weight: 40}}, SettlementPolicy: new(model.SharedPoolSettlementPolicyBalanceToStandard), ExpectedVersion: 1},
	} {
		if _, err := mutation.SetPoolFundingRule(ctx, configuration); err != nil {
			t.Fatal(err)
		}
	}
	sharedAccount, err := mutation.CreateAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "shared card", Kind: model.AccountKindCard, PoolID: &shared.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := mutation.CreatePurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-04-10T12:00:00Z", Merchant: "market", PaymentAccountID: &sharedAccount.ID,
		Items: []*model.PurchaseItemInput{{Name: "shared-paid personal item", AmountYen: 1000, Allocations: []*model.AllocationInput{{PoolID: personalA.ID, Weight: 1}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.PostPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	closed, err := mutation.ClosePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: "2026-04", ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	closedLines := settlementByMember(t, closed.Lines)
	if closedLines[memberA].MonthlyAdjustmentYen != 400 || closedLines[memberA].AmountDueYen != 400 || closedLines[memberB].MonthlyAdjustmentYen != -400 || closedLines[memberB].AmountDueYen != -400 {
		t.Fatalf("shared-pool actual spending was not balanced against 60/40 funding shares: %#v", closedLines)
	}
	sharedPolicy := model.SharedPoolSettlementPolicyReimbursePersonal
	if _, err := mutation.SetPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: shared.ID, FundingShares: []*model.PoolFundingShareInput{{MemberID: memberA, Weight: 60}, {MemberID: memberB, Weight: 40}}, SettlementPolicy: &sharedPolicy, ExpectedVersion: 2}); err != nil {
		t.Fatal(err)
	}
	reloadedClosed, err := apiResolver.Query().Settlement(ctx, household.ID, "2026-04")
	if err != nil {
		t.Fatal(err)
	}
	reloadedLines := settlementByMember(t, reloadedClosed.Lines)
	if reloadedLines[memberA].AmountDueYen != 400 || reloadedLines[memberA].MonthlyAdjustmentYen != 400 || reloadedLines[memberB].AmountDueYen != -400 || reloadedLines[memberB].MonthlyAdjustmentYen != -400 {
		t.Fatalf("editing a pool rule rewrote the closed snapshot: %#v", reloadedLines)
	}
	next, err := apiResolver.Query().Settlement(ctx, household.ID, "2026-05")
	if err != nil {
		t.Fatal(err)
	}
	nextLines := settlementByMember(t, next.Lines)
	if nextLines[memberA].PriorBalanceYen != 400 || nextLines[memberA].AmountDueYen != 400 || nextLines[memberB].PriorBalanceYen != -400 || nextLines[memberB].AmountDueYen != -400 {
		t.Fatalf("closed balance did not become the sole next-month carry: %#v", nextLines)
	}
}

func settlementByMember(t *testing.T, lines []*model.SettlementLine) map[string]*model.SettlementLine {
	t.Helper()
	result := make(map[string]*model.SettlementLine, len(lines))
	for _, line := range lines {
		result[line.MemberID] = line
	}
	return result
}

func TestAccountingPeriodsUseJapanTimeBoundaries(t *testing.T) {
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	memberID := uuid.NewV7().String()
	if _, err := db.Exec(ctx, `INSERT INTO members(id,oidc_subject,display_name) VALUES($1,$2,'JST')`, memberID, "period-boundary:"+memberID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.Exec(context.Background(), `DELETE FROM members WHERE id=$1`, memberID) }()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: "period-boundary:" + memberID, DisplayName: "JST"})
	apiResolver := &Resolver{DB: db}
	mutation := apiResolver.Mutation()
	household, err := mutation.CreateHousehold(ctx, model.CreateHouseholdInput{Name: "Japan time boundary"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.Exec(context.Background(), `DELETE FROM households WHERE id=$1`, household.ID) }()
	pool, err := mutation.CreatePool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Personal", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.SetFundingSchedule(ctx, model.SetFundingScheduleInput{HouseholdID: household.ID, MemberID: memberID, PoolID: pool.ID, AmountYen: 700, Cadence: "MONTHLY", StartsOn: "2026-10-01"}); err != nil {
		t.Fatal(err)
	}
	for _, occurredAt := range []string{"2026-09-30T23:59:59+09:00", "2026-10-01T00:00:00+09:00"} {
		purchase, err := mutation.CreatePurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: occurredAt, Merchant: "boundary", Items: []*model.PurchaseItemInput{{Name: "boundary item", AmountYen: 100}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mutation.PostPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
			t.Fatal(err)
		}
	}
	spending, err := apiResolver.Query().MonthlyCategorySpending(ctx, household.ID, "2026-09", "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	spendingByPeriod := make(map[string]int64, len(spending))
	for _, item := range spending {
		spendingByPeriod[item.Period] += int64(item.AmountYen)
	}
	if spendingByPeriod["2026-09"] != 100 || spendingByPeriod["2026-10"] != 100 {
		t.Fatalf("monthly spending did not follow Japan time boundaries: %#v", spendingByPeriod)
	}
	closed, err := mutation.ClosePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: "2026-09", ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(closed.Lines) != 1 || closed.Lines[0].ScheduledYen != 0 {
		t.Fatalf("October schedule leaked into September close: %#v", closed.Lines)
	}
	october, err := apiResolver.Query().Settlement(ctx, household.ID, "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if len(october.Lines) != 1 || october.Lines[0].ScheduledYen != 700 {
		t.Fatalf("October schedule was missing at its Japan-time start: %#v", october.Lines)
	}
	var statuses []string
	rows, err := db.Query(ctx, `SELECT status FROM purchases WHERE household_id=$1 ORDER BY occurred_at`, household.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		statuses = append(statuses, status)
	}
	rows.Close()
	if len(statuses) != 2 || statuses[0] != "CLOSED" || statuses[1] != "POSTED" {
		t.Fatalf("period close crossed the Japan-time boundary: %#v", statuses)
	}
}
