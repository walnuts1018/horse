package graph

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	uuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

	hub := newSubscriptionHub(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
