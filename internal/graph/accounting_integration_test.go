package graph

import (
	"context"
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

func TestAccountingFlowUsesScheduleAndActualFundingSeparately(t *testing.T) {
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
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberA)))).Exec(context.Background(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberB)))).Exec(context.Background(), bobpgx.NewPool(db))
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
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(context.Background(), bobpgx.NewPool(db))
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
	paymentAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: householdID, Name: "Card", Kind: model.AccountKindCard, PoolID: &poolA.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: householdID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 3}, {PoolID: poolB.ID, Weight: 2}}}); err != nil {
		t.Fatal(err)
	}
	for _, schedule := range []model.SetFundingScheduleInput{
		{HouseholdID: householdID, MemberID: memberA, PoolID: poolA.ID, AmountYen: 6000, Cadence: "MONTHLY", StartsOn: "2026-01-01"},
		{HouseholdID: householdID, MemberID: memberB, PoolID: poolB.ID, AmountYen: 4000, Cadence: "MONTHLY", StartsOn: "2026-01-01"},
	} {
		if _, err := resolver.setFundingSchedule(ctx, schedule); err != nil {
			t.Fatal(err)
		}
	}
	category, err := resolver.createCategory(ctx, model.CreateCategoryInput{HouseholdID: householdID, Name: "groceries"})
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
	categoryRule, err := resolver.setAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, CategoryID: &category.ID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 1}, {PoolID: poolB.ID, Weight: 3}}})
	if err != nil || categoryRule.Scope != model.AllocationRuleScopeCategory {
		t.Fatalf("category allocation rule was not saved: rule=%#v error=%v", categoryRule, err)
	}
	storeRule, err := resolver.setAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, Merchant: &storeRuleMerchant, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 4}, {PoolID: poolB.ID, Weight: 1}}})
	if err != nil || storeRule.Scope != model.AllocationRuleScopeMerchant || *storeRule.Merchant != "market" {
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
	productConceptRule, err := resolver.setAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, ProductConceptID: &productConcept.ID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 1}, {PoolID: poolB.ID, Weight: 4}}})
	if err != nil || productConceptRule.Scope != model.AllocationRuleScopeProduct {
		t.Fatalf("product concept rule was not saved: rule=%#v error=%v", productConceptRule, err)
	}
	conceptPurchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T11:59:00Z", Merchant: "market", Items: []*model.PurchaseItemInput{{Name: "YOG-PB 400", AmountYen: 500, ProductVariantID: &productVariant.ID}}})
	if err != nil || conceptPurchase.Items[0].CategoryID == nil || *conceptPurchase.Items[0].CategoryID != category.ID || conceptPurchase.Items[0].Allocations[0].AmountYen == nil || *conceptPurchase.Items[0].Allocations[0].AmountYen != 100 {
		t.Fatalf("linked product should inherit its category and concept rule: purchase=%#v error=%v", conceptPurchase, err)
	}
	productVariantRule, err := resolver.setAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, ProductVariantID: &productVariant.ID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 4}, {PoolID: poolB.ID, Weight: 1}}})
	if err != nil || productVariantRule.Scope != model.AllocationRuleScopeProductVariant {
		t.Fatalf("product variant rule was not saved: rule=%#v error=%v", productVariantRule, err)
	}
	variantPurchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T11:58:00Z", Merchant: "market", Items: []*model.PurchaseItemInput{{Name: "YOG-PB 400", AmountYen: 600, ProductVariantID: &productVariant.ID}}})
	if err != nil || variantPurchase.Items[0].Allocations[0].AmountYen == nil || *variantPurchase.Items[0].Allocations[0].AmountYen != 480 {
		t.Fatalf("product variant rule should precede concept/category/merchant rules: purchase=%#v error=%v", variantPurchase, err)
	}
	correctedPurchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T11:57:00Z", Merchant: "market", Items: []*model.PurchaseItemInput{{Name: "YOG-PB 400", AmountYen: 350}}})
	if err != nil || correctedPurchase.Items[0].ProductVariantID != nil {
		t.Fatalf("unmatched product should remain unnormalized until the user selects a candidate: purchase=%#v error=%v", correctedPurchase, err)
	}
	learnedItem, err := resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: correctedPurchase.Items[0].ID, BaseRevision: correctedPurchase.Items[0].Revision, ProductVariantID: &productVariant.ID, ProductLearning: new(model.ProductLearningScopeMerchantAlias), ChangedFields: []string{"productVariantId"}})
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
	learnedItem, err = resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: learnedItem.ID, BaseRevision: learnedItem.Revision, ProductVariantID: &productVariant.ID, ProductLearning: new(model.ProductLearningScopeVariantName), VariantName: &canonicalName, ChangedFields: []string{"productVariantId"}})
	if err != nil {
		t.Fatalf("normalized product name correction failed: %v", err)
	}
	suggestions, err = apiResolver.Query().ProductSuggestions(ctx, householdID, "unsweetened", nil, nil)
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
	rules, err := apiResolver.Query().AllocationRules(ctx, householdID)
	if err != nil || len(rules) != 4 {
		t.Fatalf("allocation rule query failed: rules=%#v error=%v", rules, err)
	}
	purchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, OccurredAt: "2026-04-12T12:00:00Z", Merchant: "market",
		Items: []*model.PurchaseItemInput{{Name: "yogurt", AmountYen: 500, CategoryID: &category.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if purchase.Items[0].Allocations[0].AmountYen == nil || *purchase.Items[0].Allocations[0].AmountYen != 125 || purchase.Items[0].Allocations[1].AmountYen == nil || *purchase.Items[0].Allocations[1].AmountYen != 375 {
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
	updatedCategoryRule, err := resolver.setAllocationRule(ctx, model.SetAllocationRuleInput{HouseholdID: householdID, CategoryID: &category.ID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 3}, {PoolID: poolB.ID, Weight: 2}}})
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
	if err != nil || merchantOnlyPurchase.Items[0].Allocations[0].AmountYen == nil || *merchantOnlyPurchase.Items[0].Allocations[0].AmountYen != 400 || merchantOnlyPurchase.Items[0].Allocations[1].AmountYen == nil || *merchantOnlyPurchase.Items[0].Allocations[1].AmountYen != 100 {
		t.Fatalf("merchant rule should apply when no category rule matches: purchase=%#v error=%v", merchantOnlyPurchase, err)
	}
	if _, err := models.Purchases.Delete(dm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(merchantOnlyPurchase.ID)))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	if purchase.Items[0].Allocations[0].AmountYen == nil || *purchase.Items[0].Allocations[0].AmountYen != 125 || purchase.Items[0].Allocations[1].AmountYen == nil || *purchase.Items[0].Allocations[1].AmountYen != 375 {
		t.Fatalf("updating the category rule changed an existing purchase snapshot: %#v", purchase.Items[0].Allocations)
	}
	changedAmount := model.Long(500)
	changedAllocations := []*model.AllocationInput{{PoolID: poolA.ID, Weight: 3}, {PoolID: poolB.ID, Weight: 2}}
	updatedItem, err := resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: purchase.Items[0].ID, BaseRevision: purchase.Items[0].Revision, AmountYen: &changedAmount, Allocations: changedAllocations, ChangedFields: []string{"amountYen", "allocations"}})
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
	_, err = resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: updatedItem.ID, BaseRevision: updatedItem.Revision, AmountYen: &invalidAmount, Allocations: invalidAllocations, ChangedFields: []string{"amountYen", "allocations"}})
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
	purchase, err = resolver.updatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: purchase.ID, BaseRevision: purchase.Revision, Patch: &model.PurchasePatch{Merchant: &merchant}, ChangedFields: []string{"merchant"}})
	if err != nil {
		t.Fatal(err)
	}
	occurredAt := "2026-04-13T12:00:00Z"
	purchase, err = resolver.updatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: purchase.ID, BaseRevision: 1, Patch: &model.PurchasePatch{OccurredAt: &occurredAt}, ChangedFields: []string{"occurredAt"}})
	if err != nil {
		t.Fatalf("disjoint stale fields should merge: %v", err)
	}
	if _, err := resolver.updatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: purchase.ID, BaseRevision: 1, Patch: &model.PurchasePatch{Merchant: &merchant}, ChangedFields: []string{"merchant"}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("same stale field should conflict, got %v", err)
	}
	if len(purchase.Items) != 1 || purchase.Items[0].Allocations[0].AmountYen == nil || *purchase.Items[0].Allocations[0].AmountYen != 300 || *purchase.Items[0].Allocations[1].AmountYen != 200 {
		t.Fatalf("unexpected allocation snapshot: %#v", purchase.Items)
	}
	documentID := uuid.NewV7().String()
	documentUUID, _ := gofrsuuid.FromString(documentID)
	memberAUUID, _ := gofrsuuid.FromString(memberA)
	hash, objectKey, mimeType, sourceKind := "hash-"+documentID, householdID+"/originals/"+documentID, "image/jpeg", "receipt"
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &documentUUID, HouseholdID: &householdUUID, Sha256: &hash, ObjectKey: &objectKey, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberAUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	jobID := uuid.NewV7().String()
	jobUUID, _ := gofrsuuid.FromString(jobID)
	jobVersion, jobStatus := int32(1), "succeeded"
	verified := ingestion.Verification{
		Receipt: ingestion.Receipt{Total: 250, Items: []ingestion.Item{
			{Name: "milk", PrintedName: "milk original", PrintedAmount: 150, GrossAmount: 150, Region: &ingestion.Region{Page: 1, Top: 0.1, Left: 0.2, Bottom: 0.3, Right: 0.4}},
			{Name: "bread", PrintedName: "bread original", PrintedAmount: 100, GrossAmount: 100},
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
	fromSource := model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, SourceDocumentID: &documentID, OccurredAt: "2026-04-20T12:00:00Z", Merchant: "source", Items: []*model.PurchaseItemInput{{Name: "milk", AmountYen: 150}, {Name: "bread", AmountYen: 100}}}
	mismatchedSource := fromSource
	mismatchedSource.Items = fromSource.Items[:1]
	if _, err := resolver.createPurchase(ctx, mismatchedSource); !errors.Is(err, ErrBadInput) {
		t.Fatalf("receipt import should reject an unmatched item count, got %v", err)
	}
	wrongAmountSource := fromSource
	wrongAmountSource.Items = []*model.PurchaseItemInput{{Name: "milk", AmountYen: 151}, {Name: "bread", AmountYen: 99}}
	if _, err := resolver.createPurchase(ctx, wrongAmountSource); !errors.Is(err, ErrBadInput) {
		t.Fatalf("receipt import should reject unverified item amounts, got %v", err)
	}
	confirmed, err := resolver.createPurchase(ctx, fromSource)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Items[0].Allocations[0].AmountYen == nil || *confirmed.Items[0].Allocations[0].AmountYen != 90 || confirmed.Items[0].Allocations[1].AmountYen == nil || *confirmed.Items[0].Allocations[1].AmountYen != 60 || confirmed.Items[1].Allocations[0].AmountYen == nil || *confirmed.Items[1].Allocations[0].AmountYen != 60 || confirmed.Items[1].Allocations[1].AmountYen == nil || *confirmed.Items[1].Allocations[1].AmountYen != 40 {
		t.Fatalf("global default allocation was not snapshotted: %#v", confirmed.Items)
	}
	evidenceLinks, err := models.EvidenceLinks.Query(sm.Where(models.EvidenceLinks.Columns.PurchaseID.EQ(psql.Arg(generatedUUID(confirmed.ID))))).All(ctx, bobpgx.NewPool(db))
	if err != nil || len(evidenceLinks) != 3 {
		t.Fatalf("source purchase should persist document and item evidence in the create transaction: links=%d error=%v", len(evidenceLinks), err)
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
	if err != nil || confirmed.Items[0].Allocations[0].AmountYen == nil || *confirmed.Items[0].Allocations[0].AmountYen != 90 || confirmed.Items[0].Allocations[1].AmountYen == nil || *confirmed.Items[0].Allocations[1].AmountYen != 60 || confirmed.Items[1].Allocations[0].AmountYen == nil || *confirmed.Items[1].Allocations[0].AmountYen != 60 || confirmed.Items[1].Allocations[1].AmountYen == nil || *confirmed.Items[1].Allocations[1].AmountYen != 40 {
		t.Fatalf("changing the global rule changed a prior purchase snapshot: purchase=%#v error=%v", confirmed.Items, err)
	}
	fromSource.IdempotencyKey = uuid.NewV7().String()
	fromSource.Items = []*model.PurchaseItemInput{{Name: "duplicate", AmountYen: 999}}
	replayed, err := resolver.createPurchase(ctx, fromSource)
	if err != nil || replayed.ID != confirmed.ID {
		t.Fatalf("source confirmation should be idempotent: purchase=%#v error=%v", replayed, err)
	}
	rowReference := "transaction:card-2"
	rowInput := model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: householdID, SourceDocumentID: &documentID, SourceReference: &rowReference, OccurredAt: "2026-04-20T12:01:00Z", Merchant: "statement row", Items: []*model.PurchaseItemInput{{Name: "statement row", AmountYen: 1}}}
	rowPurchase, err := resolver.createPurchase(ctx, rowInput)
	if err != nil || rowPurchase.ID == confirmed.ID {
		t.Fatalf("a distinct statement row should confirm independently: purchase=%#v error=%v", rowPurchase, err)
	}
	rowInput.IdempotencyKey = uuid.NewV7().String()
	rowReplay, err := resolver.createPurchase(ctx, rowInput)
	if err != nil || rowReplay.ID != rowPurchase.ID {
		t.Fatalf("statement row confirmation should be idempotent: purchase=%#v error=%v", rowReplay, err)
	}
	if _, err := resolver.postPurchase(ctx, confirmed.ID, confirmed.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := models.DocumentConfirmations.Delete(dm.Where(psql.And(models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)), models.DocumentConfirmations.Columns.SourceReference.EQ(psql.Arg(rowReference))))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	rowPurchaseUUID, _ := gofrsuuid.FromString(rowPurchase.ID)
	if _, err := psql.Delete(dm.From(models.EvidenceLinks.NameExpr()), dm.Where(models.EvidenceLinks.Columns.PurchaseID.EQ(psql.Arg(rowPurchaseUUID)))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	if _, err := models.Purchases.Delete(dm.Where(models.Purchases.Columns.ID.EQ(psql.Arg(rowPurchaseUUID)))).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	confirmedRows, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.HouseholdID.EQ(psql.Arg(householdUUID)))).All(ctx, bobpgx.NewPool(db))
	confirmedPurchases := len(confirmedRows)
	if err != nil || confirmedPurchases != 2 {
		t.Fatalf("source replay created a duplicate purchase: count=%d error=%v", confirmedPurchases, err)
	}
	if _, err := resolver.recordFunding(ctx, model.RecordFundingInput{HouseholdID: householdID, MemberID: memberA, PoolID: poolA.ID, AmountYen: 6000, OccurredAt: "2026-04-15T10:00:00Z", IdempotencyKey: uuid.NewV7().String()}); err != nil {
		t.Fatal(err)
	}
	postedPurchase, err := resolver.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String())
	if err != nil {
		t.Fatal(err)
	}
	postedMerchant := "edited after posting"
	postedOccurredAt := "2026-04-14T12:00:00Z"
	postedPaymentAccountID := paymentAccount.ID
	postedPurchase, err = resolver.updatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: postedPurchase.ID, BaseRevision: postedPurchase.Revision, Patch: &model.PurchasePatch{Merchant: &postedMerchant, OccurredAt: &postedOccurredAt, PaymentAccountID: &postedPaymentAccountID}, ChangedFields: []string{"merchant", "occurredAt", "paymentAccountId"}})
	if err != nil {
		t.Fatalf("open-period metadata update should allow a posted purchase: %v", err)
	}
	postedAmount := model.Long(700)
	postedAllocations := []*model.AllocationInput{{PoolID: poolA.ID, Weight: 3}, {PoolID: poolB.ID, Weight: 2}}
	postedItem, err := resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: postedPurchase.Items[0].ID, BaseRevision: postedPurchase.Items[0].Revision, AmountYen: &postedAmount, Allocations: postedAllocations, ChangedFields: []string{"amountYen", "allocations"}})
	if err != nil || postedItem.AmountYen != 700 {
		t.Fatalf("open-period amount/allocation update should allow a posted purchase: item=%#v error=%v", postedItem, err)
	}
	postedItem, err = resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: postedItem.ID, BaseRevision: postedItem.Revision, ChangedFields: []string{"categoryId"}})
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
	postedItem, err = resolver.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{ItemID: postedItem.ID, BaseRevision: postedItem.Revision, CategoryID: &category.ID, ChangedFields: []string{"categoryId"}})
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
	if err != nil || len(ledgerRows) != 1 || ledgerDescription != postedMerchant || ledgerAmount != 700 || ledgerOccurredAt != postedOccurredAt {
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
		t.Fatalf("posted purchase payment account was not updated: account=%q error=%v", journalAccountID, err)
	}
	postedPurchase, err = resolver.updatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: postedPurchase.ID, BaseRevision: postedPurchase.Revision, Patch: &model.PurchasePatch{}, ChangedFields: []string{"paymentAccountId"}})
	if err != nil || postedPurchase.PaymentAccountID != nil {
		t.Fatalf("explicit null payment account should clear the account: purchase=%#v error=%v", postedPurchase, err)
	}
	journals, err = models.Journals.Query(sm.Where(models.Journals.Columns.SourceID.EQ(psql.Arg(postedPurchaseUUID)))).All(ctx, bobpgx.NewPool(db))
	accountIsNull := false
	if len(journals) > 0 {
		journalEntries, err = models.JournalEntries.Query(sm.Where(psql.And(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(journals[0].ID)), models.JournalEntries.Columns.CreditYen.GT(psql.Arg(0))))).All(ctx, bobpgx.NewPool(db))
		accountIsNull = len(journalEntries) > 0 && !journalEntries[0].AccountID.Valid
	}
	if err != nil || !accountIsNull {
		t.Fatalf("cleared payment account remained in the posted journal: accountIsNull=%v error=%v", accountIsNull, err)
	}
	settlement, err := resolver.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: householdID, Period: "2026-04", ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(settlement.Lines) != 2 {
		t.Fatalf("expected two settlement lines, got %#v", settlement.Lines)
	}
	if _, err := resolver.updatePurchase(ctx, model.UpdatePurchaseInput{PurchaseID: postedPurchase.ID, BaseRevision: postedPurchase.Revision, Patch: &model.PurchasePatch{Merchant: &postedMerchant}, ChangedFields: []string{"merchant"}}); !errors.Is(err, ErrConflict) {
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
		memberUUID, _ := gofrsuuid.FromString(id)
		subject := "settlement-integration:" + id
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberA)))).Exec(context.Background(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberB)))).Exec(context.Background(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "settlement-integration:" + memberA, DisplayName: "A"})
	apiResolver := &Resolver{DB: db}
	mutation := &mutationResolver{Resolver: apiResolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "settlement snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(household.ID)))).Exec(context.Background(), bobpgx.NewPool(db))
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
	shared, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Shared", Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	for _, configuration := range []model.SetPoolFundingRuleInput{
		{HouseholdID: household.ID, PoolID: personalA.ID, OwnerMemberID: &memberA, FundingShares: []*model.PoolFundingShareInput{}, ExpectedVersion: 1},
		{HouseholdID: household.ID, PoolID: personalB.ID, OwnerMemberID: &memberB, FundingShares: []*model.PoolFundingShareInput{}, ExpectedVersion: 1},
		{HouseholdID: household.ID, PoolID: shared.ID, FundingShares: []*model.PoolFundingShareInput{{MemberID: memberA, Weight: 60}, {MemberID: memberB, Weight: 40}}, SettlementPolicy: new(model.SharedPoolSettlementPolicyBalanceToStandard), ExpectedVersion: 1},
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
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-04-10T12:00:00Z", Merchant: "market", PaymentAccountID: &sharedAccount.ID,
		Items: []*model.PurchaseItemInput{{Name: "shared-paid personal item", AmountYen: 1000, Allocations: []*model.AllocationInput{{PoolID: personalA.ID, Weight: 1}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	sharedPolicy := model.SharedPoolSettlementPolicyReimbursePersonal
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: shared.ID, FundingShares: []*model.PoolFundingShareInput{{MemberID: memberA, Weight: 40}, {MemberID: memberB, Weight: 60}}, SettlementPolicy: &sharedPolicy, ExpectedVersion: 2}); err != nil {
		t.Fatal(err)
	}
	closed, err := mutation.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: "2026-04", ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	closedLines := settlementByMember(t, closed.Lines)
	if closedLines[memberA].MonthlyAdjustmentYen != 400 || closedLines[memberA].AmountDueYen != 400 || closedLines[memberB].MonthlyAdjustmentYen != -400 || closedLines[memberB].AmountDueYen != -400 {
		t.Fatalf("shared-pool actual spending was not balanced against 60/40 funding shares: %#v", closedLines)
	}
	sharedPolicy = model.SharedPoolSettlementPolicyBalanceToStandard
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: shared.ID, FundingShares: []*model.PoolFundingShareInput{{MemberID: memberA, Weight: 40}, {MemberID: memberB, Weight: 60}}, SettlementPolicy: &sharedPolicy, ExpectedVersion: 3}); err != nil {
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

func TestSharedPoolRepaymentSettlesLoanAndJournal(t *testing.T) {
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
	for id, name := range map[string]string{memberA: "Lender", memberB: "Borrower"} {
		memberUUID, _ := gofrsuuid.FromString(id)
		subject := "shared-repayment:" + id
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &name}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberA)))).Exec(context.Background(), bobpgx.NewPool(db))
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberB)))).Exec(context.Background(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberA, Subject: "shared-repayment:" + memberA, DisplayName: "Lender"})
	resolver := &mutationResolver{Resolver: (&Resolver{DB: db})}
	household, err := resolver.createHousehold(ctx, model.CreateHouseholdInput{Name: "shared repayment"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(household.ID)))).Exec(context.Background(), bobpgx.NewPool(db))
	}()
	householdUUID, _ := gofrsuuid.FromString(household.ID)
	memberBUUID, _ := gofrsuuid.FromString(memberB)
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &memberBUUID}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	sharedPool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Shared", Kind: model.PoolKindShared})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{
		HouseholdID: household.ID, PoolID: sharedPool.ID,
		FundingShares:    []*model.PoolFundingShareInput{{MemberID: memberA, Weight: 50}, {MemberID: memberB, Weight: 50}},
		SettlementPolicy: new(model.SharedPoolSettlementPolicyBalanceToStandard), ExpectedVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	lenderPool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Lender", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	borrowerPool, err := resolver.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Borrower", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	for _, personalPool := range []struct{ poolID, ownerID string }{{lenderPool.ID, memberA}, {borrowerPool.ID, memberB}} {
		if _, err := resolver.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: personalPool.poolID, OwnerMemberID: &personalPool.ownerID, ExpectedVersion: 1}); err != nil {
			t.Fatal(err)
		}
	}
	openingAt := "2026-05-01T00:00:00Z"
	lenderAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Lender bank", Kind: model.AccountKindBank, PoolID: &lenderPool.ID, OpeningBalanceYen: new(model.Long(5000)), OpeningBalanceAt: &openingAt})
	if err != nil {
		t.Fatal(err)
	}
	borrowerAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Borrower bank", Kind: model.AccountKindBank, PoolID: &borrowerPool.ID, OpeningBalanceYen: new(model.Long(5000)), OpeningBalanceAt: &openingAt})
	if err != nil {
		t.Fatal(err)
	}
	sharedAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Shared bank", Kind: model.AccountKindBank, PoolID: &sharedPool.ID, OpeningBalanceYen: new(model.Long(5000)), OpeningBalanceAt: &openingAt})
	if err != nil {
		t.Fatal(err)
	}
	loan, err := resolver.recordTransfer(ctx, model.RecordTransferInput{
		HouseholdID: household.ID, FromMemberID: memberA, ToMemberID: memberB, Kind: model.TransferKindLoan,
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
	repayment, err := resolver.recordTransfer(ctx, model.RecordTransferInput{
		HouseholdID: household.ID, FromMemberID: memberB, ToMemberID: memberA, Kind: model.TransferKindRepayment,
		LoanID: loan.TransferID, PaymentAccountID: &sharedAccount.ID, DestinationAccountID: &lenderAccount.ID, AmountYen: 5000,
		OccurredAt: "2026-06-13T12:00:00Z", Description: "Shared repayment", IdempotencyKey: uuid.NewV7().String(),
	})
	if err != nil || repayment.TransferID == nil {
		t.Fatalf("shared-pool repayment failed: transfer=%#v error=%v", repayment, err)
	}
	loanBalances, err := (&Resolver{DB: db}).Query().LoanBalances(ctx, household.ID)
	if err != nil || len(loanBalances) != 1 || loanBalances[0].LoanID != *loan.TransferID || loanBalances[0].BalanceYen != 0 || loanBalances[0].PrincipalYen != 5000 || loanBalances[0].RepaidYen != 5000 {
		t.Fatalf("loan balance was not settled: balances=%#v error=%v", loanBalances, err)
	}
	settlement, err := (&Resolver{DB: db}).Query().Settlement(ctx, household.ID, "2026-06")
	if err != nil {
		t.Fatal(err)
	}
	if len(settlement.Lines) != 2 {
		t.Fatalf("expected two member settlement lines: %#v", settlement.Lines)
	}
	linesByMember := settlementByMember(t, settlement.Lines)
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
	category, err := resolver.createCategory(ctx, model.CreateCategoryInput{HouseholdID: household.ID, Name: "groceries"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: sharedPool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	budget, err := resolver.setBudget(ctx, model.SetBudgetInput{HouseholdID: household.ID, CategoryID: &category.ID, Name: "June groceries", AmountYen: 2000, StartsOn: "2026-06-01"})
	if err != nil {
		t.Fatal(err)
	}
	cardAccount, err := resolver.createAccount(ctx, model.CreateAccountInput{HouseholdID: household.ID, Name: "Shared card", Kind: model.AccountKindCard, PoolID: &sharedPool.ID})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := resolver.createPurchase(ctx, model.CreatePurchaseInput{
		HouseholdID: household.ID, IdempotencyKey: uuid.NewV7().String(), OccurredAt: "2026-06-15T12:00:00Z", Merchant: "market", PaymentAccountID: &cardAccount.ID,
		Items: []*model.PurchaseItemInput{{Name: "groceries", AmountYen: 1000, CategoryID: &category.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.recordAccountMovement(ctx, model.RecordAccountMovementInput{
		HouseholdID: household.ID, Kind: model.AccountMovementKindCardBillPayment, FromAccountID: sharedAccount.ID, ToAccountID: cardAccount.ID,
		AmountYen: 1000, OccurredAt: "2026-06-20T12:00:00Z", IdempotencyKey: uuid.NewV7().String(),
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
	analysis, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, sharedAccount.ID, "2026-06", "2026-06")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.OpeningBalanceYen != 5000 || analysis.ClosingBalanceYen != 4250 || len(analysis.Changes) != 4 {
		t.Fatalf("shared account history did not include the prior-period baseline and every in-period movement: %#v", analysis)
	}
	wantKinds := []string{"FUNDING", "TRANSFER", "ACCOUNT_MOVEMENT", "ACCOUNT_INCOME"}
	for index, wantKind := range wantKinds {
		if analysis.Changes[index].SourceKind != wantKind {
			t.Fatalf("account history is not chronological or omitted a source type: got %#v want source kinds %v", analysis.Changes, wantKinds)
		}
	}
	borrowerHistory, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, borrowerAccount.ID, "2026-06", "2026-06")
	if err != nil {
		t.Fatal(err)
	}
	if borrowerHistory.OpeningBalanceYen != 5000 || borrowerHistory.ClosingBalanceYen != 5000 || len(borrowerHistory.Changes) != 2 || borrowerHistory.Changes[0].SourceKind != "TRANSFER" || borrowerHistory.Changes[0].ChangeYen != 5000 || borrowerHistory.Changes[1].SourceKind != "FUNDING" || borrowerHistory.Changes[1].ChangeYen != -5000 {
		t.Fatalf("borrower account history did not track loan proceeds and attributed funding outflow: %#v", borrowerHistory)
	}
	lenderHistory, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, lenderAccount.ID, "2026-06", "2026-06")
	if err != nil {
		t.Fatal(err)
	}
	if lenderHistory.OpeningBalanceYen != 5000 || lenderHistory.ClosingBalanceYen != 5000 || len(lenderHistory.Changes) != 2 || lenderHistory.Changes[0].SourceKind != "TRANSFER" || lenderHistory.Changes[0].ChangeYen != -5000 || lenderHistory.Changes[1].SourceKind != "TRANSFER" || lenderHistory.Changes[1].ChangeYen != 5000 {
		t.Fatalf("lender account history did not track outgoing loan and repayment receipt: %#v", lenderHistory)
	}
	beforeOpening, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, lenderAccount.ID, "2026-04", "2026-04")
	if err != nil {
		t.Fatal(err)
	}
	if beforeOpening.OpeningBalanceYen != 0 || beforeOpening.ClosingBalanceYen != 0 || len(beforeOpening.Changes) != 0 {
		t.Fatalf("an account opening balance was applied before its effective date: %#v", beforeOpening)
	}
	openingMonth, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, lenderAccount.ID, "2026-05", "2026-05")
	if err != nil {
		t.Fatal(err)
	}
	if openingMonth.OpeningBalanceYen != 0 || openingMonth.ClosingBalanceYen != 5000 || len(openingMonth.Changes) != 1 || openingMonth.Changes[0].SourceKind != "ACCOUNT_OPENING_BALANCE" {
		t.Fatalf("the opening balance did not appear at its effective date: %#v", openingMonth)
	}
	cardAnalysis, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, cardAccount.ID, "2026-06", "2026-06")
	if err != nil {
		t.Fatal(err)
	}
	if cardAnalysis.OpeningBalanceYen != 0 || cardAnalysis.ClosingBalanceYen != 0 || len(cardAnalysis.Changes) != 2 || cardAnalysis.Changes[0].ChangeYen != 1000 || cardAnalysis.Changes[1].ChangeYen != -1000 {
		t.Fatalf("card history did not show purchase liability increase and bill-payment decrease: %#v", cardAnalysis)
	}
	outsiderCtx := auth.WithPrincipal(ctx, auth.Principal{MemberID: uuid.NewV7().String(), Subject: "outsider"})
	if _, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(outsiderCtx, household.ID, sharedAccount.ID, "2026-06", "2026-06"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("account history should be household-authorized: %v", err)
	}
	spending, err := (&Resolver{DB: db}).Query().MonthlyCategorySpending(ctx, household.ID, "2026-06", "2026-07")
	if err != nil || len(spending) != 1 || spending[0].AmountYen != 1000 {
		t.Fatalf("income, funding, and account transfers changed household spending: spending=%#v error=%v", spending, err)
	}
	budgets, err := (&Resolver{DB: db}).Query().Budgets(ctx, household.ID, "2026-06")
	if err != nil || len(budgets) != 1 || budgets[0].ID != budget.ID || budgets[0].ActualYen != 1000 {
		t.Fatalf("account activity changed budget actuals: budgets=%#v error=%v", budgets, err)
	}
	correctionKey := uuid.NewV7().String()
	correctionInput := model.CorrectPurchaseInput{HouseholdID: household.ID, PurchaseID: purchase.ID, AmountDeltaYen: -1000, Reason: "Full return", IdempotencyKey: correctionKey}
	correctedPurchase, err := resolver.correctPurchase(ctx, correctionInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.correctPurchase(ctx, correctionInput); err != nil {
		t.Fatalf("retrying a full refund with the same request should succeed: %v", err)
	}
	correctionMonth := correctedPurchase.Corrections[0].OccurredAt[:7]
	correctionAnalysis, err := (&Resolver{DB: db}).Query().AccountBalanceAnalysis(ctx, household.ID, cardAccount.ID, correctionMonth, correctionMonth)
	if err != nil {
		t.Fatal(err)
	}
	if len(correctionAnalysis.Changes) != 1 || correctionAnalysis.Changes[0].SourceKind != "CORRECTION" || correctionAnalysis.Changes[0].Description != "Full return" || correctionAnalysis.Changes[0].ChangeYen != -1000 || correctionAnalysis.ClosingBalanceYen != -1000 {
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
	memberUUID, _ := gofrsuuid.FromString(memberID)
	subject, displayName := "period-boundary:"+memberID, "JST"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.Background(), bobpgx.NewPool(db))
	}()
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: "period-boundary:" + memberID, DisplayName: "JST"})
	apiResolver := &Resolver{DB: db}
	mutation := &mutationResolver{Resolver: apiResolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Japan time boundary"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(household.ID)))).Exec(context.Background(), bobpgx.NewPool(db))
	}()
	pool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Personal", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setFundingSchedule(ctx, model.SetFundingScheduleInput{HouseholdID: household.ID, MemberID: memberID, PoolID: pool.ID, AmountYen: 700, Cadence: "MONTHLY", StartsOn: "2026-10-01"}); err != nil {
		t.Fatal(err)
	}
	for _, occurredAt := range []string{"2026-09-30T23:59:59+09:00", "2026-10-01T00:00:00+09:00"} {
		purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: occurredAt, Merchant: "boundary", Items: []*model.PurchaseItemInput{{Name: "boundary item", AmountYen: 100}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mutation.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String()); err != nil {
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
	closed, err := mutation.closePeriod(ctx, model.ClosePeriodInput{HouseholdID: household.ID, Period: "2026-09", ExpectedRevision: 0})
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
	purchaseRows, err := models.Purchases.Query(sm.Where(models.Purchases.Columns.HouseholdID.EQ(psql.Arg(household.ID))), sm.OrderBy(models.Purchases.Columns.OccurredAt)).All(ctx, bobpgx.NewPool(db))
	if err != nil {
		t.Fatal(err)
	}
	statuses := make([]string, 0, len(purchaseRows))
	for _, row := range purchaseRows {
		statuses = append(statuses, row.Status)
	}
	if len(statuses) != 2 || statuses[0] != "CLOSED" || statuses[1] != "POSTED" {
		t.Fatalf("period close crossed the Japan-time boundary: %#v", statuses)
	}
}
