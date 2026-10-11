package api

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/ingestion"
	"uuid"
)

func TestHouseholdIsolationForGraphQLAndOriginalDocument(t *testing.T) {
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
	nonMemberID, ownerID, householdID, documentID := uuid.NewV7().String(), uuid.NewV7().String(), uuid.NewV7().String(), uuid.NewV7().String()
	for _, member := range []struct{ id, subject string }{{nonMemberID, "access-test:non-member"}, {ownerID, "access-test:owner"}} {
		if _, err := db.Exec(ctx, `INSERT INTO members(id,oidc_subject,display_name) VALUES ($1,$2,$3)`, member.id, member.subject, member.subject); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `INSERT INTO households(id,name) VALUES ($1,'access test')`, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO household_members(household_id,member_id,role) VALUES ($1,$2,'OWNER')`, householdID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO source_documents(id,household_id,sha256,object_key,mime_type,source_kind,created_by) VALUES ($1,$2,$3,$4,'image/jpeg','receipt',$5)`, documentID, householdID, "access-test:"+documentID, householdID+"/originals/"+documentID, ownerID); err != nil {
		t.Fatal(err)
	}
	cookieValue := "integration-session-" + uuid.NewV7().String()
	cookieHash := sha256.Sum256([]byte(cookieValue))
	if _, err := db.Exec(ctx, `INSERT INTO oidc_sessions(token_hash,member_id,expiry) VALUES ($1,$2,now()+interval '10 minutes')`, cookieHash[:], nonMemberID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM households WHERE id=$1`, householdID)
		_, _ = db.Exec(context.Background(), `DELETE FROM members WHERE id=$1`, nonMemberID)
		_, _ = db.Exec(context.Background(), `DELETE FROM members WHERE id=$1`, ownerID)
	}()

	var issuerURL string
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.MarshalWrite(w, map[string]any{
			"issuer":                                issuerURL,
			"authorization_endpoint":                "http://" + r.Host + "/authorize",
			"token_endpoint":                        "http://" + r.Host + "/token",
			"jwks_uri":                              "http://" + r.Host + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	defer issuer.Close()
	issuerURL = issuer.URL
	authService, err := auth.New(ctx, db, auth.Config{IssuerURL: issuerURL, ClientID: "access-test", RedirectURL: "http://horse.test/auth/callback", PublicOrigin: "http://horse.test"})
	if err != nil {
		t.Fatal(err)
	}
	server := New(db, authService, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{PublicOrigin: "http://horse.test", Store: &ingestion.ObjectStore{}}).Handler()

	graphqlRequest := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"query($id: ID!) { household(id: $id) { id name } }","variables":{"id":"`+householdID+`"}}`))
	graphqlRequest.Header.Set("Origin", "http://horse.test")
	graphqlRequest.Header.Set("Content-Type", "application/json")
	graphqlRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: cookieValue, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	graphqlResponse := httptest.NewRecorder()
	server.ServeHTTP(graphqlResponse, graphqlRequest)
	if graphqlResponse.Code != http.StatusOK || !strings.Contains(graphqlResponse.Body.String(), `"code":"NOT_FOUND"`) {
		t.Fatalf("non-member GraphQL request was not rejected: status=%d body=%s", graphqlResponse.Code, graphqlResponse.Body.String())
	}

	contentRequest := httptest.NewRequest(http.MethodGet, "/documents/"+documentID+"/content", nil)
	contentRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: cookieValue, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	contentResponse := httptest.NewRecorder()
	server.ServeHTTP(contentResponse, contentRequest)
	if contentResponse.Code != http.StatusNotFound {
		t.Fatalf("non-member original document request was not hidden: status=%d body=%s", contentResponse.Code, contentResponse.Body.String())
	}
}
