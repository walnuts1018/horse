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
	"time"

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

	purchaseID, itemID, jobID := uuid.NewV7().String(), uuid.NewV7().String(), uuid.NewV7().String()
	if _, err := db.Exec(ctx, `INSERT INTO purchases(id,household_id,occurred_at,allocation_rule_version,created_by) VALUES ($1,$2,$3,1,$4)`, purchaseID, householdID, time.Now(), ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO purchase_items(id,household_id,purchase_id,name,amount_yen) VALUES ($1,$2,$3,'test item',0)`, itemID, householdID, purchaseID); err != nil {
		t.Fatal(err)
	}
	verification := ingestion.Verification{Receipt: ingestion.Receipt{Items: []ingestion.Item{{PrintedName: "printed item", Name: "test item", Region: &ingestion.Region{Top: 0.1, Left: 0.2, Bottom: 0.3, Right: 0.8}}}}}
	payload, err := json.Marshal(verification)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO extraction_jobs(id,household_id,source_document_id,status,result) VALUES ($1,$2,$3,'succeeded',$4::jsonb)`, jobID, householdID, documentID, string(payload)); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/documents/" + documentID + "/matches", "/documents?householdId=" + householdID} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.AddCookie(&http.Cookie{Name: "horse_session", Value: cookieValue, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("non-member source observation exposed: path=%s status=%d", path, response.Code)
		}
	}
	evidenceRequest := httptest.NewRequest(http.MethodGet, "/purchases/"+purchaseID+"/evidence", nil)
	evidenceRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: cookieValue, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	evidenceResponse := httptest.NewRecorder()
	server.ServeHTTP(evidenceResponse, evidenceRequest)
	if evidenceResponse.Code != http.StatusNotFound {
		t.Fatalf("non-member evidence was exposed: %d", evidenceResponse.Code)
	}

	ownerCookie := "integration-owner-" + uuid.NewV7().String()
	ownerHash := sha256.Sum256([]byte(ownerCookie))
	if _, err := db.Exec(ctx, `INSERT INTO oidc_sessions(token_hash,member_id,expiry) VALUES ($1,$2,now()+interval '10 minutes')`, ownerHash[:], ownerID); err != nil {
		t.Fatal(err)
	}
	confirm := func(index int) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]any{"purchaseId": purchaseID, "itemId": itemID, "sourceItemIndex": index, "observationJobId": jobID})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/documents/"+documentID+"/confirm", strings.NewReader(string(body)))
		request.Header.Set("Origin", "http://horse.test")
		request.AddCookie(&http.Cookie{Name: "horse_session", Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	if response := confirm(1); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid observation index accepted: %d %s", response.Code, response.Body.String())
	}
	var confirmations int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM document_confirmations WHERE source_document_id=$1`, documentID).Scan(&confirmations); err != nil || confirmations != 0 {
		t.Fatalf("invalid evidence confirmation persisted: %d %v", confirmations, err)
	}
	if response := confirm(0); response.Code != http.StatusOK {
		t.Fatalf("evidence link failed: %d %s", response.Code, response.Body.String())
	}
	// 解析結果の更新後も、確認した観測と原本座標は証拠に保存された状態を保つ。
	if _, err := db.Exec(ctx, `UPDATE extraction_jobs SET result='{}'::jsonb WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	evidenceRequest = httptest.NewRequest(http.MethodGet, "/purchases/"+purchaseID+"/evidence", nil)
	evidenceRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	evidenceResponse = httptest.NewRecorder()
	server.ServeHTTP(evidenceResponse, evidenceRequest)
	if evidenceResponse.Code != http.StatusOK || !strings.Contains(evidenceResponse.Body.String(), `"printedName":"printed item"`) || !strings.Contains(evidenceResponse.Body.String(), `"top":0.1`) {
		t.Fatalf("observation snapshot missing: %d %s", evidenceResponse.Code, evidenceResponse.Body.String())
	}
	documentRequest := httptest.NewRequest(http.MethodGet, "/documents/"+documentID, nil)
	documentRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	documentResponse := httptest.NewRecorder()
	server.ServeHTTP(documentResponse, documentRequest)
	if documentResponse.Code != http.StatusOK || !strings.Contains(documentResponse.Body.String(), `"purchaseId":"`+purchaseID+`"`) {
		t.Fatalf("confirmation reference missing: %d %s", documentResponse.Code, documentResponse.Body.String())
	}
}
