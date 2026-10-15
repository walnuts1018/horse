package graph

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"

	uuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type subscriptionChunkQueryCounter struct {
	calls atomic.Int64
}

func (c *subscriptionChunkQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.calls.Add(1)
	return ctx
}

func (*subscriptionChunkQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestChunkSubscriptionCursorsPreservesEveryCursorWithinBatchLimit(t *testing.T) {
	const cursorCount = subscriptionHubCursorBatchSize + 1
	cursors := make([]int, cursorCount)
	for index := range cursors {
		cursors[index] = index
	}

	chunks := chunkSubscriptionCursors(cursors, subscriptionHubCursorBatchSize)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks; want 2", len(chunks))
	}

	seen := make([]bool, cursorCount)
	for _, chunk := range chunks {
		if len(chunk) > subscriptionHubCursorBatchSize {
			t.Fatalf("got chunk with %d cursors; limit is %d", len(chunk), subscriptionHubCursorBatchSize)
		}
		for _, cursor := range chunk {
			if seen[cursor] {
				t.Fatalf("cursor %d appears in multiple chunks", cursor)
			}
			seen[cursor] = true
		}
	}
	for cursor, found := range seen {
		if !found {
			t.Fatalf("cursor %d was omitted", cursor)
		}
	}
}

func TestSubscriptionPollsChunkLargeCursorSets(t *testing.T) {
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
	counter := new(subscriptionChunkQueryCounter)
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

	cursors := make([]purchaseSubscriptionCursor, subscriptionHubCursorBatchSize+1)
	draftCursors := make([]sharedDraftSubscriptionCursor, subscriptionHubCursorBatchSize+1)
	for index := range cursors {
		cursors[index] = purchaseSubscriptionCursor{householdID: uuid.Must(uuid.NewV4())}
		draftCursors[index] = sharedDraftSubscriptionCursor{draftID: uuid.Must(uuid.NewV4())}
	}
	hub := newSubscriptionHub(db, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := hub.pollPurchases(ctx, cursors); err != nil {
		t.Fatal(err)
	}
	if calls := counter.calls.Load(); calls != 2 {
		t.Fatalf("purchase poll ran %d PostgreSQL queries; want 2", calls)
	}

	counter.calls.Store(0)
	if _, err := hub.pollDrafts(ctx, draftCursors); err != nil {
		t.Fatal(err)
	}
	if calls := counter.calls.Load(); calls != 2 {
		t.Fatalf("draft poll ran %d PostgreSQL queries; want 2", calls)
	}
}
