package graph

import (
	"io"
	"log/slog"
	"testing"
	"time"

	uuid "github.com/gofrs/uuid/v5"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func TestSubscriptionPollIntervalBacksOffWhenIdle(t *testing.T) {
	interval := subscriptionHubPollInterval
	for _, want := range []time.Duration{2 * time.Second, 4 * time.Second, subscriptionHubMaxIdlePollInterval, subscriptionHubMaxIdlePollInterval} {
		interval = nextSubscriptionPollInterval(interval, false)
		if interval != want {
			t.Fatalf("idle poll interval = %s; want %s", interval, want)
		}
	}
	if got := nextSubscriptionPollInterval(interval, true); got != subscriptionHubPollInterval {
		t.Fatalf("active poll interval = %s; want %s", got, subscriptionHubPollInterval)
	}
}

func TestSubscriptionHubSharesMinimumCursorAndFansOutChanges(t *testing.T) {
	hub := newSubscriptionHub(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	householdID := uuid.Must(uuid.NewV4())
	firstChanges, stopFirst := hub.watchPurchases(householdID, 4)
	defer stopFirst()
	secondChanges, stopSecond := hub.watchPurchases(householdID, 9)
	defer stopSecond()
	draftID := uuid.Must(uuid.NewV4())
	_, stopFirstDraft := hub.watchDraft(draftID, 3)
	defer stopFirstDraft()
	_, stopSecondDraft := hub.watchDraft(draftID, 6)
	defer stopSecondDraft()

	purchaseCursors, draftCursors := hub.subscriptionCursors()
	if len(purchaseCursors) != 1 || purchaseCursors[0].cursor != 4 {
		t.Fatalf("shared purchase cursors = %#v; want one household at cursor 4", purchaseCursors)
	}
	if len(draftCursors) != 1 || draftCursors[0].cursor != 3 {
		t.Fatalf("shared draft cursors = %#v; want one draft at cursor 3", draftCursors)
	}

	change := &model.PurchaseChange{Cursor: "5"}
	if !hub.publishPurchaseChange(householdID, 5, change, make(map[uint64]bool)) {
		t.Fatal("change was not reported as delivered")
	}
	select {
	case got := <-firstChanges:
		if got != change {
			t.Fatalf("first watcher received %#v; want shared change %#v", got, change)
		}
	default:
		t.Fatal("first watcher did not receive the change")
	}
	select {
	case got := <-secondChanges:
		t.Fatalf("watcher at cursor 9 received old change %#v", got)
	default:
	}

	purchaseCursors, _ = hub.subscriptionCursors()
	if len(purchaseCursors) != 1 || purchaseCursors[0].cursor != 5 {
		t.Fatalf("shared purchase cursors after fanout = %#v; want cursor 5", purchaseCursors)
	}
}

func TestSubscriptionHubRetriesBackpressuredPurchaseChange(t *testing.T) {
	hub := newSubscriptionHub(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	householdID := uuid.Must(uuid.NewV4())
	changes, stop := hub.watchPurchases(householdID, 4)
	defer stop()
	hub.mu.Lock()
	watch := hub.purchases[1]
	hub.mu.Unlock()
	for range cap(watch.changes) {
		watch.changes <- new(model.PurchaseChange)
	}

	blocked := make(map[uint64]bool)
	if hub.hasAvailablePurchaseWatch(householdID, 5, blocked) {
		t.Fatal("full watcher buffer was reported as available")
	}
	if hub.publishPurchaseChange(householdID, 5, new(model.PurchaseChange), blocked) {
		t.Fatal("change to a full watcher was reported as delivered")
	}
	if got := nextSubscriptionPollInterval(subscriptionHubPollInterval, false); got != 2*time.Second {
		t.Fatalf("poll interval after backpressure = %s; want %s", got, 2*time.Second)
	}
	hub.blockFullPurchaseWatches(householdID, 5, blocked)
	hub.advancePurchaseCursors(householdID, 5, blocked)
	if watch.cursor != 4 {
		t.Fatalf("cursor after a full buffer = %d; want 4", watch.cursor)
	}

	<-changes
	change := &model.PurchaseChange{Cursor: "5"}
	retryBlocked := make(map[uint64]bool)
	if !hub.hasAvailablePurchaseWatch(householdID, 5, retryBlocked) {
		t.Fatal("watcher buffer did not become available after draining")
	}
	if !hub.publishPurchaseChange(householdID, 5, change, retryBlocked) {
		t.Fatal("change was not reported as delivered after draining")
	}
	if watch.cursor != 5 {
		t.Fatalf("cursor after retry = %d; want 5", watch.cursor)
	}
	for range cap(watch.changes) - 1 {
		<-changes
	}
	if got := <-changes; got != change {
		t.Fatalf("retried watcher change = %#v; want %#v", got, change)
	}
}

func TestSubscriptionHubClosesWatchOnUnsubscribe(t *testing.T) {
	hub := newSubscriptionHub(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	changes, stop := hub.watchDraft(uuid.Must(uuid.NewV4()), 2)
	stop()
	if _, ok := <-changes; ok {
		t.Fatal("watch channel remained open after unsubscribe")
	}
}
