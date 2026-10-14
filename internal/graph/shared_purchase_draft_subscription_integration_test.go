package graph

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/auth"
)

func TestSharedPurchaseDraftChangesEmitsLatestRevision(t *testing.T) {
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

	owner := createSharedDraftTestMember(t, ctx, db, "shared-draft-subscription-owner")
	outsider := createSharedDraftTestMember(t, ctx, db, "shared-draft-subscription-outsider")
	householdID := createSharedDraftTestHousehold(t, ctx, db, owner)
	resolver := &Resolver{DB: db}
	ownerContext, cancel := context.WithTimeout(auth.WithPrincipal(ctx, auth.Principal{MemberID: owner.String()}), 5*time.Second)
	defer cancel()
	payload := `{"merchant":"Market","occurredAt":"2026-10-14T12:00:00+09:00","itemName":"","itemAmount":"","categoryId":"","items":[]}`
	draft, err := purchase.NewService(db).CreateSharedPurchaseDraft(ctx, owner.String(), householdID.String(), payload)
	if err != nil {
		t.Fatal(err)
	}
	cursor := "1"
	changes, err := (&subscriptionResolver{Resolver: resolver}).SharedPurchaseDraftChanges(ownerContext, draft.ID, &cursor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := purchase.NewService(db).UpdateSharedPurchaseDraft(ctx, owner.String(), draft.ID, 1, payload); err != nil {
		t.Fatal(err)
	}
	select {
	case change, ok := <-changes:
		if !ok {
			t.Fatal("subscription closed before emitting the new revision")
		}
		if change.Cursor != "2" {
			t.Fatalf("change cursor = %q, want 2", change.Cursor)
		}
	case <-ownerContext.Done():
		t.Fatalf("timed out waiting for shared draft change: %v", ownerContext.Err())
	}

	outsiderContext := auth.WithPrincipal(ctx, auth.Principal{MemberID: outsider.String()})
	if _, err := (&subscriptionResolver{Resolver: resolver}).SharedPurchaseDraftChanges(outsiderContext, draft.ID, &cursor); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider subscription error = %v, want not found", err)
	}
}
