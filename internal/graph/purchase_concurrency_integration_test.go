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
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
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
	if loaded.Revision != sharedRevision+1 || loaded.Merchant != successfulMerchant || loaded.OccurredAt != occurredAt {
		t.Fatalf("reloaded purchase does not contain the concurrent updates: revision=%d merchant=%q occurredAt=%q", loaded.Revision, loaded.Merchant, loaded.OccurredAt)
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
