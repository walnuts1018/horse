package graph

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func TestPurchaseConcurrentUpdatesMergeDisjointFieldsAndConflictOnSameField(t *testing.T) {
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
	memberUUID, err := gofrsuuid.FromString(memberID)
	if err != nil {
		t.Fatal(err)
	}
	subject := "purchase-concurrency-integration:" + memberID
	displayName := "purchase concurrency integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	resolver := &Resolver{DB: db}
	mutation := &mutationResolver{Resolver: resolver}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "purchase concurrency integration"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "purchase concurrency pool", Kind: model.PoolKindPersonal}); err != nil {
		t.Fatal(err)
	}
	householdUUID, err := gofrsuuid.FromString(household.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(),
		HouseholdID:    household.ID,
		OccurredAt:     testPurchaseTime,
		Merchant:       "Initial merchant",
		Items:          []*model.PurchaseItemInput{{Name: testPurchaseItemName, AmountYen: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	baselineCursor, err := latestHouseholdEventCursor(ctx, db, householdUUID)
	if err != nil {
		t.Fatal(err)
	}

	occurredAt := model.DateTime("2026-10-14T11:00:00+09:00")
	merchant := "Independent update"
	disjointResults := runConcurrentPurchaseUpdates(ctx, mutation,
		model.UpdatePurchaseInput{PurchaseID: purchase.ID, BaseRevision: purchase.Revision, Patch: &model.PurchasePatch{Merchant: omittable(&merchant)}},
		model.UpdatePurchaseInput{PurchaseID: purchase.ID, BaseRevision: purchase.Revision, Patch: &model.PurchasePatch{OccurredAt: omittable(&occurredAt)}},
	)
	for _, result := range disjointResults {
		if result.err != nil {
			t.Fatalf("disjoint update failed: %v", result.err)
		}
		if _, ok := result.value.(*model.UpdatePurchaseSuccess); !ok {
			t.Fatalf("disjoint update returned an unexpected result: %#v", result.value)
		}
	}

	sharedRevision := purchase.Revision + 2
	merchantA := "Concurrent merchant A"
	merchantB := "Concurrent merchant B"
	sameFieldResults := runConcurrentPurchaseUpdates(ctx, mutation,
		model.UpdatePurchaseInput{PurchaseID: purchase.ID, BaseRevision: sharedRevision, Patch: &model.PurchasePatch{Merchant: omittable(&merchantA)}},
		model.UpdatePurchaseInput{PurchaseID: purchase.ID, BaseRevision: sharedRevision, Patch: &model.PurchasePatch{Merchant: omittable(&merchantB)}},
	)
	var successfulMerchant string
	successCount, conflictCount := 0, 0
	for _, result := range sameFieldResults {
		if result.err != nil {
			t.Fatalf("same-field update returned an execution error instead of a typed result: %v", result.err)
		}
		switch typed := result.value.(type) {
		case *model.UpdatePurchaseSuccess:
			successCount++
			successfulMerchant = typed.Result.Merchant
		case *model.UpdatePurchaseUserError:
			if typed.Code != model.MutationErrorCodeConflict {
				t.Fatalf("same-field update returned %q; want typed CONFLICT", typed.Code)
			}
			conflictCount++
		default:
			t.Fatalf("same-field update returned an unexpected result: %#v", result.value)
		}
	}
	if successCount != 1 || conflictCount != 1 {
		t.Fatalf("same-field updates returned %d successes and %d conflicts; want one of each", successCount, conflictCount)
	}
	loaded, err := loadPurchase(ctx, db, purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantOccurredAt, err := parseTime(occurredAt)
	if err != nil {
		t.Fatal(err)
	}
	gotOccurredAt, err := parseTime(loaded.OccurredAt)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != sharedRevision+1 || loaded.Merchant != successfulMerchant || !gotOccurredAt.Equal(wantOccurredAt) {
		t.Fatalf("reloaded purchase does not contain the concurrent updates: revision=%d merchant=%q occurredAt=%q", loaded.Revision, loaded.Merchant, loaded.OccurredAt)
	}
	updatedItemName := "updated purchase item"
	updatedItem, err := mutation.updatePurchaseItem(ctx, model.UpdatePurchaseItemInput{
		ItemID: purchase.Items[0].ID, BaseRevision: purchase.Items[0].Revision, Name: omittable(&updatedItemName),
	})
	if err != nil {
		t.Fatal(err)
	}

	subscriptionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	t.Cleanup(cancel)
	baseline := fmt.Sprint(baselineCursor)
	changes, err := (&subscriptionResolver{Resolver: resolver}).PurchaseChanges(subscriptionCtx, household.ID, &baseline)
	if err != nil {
		t.Fatal(err)
	}
	itemChangeReceived := false
	for range 4 {
		select {
		case change, ok := <-changes:
			if !ok {
				t.Fatal("purchaseChanges closed before all update events arrived")
			}
			if change.EntityKind == model.PurchaseChangeEntityPurchaseItem {
				if change.Kind != model.PurchaseChangeKindItemUpdated || change.EntityID != updatedItem.ID {
					t.Fatalf("purchaseChanges returned an unexpected item event: %#v", change)
				}
				itemChangeReceived = true
			} else if change.Kind != model.PurchaseChangeKindUpdated || change.EntityID != purchase.ID {
				t.Fatalf("purchaseChanges returned an unexpected event: %#v", change)
			}
			if change.Purchase == nil || change.Purchase.ID != purchase.ID {
				t.Fatalf("purchaseChanges did not include the updated purchase node: %#v", change.Purchase)
			}
			if change.EntityKind == model.PurchaseChangeEntityPurchaseItem && (len(change.Purchase.Items) != 1 || change.Purchase.Items[0].Name != updatedItemName) {
				t.Fatalf("purchaseChanges did not include the updated purchase item node: %#v", change.Purchase.Items)
			}
		case <-subscriptionCtx.Done():
			t.Fatalf("timed out waiting for purchaseChanges events: %v", subscriptionCtx.Err())
		}
	}
	if !itemChangeReceived {
		t.Fatal("purchaseChanges did not publish the purchase item update")
	}

	purchaseUUID, err := gofrsuuid.FromString(purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertEventCursorCommitOrder(t, ctx, db, householdUUID, memberUUID, purchaseUUID)
}

func TestPurchaseItemConcurrentUpdatesPreserveAmountAllocationsAndJournal(t *testing.T) {
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
	subject, displayName := "purchase-item-concurrency:"+memberID, "Purchase item concurrency"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	mutation := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Purchase item concurrency"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})

	poolA, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "A", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	poolB, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "B", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	for _, pool := range []*model.Pool{poolA, poolB} {
		if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{
			HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mutation.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{
		HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: poolA.ID, Weight: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	account, err := mutation.createAccount(ctx, model.CreateAccountInput{
		HouseholdID: household.ID, Name: "Purchase card", Kind: model.AccountKindCard, PoolID: &poolA.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.NewV7().String(), HouseholdID: household.ID, OccurredAt: "2026-10-14T11:00:00+09:00", Merchant: "Concurrent market",
		PaymentAllocations: []*model.PurchasePaymentInput{{PaymentAccountID: account.ID, AmountYen: 1000}},
		Items:              []*model.PurchaseItemInput{{Name: testPurchaseItemName, AmountYen: 1000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	purchase, err = mutation.postPurchase(ctx, purchase.ID, purchase.Revision, uuid.NewV7().String())
	if err != nil {
		t.Fatal(err)
	}
	item := purchase.Items[0]

	newAmount := model.Long(1200)
	newName := "Concurrent item name"
	baseRevision := int(item.Revision)
	mergedResults := runConcurrentPurchaseItemUpdates(ctx, mutation,
		model.UpdatePurchaseItemInput{ItemID: item.ID, BaseRevision: baseRevision, AmountYen: omittable(&newAmount)},
		model.UpdatePurchaseItemInput{
			ItemID: item.ID, BaseRevision: baseRevision, Name: omittable(&newName),
			Allocations: omittable([]*model.AllocationInput{{PoolID: poolA.ID, Weight: 1}, {PoolID: poolB.ID, Weight: 1}}),
		},
	)
	for _, result := range mergedResults {
		if result.err != nil {
			t.Fatalf("disjoint item update failed: %v", result.err)
		}
		if _, ok := result.value.(*model.UpdatePurchaseItemSuccess); !ok {
			t.Fatalf("disjoint item update returned an unexpected result: %#v", result.value)
		}
	}
	updatedPurchase, err := loadPurchase(ctx, db, purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedItem := updatedPurchase.Items[0]
	if updatedItem.Name != newName || updatedItem.AmountYen != newAmount {
		t.Fatalf("disjoint item fields were lost: item=%#v", updatedItem)
	}
	assertConcurrentPurchaseItemState(t, ctx, db, householdUUID, purchase.ID, updatedItem.ID, 1200, map[gofrsuuid.UUID]int64{
		generatedUUID(poolA.ID): 600,
		generatedUUID(poolB.ID): 600,
	})

	amountA, amountB := model.Long(1600), model.Long(2000)
	conflictResults := runConcurrentPurchaseItemUpdates(ctx, mutation,
		model.UpdatePurchaseItemInput{ItemID: item.ID, BaseRevision: int(updatedItem.Revision), AmountYen: omittable(&amountA)},
		model.UpdatePurchaseItemInput{ItemID: item.ID, BaseRevision: int(updatedItem.Revision), AmountYen: omittable(&amountB)},
	)
	var successfulAmount model.Long
	successCount, conflictCount := 0, 0
	for _, result := range conflictResults {
		if result.err != nil {
			t.Fatalf("same-field item update returned an execution error instead of a typed result: %v", result.err)
		}
		switch typed := result.value.(type) {
		case *model.UpdatePurchaseItemSuccess:
			successCount++
			successfulAmount = typed.Result.AmountYen
		case *model.UpdatePurchaseItemUserError:
			if typed.Code != model.MutationErrorCodeConflict {
				t.Fatalf("same-field item update returned %q; want typed CONFLICT", typed.Code)
			}
			conflictCount++
		default:
			t.Fatalf("same-field item update returned an unexpected result: %#v", result.value)
		}
	}
	if successCount != 1 || conflictCount != 1 {
		t.Fatalf("same-field item updates returned %d successes and %d conflicts; want one of each", successCount, conflictCount)
	}
	updatedPurchase, err = loadPurchase(ctx, db, purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedItem = updatedPurchase.Items[0]
	if updatedItem.AmountYen != successfulAmount {
		t.Fatalf("reloaded item amount %d does not match successful update %d", updatedItem.AmountYen, successfulAmount)
	}
	firstAllocation, secondAllocation := int64(successfulAmount)/2, int64(successfulAmount)/2
	if int64(successfulAmount)%2 != 0 {
		secondAllocation++
	}
	assertConcurrentPurchaseItemState(t, ctx, db, householdUUID, purchase.ID, updatedItem.ID, int64(successfulAmount), map[gofrsuuid.UUID]int64{
		generatedUUID(poolA.ID): firstAllocation,
		generatedUUID(poolB.ID): secondAllocation,
	})
}

func assertConcurrentPurchaseItemState(t *testing.T, ctx context.Context, db *pgxpool.Pool, householdID gofrsuuid.UUID, purchaseID, itemID string, amount int64, wantAllocations map[gofrsuuid.UUID]int64) {
	t.Helper()
	executor := bobpgx.NewPool(db)
	purchaseUUID, itemUUID := generatedUUID(purchaseID), generatedUUID(itemID)
	storedItem, err := models.PurchaseItems.Query(sm.Where(psql.And(
		models.PurchaseItems.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
		models.PurchaseItems.Columns.ID.EQ(psql.Arg(itemUUID)),
	))).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if storedItem.AmountYen != amount {
		t.Fatalf("stored item amount = %d; want %d", storedItem.AmountYen, amount)
	}
	allocations, err := models.ItemAllocations.Query(sm.Where(psql.And(
		models.ItemAllocations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.ItemAllocations.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
		models.ItemAllocations.Columns.ItemID.EQ(psql.Arg(itemUUID)),
	))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	var allocated int64
	gotAllocations := make(map[gofrsuuid.UUID]int64, len(allocations))
	for _, allocation := range allocations {
		allocated += allocation.AmountYen
		gotAllocations[allocation.PoolID] = allocation.AmountYen
	}
	if allocated != amount || len(gotAllocations) != len(wantAllocations) {
		t.Fatalf("allocation total or rows differ: allocated=%d rows=%v; want total=%d rows=%v", allocated, gotAllocations, amount, wantAllocations)
	}
	for poolID, wantAmount := range wantAllocations {
		if gotAllocations[poolID] != wantAmount {
			t.Errorf("pool %s allocation = %d; want %d", poolID, gotAllocations[poolID], wantAmount)
		}
	}

	payments, err := models.PurchasePayments.Query(sm.Where(psql.And(
		models.PurchasePayments.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.PurchasePayments.Columns.PurchaseID.EQ(psql.Arg(purchaseUUID)),
	))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	var paymentTotal int64
	for _, payment := range payments {
		paymentTotal += payment.AmountYen
	}
	if paymentTotal != amount {
		t.Fatalf("purchase payment total = %d; want %d", paymentTotal, amount)
	}

	journals, err := models.Journals.Query(sm.Where(psql.And(
		models.Journals.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.Journals.Columns.SourceKind.EQ(psql.Arg(sourceKindPurchase)),
		models.Journals.Columns.SourceID.EQ(psql.Arg(purchaseUUID)),
	))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(journals) != 1 {
		t.Fatalf("purchase journal count = %d; want 1", len(journals))
	}
	entries, err := models.JournalEntries.Query(sm.Where(models.JournalEntries.Columns.JournalID.EQ(psql.Arg(journals[0].ID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	var debits, credits int64
	for _, entry := range entries {
		debits += entry.DebitYen
		credits += entry.CreditYen
	}
	if debits != amount || credits != amount {
		t.Fatalf("purchase journal debit/credit = %d/%d; want %d/%d", debits, credits, amount, amount)
	}
	assertHouseholdJournalsBalance(t, ctx, db, householdID)
}

func assertEventCursorCommitOrder(t *testing.T, ctx context.Context, db *pgxpool.Pool, householdID, actorID, entityID gofrsuuid.UUID) {
	t.Helper()
	executor := bobpgx.NewPool(db)
	baseline, err := latestHouseholdEventCursor(ctx, db, householdID)
	if err != nil {
		t.Fatal(err)
	}
	firstTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = firstTx.Rollback(context.WithoutCancel(ctx)) }()
	if err := accounting.RecordEvent(ctx, bobpgx.NewTx(firstTx, nil), householdID, entityID, "test.cursor.first", 1, []string{"first"}, actorID); err != nil {
		t.Fatal(err)
	}

	secondCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	secondTx, err := db.Begin(secondCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = secondTx.Rollback(context.WithoutCancel(ctx)) }()
	secondEvent := make(chan error, 1)
	go func() {
		err := accounting.RecordEvent(secondCtx, bobpgx.NewTx(secondTx, nil), householdID, entityID, "test.cursor.second", 2, []string{"second"}, actorID)
		if err == nil {
			err = secondTx.Commit(secondCtx)
		}
		secondEvent <- err
	}()

	select {
	case err := <-secondEvent:
		t.Fatalf("second event completed before the first transaction committed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := firstTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-secondEvent:
		if err != nil {
			t.Fatalf("write second event: %v", err)
		}
	case <-secondCtx.Done():
		t.Fatalf("second event did not continue after the first transaction committed: %v", secondCtx.Err())
	}

	events, err := models.HouseholdEvents.Query(
		sm.Where(psql.And(
			models.HouseholdEvents.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.HouseholdEvents.Columns.Cursor.GT(psql.Arg(baseline)),
		)),
		sm.OrderBy(models.HouseholdEvents.Columns.Cursor),
	).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != "test.cursor.first" || events[1].Type != "test.cursor.second" || events[0].Cursor >= events[1].Cursor {
		t.Fatalf("event cursor order does not match commit order: %#v", events)
	}
}

type concurrentPurchaseUpdateResult struct {
	value model.UpdatePurchaseResult
	err   error
}

func runConcurrentPurchaseUpdates(ctx context.Context, mutation *mutationResolver, inputs ...model.UpdatePurchaseInput) []concurrentPurchaseUpdateResult {
	start := make(chan struct{})
	results := make(chan concurrentPurchaseUpdateResult, len(inputs))
	var workers sync.WaitGroup
	workers.Add(len(inputs))
	for _, input := range inputs {
		go func() {
			defer workers.Done()
			<-start
			value, err := mutation.UpdatePurchase(ctx, input)
			results <- concurrentPurchaseUpdateResult{value: value, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	updates := make([]concurrentPurchaseUpdateResult, 0, len(inputs))
	for result := range results {
		updates = append(updates, result)
	}
	return updates
}

type concurrentPurchaseItemUpdateResult struct {
	value model.UpdatePurchaseItemResult
	err   error
}

func runConcurrentPurchaseItemUpdates(ctx context.Context, mutation *mutationResolver, inputs ...model.UpdatePurchaseItemInput) []concurrentPurchaseItemUpdateResult {
	start := make(chan struct{})
	results := make(chan concurrentPurchaseItemUpdateResult, len(inputs))
	var workers sync.WaitGroup
	for _, input := range inputs {
		workers.Go(func() {
			<-start
			value, err := mutation.UpdatePurchaseItem(ctx, input)
			results <- concurrentPurchaseItemUpdateResult{value: value, err: err}
		})
	}
	close(start)
	workers.Wait()
	close(results)
	updates := make([]concurrentPurchaseItemUpdateResult, 0, len(inputs))
	for result := range results {
		updates = append(updates, result)
	}
	return updates
}
