package graph

import (
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"os"
	"testing"
	"time"

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

const (
	testApplicationPDF = "application/pdf"
	testCardSourceKind = "card"
)

const (
	loanTransferLedgerEntryType      = "LoanTransferLedgerEntry"
	repaymentTransferLedgerEntryType = "RepaymentTransferLedgerEntry"
	testMerchant                     = "Shop"
)

func accountBalanceSourceType(source model.AccountBalanceSource) string {
	switch source.(type) {
	case *model.AccountIncome:
		return "AccountIncome"
	case *model.AccountMovement:
		return graphQLAccountMovementType
	case *model.AccountOpeningBalance:
		return "AccountOpeningBalance"
	case *model.CorrectionLedgerEntry:
		return "CorrectionLedgerEntry"
	case *model.FundingLedgerEntry:
		return graphQLFundingLedgerEntryType
	case *model.Purchase:
		return "Purchase"
	case *model.PurchaseWideCorrection:
		return "PurchaseWideCorrection"
	case *model.PurchaseItemCorrection:
		return "PurchaseItemCorrection"
	case *model.PurchaseLedgerEntry:
		return "PurchaseLedgerEntry"
	case *model.AdvanceTransferLedgerEntry:
		return "AdvanceTransferLedgerEntry"
	case *model.LoanTransferLedgerEntry:
		return loanTransferLedgerEntryType
	case *model.RepaymentTransferLedgerEntry:
		return repaymentTransferLedgerEntryType
	case *model.GiftTransferLedgerEntry:
		return "GiftTransferLedgerEntry"
	default:
		return ""
	}
}

func TestAccountingFlowUsesScheduleAndActualFundingSeparately(t *testing.T) {
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
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	memberA, memberB := uuid.NewV7().String(), uuid.NewV7().String()
	for id, name := range map[string]string{memberA: "A", memberB: "B"} {
		memberUUID, _ := gofrsuuid.FromString(id)
		subject := "integration:" + id
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberA)))).Exec(t.Context(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberB)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "integration:" + memberA, DisplayName: "A"})
	apiResolver := &Resolver{DB: db}
	resolver := &mutationResolver{Resolver: apiResolver}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdID := household.ID
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	householdUUID, _ := gofrsuuid.FromString(householdID)
	memberBUUID, _ := gofrsuuid.FromString(memberB)
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &memberBUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	poolA, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: householdID, Name: "A", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	poolB, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: householdID, Name: "B", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	paymentAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: householdID, Name: testCardAccountName, Kind: model.AccountKindCard, PoolID: &poolA.ID})
	if err != nil {
		t.Fatal(err)
	}
	alternatePaymentAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: householdID, Name: "Alternate card", Kind: model.AccountKindCard, PoolID: &poolA.ID})
	if err != nil {
		t.Fatal(err)
	}
	fundingSourceAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{
		HouseholdID: householdID, Name: "Funding source", Kind: model.AccountKindBank, PoolID: &poolA.ID,
		OpeningBalance: &model.AccountOpeningBalanceInput{AmountYen: 5000, At: "2026-01-01T00:00:00Z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fundingRule := range []model.SetPoolFundingRuleInput{
		{HouseholdID: householdID, PoolID: poolA.ID, Configuration: personalPoolFundingConfiguration(memberA), ExpectedVersion: 1},
		{HouseholdID: householdID, PoolID: poolB.ID, Configuration: personalPoolFundingConfiguration(memberB), ExpectedVersion: 1},
	} {
		if _, err := resolver.setPoolFundingRule(ctx, fundingRule); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: householdID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 3}, {PoolID: poolB.ID, Weight: 2}}}); err != nil {
		t.Fatal(err)
	}
	for _, schedule := range []model.SetFundingScheduleInput{
		{HouseholdID: householdID, MemberID: memberA, PoolID: poolA.ID, AmountYen: 6000, Cadence: testMonthlyCadence, StartsOn: testJanuaryFirst2026},
		{HouseholdID: householdID, MemberID: memberB, PoolID: poolB.ID, AmountYen: 4000, Cadence: testMonthlyCadence, StartsOn: testJanuaryFirst2026},
	} {
		if _, err := resolver.setFundingSchedule(ctx, schedule); err != nil {
			t.Fatal(err)
		}
	}
	category, err := resolver.createCategory(ctx, model.CreateCategoryInput{HouseholdID: householdID, Name: testGroceriesCategoryName})
	if err != nil {
		t.Fatal(err)
	}
	category, err = resolver.setCategoryArchived(ctx, category.ID, true)
	if err != nil || !category.Archived {
		t.Fatalf("category archival failed: category=%#v error=%v", category, err)
	}
	category, err = resolver.setCategoryArchived(ctx, category.ID, false)
	if err != nil || category.Archived {
		t.Fatalf("category restoration failed: category=%#v error=%v", category, err)
	}
	storeRuleMerchant := "  MARKET  "
	categoryRule, err := resolver.setAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, Target: &model.AllocationRuleTargetInput{CategoryID: &category.ID}, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 1}, {PoolID: poolB.ID, Weight: 3}}})
	if err != nil || categoryRule.Target.GetName() != category.Name {
		t.Fatalf("category allocation rule was not saved: rule=%#v error=%v", categoryRule, err)
	}
	storeRule, err := resolver.setAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, Target: &model.AllocationRuleTargetInput{Merchant: &storeRuleMerchant}, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 4}, {PoolID: poolB.ID, Weight: 1}}})
	if err != nil || storeRule.Target.GetName() != testMarketName {
		t.Fatalf("merchant allocation rule was not normalized: rule=%#v error=%v", storeRule, err)
	}
	productConcept, err := resolver.createProductConcept(ctx, model.CreateProductConceptInput{HouseholdID: householdID, Name: "Yogurt", CategoryID: &category.ID})
	if err != nil {
		t.Fatal(err)
	}
	productVariant, err := resolver.createProductVariant(ctx, model.CreateProductVariantInput{HouseholdID: householdID, ConceptID: productConcept.ID, Name: "Plain yogurt 400g", Brand: new("Private Brand"), PackageSize: new("400g")})
	if err != nil {
		t.Fatal(err)
	}
	productConceptRule, err := resolver.setAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, Target: &model.AllocationRuleTargetInput{ProductConceptID: &productConcept.ID}, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 1}, {PoolID: poolB.ID, Weight: 4}}})
	if err != nil || productConceptRule.Target.GetName() != productConcept.Name {
		t.Fatalf("product concept rule was not saved: rule=%#v error=%v", productConceptRule, err)
	}
	conceptPurchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T11:59:00Z", Merchant: testMarketName, Items: []*model.PurchaseItemInput{{Name: testYogurtProductName, AmountYen: 500, ProductVariantID: &productVariant.ID}}})
	if err != nil || conceptPurchase.Items[0].CategoryID == nil || *conceptPurchase.Items[0].CategoryID != category.ID || conceptPurchase.Items[0].Allocations[0].AmountYen != 100 {
		t.Fatalf("linked product should inherit its category and concept rule: purchase=%#v error=%v", conceptPurchase, err)
	}
	productVariantRule, err := resolver.setAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, Target: &model.AllocationRuleTargetInput{ProductVariantID: &productVariant.ID}, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 4}, {PoolID: poolB.ID, Weight: 1}}})
	if err != nil || productVariantRule.Target.GetName() != productVariant.Name {
		t.Fatalf("product variant rule was not saved: rule=%#v error=%v", productVariantRule, err)
	}
	variantPurchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T11:58:00Z", Merchant: testMarketName, Items: []*model.PurchaseItemInput{{Name: testYogurtProductName, AmountYen: 600, ProductVariantID: &productVariant.ID}}})
	if err != nil || variantPurchase.Items[0].Allocations[0].AmountYen != 480 {
		t.Fatalf("product variant rule should precede concept/category/merchant rules: purchase=%#v error=%v", variantPurchase, err)
	}
	correctedPurchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T11:57:00Z", Merchant: testMarketName, Items: []*model.PurchaseItemInput{{Name: testYogurtProductName, AmountYen: 350}}})
	if err != nil || correctedPurchase.Items[0].ProductVariantID != nil {
		t.Fatalf("unmatched product should remain unnormalized until the user selects a candidate: purchase=%#v error=%v", correctedPurchase, err)
	}
	learnedItem, err := resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: correctedPurchase.Items[0].ID, BaseRevision: correctedPurchase.Items[0].Revision, ProductVariantID: omittable(&productVariant.ID), ProductLearning: omittable(&model.ProductLearningConfigurationInput{MerchantAlias: new(true)})})
	if err != nil || learnedItem.ProductVariantID == nil || *learnedItem.ProductVariantID != productVariant.ID {
		t.Fatalf("merchant alias correction did not link the canonical variant: item=%#v error=%v", learnedItem, err)
	}
	if learnedItem.Allocations[0].AmountYen != 280 {
		t.Fatalf("linking a product should apply its higher-priority rule: allocations=%#v", learnedItem.Allocations)
	}
	market := " MARKET "
	suggestions, err := apiResolver.Query().ProductSuggestions(ctx, householdID, "YOG-PB", &market, 8)
	if err != nil || len(suggestions) == 0 || suggestions[0].Variant.ID != productVariant.ID || suggestions[0].MatchedName != testYogurtProductName {
		t.Fatalf("learned store alias was not suggested first: suggestions=%#v error=%v", suggestions, err)
	}
	canonicalName := "Plain yogurt 400g unsweetened"
	learnedItem, err = resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: learnedItem.ID, BaseRevision: learnedItem.Revision, ProductVariantID: omittable(&productVariant.ID), ProductLearning: omittable(&model.ProductLearningConfigurationInput{VariantName: &canonicalName})})
	if err != nil {
		t.Fatalf("normalized product name correction failed: %v", err)
	}
	suggestions, err = apiResolver.Query().ProductSuggestions(ctx, householdID, "unsweetened", nil, 8)
	if err != nil || len(suggestions) == 0 || suggestions[0].Variant.Name != canonicalName {
		t.Fatalf("corrected canonical variant name was not searchable: suggestions=%#v error=%v", suggestions, err)
	}
	learnedItemUUID, _ := gofrsuuid.FromString(learnedItem.ID)
	corrections, err := models.ProductCorrections.Query(sm.Where(models.ProductCorrections.Columns.ItemID.EQ(psql.Arg(learnedItemUUID)))).All(ctx, bobpgx.NewPool(db))
	correctionCount := len(corrections)
	if err != nil || correctionCount != 2 {
		t.Fatalf("product correction learning was not recorded: count=%d error=%v", correctionCount, err)
	}
	if _, err := models.Purchases.Delete(dm.Where(psql.Or(models.Purchases.Columns.ID.EQ(psql.Arg(conceptPurchase.ID)), models.Purchases.Columns.ID.EQ(psql.Arg(variantPurchase.ID)), models.Purchases.Columns.ID.EQ(psql.Arg(correctedPurchase.ID))))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	pageSize := 2
	rules, err := apiResolver.Query().AllocationRules(ctx, householdID, nil, nil, &pageSize, nil)
	if err != nil || len(rules.Edges) != pageSize || rules.TotalCount != 4 || !rules.PageInfo.HasNextPage || rules.PageInfo.EndCursor == nil {
		t.Fatalf("allocation rule query failed: rules=%#v error=%v", rules, err)
	}
	assertLoadedRule := func(rule *model.AllocationRule) {
		t.Helper()
		if rule.Target == nil || len(rule.Allocations) != 2 {
			t.Errorf("allocation rule target or weights were not loaded: rule=%#v", rule)
			return
		}
		for _, allocation := range rule.Allocations {
			if allocation.PoolName == "" || allocation.Weight <= 0 {
				t.Errorf("allocation rule pool or weight was not loaded: allocation=%#v", allocation)
			}
		}
	}
	seenRuleIDs := make(map[string]struct{}, rules.TotalCount)
	orderedRuleIDs := make([]string, 0, rules.TotalCount)
	for _, edge := range rules.Edges {
		assertLoadedRule(edge.Node)
		seenRuleIDs[edge.Node.ID] = struct{}{}
		orderedRuleIDs = append(orderedRuleIDs, edge.Node.ID)
	}
	for rules.PageInfo.HasNextPage {
		rules, err = apiResolver.Query().AllocationRules(ctx, householdID, rules.PageInfo.EndCursor, nil, &pageSize, nil)
		if err != nil {
			t.Fatalf("allocation rule cursor query failed: %v", err)
		}
		for _, edge := range rules.Edges {
			assertLoadedRule(edge.Node)
			if _, exists := seenRuleIDs[edge.Node.ID]; exists {
				t.Fatalf("allocation rule cursor returned a duplicate: %s", edge.Node.ID)
			}
			seenRuleIDs[edge.Node.ID] = struct{}{}
			orderedRuleIDs = append(orderedRuleIDs, edge.Node.ID)
		}
	}
	if len(seenRuleIDs) != 4 {
		t.Fatalf("allocation rule pages returned %d unique rules; want 4", len(seenRuleIDs))
	}
	lastRules, err := apiResolver.Query().AllocationRules(ctx, householdID, nil, nil, nil, &pageSize)
	if err != nil || len(lastRules.Edges) != pageSize || lastRules.Edges[0].Node.ID != orderedRuleIDs[2] || lastRules.Edges[1].Node.ID != orderedRuleIDs[3] || !lastRules.PageInfo.HasPreviousPage || lastRules.PageInfo.HasNextPage {
		t.Fatalf("allocation rule backward pagination failed: rules=%#v error=%v", lastRules, err)
	}
	for _, edge := range lastRules.Edges {
		assertLoadedRule(edge.Node)
	}
	purchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T12:00:00Z", Merchant: testMarketName, PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: paymentAccount.ID, AmountYen: 500}},
		Items: []*model.PurchaseItemInput{{Name: "yogurt", AmountYen: 500, CategoryID: &category.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if purchase.Items[0].Allocations[0].AmountYen != 125 || purchase.Items[0].Allocations[1].AmountYen != 375 {
		t.Fatalf("category rule should precede merchant and household defaults: %#v", purchase.Items[0].Allocations)
	}
	purchaseItemUUID, _ := gofrsuuid.FromString(purchase.Items[0].ID)
	allocations, err := models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(purchaseItemUUID))), sm.OrderBy(models.ItemAllocations.Columns.PoolID)).All(ctx, bobpgx.NewPool(db))
	var appliedRuleID *string
	var appliedRuleRevision int64
	if len(allocations) > 0 && allocations[0].RuleID.Valid {
		ruleID := allocations[0].RuleID.V.String()
		appliedRuleID = &ruleID
		appliedRuleRevision = allocations[0].RuleVersion
	}
	if err != nil || len(allocations) == 0 || appliedRuleID == nil || *appliedRuleID != categoryRule.ID || appliedRuleRevision != 1 {
		t.Fatalf("category rule snapshot metadata mismatch: id=%v revision=%d error=%v", appliedRuleID, appliedRuleRevision, err)
	}
	updatedCategoryRule, err := resolver.setAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, Target: &model.AllocationRuleTargetInput{CategoryID: &category.ID}, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 3}, {PoolID: poolB.ID, Weight: 2}}})
	if err != nil || updatedCategoryRule.Revision != categoryRule.Revision+1 {
		t.Fatalf("category rule revision did not advance: rule=%#v error=%v", updatedCategoryRule, err)
	}
	allocations, err = models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(purchaseItemUUID))), sm.OrderBy(models.ItemAllocations.Columns.PoolID)).All(ctx, bobpgx.NewPool(db))
	appliedRuleID = nil
	if len(allocations) > 0 && allocations[0].RuleID.Valid {
		ruleID := allocations[0].RuleID.V.String()
		appliedRuleID = &ruleID
		appliedRuleRevision = allocations[0].RuleVersion
	}
	if err != nil || len(allocations) == 0 || appliedRuleID == nil || *appliedRuleID != categoryRule.ID || appliedRuleRevision != 1 {
		t.Fatalf("changing a rule rewrote the stored snapshot metadata: id=%v revision=%d error=%v", appliedRuleID, appliedRuleRevision, err)
	}
	merchantOnlyPurchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T12:01:00Z", Merchant: "Market", Items: []*model.PurchaseItemInput{{Name: "merchant rule item", AmountYen: 500}}})
	if err != nil || merchantOnlyPurchase.Items[0].Allocations[0].AmountYen != 400 || merchantOnlyPurchase.Items[0].Allocations[1].AmountYen != 100 {
		t.Fatalf("merchant rule should apply when no category rule matches: purchase=%#v error=%v", merchantOnlyPurchase, err)
	}
	if _, err := models.Purchases.Delete(dm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(merchantOnlyPurchase.ID)))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	if purchase.Items[0].Allocations[0].AmountYen != 125 || purchase.Items[0].Allocations[1].AmountYen != 375 {
		t.Fatalf("updating the category rule changed an existing purchase snapshot: %#v", purchase.Items[0].Allocations)
	}
	changedAmount := model.Long(500)
	changedAllocations := []*model.AllocationInput{{PoolID: poolA.ID, Weight: 3}, {PoolID: poolB.ID, Weight: 2}}
	updatedItem, err := resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: purchase.Items[0].ID, BaseRevision: purchase.Items[0].Revision, AmountYen: omittable(&changedAmount), Allocations: omittable(changedAllocations)})
	if err != nil || updatedItem.AmountYen != 500 {
		t.Fatalf("amount and allocation update failed: item=%#v error=%v", updatedItem, err)
	}
	updatedItemUUID, _ := gofrsuuid.FromString(updatedItem.ID)
	purchaseItemRow, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(updatedItemUUID)))).One(ctx, bobpgx.NewPool(db))
	isOverride := err == nil && purchaseItemRow.AllocationOverride
	if err != nil || !isOverride {
		t.Fatalf("explicit item weights were not marked as an override: value=%v error=%v", isOverride, err)
	}
	invalidAmount := model.Long(900)
	invalidAllocations := []*model.AllocationInput{{PoolID: uuid.NewV7().String(), Weight: 1}}
	_, err = resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: updatedItem.ID, BaseRevision: updatedItem.Revision, AmountYen: omittable(&invalidAmount), Allocations: omittable(invalidAllocations)})
	if err == nil {
		t.Fatal("invalid allocation update should fail")
	}
	persistedItem, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(updatedItemUUID)))).One(ctx, bobpgx.NewPool(db))
	persistedAmount, allocationTotal := persistedItem.AmountYen, int64(0)
	allocationRows, allocationErr := models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(updatedItemUUID)))).All(ctx, bobpgx.NewPool(db))
	for _, allocation := range allocationRows {
		allocationTotal += allocation.AmountYen
	}
	if err != nil || allocationErr != nil || persistedAmount != 500 || allocationTotal != 500 {
		t.Fatalf("failed update was not rolled back atomically: amount=%d allocated=%d error=%v", persistedAmount, allocationTotal, err)
	}
	merchant := "market changed"
	purchase, err = resolver.updatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: purchase.ID, BaseRevision: purchase.Revision, Patch: &model.PurchasePatch{Merchant: omittable(&merchant)}})
	if err != nil {
		t.Fatal(err)
	}
	occurredAt := model.DateTime("2026-04-13T12:00:00Z")
	purchase, err = resolver.updatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: purchase.ID, BaseRevision: 1, Patch: &model.PurchasePatch{OccurredAt: omittable(&occurredAt)}})
	if err != nil {
		t.Fatalf("disjoint stale fields should merge: %v", err)
	}
	if _, err := resolver.updatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: purchase.ID, BaseRevision: 1, Patch: &model.PurchasePatch{Merchant: omittable(&merchant)}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("same stale field should conflict, got %v", err)
	}
	if len(purchase.Items) != 1 || purchase.Items[0].Allocations[0].AmountYen != 300 || purchase.Items[0].Allocations[1].AmountYen != 200 {
		t.Fatalf("unexpected allocation snapshot: %#v", purchase.Items)
	}
	documentID := uuid.NewV7().String()
	documentUUID, _ := gofrsuuid.FromString(documentID)
	memberAUUID, _ := gofrsuuid.FromString(memberA)
	hash, objectKey, mimeType, sourceKind := "hash-"+documentID, householdID+"/originals/"+documentID, "image/jpeg", testReceiptSourceKind
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &documentUUID, HouseholdID: &householdUUID, Sha256: &hash, ObjectKey: &objectKey, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberAUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	jobID := uuid.NewV7().String()
	jobUUID, _ := gofrsuuid.FromString(jobID)
	jobVersion, jobStatus := int32(1), extractionJobSucceededStatus
	verified := ingestion.Verification{
		Receipt: ingestion.Receipt{Total: 250, Items: []ingestion.Item{
			{Name: testMilkName, PrintedName: "milk original", PrintedAmount: 150, GrossAmount: 150, Region: &ingestion.Region{Page: 1, Top: 0.1, Left: 0.2, Bottom: 0.3, Right: 0.4}},
			{Name: testBreadName, PrintedName: "bread original", PrintedAmount: 100, GrossAmount: 100},
		}},
		Transform: [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}, TransformPage: 1, Balanced: true,
	}
	jobResult, err := stdjson.Marshal(verified)
	if err != nil {
		t.Fatal(err)
	}
	jobResultValue := sql.Null[types.JSON[stdjson.RawMessage]]{V: types.JSON[stdjson.RawMessage]{Val: jobResult}, Valid: true}
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{ID: &jobUUID, HouseholdID: &householdUUID, SourceDocumentID: &documentUUID, Version: &jobVersion, Status: &jobStatus, Result: &jobResultValue}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	fromSource := model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, SourceDocumentID: &documentID, OccurredAt: "2026-04-20T12:00:00Z", Merchant: "source", PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: paymentAccount.ID, AmountYen: 250}}, Items: []*model.PurchaseItemInput{{Name: testMilkName, AmountYen: 150}, {Name: testBreadName, AmountYen: 100}}}
	mismatchedSource := fromSource
	mismatchedSource.Items = fromSource.Items[:1]
	if _, err := resolver.createPurchase(ctx, mismatchedSource); !errors.Is(err, ErrBadInput) {
		t.Fatalf("receipt import should reject an unmatched item count, got %v", err)
	}
	wrongAmountSource := fromSource
	wrongAmountSource.Items = []*model.PurchaseItemInput{{Name: testMilkName, AmountYen: 151}, {Name: testBreadName, AmountYen: 99}}
	if _, err := resolver.createPurchase(ctx, wrongAmountSource); !errors.Is(err, ErrBadInput) {
		t.Fatalf("receipt import should reject unverified item amounts, got %v", err)
	}
	confirmed, err := resolver.createPurchase(ctx, fromSource)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Items[0].Allocations[0].AmountYen != 90 || confirmed.Items[0].Allocations[1].AmountYen != 60 || confirmed.Items[1].Allocations[0].AmountYen != 60 || confirmed.Items[1].Allocations[1].AmountYen != 40 {
		t.Fatalf("global default allocation was not snapshotted: %#v", confirmed.Items)
	}
	evidenceLinks, err := models.EvidenceLinks.Query(sm.Where(models.EvidenceLinks.Columns.PurchaseID.EQ(psql.Arg(generatedUUID(confirmed.ID))))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(evidenceLinks) != len(fromSource.Items) {
		t.Fatalf("source purchase should persist one item evidence link per extracted item: links=%d error=%v", len(evidenceLinks), err)
	}
	itemEvidence := 0
	seenItemIndexes := make(map[int]struct{}, len(fromSource.Items))
	for _, link := range evidenceLinks {
		if !link.ItemID.Valid {
			continue
		}
		itemEvidence++
		var observation struct {
			JobID     string         `json:"jobId"`
			ItemIndex int            `json:"itemIndex"`
			Item      ingestion.Item `json:"item"`
		}
		if err := stdjson.Unmarshal(link.Observation.Val, &observation); err != nil {
			t.Fatal(err)
		}
		if observation.JobID != jobID || observation.ItemIndex < 0 || observation.ItemIndex >= len(verified.Receipt.Items) || observation.Item.PrintedName == "" {
			t.Fatalf("item evidence did not retain its extraction observation: %#v", observation)
		}
		if _, exists := seenItemIndexes[observation.ItemIndex]; exists {
			t.Fatalf("multiple purchase items reference extraction item %d", observation.ItemIndex)
		}
		seenItemIndexes[observation.ItemIndex] = struct{}{}
		if link.EvidenceRegion.Valid != (verified.Receipt.Items[observation.ItemIndex].Region != nil) {
			t.Fatalf("item evidence region does not match extraction item %d", observation.ItemIndex)
		}
	}
	if itemEvidence != len(fromSource.Items) {
		t.Fatalf("source purchase should link each extracted item: got %d links", itemEvidence)
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: householdID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 2}, {PoolID: poolB.ID, Weight: 3}}}); err != nil {
		t.Fatal(err)
	}
	confirmed, err = loadPurchase(ctx, db, confirmed.ID)
	if err != nil || confirmed.Items[0].Allocations[0].AmountYen != 90 || confirmed.Items[0].Allocations[1].AmountYen != 60 || confirmed.Items[1].Allocations[0].AmountYen != 60 || confirmed.Items[1].Allocations[1].AmountYen != 40 {
		t.Fatalf("changing the global rule changed a prior purchase snapshot: purchase=%#v error=%v", confirmed.Items, err)
	}
	fromSource.IdempotencyKey = uuid.NewV7().String()
	fromSource.Items = []*model.PurchaseItemInput{{Name: "duplicate", AmountYen: 999}}
	replayed, err := resolver.createPurchase(ctx, fromSource)
	if err != nil || replayed.ID != confirmed.ID {
		t.Fatalf("source confirmation should be idempotent: purchase=%#v error=%v", replayed, err)
	}
	const statementTransactionID = "card-2"
	const statementRowDescription = "statement row"
	const statementOccurredAt = "2026-04-20T12:01:00Z"
	statementDocumentID := uuid.NewV7().String()
	statementDocumentUUID, _ := gofrsuuid.FromString(statementDocumentID)
	statementHash, statementObjectKey, statementMimeType, statementSourceKind := "hash-"+statementDocumentID, householdID+"/originals/"+statementDocumentID, testApplicationPDF, testCardSourceKind
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &statementDocumentUUID, HouseholdID: &householdUUID, Sha256: &statementHash, ObjectKey: &statementObjectKey, MimeType: &statementMimeType, SourceKind: &statementSourceKind, CreatedBy: &memberAUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	statementObservations := ingestion.SourceObservations(statementDocumentID, statementHash, testCardSourceKind, ingestion.Verification{Statements: []ingestion.StatementEntry{
		{Row: 2, TransactionID: statementTransactionID, Merchant: statementRowDescription, Amount: 1, OccurredAt: statementOccurredAt},
		{Row: 3, TransactionID: statementTransactionID, Merchant: statementRowDescription, Amount: 1, OccurredAt: statementOccurredAt},
	}})
	rowReference, secondRowReference := statementObservations[0].SourceReference, statementObservations[1].SourceReference
	rowInput := model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, SourceDocumentID: &statementDocumentID, SourceReference: &rowReference, OccurredAt: statementOccurredAt, Merchant: statementRowDescription, PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: paymentAccount.ID, AmountYen: 1}}, Items: []*model.PurchaseItemInput{{Name: statementRowDescription, AmountYen: 1, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 1}}}}}
	rowPurchase, err := resolver.createAndPostPurchase(ctx, rowInput)
	if err != nil || rowPurchase.ID == confirmed.ID || rowPurchase.Status != model.PurchaseStatusPosted || rowPurchase.Revision != 2 {
		t.Fatalf("a card statement row should create and post independently: purchase=%#v error=%v", rowPurchase, err)
	}
	rowPurchaseUUID, _ := gofrsuuid.FromString(rowPurchase.ID)
	confirmations, err := models.DocumentConfirmations.Query(sm.Where(psql.And(
		models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(statementDocumentUUID)),
		models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(rowReference)),
		models.DocumentConfirmations.Columns.PurchaseID.EQ(psql.Arg(rowPurchaseUUID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(confirmations) != 1 {
		t.Fatalf("posted purchase should retain its statement row confirmation: confirmations=%d error=%v", len(confirmations), err)
	}
	statementEvidence, err := models.EvidenceLinks.Query(sm.Where(psql.And(
		models.EvidenceLinks.Columns.SourceDocumentID.EQ(psql.Arg(statementDocumentUUID)),
		models.EvidenceLinks.Columns.PurchaseID.EQ(psql.Arg(rowPurchaseUUID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(statementEvidence) != 1 || statementEvidence[0].ItemID.Valid {
		t.Fatalf("posted statement purchase should retain one document evidence link: links=%d error=%v", len(statementEvidence), err)
	}
	statementJournals, err := models.Journals.Query(sm.Where(psql.And(
		models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Journals.Columns.SourceKind.EQ(psql.Arg("PURCHASE")),
		models.Journals.Columns.SourceID.EQ(psql.Arg(rowPurchaseUUID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	statementLedgers, err := models.LedgerEntries.Query(sm.Where(psql.And(
		models.LedgerEntries.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.LedgerEntries.Columns.SourcePurchaseID.EQ(psql.Arg(sql.Null[gofrsuuid.UUID]{V: rowPurchaseUUID, Valid: true})),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(statementJournals) != 1 || len(statementLedgers) != 1 {
		t.Fatalf("posted statement purchase should have one journal and ledger entry: journals=%d ledgers=%d error=%v", len(statementJournals), len(statementLedgers), err)
	}
	rowInput.IdempotencyKey = uuid.NewV7().String()
	rowReplay, err := resolver.createAndPostPurchase(ctx, rowInput)
	if err != nil || rowReplay.ID != rowPurchase.ID {
		t.Fatalf("statement row create and post should be idempotent: purchase=%#v error=%v", rowReplay, err)
	}
	secondRowInput := rowInput
	secondRowInput.IdempotencyKey = uuid.NewV7().String()
	secondRowInput.SourceReference = &secondRowReference
	secondRowInput.PaymentAllocations = nil
	secondRowPurchase, err := resolver.createPurchase(ctx, secondRowInput)
	if err != nil || secondRowPurchase.ID == rowPurchase.ID {
		t.Fatalf("a second row with the same transaction id should confirm independently: purchase=%#v error=%v", secondRowPurchase, err)
	}
	secondRowInput.IdempotencyKey = uuid.NewV7().String()
	secondRowReplay, err := resolver.createPurchase(ctx, secondRowInput)
	if err != nil || secondRowReplay.ID != secondRowPurchase.ID {
		t.Fatalf("second statement row confirmation should be idempotent: purchase=%#v error=%v", secondRowReplay, err)
	}
	if _, err := resolver.postPurchase(ctx, confirmed.ID, confirmed.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := models.DocumentConfirmations.Delete(dm.Where(psql.And(models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(statementDocumentUUID)), models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(secondRowReference))))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	secondRowPurchaseUUID, _ := gofrsuuid.FromString(secondRowPurchase.ID)
	if _, err := psql.Delete(dm.From(models.EvidenceLinks.NameExpr()), dm.Where(models.EvidenceLinks.Columns.PurchaseID.EQ(psql.Arg(secondRowPurchaseUUID)))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	if _, err := models.Purchases.Delete(dm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(secondRowPurchaseUUID)))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	confirmedRows, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, bobpgx.NewPool(db))
	confirmedPurchases := len(confirmedRows)
	if err != nil || confirmedPurchases != 3 {
		t.Fatalf("source replay created a duplicate purchase: count=%d error=%v", confirmedPurchases, err)
	}
	if _, err := resolver.recordFunding(ctx, model.RecordFundingInput{HouseholdID: householdID, MemberID: memberA, PoolID: poolA.ID, AmountYen: 6000, OccurredAt: "2026-04-15T10:00:00Z", IdempotencyKey: uuid.NewV7().String()}); err != nil {
		t.Fatal(err)
	}
	fundingEntry, err := resolver.recordFunding(ctx, model.RecordFundingInput{
		HouseholdID: householdID, MemberID: memberA, PoolID: poolA.ID, AmountYen: 1000,
		SourceAccountID: &fundingSourceAccount.ID, OccurredAt: "2026-05-15T10:00:00Z", IdempotencyKey: uuid.NewV7().String(),
	})
	if err != nil {
		t.Fatalf("funding from a source account without a destination account failed: %v", err)
	}
	fundingLedgerUUID, err := gofrsuuid.FromString(fundingEntry.ID)
	if err != nil {
		t.Fatal(err)
	}
	fundingJournal, err := models.Journals.Query(sm.Where(models.Journals.Columns.SourceID.EQ(psql.Arg(fundingLedgerUUID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	fundingLines, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(fundingJournal.ID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	var fundingDebits, fundingCredits int64
	var sourceAccountCredit, unassignedCounterDebit int64
	fundingSourceUUID, _ := gofrsuuid.FromString(fundingSourceAccount.ID)
	for _, line := range fundingLines {
		fundingDebits += line.DebitYen
		fundingCredits += line.CreditYen
		if line.AccountID.Valid && line.AccountID.V == fundingSourceUUID {
			sourceAccountCredit += line.CreditYen
		}
		if !line.AccountID.Valid && !line.PoolID.Valid && !line.MemberID.Valid {
			unassignedCounterDebit += line.DebitYen
		}
	}
	if fundingDebits != fundingCredits || sourceAccountCredit != 1000 || unassignedCounterDebit != 1000 {
		t.Fatalf("source-only funding journal is unbalanced or has incorrect account entries: debits=%d credits=%d sourceCredit=%d counterDebit=%d", fundingDebits, fundingCredits, sourceAccountCredit, unassignedCounterDebit)
	}
	fundingBalances, err := (&Resolver{DB: db}).Query().AccountBalances(ctx, householdID)
	if err != nil {
		t.Fatal(err)
	}
	for _, balance := range fundingBalances {
		if balance.AccountID == fundingSourceAccount.ID && balance.BalanceYen != 4000 {
			t.Fatalf("source account balance did not decrease by the funding amount: balance=%d", balance.BalanceYen)
		}
	}
	postedAmount := model.Long(700)
	postedAllocations := []*model.AllocationInput{{PoolID: poolA.ID, Weight: 3}, {PoolID: poolB.ID, Weight: 2}}
	updatedPurchaseItem, err := resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{
		ItemID: purchase.Items[0].ID, BaseRevision: purchase.Items[0].Revision, AmountYen: omittable(&postedAmount),
		Allocations: omittable(postedAllocations),
	})
	if err != nil || updatedPurchaseItem.AmountYen != 700 {
		t.Fatalf("open-period draft amount/allocation update should succeed: item=%#v error=%v", updatedPurchaseItem, err)
	}
	purchase, err = loadPurchase(ctx, db, purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	postedPurchase, err := resolver.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String())
	if err != nil {
		t.Fatal(err)
	}
	postedMerchant := "edited after posting"
	postedOccurredAt := model.DateTime("2026-04-14T12:00:00Z")
	postedPurchase, err = resolver.updatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: postedPurchase.ID, BaseRevision: postedPurchase.Revision, Patch: &model.PurchasePatch{
		Merchant: omittable(&postedMerchant), OccurredAt: omittable(&postedOccurredAt),
	}})
	if err != nil {
		t.Fatalf("open-period metadata update should allow a posted purchase: %v", err)
	}
	postedItem := postedPurchase.Items[0]
	postedItem, err = resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: postedItem.ID, BaseRevision: postedItem.Revision, CategoryID: omittable[*string](nil)})
	if err != nil || postedItem.CategoryID != nil {
		t.Fatalf("explicit null category should clear the category: item=%#v error=%v", postedItem, err)
	}
	postedItemUUID, _ := gofrsuuid.FromString(postedItem.ID)
	postedItemRow, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.ID.EQ(psql.Arg(postedItemUUID)))).One(ctx, bobpgx.NewPool(db))
	postedAllocationRows, allocationErr := models.ItemAllocations.Query(sm.Where(models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(postedItemUUID)))).All(ctx, bobpgx.NewPool(db))
	categoryIsNull, uncategorizedAllocationTotal := !postedItemRow.CategoryID.Valid, int64(0)
	for _, allocation := range postedAllocationRows {
		uncategorizedAllocationTotal += allocation.AmountYen
	}
	if err != nil || allocationErr != nil || !categoryIsNull || uncategorizedAllocationTotal != 700 {
		t.Fatalf("category clearing changed the allocation total: categoryIsNull=%v allocated=%d error=%v", categoryIsNull, uncategorizedAllocationTotal, err)
	}
	postedItem, err = resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: postedItem.ID, BaseRevision: postedItem.Revision, CategoryID: omittable(&category.ID)})
	if err != nil || postedItem.CategoryID == nil || *postedItem.CategoryID != category.ID {
		t.Fatalf("category restoration should succeed after clearing: item=%#v error=%v", postedItem, err)
	}
	postedPurchaseUUID, _ := gofrsuuid.FromString(postedPurchase.ID)
	ledgerRows, err := models.LedgerEntries.Query(sm.Where(models.LedgerEntries.Columns.SourcePurchaseID.EQ(psql.Arg(postedPurchaseUUID)))).All(ctx, bobpgx.NewPool(db))
	var ledgerDescription, ledgerOccurredAt string
	var ledgerAmount int64
	if len(ledgerRows) > 0 {
		ledgerDescription, ledgerAmount, ledgerOccurredAt = ledgerRows[0].Description, ledgerRows[0].AmountYen, ledgerRows[0].OccurredAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if err != nil || len(ledgerRows) != 1 || ledgerDescription != postedMerchant || ledgerAmount != 700 || ledgerOccurredAt != string(postedOccurredAt) {
		t.Fatalf("posted purchase ledger was not updated atomically: description=%q amount=%d occurredAt=%q error=%v", ledgerDescription, ledgerAmount, ledgerOccurredAt, err)
	}
	journals, err := models.Journals.Query(sm.Where(models.Journals.Columns.SourceID.EQ(psql.Arg(postedPurchaseUUID)))).All(ctx, bobpgx.NewPool(db))
	var journalEntries models.JournalEntrySlice
	if len(journals) > 0 {
		journalEntries, err = models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(journals[0].ID)))).All(ctx, bobpgx.NewPool(db))
	}
	var journalDebits, journalCredits int64
	for _, entry := range journalEntries {
		journalDebits += entry.DebitYen
		journalCredits += entry.CreditYen
	}
	if err != nil || len(journals) != 1 || journalDebits != 700 || journalCredits != 700 {
		t.Fatalf("posted purchase journal was not updated atomically: debit=%d credit=%d error=%v", journalDebits, journalCredits, err)
	}
	var journalAccountID string
	for _, entry := range journalEntries {
		if entry.CreditYen > 0 && entry.AccountID.Valid {
			journalAccountID = entry.AccountID.V.String()
			break
		}
	}
	if err != nil || journalAccountID != paymentAccount.ID {
		t.Fatalf("posted purchase payment account mismatch: account=%q error=%v", journalAccountID, err)
	}
	settlement, err := resolver.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: householdID, Period: testApril2026, ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	settlementData := settlementData(t, settlement)
	if len(settlementData.Lines) != 2 {
		t.Fatalf("expected two settlement lines, got %#v", settlementData.Lines)
	}
	if _, err := resolver.updatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: postedPurchase.ID, BaseRevision: postedPurchase.Revision, Patch: &model.PurchasePatch{Merchant: omittable(&postedMerchant)}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("closed purchase edits must require a correction: %v", err)
	}
	accountSwitchPurchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-05-12T12:00:00Z", Merchant: "account switch",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: paymentAccount.ID, AmountYen: 500}}, Items: []*model.PurchaseItemInput{{Name: "switch item", AmountYen: 500, CategoryID: &category.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	accountSwitchPurchase, err = resolver.postPurchase(ctx, accountSwitchPurchase.ID, accountSwitchPurchase.Revision, uuid.NewV7().String())
	if err != nil {
		t.Fatal(err)
	}
	accountSwitchUUID, _ := gofrsuuid.FromString(accountSwitchPurchase.ID)
	accountSwitchJournal, err := models.Journals.Query(sm.Where(models.Journals.Columns.SourceID.EQ(psql.Arg(accountSwitchUUID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	accountSwitchEntries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(accountSwitchJournal.ID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	debitPools := make(map[string]int64)
	for _, entry := range accountSwitchEntries {
		if entry.DebitYen > 0 && entry.PoolID.Valid {
			debitPools[entry.PoolID.V.String()] += entry.DebitYen
		}
	}
	accountSwitchPurchase, err = resolver.updatePurchase(ctx, model.UpdatePurchaseInput{
		PurchaseID: accountSwitchPurchase.ID, BaseRevision: accountSwitchPurchase.Revision,
		Patch: &model.PurchasePatch{PaymentAllocations: omittable([]*model.PurchasePaymentInput{{PaymentAccountID: alternatePaymentAccount.ID, AmountYen: 500}})},
	})
	if err != nil || len(accountSwitchPurchase.PaymentAllocations) != 1 || accountSwitchPurchase.PaymentAllocations[0].PaymentAccountID != alternatePaymentAccount.ID {
		t.Fatalf("posted purchase payment account should change: purchase=%#v error=%v", accountSwitchPurchase, err)
	}
	accountSwitchEntries, err = models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(accountSwitchJournal.ID)))).All(ctx, bobpgx.NewPool(db))
	updatedDebitPools := make(map[string]int64)
	journalAccountID = ""
	for _, entry := range accountSwitchEntries {
		if entry.CreditYen > 0 && entry.AccountID.Valid {
			journalAccountID = entry.AccountID.V.String()
		}
		if entry.DebitYen > 0 && entry.PoolID.Valid {
			updatedDebitPools[entry.PoolID.V.String()] += entry.DebitYen
		}
	}
	if err != nil || journalAccountID != alternatePaymentAccount.ID || len(updatedDebitPools) != len(debitPools) {
		t.Fatalf("posted purchase journal did not follow the changed payment account and preserve pool attribution: account=%q debitPools=%v error=%v", journalAccountID, updatedDebitPools, err)
	}
	for poolID, amount := range debitPools {
		if updatedDebitPools[poolID] != amount {
			t.Fatalf("payment account change altered purchase pool attribution: before=%v after=%v", debitPools, updatedDebitPools)
		}
	}
	bulkUpdatedPurchases, err := resolver.bulkUpdatePurchases(ctx, model.BulkUpdatePurchasesInput{
		Targets: []*model.PurchaseUpdateTargetInput{{
			PurchaseID:   accountSwitchPurchase.ID,
			BaseRevision: accountSwitchPurchase.Revision,
			Patch: &model.BulkPurchasePatch{PaymentAllocations: omittable([]*model.PurchasePaymentInput{
				{PaymentAccountID: paymentAccount.ID, AmountYen: 200},
				{PaymentAccountID: alternatePaymentAccount.ID, AmountYen: 300},
			})},
		}},
	})
	if err != nil || len(bulkUpdatedPurchases) != 1 {
		t.Fatalf("posted purchase payment allocations should update in bulk: %v", err)
	}
	purchasePaymentRows, err := models.PurchasePayments.Query(
		sm.Where(models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(accountSwitchUUID))),
		sm.OrderBy(models.PurchasePayments.Columns.LineNo),
	).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(purchasePaymentRows) != 2 {
		t.Fatalf("posted purchase payment rows were not replaced: rows=%#v error=%v", purchasePaymentRows, err)
	}
	if purchasePaymentRows[0].AccountID != generatedUUID(paymentAccount.ID) || purchasePaymentRows[0].AmountYen != 200 ||
		purchasePaymentRows[1].AccountID != generatedUUID(alternatePaymentAccount.ID) || purchasePaymentRows[1].AmountYen != 300 {
		t.Fatalf("posted purchase payment rows do not match the bulk allocation: rows=%#v", purchasePaymentRows)
	}
	accountSwitchEntries, err = models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(accountSwitchJournal.ID)))).All(ctx, bobpgx.NewPool(db))
	creditByAccount := make(map[string]int64)
	var allocationDebits, allocationCredits int64
	for _, entry := range accountSwitchEntries {
		allocationDebits += entry.DebitYen
		allocationCredits += entry.CreditYen
		if entry.CreditYen > 0 && entry.AccountID.Valid {
			creditByAccount[entry.AccountID.V.String()] += entry.CreditYen
		}
	}
	if err != nil || allocationDebits != 500 || allocationCredits != 500 ||
		creditByAccount[paymentAccount.ID] != 200 || creditByAccount[alternatePaymentAccount.ID] != 300 || len(creditByAccount) != 2 {
		t.Fatalf("posted purchase journal does not match the bulk allocation: credits=%v debits=%d totalCredits=%d error=%v", creditByAccount, allocationDebits, allocationCredits, err)
	}
	byMember := map[string]*model.SettlementLine{}
	for _, line := range settlementData.Lines {
		byMember[line.MemberID] = line
	}
	if byMember[memberA].ScheduledYen != 6000 || byMember[memberA].PaidYen != 6000 || byMember[memberA].PriorBalanceYen != 18000 || byMember[memberA].AdvanceCreditYen != 380 || byMember[memberA].AmountDueYen != 17620 {
		t.Fatalf("member A schedule/payment and three prior unpaid months mismatch: %#v", byMember[memberA])
	}
	if byMember[memberB].ScheduledYen != 4000 || byMember[memberB].PaidYen != 0 || byMember[memberB].PriorBalanceYen != 12000 || byMember[memberB].MonthlyAdjustmentYen != 380 || byMember[memberB].AmountDueYen != 16380 {
		t.Fatalf("member B missed payments were not carried through the prior open months: %#v", byMember[memberB])
	}
	spending, err := apiResolver.Query().MonthlyCategorySpending(ctx, householdID, testApril2026, testApril2026)
	if err != nil || len(spending) != 2 {
		t.Fatalf("monthly category spending query failed: rows=%#v error=%v", spending, err)
	}
	if spending[0].CategoryID == nil || *spending[0].CategoryID != category.ID || spending[0].AmountYen != 700 {
		t.Fatalf("category spending mismatch: %#v", spending[0])
	}
	assertHouseholdJournalsBalance(t, ctx, db, generatedUUID(householdID))
}

func TestSettlementUsesPoolFundingSnapshotAndCarriesClosedBalanceOnce(t *testing.T) {
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
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	memberA, memberB := uuid.NewV7().String(), uuid.NewV7().String()
	for id, name := range map[string]string{memberA: "A", memberB: "B"} {
		memberUUID, _ := gofrsuuid.FromString(id)
		subject := "settlement-integration:" + id
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberA)))).Exec(t.Context(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberB)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "settlement-integration:" + memberA, DisplayName: "A"})
	apiResolver := &Resolver{DB: db}
	mutation := &mutationResolver{Resolver: apiResolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "settlement snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(household.ID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	householdUUID, _ := gofrsuuid.FromString(household.ID)
	memberBUUID, _ := gofrsuuid.FromString(memberB)
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &memberBUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	personalA, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "A wallet", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	personalB, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "B wallet", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: testSharedPoolName, Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	for _, configuration := range []model.SetPoolFundingRuleInput{
		{HouseholdID: household.ID, PoolID: personalA.ID, Configuration: personalPoolFundingConfiguration(memberA), ExpectedVersion: 1},
		{HouseholdID: household.ID, PoolID: personalB.ID, Configuration: personalPoolFundingConfiguration(memberB), ExpectedVersion: 1},
		{HouseholdID: household.ID, PoolID: shared.ID, Configuration: sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 60}, {MemberID: memberB, Weight: 40}}, model.SharedPoolSettlementPolicyBalanceToStandard), ExpectedVersion: 1},
	} {
		if _, err := mutation.setPoolFundingRule(ctx, configuration); err != nil {
			t.Fatal(err)
		}
	}
	sharedAccount, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "shared card", Kind: model.AccountKindCard, PoolID: &shared.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-04-10T12:00:00Z", Merchant: testMarketName, PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: sharedAccount.ID, AmountYen: 1000}},
		Items: []*model.PurchaseItemInput{{Name: "shared-paid personal item", AmountYen: 1000, Allocations: []*model.AllocationInput{{PoolID: personalA.ID, Weight: 1}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	sharedPolicy := model.SharedPoolSettlementPolicyReimbursePersonal
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: shared.ID, Configuration: sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 40}, {MemberID: memberB, Weight: 60}}, sharedPolicy), ExpectedVersion: 2}); err != nil {
		t.Fatal(err)
	}
	closed, err := mutation.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: testApril2026, ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: testApril2026, ExpectedRevision: 0}); !errors.Is(err, ErrConflict) {
		t.Fatalf("closing the same settlement revision twice must not apply it again: %v", err)
	}
	closedLines := settlementByMember(t, settlementData(t, closed).Lines)
	if closedLines[memberA].MonthlyAdjustmentYen != 400 || closedLines[memberA].AmountDueYen != 400 || closedLines[memberB].MonthlyAdjustmentYen != -400 || closedLines[memberB].AmountDueYen != -400 {
		t.Fatalf("shared-pool actual spending was not balanced against 60/40 funding shares: %#v", closedLines)
	}
	sharedPolicy = model.SharedPoolSettlementPolicyBalanceToStandard
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: shared.ID, Configuration: sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 40}, {MemberID: memberB, Weight: 60}}, sharedPolicy), ExpectedVersion: 3}); err != nil {
		t.Fatal(err)
	}
	reloadedClosed, err := apiResolver.Query().Settlement(ctx, household.ID, testApril2026)
	if err != nil {
		t.Fatal(err)
	}
	closedNodeID, err := encodeSettlementNodeID(household.ID, testApril2026)
	if err != nil {
		t.Fatal(err)
	}
	openNodeID, err := encodeSettlementNodeID(household.ID, "2026-06")
	if err != nil {
		t.Fatal(err)
	}
	settlementNodes, err := apiResolver.Query().Nodes(ctx, []string{closedNodeID, openNodeID, closedNodeID})
	if err != nil || len(settlementNodes) != 3 {
		t.Fatalf("batch settlement node lookup failed: nodes=%#v error=%v", settlementNodes, err)
	}
	batchClosed, closedOK := settlementNodes[0].(*model.ClosedSettlement)
	batchOpen, openOK := settlementNodes[1].(*model.OpenSettlement)
	duplicateClosed, duplicateOK := settlementNodes[2].(*model.ClosedSettlement)
	if !closedOK || !openOK || !duplicateOK || batchClosed.ID != closedNodeID || duplicateClosed.ID != closedNodeID || batchOpen.Period != "2026-06" {
		t.Fatalf("batch settlement nodes did not preserve order, duplicates, or open periods: %#v", settlementNodes)
	}
	otherMemberContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: memberB, Subject: "settlement-integration:" + memberB, DisplayName: "B"})
	foreignHousehold, err := mutation.createHousehold(otherMemberContext, model.CreateHouseholdInput{Name: "other settlement household"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(foreignHousehold.ID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	foreignSettlementID, err := encodeSettlementNodeID(foreignHousehold.ID, testApril2026)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apiResolver.Query().Nodes(ctx, []string{closedNodeID, foreignSettlementID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("batch settlement node lookup exposed another household: %v", err)
	}
	reloadedLines := settlementByMember(t, settlementData(t, reloadedClosed).Lines)
	if reloadedLines[memberA].AmountDueYen != 400 || reloadedLines[memberA].MonthlyAdjustmentYen != 400 || reloadedLines[memberB].AmountDueYen != -400 || reloadedLines[memberB].MonthlyAdjustmentYen != -400 {
		t.Fatalf("editing a pool rule rewrote the closed snapshot: %#v", reloadedLines)
	}
	next, err := apiResolver.Query().Settlement(ctx, household.ID, "2026-05")
	if err != nil {
		t.Fatal(err)
	}
	nextLines := settlementByMember(t, settlementData(t, next).Lines)
	if nextLines[memberA].PriorBalanceYen != 400 || nextLines[memberA].AmountDueYen != 400 || nextLines[memberB].PriorBalanceYen != -400 || nextLines[memberB].AmountDueYen != -400 {
		t.Fatalf("closed balance did not become the sole next-month carry: %#v", nextLines)
	}
}

func TestPersonalCardPaysSharedPoolPurchase(t *testing.T) {
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
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	memberA, memberB := uuid.NewV7().String(), uuid.NewV7().String()
	for id, name := range map[string]string{memberA: "A", memberB: "B"} {
		memberID := generatedUUID(id)
		subject := "personal-card-shared-purchase:" + id
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(generatedUUID(memberA))))).Exec(t.Context(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(generatedUUID(memberB))))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "personal-card-shared-purchase:" + memberA, DisplayName: "A"})
	resolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "personal card shared purchase"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	memberBUUID := generatedUUID(memberB)
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &memberBUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	personalA, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "A personal", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	sharedPool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: testSharedPoolName, Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	for _, fundingRule := range []model.SetPoolFundingRuleInput{
		{HouseholdID: household.ID, PoolID: personalA.ID, Configuration: personalPoolFundingConfiguration(memberA), ExpectedVersion: 1},
		{HouseholdID: household.ID, PoolID: sharedPool.ID, Configuration: sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 60}, {MemberID: memberB, Weight: 40}}, model.SharedPoolSettlementPolicyBalanceToStandard), ExpectedVersion: 1},
	} {
		if _, err := resolver.setPoolFundingRule(ctx, fundingRule); err != nil {
			t.Fatal(err)
		}
	}
	card, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "A personal card", Kind: model.AccountKindCard, PoolID: &personalA.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-04-10T12:00:00Z", Merchant: testMerchant,
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: card.ID, AmountYen: 1000}},
		Items:              []*model.PurchaseItemInput{{Name: "Shared groceries", AmountYen: 1000, Allocations: []*model.AllocationInput{{PoolID: sharedPool.ID, Weight: 1}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	posted, err := resolver.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String())
	if err != nil {
		t.Fatal(err)
	}
	purchaseUUID := generatedUUID(posted.ID)
	journals, err := models.Journals.Query(sm.Where(psql.And(
		models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.Journals.Columns.SourceKind.EQ(psql.Arg("PURCHASE")),
		models.Journals.Columns.SourceID.EQ(psql.Arg(purchaseUUID)),
	))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(journals) != 1 {
		t.Fatalf("posted purchase should have one journal: journals=%d error=%v", len(journals), err)
	}
	entries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(journals[0].ID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	var debits, credits, cardCredits int64
	for _, entry := range entries {
		debits += entry.DebitYen
		credits += entry.CreditYen
		if entry.AccountID.Valid && entry.AccountID.V == generatedUUID(card.ID) {
			cardCredits += entry.CreditYen
		}
	}
	if len(entries) != 2 || debits != 1000 || credits != 1000 || cardCredits != 1000 {
		t.Fatalf("personal card purchase journal should balance and credit the payer account: entries=%#v debits=%d credits=%d cardCredits=%d", entries, debits, credits, cardCredits)
	}
	balances, err := (&Resolver{DB: db}).Query().AccountBalances(ctx, household.ID)
	if err != nil {
		t.Fatal(err)
	}
	cardBalanceFound := false
	for _, balance := range balances {
		if balance.AccountID == card.ID {
			cardBalanceFound = true
			if balance.BalanceYen != 1000 {
				t.Fatalf("personal card balance should reflect the posted purchase: got %d want 1000", balance.BalanceYen)
			}
		}
	}
	if !cardBalanceFound {
		t.Fatal("personal card balance was not returned")
	}
	settlement, err := resolver.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: testApril2026, ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	lines := settlementByMember(t, settlementData(t, settlement).Lines)
	if lines[memberA].AdvanceCreditYen != 400 || lines[memberA].AmountDueYen != -400 || lines[memberB].MonthlyAdjustmentYen != 400 || lines[memberB].AmountDueYen != 400 {
		t.Fatalf("shared purchase paid by A's personal card should settle the 60/40 responsibility against A's 1000 yen advance: %#v", lines)
	}
	assertHouseholdJournalsBalance(t, ctx, db, generatedUUID(household.ID))
}

func TestSharedPoolRepaymentSettlesLoanAndJournal(t *testing.T) {
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
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	memberA, memberB := uuid.NewV7().String(), uuid.NewV7().String()
	for id, name := range map[string]string{memberA: testLenderName, memberB: testBorrowerName} {
		memberUUID, _ := gofrsuuid.FromString(id)
		subject := "shared-repayment:" + id
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberA)))).Exec(t.Context(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberB)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "shared-repayment:" + memberA, DisplayName: testLenderName})
	resolver := &mutationResolver{Resolver: (&Resolver{DB: db})}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "shared repayment"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(household.ID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	householdUUID, _ := gofrsuuid.FromString(household.ID)
	memberAUUID, _ := gofrsuuid.FromString(memberA)
	memberBUUID, _ := gofrsuuid.FromString(memberB)
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &memberBUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	sharedPool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: testSharedPoolName, Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{
		HouseholdID: household.ID, PoolID: sharedPool.ID,
		Configuration:   sharedPoolFundingConfiguration([]*model.PoolFundingShareInput{{MemberID: memberA, Weight: 50}, {MemberID: memberB, Weight: 50}}, model.SharedPoolSettlementPolicyBalanceToStandard),
		ExpectedVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	lenderPool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: testLenderName, Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	borrowerPool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: testBorrowerName, Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	for _, personalPool := range []struct{ poolID, ownerID string }{{lenderPool.ID, memberA}, {borrowerPool.ID, memberB}} {
		if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: personalPool.poolID, Configuration: personalPoolFundingConfiguration(personalPool.ownerID), ExpectedVersion: 1}); err != nil {
			t.Fatal(err)
		}
	}
	openingAt := model.DateTime("2026-05-01T00:00:00Z")
	lenderAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Lender bank", Kind: model.AccountKindBank, PoolID: &lenderPool.ID, OpeningBalance: &model.AccountOpeningBalanceInput{AmountYen: model.Long(5000), At: openingAt}})
	if err != nil {
		t.Fatal(err)
	}
	borrowerAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Borrower bank", Kind: model.AccountKindBank, PoolID: &borrowerPool.ID, OpeningBalance: &model.AccountOpeningBalanceInput{AmountYen: model.Long(5000), At: openingAt}})
	if err != nil {
		t.Fatal(err)
	}
	sharedAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Shared bank", Kind: model.AccountKindBank, PoolID: &sharedPool.ID, OpeningBalance: &model.AccountOpeningBalanceInput{AmountYen: model.Long(5000), At: openingAt}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.recordExternalIncome(ctx, model.RecordExternalIncomeInput{
		HouseholdID: household.ID, AccountID: lenderAccount.ID, AmountYen: 700,
		OccurredAt: "2026-04-30T12:00:00Z", IdempotencyKey: uuid.NewV7().String(), Description: "Before opening balance",
	}); err != nil {
		t.Fatal(err)
	}
	loan, err := resolver.recordTransfer(ctx, model.RecordTransferInput{
		HouseholdID: household.ID, FromMemberID: memberA, ToMemberID: memberB, Kind: model.TransferInputKindLoan,
		PaymentAccountID: &lenderAccount.ID, DestinationAccountID: &borrowerAccount.ID,
		AmountYen: 5000, OccurredAt: "2026-06-10T12:00:00Z", Description: "Loan", IdempotencyKey: uuid.NewV7().String(),
	})
	if err != nil || loan.TransferID == nil {
		t.Fatalf("loan transfer failed: transfer=%#v error=%v", loan, err)
	}
	if _, err := resolver.recordFunding(ctx, model.RecordFundingInput{
		HouseholdID: household.ID, MemberID: memberB, PoolID: sharedPool.ID, AmountYen: 5000,
		SourceAccountID: &borrowerAccount.ID, DestinationAccountID: &sharedAccount.ID,
		OccurredAt: "2026-06-12T12:00:00Z", IdempotencyKey: uuid.NewV7().String(),
	}); err != nil {
		t.Fatal(err)
	}
	repayment, err := resolver.recordRepayment(ctx, model.RecordRepaymentInput{
		HouseholdID: household.ID, FromMemberID: memberB, ToMemberID: memberA,
		LoanID: *loan.TransferID, PaymentAccountID: &sharedAccount.ID, DestinationAccountID: &lenderAccount.ID, AmountYen: 5000,
		OccurredAt: "2026-06-13T12:00:00Z", Description: "Shared repayment", IdempotencyKey: uuid.NewV7().String(),
	})
	if err != nil || repayment.TransferID == nil {
		t.Fatalf("shared-pool repayment failed: transfer=%#v error=%v", repayment, err)
	}
	gift, err := resolver.recordTransfer(ctx, model.RecordTransferInput{
		HouseholdID: household.ID, FromMemberID: memberA, ToMemberID: memberB, Kind: model.TransferInputKindGift,
		AmountYen: 300, OccurredAt: "2026-06-14T12:00:00Z", Description: "Gift", IdempotencyKey: uuid.NewV7().String(),
	})
	if err != nil || gift.TransferID == nil {
		t.Fatalf("gift transfer failed: transfer=%#v error=%v", gift, err)
	}
	giftUUID, err := gofrsuuid.FromString(*gift.TransferID)
	if err != nil {
		t.Fatal(err)
	}
	giftRow, err := models.Transfers.Query(sm.Where(models.Transfers.Columns.ID.EQ(psql.Arg(giftUUID)))).One(ctx, bobpgx.NewPool(db))
	if err != nil || giftRow.Kind != string(model.TransferInputKindGift) || giftRow.FromMemberID.String() != memberA || giftRow.ToMemberID.String() != memberB {
		t.Fatalf("gift kind or participants were not persisted: gift=%#v error=%v", giftRow, err)
	}
	ledgerNodeID, err := encodeNodeID("LedgerEntry", loan.ID)
	if err != nil {
		t.Fatal(err)
	}
	ledgerNode, err := (&Resolver{DB: db}).Query().Node(ctx, ledgerNodeID)
	if err != nil {
		t.Fatal(err)
	}
	loanNodeID, err := encodeNodeID("LoanBalance", *loan.TransferID)
	if err != nil {
		t.Fatal(err)
	}
	loanLedgerEntry, ok := ledgerNode.(*model.LoanTransferLedgerEntry)
	if !ok || loanLedgerEntry.Loan == nil || loanLedgerEntry.Loan.ID != loanNodeID {
		t.Fatalf("single ledger node did not resolve the loan transfer as a loan balance: %#v", ledgerNode)
	}
	ledgerNodes, err := (&Resolver{DB: db}).Query().Nodes(ctx, []string{ledgerNodeID})
	if err != nil || len(ledgerNodes) != 1 {
		t.Fatalf("batch ledger node lookup failed: nodes=%#v error=%v", ledgerNodes, err)
	}
	batchLoanLedgerEntry, ok := ledgerNodes[0].(*model.LoanTransferLedgerEntry)
	if !ok || batchLoanLedgerEntry.Loan == nil || batchLoanLedgerEntry.Loan.ID != loanNodeID {
		t.Fatalf("batch ledger node did not resolve the loan transfer as a loan balance: %#v", ledgerNodes)
	}
	if _, ok := ledgerNode.(model.LedgerEntry); !ok {
		t.Fatalf("ledger node returned an unexpected value: %#v", ledgerNode)
	}
	loanBalances, err := (&Resolver{DB: db}).Query().LoanBalances(ctx, household.ID, nil, nil, nil, nil)
	if err != nil || len(loanBalances.Edges) != 1 || loanBalances.Edges[0].Node.ID != loanNodeID || loanBalances.Edges[0].Node.BalanceYen != 0 || loanBalances.Edges[0].Node.PrincipalYen != 5000 || loanBalances.Edges[0].Node.RepaidYen != 5000 {
		t.Fatalf("loan balance was not settled: balances=%#v error=%v", loanBalances, err)
	}
	loanNode, err := (&Resolver{DB: db}).Query().Node(ctx, loanNodeID)
	if err != nil {
		t.Fatal(err)
	}
	loanBalanceNode, ok := loanNode.(*model.LoanBalance)
	if !ok || loanBalanceNode.BalanceYen != 0 {
		t.Fatalf("loan node returned an unsettled balance: %#v", loanNode)
	}
	foreignMemberID := uuid.NewV7().String()
	foreignMemberUUID, _ := gofrsuuid.FromString(foreignMemberID)
	foreignSubject := "loan-node-other-household:" + foreignMemberID
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &foreignMemberUUID, OidcSubject: &foreignSubject, DisplayName: new("Other household member")}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(foreignMemberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	foreignCtx := auth.WithPrincipal(ctx, auth.Principal{MemberID: foreignMemberID, Subject: foreignSubject})
	foreignResolver := &mutationResolver{Resolver: &Resolver{DB: db}}
	foreignHousehold, err := foreignResolver.createHousehold(foreignCtx, model.CreateHouseholdInput{Name: "other loan household"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(foreignHousehold.ID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	loanNode, err = (&Resolver{DB: db}).Query().Node(foreignCtx, loanNodeID)
	if err != nil || loanNode != nil {
		t.Fatalf("loan node was available to a different household: node=%#v error=%v", loanNode, err)
	}
	settlement, err := (&Resolver{DB: db}).Query().Settlement(ctx, household.ID, "2026-06")
	if err != nil {
		t.Fatal(err)
	}
	settlementData := settlementData(t, settlement)
	if len(settlementData.Lines) != 2 {
		t.Fatalf("expected two member settlement lines: %#v", settlementData.Lines)
	}
	settlementNodeID, err := encodeSettlementNodeID(household.ID, string(settlementData.Period))
	if err != nil {
		t.Fatal(err)
	}
	settlementNode, err := (&Resolver{DB: db}).Query().Node(ctx, settlementNodeID)
	if err != nil {
		t.Fatal(err)
	}
	loadedSettlement, ok := settlementNode.(*model.OpenSettlement)
	if !ok || loadedSettlement.HouseholdID != household.ID || loadedSettlement.Period != settlementData.Period {
		t.Fatalf("settlement node did not resolve by household and period: %#v", settlementNode)
	}
	settlementNodes, err := (&Resolver{DB: db}).Query().Nodes(ctx, []string{settlementNodeID})
	if err != nil || len(settlementNodes) != 1 {
		t.Fatalf("settlement batch node lookup failed: nodes=%#v error=%v", settlementNodes, err)
	}
	loadedBatchSettlement, ok := settlementNodes[0].(*model.OpenSettlement)
	if !ok || loadedBatchSettlement.HouseholdID != household.ID || loadedBatchSettlement.Period != settlementData.Period {
		t.Fatalf("settlement batch node did not resolve by household and period: %#v", settlementNodes[0])
	}
	linesByMember := settlementByMember(t, settlementData.Lines)
	borrowerLine := linesByMember[memberB]
	if borrowerLine == nil || borrowerLine.MonthlyAdjustmentYen != 5000 || borrowerLine.PaidYen != 5000 || borrowerLine.AmountDueYen != 0 {
		t.Fatalf("borrower funding should cover the shared repayment obligation: %#v", borrowerLine)
	}
	lenderLine := linesByMember[memberA]
	if lenderLine == nil || lenderLine.MonthlyAdjustmentYen != 0 || lenderLine.AmountDueYen != 0 {
		t.Fatalf("shared repayment should settle the lender adjustment: %#v", lenderLine)
	}
	balances, err := (&Resolver{DB: db}).Query().AccountBalances(ctx, household.ID)
	if err != nil {
		t.Fatal(err)
	}
	balancesByAccount := make(map[string]int64, len(balances))
	for _, balance := range balances {
		balancesByAccount[balance.AccountID] = int64(balance.BalanceYen)
	}
	if balancesByAccount[sharedAccount.ID] != 5000 || balancesByAccount[lenderAccount.ID] != 5000 || balancesByAccount[borrowerAccount.ID] != 5000 {
		t.Fatalf("bank balances do not reflect opening balances, loan, funding, and repayment: %#v", balancesByAccount)
	}
	category, err := resolver.createCategory(ctx, model.CreateCategoryInput{HouseholdID: household.ID, Name: testGroceriesCategoryName})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: sharedPool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	budget, err := resolver.setBudget(ctx, budgetWriteInput{HouseholdID: household.ID, CategoryID: &category.ID, Name: "June groceries", AmountYen: 2000, StartsOn: "2026-06-01"})
	if err != nil {
		t.Fatal(err)
	}
	cardAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Shared card", Kind: model.AccountKindCard, PoolID: &sharedPool.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		HouseholdID: household.ID, IdempotencyKey: uuid.NewV7().String(), OccurredAt: "2026-06-15T12:00:00Z", Merchant: testMarketName, PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: cardAccount.ID, AmountYen: 1000}},
		Items: []*model.PurchaseItemInput{{Name: testGroceriesCategoryName, AmountYen: 1000, CategoryID: &category.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.recordAccountMovement(ctx, model.RecordAccountMovementInput{
		HouseholdID: household.ID, Kind: model.AccountMovementKindCardBillPayment, FromAccountID: sharedAccount.ID, ToAccountID: cardAccount.ID,
		AmountYen: 1000, OccurredAt: "2026-06-21T12:00:00Z", IdempotencyKey: uuid.NewV7().String(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.recordExternalIncome(ctx, model.RecordExternalIncomeInput{
		HouseholdID: household.ID, AccountID: sharedAccount.ID, AmountYen: 250, OccurredAt: "2026-06-21T12:00:00Z", IdempotencyKey: uuid.NewV7().String(), Description: "Interest",
	}); err != nil {
		t.Fatal(err)
	}
	balances, err = (&Resolver{DB: db}).Query().AccountBalances(ctx, household.ID)
	if err != nil {
		t.Fatal(err)
	}
	balancesByAccount = make(map[string]int64, len(balances))
	for _, balance := range balances {
		balancesByAccount[balance.AccountID] = int64(balance.BalanceYen)
	}
	if balancesByAccount[sharedAccount.ID] != 4250 || balancesByAccount[cardAccount.ID] != 0 {
		t.Fatalf("card clearing and external income produced incorrect balances: %#v", balancesByAccount)
	}
	analysis, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, sharedAccount.ID, "2026-06", "2026-06", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.OpeningBalanceYen != 5000 || analysis.ClosingBalanceYen != 4250 || len(analysis.Changes.Edges) != 4 {
		t.Fatalf("shared account history did not include the prior-period baseline and every in-period movement: %#v", analysis)
	}
	wantTypes := []string{"AccountIncome", graphQLAccountMovementType, "RepaymentTransferLedgerEntry", graphQLFundingLedgerEntryType}
	for index, wantType := range wantTypes {
		if sourceType := accountBalanceSourceType(analysis.Changes.Edges[index].Node.Source); sourceType != wantType {
			t.Fatalf("account history is not in reverse chronological order or omitted a source type: got %#v want source types %v", analysis.Changes, wantTypes)
		}
	}
	pageSize := 2
	allChanges, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, sharedAccount.ID, "2025-05", "2026-06", new(100), nil)
	if err != nil {
		t.Fatal(err)
	}
	firstPage, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, sharedAccount.ID, "2025-05", "2026-06", &pageSize, nil)
	if err != nil || len(firstPage.Changes.Edges) != pageSize || !firstPage.Changes.PageInfo.HasNextPage || firstPage.Changes.PageInfo.HasPreviousPage || firstPage.Changes.PageInfo.EndCursor == nil {
		t.Fatalf("first account history page is invalid: page=%#v error=%v", firstPage, err)
	}
	secondPage, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, sharedAccount.ID, "2025-05", "2026-06", &pageSize, firstPage.Changes.PageInfo.EndCursor)
	if err != nil || len(secondPage.Changes.Edges) != pageSize || !secondPage.Changes.PageInfo.HasPreviousPage || secondPage.Changes.PageInfo.EndCursor == nil {
		t.Fatalf("second account history page is invalid: page=%#v error=%v", secondPage, err)
	}
	thirdPage, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, sharedAccount.ID, "2025-05", "2026-06", &pageSize, secondPage.Changes.PageInfo.EndCursor)
	if err != nil || len(thirdPage.Changes.Edges) != 1 || !thirdPage.Changes.PageInfo.HasPreviousPage || thirdPage.Changes.PageInfo.HasNextPage || thirdPage.Changes.PageInfo.EndCursor == nil {
		t.Fatalf("final account history page is invalid: page=%#v error=%v", thirdPage, err)
	}
	if firstPage.OpeningBalanceYen != allChanges.OpeningBalanceYen || firstPage.ClosingBalanceYen != allChanges.ClosingBalanceYen || secondPage.OpeningBalanceYen != allChanges.OpeningBalanceYen || secondPage.ClosingBalanceYen != allChanges.ClosingBalanceYen || thirdPage.OpeningBalanceYen != allChanges.OpeningBalanceYen || thirdPage.ClosingBalanceYen != allChanges.ClosingBalanceYen || len(allChanges.Changes.Edges) != 5 {
		t.Fatalf("pagination changed the multi-year analysis range or complete balance: all=%#v first=%#v second=%#v third=%#v", allChanges, firstPage, secondPage, thirdPage)
	}
	hasTimestampTie := false
	for index := 1; index < len(allChanges.Changes.Edges); index++ {
		previous, current := allChanges.Changes.Edges[index-1], allChanges.Changes.Edges[index]
		if previous.Node.OccurredAt == current.Node.OccurredAt {
			hasTimestampTie = true
			previousID, err := cursorID(&previous.Cursor, cursorTypeAccountBalanceChange)
			if err != nil {
				t.Fatal(err)
			}
			currentID, err := cursorID(&current.Cursor, cursorTypeAccountBalanceChange)
			if err != nil {
				t.Fatal(err)
			}
			if *previousID < *currentID {
				t.Fatalf("account history ties are not ordered by descending journal ID: previous=%s current=%s", *previousID, *currentID)
			}
		}
	}
	if !hasTimestampTie {
		t.Fatal("account history fixture must include changes with the same timestamp")
	}
	start := 0
	for _, page := range []*model.AccountBalanceChangeConnection{firstPage.Changes, secondPage.Changes, thirdPage.Changes} {
		for changeIndex, edge := range page.Edges {
			want := allChanges.Changes.Edges[start+changeIndex].Node
			if edge.Node.ChangeYen != want.ChangeYen || edge.Node.BalanceYen != want.BalanceYen || edge.Node.OccurredAt != want.OccurredAt {
				t.Fatalf("paged account history changed a change or its running balance: got=%#v want=%#v", edge.Node, want)
			}
		}
		start += len(page.Edges)
	}
	borrowerHistory, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, borrowerAccount.ID, "2026-06", "2026-06", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if borrowerHistory.OpeningBalanceYen != 5000 || borrowerHistory.ClosingBalanceYen != 5000 || len(borrowerHistory.Changes.Edges) != 2 || accountBalanceSourceType(borrowerHistory.Changes.Edges[0].Node.Source) != graphQLFundingLedgerEntryType || borrowerHistory.Changes.Edges[0].Node.ChangeYen != -5000 || accountBalanceSourceType(borrowerHistory.Changes.Edges[1].Node.Source) != "LoanTransferLedgerEntry" || borrowerHistory.Changes.Edges[1].Node.ChangeYen != 5000 {
		t.Fatalf("borrower account history did not track loan proceeds and attributed funding outflow: %#v", borrowerHistory)
	}
	lenderHistory, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, lenderAccount.ID, "2026-06", "2026-06", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if lenderHistory.OpeningBalanceYen != 5000 || lenderHistory.ClosingBalanceYen != 5000 || len(lenderHistory.Changes.Edges) != 2 || accountBalanceSourceType(lenderHistory.Changes.Edges[0].Node.Source) != "RepaymentTransferLedgerEntry" || lenderHistory.Changes.Edges[0].Node.ChangeYen != 5000 || accountBalanceSourceType(lenderHistory.Changes.Edges[1].Node.Source) != "LoanTransferLedgerEntry" || lenderHistory.Changes.Edges[1].Node.ChangeYen != -5000 {
		t.Fatalf("lender account history did not track outgoing loan and repayment receipt: %#v", lenderHistory)
	}
	beforeOpening, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, lenderAccount.ID, testApril2026, testApril2026, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if beforeOpening.OpeningBalanceYen != 0 || beforeOpening.ClosingBalanceYen != 0 || len(beforeOpening.Changes.Edges) != 0 {
		t.Fatalf("an account opening balance was applied before its effective date: %#v", beforeOpening)
	}
	openingMonth, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, lenderAccount.ID, "2026-05", "2026-05", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if openingMonth.OpeningBalanceYen != 0 || openingMonth.ClosingBalanceYen != 5000 || len(openingMonth.Changes.Edges) != 1 || accountBalanceSourceType(openingMonth.Changes.Edges[0].Node.Source) != "AccountOpeningBalance" {
		t.Fatalf("the opening balance did not appear at its effective date: %#v", openingMonth)
	}
	openingSource, ok := openingMonth.Changes.Edges[0].Node.Source.(*model.AccountOpeningBalance)
	if !ok || openingSource.Account == nil || openingSource.Account.ID != lenderAccount.ID || openingSource.Account.Name != "Lender bank" {
		t.Fatalf("the opening balance source did not include its account: %#v", openingMonth.Changes.Edges[0].Node.Source)
	}
	resolvedNode, err := (&Resolver{DB: db}).Query().Node(ctx, openingSource.ID)
	if err != nil {
		t.Fatal(err)
	}
	resolvedOpeningSource, ok := resolvedNode.(*model.AccountOpeningBalance)
	if !ok || resolvedOpeningSource.Account == nil || resolvedOpeningSource.Account.ID != lenderAccount.ID {
		t.Fatalf("the opening balance node did not resolve its account: %#v", resolvedNode)
	}
	cardAnalysis, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, cardAccount.ID, "2026-06", "2026-06", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cardAnalysis.OpeningBalanceYen != 0 || cardAnalysis.ClosingBalanceYen != 0 || len(cardAnalysis.Changes.Edges) != 2 || cardAnalysis.Changes.Edges[0].Node.ChangeYen != -1000 || cardAnalysis.Changes.Edges[1].Node.ChangeYen != 1000 {
		t.Fatalf("card history did not show purchase liability increase and bill-payment decrease: %#v", cardAnalysis)
	}
	outsiderCtx := auth.WithPrincipal(ctx, auth.Principal{MemberID: uuid.NewV7().String(), Subject: "outsider"})
	if _, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(outsiderCtx, household.ID, sharedAccount.ID, "2026-06", "2026-06", nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("account history should be household-authorized: %v", err)
	}
	spending, err := (&Resolver{DB: db}).Query().MonthlyCategorySpending(ctx, household.ID, "2026-06", "2026-07")
	if err != nil || len(spending) != 1 || spending[0].AmountYen != 1000 {
		t.Fatalf("income, funding, and account transfers changed household spending: spending=%#v error=%v", spending, err)
	}
	budgets, err := (&Resolver{DB: db}).Query().Budgets(ctx, household.ID, model.YearMonth("2026-06"), nil, nil, nil, nil)
	if err != nil || len(budgets.Edges) != 1 || budgets.Edges[0].Node.ID != budget.ID || budgets.Edges[0].Node.ActualYen != 1000 {
		t.Fatalf("account activity changed budget actuals: budgets=%#v error=%v", budgets, err)
	}
	correctionKey := uuid.NewV7().String()
	correctionInput := model.CorrectPurchaseInput{HouseholdID: household.ID, Target: &model.CorrectPurchaseTargetInput{Purchase: &model.PurchaseCorrectionPurchaseTargetInput{PurchaseID: purchase.ID}}, AmountDeltaYen: -1000, Reason: "Full return", IdempotencyKey: correctionKey}
	correctedPurchase, err := resolver.correctPurchase(ctx, correctionInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.correctPurchase(ctx, correctionInput); err != nil {
		t.Fatalf("retrying a full refund with the same request should succeed: %v", err)
	}
	if _, err := resolver.correctPurchase(ctx, model.CorrectPurchaseInput{
		HouseholdID:    household.ID,
		Target:         &model.CorrectPurchaseTargetInput{Item: &model.PurchaseCorrectionItemTargetInput{PurchaseID: purchase.ID, ItemID: correctedPurchase.Items[0].ID}},
		AmountDeltaYen: -1,
		Reason:         "Refund after full return",
		IdempotencyKey: uuid.NewV7().String(),
	}); !errors.Is(err, ErrBadInput) {
		t.Fatalf("item refund after a full purchase refund should exceed the remaining purchase amount: %v", err)
	}
	correctionConnection, err := purchaseCorrectionConnection(ctx, db, generatedUUID(correctedPurchase.ID), nil, nil)
	if err != nil || len(correctionConnection.Edges) != 1 {
		t.Fatalf("purchase correction history is missing: connection=%#v error=%v", correctionConnection, err)
	}
	correctionOccurredAt, err := time.Parse(time.RFC3339Nano, string(correctionConnection.Edges[0].Node.GetOccurredAt()))
	if err != nil {
		t.Fatal(err)
	}
	correctionMonth := accountingPeriod(correctionOccurredAt)
	correctionAnalysis, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, cardAccount.ID, model.YearMonth(correctionMonth), model.YearMonth(correctionMonth), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(correctionAnalysis.Changes.Edges) != 1 || accountBalanceSourceType(correctionAnalysis.Changes.Edges[0].Node.Source) != "PurchaseWideCorrection" || correctionAnalysis.Changes.Edges[0].Node.Description != "Full return" || correctionAnalysis.Changes.Edges[0].Node.ChangeYen != -1000 || correctionAnalysis.ClosingBalanceYen != -1000 {
		t.Fatalf("account history omitted the card refund correction: %#v", correctionAnalysis)
	}
	correctionInput.AmountDeltaYen = -500
	if _, err := resolver.correctPurchase(ctx, correctionInput); !errors.Is(err, ErrConflict) {
		t.Fatalf("reusing a correction idempotency key with a different payload should conflict: %v", err)
	}
	repaymentUUID, _ := gofrsuuid.FromString(*repayment.TransferID)
	journals, err := models.Journals.Query(sm.Where(models.Journals.Columns.SourceID.EQ(psql.Arg(repaymentUUID)))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(journals) != 1 {
		t.Fatalf("repayment journal was not created: journals=%#v error=%v", journals, err)
	}
	entries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(journals[0].ID))), sm.OrderBy(models.JournalEntries.Columns.LineNo)).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(entries) != 8 {
		t.Fatalf("repayment journal has unexpected entries: entries=%#v error=%v", entries, err)
	}
	sharedAccountUUID, _ := gofrsuuid.FromString(sharedAccount.ID)
	var sourceAccountCredit, destinationAccountDebit int64
	for _, entry := range entries {
		if !entry.AccountID.Valid {
			continue
		}
		if entry.AccountID.V == sharedAccountUUID {
			sourceAccountCredit += entry.CreditYen
		}
		if entry.AccountID.V.String() == lenderAccount.ID {
			destinationAccountDebit += entry.DebitYen
		}
	}
	if sourceAccountCredit != 5000 || destinationAccountDebit != 5000 {
		t.Fatalf("repayment journal did not reduce the shared bank and increase the lender bank: source credit=%d destination debit=%d", sourceAccountCredit, destinationAccountDebit)
	}
	assertHouseholdJournalsBalance(t, ctx, db, generatedUUID(household.ID))

	for index, day := range []string{"2026-06-15", "2026-06-16"} {
		loan, err := resolver.recordTransfer(ctx, model.RecordTransferInput{
			HouseholdID: household.ID, FromMemberID: memberA, ToMemberID: memberB, Kind: model.TransferInputKindLoan,
			AmountYen: model.Long(1000 + index), OccurredAt: model.DateTime(day + "T12:00:00Z"), Description: "Additional loan",
			IdempotencyKey: uuid.NewV7().String(),
		})
		if err != nil || loan.TransferID == nil {
			t.Fatalf("additional loan failed: loan=%#v error=%v", loan, err)
		}
	}
	query := (&Resolver{DB: db}).Query()
	allLoans, err := query.LoanBalances(ctx, household.ID, nil, nil, new(10), nil)
	if err != nil || len(allLoans.Edges) != 3 || allLoans.TotalCount != 3 {
		t.Fatalf("loan list did not exclude unrelated transfers: loans=%#v error=%v", allLoans, err)
	}
	loanPageSize := 2
	firstLoans, err := query.LoanBalances(ctx, household.ID, nil, nil, &loanPageSize, nil)
	if err != nil || len(firstLoans.Edges) != loanPageSize || !firstLoans.PageInfo.HasNextPage || firstLoans.PageInfo.HasPreviousPage || firstLoans.PageInfo.EndCursor == nil {
		t.Fatalf("first loan page is invalid: loans=%#v error=%v", firstLoans, err)
	}
	lastLoans, err := query.LoanBalances(ctx, household.ID, firstLoans.PageInfo.EndCursor, nil, &loanPageSize, nil)
	if err != nil || len(lastLoans.Edges) != 1 || !lastLoans.PageInfo.HasPreviousPage || lastLoans.PageInfo.HasNextPage {
		t.Fatalf("last loan page is invalid: loans=%#v error=%v", lastLoans, err)
	}
	for index, edge := range firstLoans.Edges {
		if edge.Node.ID != allLoans.Edges[index].Node.ID {
			t.Fatalf("first loan page changed order at %d: got=%s want=%s", index, edge.Node.ID, allLoans.Edges[index].Node.ID)
		}
	}
	if lastLoans.Edges[0].Node.ID != allLoans.Edges[2].Node.ID {
		t.Fatalf("last loan page changed order: got=%s want=%s", lastLoans.Edges[0].Node.ID, allLoans.Edges[2].Node.ID)
	}
	newestLoanCursor, oldestLoanCursor := allLoans.Edges[0].Cursor, allLoans.Edges[2].Cursor
	singleLoanPageSize := 1
	afterNewestLoan, err := query.LoanBalances(ctx, household.ID, &newestLoanCursor, nil, &singleLoanPageSize, nil)
	if err != nil || len(afterNewestLoan.Edges) != 1 || afterNewestLoan.PageInfo.HasPreviousPage || afterNewestLoan.Edges[0].Node.ID != allLoans.Edges[1].Node.ID {
		t.Fatalf("after newest loan cursor page is invalid: page=%#v error=%v", afterNewestLoan, err)
	}
	afterOldestLoan, err := query.LoanBalances(ctx, household.ID, &oldestLoanCursor, nil, &singleLoanPageSize, nil)
	if err != nil || len(afterOldestLoan.Edges) != 0 || !afterOldestLoan.PageInfo.HasPreviousPage || afterOldestLoan.PageInfo.HasNextPage {
		t.Fatalf("after oldest loan cursor page is invalid: page=%#v error=%v", afterOldestLoan, err)
	}

	invalidSourceJournalID, invalidSourceID := generatedUUID(uuid.NewV7().String()), generatedUUID(uuid.NewV7().String())
	invalidSourceKind, invalidSourceKey := "UNKNOWN_SOURCE", "unknown-source:"+uuid.NewV7().String()
	invalidSourceAt := time.Date(2026, time.May, 2, 12, 0, 0, 0, time.UTC)
	if _, err := models.Journals.Insert(&models.JournalSetter{
		ID: &invalidSourceJournalID, HouseholdID: &householdUUID, SourceKind: &invalidSourceKind, SourceID: &invalidSourceID,
		IdempotencyKey: &invalidSourceKey, OccurredAt: &invalidSourceAt, CreatedBy: &memberAUUID,
	}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	debitAccount, emptyAccount := sql.Null[gofrsuuid.UUID]{V: sharedAccountUUID, Valid: true}, sql.Null[gofrsuuid.UUID]{}
	debit, credit := int64(1), int64(1)
	debitLine, creditLine := int32(1), int32(2)
	if _, err := models.JournalEntries.Insert(
		&models.JournalEntrySetter{JournalID: &invalidSourceJournalID, LineNo: &debitLine, AccountID: &debitAccount, DebitYen: &debit, HouseholdID: &householdUUID},
		&models.JournalEntrySetter{JournalID: &invalidSourceJournalID, LineNo: &creditLine, AccountID: &emptyAccount, CreditYen: &credit, HouseholdID: &householdUUID},
	).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	firstAccountHistory, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, sharedAccount.ID, "2025-05", "2026-12", new(1), nil)
	if err != nil || len(firstAccountHistory.Changes.Edges) != 1 || firstAccountHistory.Changes.TotalCount != 6 {
		t.Fatalf("page outside an invalid source journal should remain readable: analysis=%#v error=%v", firstAccountHistory, err)
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

func settlementData(t *testing.T, settlement model.SettlementVariant) *model.Settlement {
	t.Helper()
	switch settlement := settlement.(type) {
	case *model.OpenSettlement:
		return settlement.Settlement
	case *model.ClosedSettlement:
		return settlement.Settlement
	default:
		t.Fatalf("unexpected settlement type %T", settlement)
		return nil
	}
}

func TestAccountingPeriodsUseJapanTimeBoundaries(t *testing.T) {
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
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	memberID := uuid.NewV7().String()
	memberUUID, _ := gofrsuuid.FromString(memberID)
	subject, displayName := "period-boundary:"+memberID, "JST"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: "period-boundary:" + memberID, DisplayName: "JST"})
	apiResolver := &Resolver{DB: db}
	mutation := &mutationResolver{Resolver: apiResolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Japan time boundary"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(household.ID)))).Exec(t.Context(), bobpgx.NewPool(db))
	}()
	pool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: testPersonalPoolName, Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	account, err := mutation.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: testCardAccountName, Kind: model.AccountKindCard, PoolID: &pool.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setFundingSchedule(ctx, model.SetFundingScheduleInput{HouseholdID: household.ID, MemberID: memberID, PoolID: pool.ID, AmountYen: 700, Cadence: testMonthlyCadence, StartsOn: "2026-10-01"}); err != nil {
		t.Fatal(err)
	}
	for _, occurredAt := range []string{"2026-09-30T23:59:59+09:00", "2026-10-01T00:00:00+09:00"} {
		purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: model.DateTime(occurredAt), Merchant: "boundary", PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 100}}, Items: []*model.PurchaseItemInput{{Name: "boundary item", AmountYen: 100}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mutation.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
			t.Fatal(err)
		}
	}
	spending, err := apiResolver.Query().MonthlyCategorySpending(ctx, household.ID, model.YearMonth("2026-09"), model.YearMonth(testOctober2026))
	if err != nil {
		t.Fatal(err)
	}
	spendingByPeriod := make(map[string]int64, len(spending))
	for _, item := range spending {
		spendingByPeriod[string(item.Period)] += int64(item.AmountYen)
	}
	if spendingByPeriod["2026-09"] != 100 || spendingByPeriod[testOctober2026] != 100 {
		t.Fatalf("monthly spending did not follow Japan time boundaries: %#v", spendingByPeriod)
	}
	closed, err := mutation.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: "2026-09", ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	closedData := settlementData(t, closed)
	if len(closedData.Lines) != 1 || closedData.Lines[0].ScheduledYen != 0 {
		t.Fatalf("October schedule leaked into September close: %#v", closedData.Lines)
	}
	october, err := apiResolver.Query().Settlement(ctx, household.ID, testOctober2026)
	if err != nil {
		t.Fatal(err)
	}
	octoberData := settlementData(t, october)
	if len(octoberData.Lines) != 1 || octoberData.Lines[0].ScheduledYen != 700 {
		t.Fatalf("October schedule was missing at its Japan-time start: %#v", octoberData.Lines)
	}
	purchaseRows, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.HouseholdID.EQ(psql.Arg(household.ID))), sm.OrderBy(models.Purchases.Columns.OccurredAt)).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	statuses := make([]string, 0, len(purchaseRows))
	for _, row := range purchaseRows {
		statuses = append(statuses, row.Status)
	}
	if len(statuses) != 2 || statuses[0] != "CLOSED" || statuses[1] != testPostedStatus {
		t.Fatalf("period close crossed the Japan-time boundary: %#v", statuses)
	}
}
