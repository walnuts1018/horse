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

	subscriptionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	t.Cleanup(cancel)
	baseline := fmt.Sprint(baselineCursor)
	changes, err := (&subscriptionResolver{Resolver: resolver}).PurchaseChanges(subscriptionCtx, household.ID, &baseline)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		select {
		case change, ok := <-changes:
			if !ok {
				t.Fatal("purchaseChanges closed before all update events arrived")
			}
			if change.Kind != model.PurchaseChangeKindUpdated || change.EntityID != purchase.ID {
				t.Fatalf("purchaseChanges returned an unexpected event: %#v", change)
			}
		case <-subscriptionCtx.Done():
			t.Fatalf("timed out waiting for purchaseChanges events: %v", subscriptionCtx.Err())
		}
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
	purchaseUUID, err := gofrsuuid.FromString(purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertEventCursorCommitOrder(t, ctx, db, householdUUID, memberUUID, purchaseUUID)
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
