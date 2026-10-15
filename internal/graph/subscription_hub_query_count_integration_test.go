package graph

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	uuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type subscriptionQueryCounter struct {
	calls atomic.Int64
}

func (c *subscriptionQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.calls.Add(1)
	return ctx
}

func (*subscriptionQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestSubscriptionHubPollsOnceForManyHouseholdWatches(t *testing.T) {
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	counter := new(subscriptionQueryCounter)
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Tracer = counter
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	counter.calls.Store(0)

	hub := newSubscriptionHub(nil, slog.New(slog.DiscardHandler))
	const watchCount = 50
	stops := make([]func(), 0, watchCount)
	for range watchCount {
		_, stop := hub.watchPurchases(uuid.Must(uuid.NewV4()), 0)
		stops = append(stops, stop)
	}
	for range watchCount {
		_, stop := hub.watchDraft(uuid.Must(uuid.NewV4()), 0)
		stops = append(stops, stop)
	}
	t.Cleanup(func() {
		for _, stop := range stops {
			stop()
		}
	})
	hub.db = db
	purchaseCursors, draftCursors := hub.subscriptionCursors()

	startedAt := time.Now()
	if _, err := hub.pollPurchases(ctx, purchaseCursors); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(startedAt)
	if calls := counter.calls.Load(); calls != 1 {
		t.Fatalf("purchase poll ran %d PostgreSQL queries for %d household watches; want 1", calls, watchCount)
	}
	t.Logf("purchase poll grouped %d household watches into one PostgreSQL query in %s", watchCount, elapsed)

	counter.calls.Store(0)
	startedAt = time.Now()
	if _, err := hub.pollDrafts(ctx, draftCursors); err != nil {
		t.Fatal(err)
	}
	elapsed = time.Since(startedAt)
	if calls := counter.calls.Load(); calls != 1 {
		t.Fatalf("draft poll ran %d PostgreSQL queries for %d draft watches; want 1", calls, watchCount)
	}
	t.Logf("shared draft poll grouped %d draft watches into one PostgreSQL query in %s", watchCount, elapsed)
}

func TestSubscriptionHubPollsAndFansOutPurchaseEventsInBatches(t *testing.T) {
	t.Parallel()
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	counter := new(subscriptionQueryCounter)
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Tracer = counter
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	counter.calls.Store(0)

	memberID := uuid.Must(uuid.NewV7()).String()
	memberUUID := generatedUUID(memberID)
	subject, displayName := "subscription-events:"+memberID, "Subscription event integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &subject, DisplayName: &displayName}).One(ctx, bobpgx.NewPool(db)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	ctx = auth.WithPrincipal(ctx, auth.Principal{MemberID: memberID, Subject: subject, DisplayName: displayName})
	mutation := &mutationResolver{Resolver: &Resolver{DB: db}}
	household, err := mutation.createHousehold(ctx, model.CreateHouseholdInput{Name: "Subscription event integration"})
	if err != nil {
		t.Fatal(err)
	}
	householdUUID := generatedUUID(household.ID)
	t.Cleanup(func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.WithoutCancel(t.Context()), bobpgx.NewPool(db))
	})
	pool, err := mutation.createPool(ctx, model.CreatePoolInput{HouseholdID: household.ID, Name: "Subscription event pool", Kind: model.PoolKindPersonal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setPoolFundingRule(ctx, model.SetPoolFundingRuleInput{HouseholdID: household.ID, PoolID: pool.ID, Configuration: personalPoolFundingConfiguration(memberID), ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := mutation.setDefaultAllocation(ctx, model.SetDefaultAllocationInput{HouseholdID: household.ID, Allocations: []*model.AllocationInput{{PoolID: pool.ID, Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	purchase, err := mutation.createPurchase(ctx, model.CreatePurchaseInput{
		IdempotencyKey: uuid.Must(uuid.NewV7()).String(), HouseholdID: household.ID, OccurredAt: "2026-10-15T12:00:00Z",
		Merchant: "Subscription event purchase", Items: []*model.PurchaseItemInput{{Name: "Test item", AmountYen: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	baselineCursor, err := latestHouseholdEventCursor(ctx, db, householdUUID)
	if err != nil {
		t.Fatal(err)
	}
	executor := bobpgx.NewPool(db)
	changedFields := types.JSON[json.RawMessage]{Val: json.RawMessage(`[]`)}
	var lastIgnoredCursor int64
	for range subscriptionHubPurchaseEventBatchSize {
		eventType := "subscription.test.ignored"
		event, err := models.HouseholdEvents.Insert(&models.HouseholdEventSetter{
			HouseholdID: &householdUUID, Type: &eventType, EntityID: &memberUUID, Revision: new(int64), ChangedFields: &changedFields,
		}).One(ctx, executor)
		if err != nil {
			t.Fatal(err)
		}
		lastIgnoredCursor = event.Cursor
	}
	finalType, revision := "purchase.updated", int64(1)
	changedFields = types.JSON[json.RawMessage]{Val: json.RawMessage(`["merchant"]`)}
	purchaseUUID := generatedUUID(purchase.ID)
	firstEvent, err := models.HouseholdEvents.Insert(&models.HouseholdEventSetter{
		HouseholdID: &householdUUID, Type: &finalType, EntityID: &purchaseUUID, Revision: &revision, ChangedFields: &changedFields,
	}).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	revision++
	finalEvent, err := models.HouseholdEvents.Insert(&models.HouseholdEventSetter{
		HouseholdID: &householdUUID, Type: &finalType, EntityID: &purchaseUUID, Revision: &revision, ChangedFields: &changedFields,
	}).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	counter.calls.Store(0)

	hub := newSubscriptionHub(nil, slog.New(slog.DiscardHandler))
	firstChanges, stopFirst := hub.watchPurchases(householdUUID, baselineCursor)
	secondChanges, stopSecond := hub.watchPurchases(householdUUID, baselineCursor)
	t.Cleanup(stopFirst)
	t.Cleanup(stopSecond)
	hub.db = db
	cursors, _ := hub.subscriptionCursors()
	if len(cursors) != 1 {
		t.Fatalf("purchase cursors = %d, want one shared household cursor", len(cursors))
	}

	found, err := hub.pollPurchases(ctx, cursors)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("first 100 ignored events unexpectedly produced a purchase change")
	}
	if calls := counter.calls.Load(); calls != 1 {
		t.Fatalf("poll of the first %d events ran %d PostgreSQL queries, want 1", subscriptionHubPurchaseEventBatchSize, calls)
	}
	if cursor := currentPurchaseWatchCursor(t, hub); cursor != lastIgnoredCursor {
		t.Fatalf("cursor after first batch = %d, want last cursor in the 100-event batch %d", cursor, lastIgnoredCursor)
	}
	assertNoPurchaseChange(t, firstChanges)
	assertNoPurchaseChange(t, secondChanges)

	counter.calls.Store(0)
	if _, err := loadPurchase(ctx, db, purchase.ID); err != nil {
		t.Fatal(err)
	}
	hydrationQueries := counter.calls.Load()
	if hydrationQueries != 11 {
		t.Fatalf("loading one purchase ran %d PostgreSQL queries, want 11", hydrationQueries)
	}
	counter.calls.Store(0)
	cursors, _ = hub.subscriptionCursors()
	found, err = hub.pollPurchases(ctx, cursors)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("poll did not report delivery of both purchase events")
	}
	if calls, want := counter.calls.Load(), int64(1)+hydrationQueries; calls != want {
		t.Fatalf("poll of two events for one purchase ran %d PostgreSQL queries, want one event query plus the fixed %d-query purchase load", calls, hydrationQueries)
	}
	for index, changes := range []<-chan *model.PurchaseChange{firstChanges, secondChanges} {
		for _, event := range []*models.HouseholdEvent{firstEvent, finalEvent} {
			select {
			case change := <-changes:
				if change == nil || change.Cursor != strconv.FormatInt(event.Cursor, 10) || change.EntityID != purchase.ID {
					t.Fatalf("watch %d received change %#v, want cursor %d for purchase %s", index, change, event.Cursor, purchase.ID)
				}
			case <-time.After(time.Second):
				t.Fatalf("watch %d did not receive purchase event at cursor %d", index, event.Cursor)
			}
		}
	}
	if cursor := currentPurchaseWatchCursor(t, hub); cursor != finalEvent.Cursor {
		t.Fatalf("cursor after purchase event = %d, want %d", cursor, finalEvent.Cursor)
	}

	counter.calls.Store(0)
	cursors, _ = hub.subscriptionCursors()
	found, err = hub.pollPurchases(ctx, cursors)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("next poll reported a duplicate purchase event")
	}
	if calls := counter.calls.Load(); calls != 1 {
		t.Fatalf("poll after cursor advancement ran %d PostgreSQL queries, want 1", calls)
	}
	assertNoPurchaseChange(t, firstChanges)
	assertNoPurchaseChange(t, secondChanges)
}

func currentPurchaseWatchCursor(t *testing.T, hub *subscriptionHub) int64 {
	t.Helper()
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for _, watch := range hub.purchases {
		return watch.cursor
	}
	t.Fatal("purchase watch was removed")

	return 0
}

func assertNoPurchaseChange(t *testing.T, changes <-chan *model.PurchaseChange) {
	t.Helper()
	select {
	case change := <-changes:
		t.Fatalf("unexpected purchase change: %#v", change)
	default:
	}
}
