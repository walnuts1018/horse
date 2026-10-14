package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
	"github.com/walnuts1018/horse/internal/relayid"
)

func TestBulkDeletePurchaseItemsGraphQL(t *testing.T) {
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("HORSE_TEST_DATABASE_URL is not set")
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
	executor := bobpgx.NewPool(db)

	memberID, householdID, purchaseID := newBulkDeleteUUID(t), newBulkDeleteUUID(t), newBulkDeleteUUID(t)
	subject, displayName, householdName := "bulk-delete:"+memberID.String(), "Bulk delete integration", "Bulk delete integration"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &householdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	role := "OWNER"
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberID, Role: &role}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	occurredAt := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	merchant, status, origin := "Integration purchase", "DRAFT", "MANUAL"
	allocationVersion := int64(0)
	if _, err := models.Purchases.Insert(&models.PurchaseSetter{
		ID: &purchaseID, HouseholdID: &householdID, OccurredAt: &occurredAt, Merchant: &merchant,
		Status: &status, AllocationRuleVersion: &allocationVersion, CreatedBy: &memberID, Origin: &origin,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	itemIDs := []gofrsuuid.UUID{newBulkDeleteUUID(t), newBulkDeleteUUID(t), newBulkDeleteUUID(t)}
	for _, itemID := range itemIDs {
		name, amount := "Integration item", int64(0)
		if _, err := models.PurchaseItems.Insert(&models.PurchaseItemSetter{
			ID: &itemID, HouseholdID: &householdID, PurchaseID: &purchaseID, Name: &name, AmountYen: &amount,
		}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}

	cookieValue := "bulk-delete-session-" + uuid.NewV7().String()
	cookieHash := sha256.Sum256([]byte(cookieValue))
	cookieHashBytes := cookieHash[:]
	expiry := time.Now().Add(10 * time.Minute)
	if _, err := models.OidcSessions.Insert(&models.OidcSessionSetter{TokenHash: &cookieHashBytes, MemberID: &memberID, Expiry: &expiry}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.WithoutCancel(t.Context())
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(cleanupCtx, executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberID)))).Exec(cleanupCtx, executor)
	})

	var issuerURL string
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": issuerURL, "authorization_endpoint": "http://" + r.Host + "/authorize",
			"token_endpoint": "http://" + r.Host + "/token", "jwks_uri": "http://" + r.Host + "/keys",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	t.Cleanup(issuer.Close)
	issuerURL = issuer.URL
	authService, err := auth.New(ctx, db, auth.Config{IssuerURL: issuerURL, ClientID: "bulk-delete", RedirectURL: "http://horse.test/auth/callback", PublicOrigin: "http://horse.test"})
	if err != nil {
		t.Fatal(err)
	}
	server := New(db, authService, slog.New(slog.DiscardHandler), Config{PublicOrigin: "http://horse.test"}).Handler()
	mutation := `mutation($input: BulkDeletePurchaseItemsInput!) { bulkDeletePurchaseItems(input: $input) { __typename ... on BulkDeletePurchaseItemsSuccess { deletedItemIds } ... on BulkDeletePurchaseItemsUserError { code message } } }`
	callMutation := func(targets []map[string]any) map[string]json.RawMessage {
		t.Helper()
		body, err := json.Marshal(map[string]any{"query": mutation, "variables": map[string]any{"input": map[string]any{"targets": targets}}})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(string(body)))
		request.Header.Set("Origin", "http://horse.test")
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GraphQL status = %d; body = %s", response.Code, response.Body.String())
		}
		var decoded struct {
			Data struct {
				Result json.RawMessage `json:"bulkDeletePurchaseItems"`
			} `json:"data"`
			Errors []json.RawMessage `json:"errors"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode GraphQL response: %v; body = %s", err, response.Body.String())
		}
		if len(decoded.Errors) != 0 {
			t.Fatalf("GraphQL errors = %s", response.Body.String())
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(decoded.Data.Result, &result); err != nil {
			t.Fatalf("decode mutation result: %v; body = %s", err, response.Body.String())
		}
		return result
	}

	itemNodeID := func(id gofrsuuid.UUID) string { return relayid.Encode("PurchaseItem", id[:]) }
	target := func(id gofrsuuid.UUID) map[string]any {
		return map[string]any{"itemId": itemNodeID(id), "baseRevision": 1}
	}
	result := callMutation([]map[string]any{target(itemIDs[0]), target(itemIDs[1])})
	var typename string
	if err := json.Unmarshal(result["__typename"], &typename); err != nil || typename != "BulkDeletePurchaseItemsSuccess" {
		t.Fatalf("success result typename = %q; result = %s", typename, result["__typename"])
	}
	var deleted []string
	if err := json.Unmarshal(result["deletedItemIds"], &deleted); err != nil {
		t.Fatalf("decode deleted IDs: %v", err)
	}
	if len(deleted) != 2 || deleted[0] != itemNodeID(itemIDs[0]) || deleted[1] != itemNodeID(itemIDs[1]) {
		t.Fatalf("deleted item IDs = %v; want first two target IDs", deleted)
	}
	remaining, err := models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != itemIDs[2] {
		t.Fatalf("remaining items = %#v; want only %s", remaining, itemIDs[2])
	}

	result = callMutation([]map[string]any{target(itemIDs[2])})
	if err := json.Unmarshal(result["__typename"], &typename); err != nil || typename != "BulkDeletePurchaseItemsUserError" {
		t.Fatalf("rejection result typename = %q; result = %s", typename, result["__typename"])
	}
	var code string
	if err := json.Unmarshal(result["code"], &code); err != nil || code != "CONFLICT" {
		t.Fatalf("rejection code = %q; want CONFLICT", code)
	}
	remaining, err = models.PurchaseItems.Query(sm.Where(models.PurchaseItems.Columns.PurchaseID.EQ(psql.Arg(purchaseID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != itemIDs[2] {
		t.Fatalf("rejected deletion changed remaining items: %#v", remaining)
	}
}

func newBulkDeleteUUID(t *testing.T) gofrsuuid.UUID {
	t.Helper()
	id, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
