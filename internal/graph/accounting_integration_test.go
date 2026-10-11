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
	rules, err := apiResolver.Query().AllocationRules(ctx, householdID)
	if err != nil || len(rules) != 2 {
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
	if byMember[memberA].ScheduledYen != 6000 || byMember[memberA].PaidYen != 6000 || byMember[memberA].AmountDueYen != 0 {
		t.Fatalf("member A schedule/payment mismatch: %#v", byMember[memberA])
	}
	if byMember[memberB].ScheduledYen != 4000 || byMember[memberB].PaidYen != 0 || byMember[memberB].AmountDueYen != 4000 {
		t.Fatalf("member B missed payment was not carried: %#v", byMember[memberB])
	}
	spending, err := apiResolver.Query().MonthlyCategorySpending(ctx, householdID, "2026-04", "2026-04")
	if err != nil || len(spending) != 2 {
		t.Fatalf("monthly category spending query failed: rows=%#v error=%v", spending, err)
	}
	if spending[0].CategoryID == nil || *spending[0].CategoryID != category.ID || spending[0].AmountYen != 700 {
		t.Fatalf("category spending mismatch: %#v", spending[0])
	}
}
