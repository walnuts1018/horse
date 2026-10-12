package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
	"uuid"
)

func mustIntegrationUUID(t *testing.T, value string) gofrsuuid.UUID {
	t.Helper()
	parsed, err := gofrsuuid.FromString(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

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
	nonMemberUUID := mustIntegrationUUID(t, nonMemberID)
	ownerUUID := mustIntegrationUUID(t, ownerID)
	householdUUID := mustIntegrationUUID(t, householdID)
	documentUUID := mustIntegrationUUID(t, documentID)
	executor := bobpgx.NewPool(db)
	for _, member := range []struct{ id, subject string }{{nonMemberID, "access-test:non-member"}, {ownerID, "access-test:owner"}} {
		memberID := mustIntegrationUUID(t, member.id)
		if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &member.subject, DisplayName: &member.subject}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}
	householdName := "access test"
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdUUID, Name: &householdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	role := "OWNER"
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &ownerUUID, Role: &role}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	documentHash := "access-test:" + documentID
	objectKey := householdID + "/originals/" + documentID
	mimeType, sourceKind := "image/jpeg", "receipt"
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &documentUUID, HouseholdID: &householdUUID, Sha256: &documentHash, ObjectKey: &objectKey, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &ownerUUID}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	cookieValue := "integration-session-" + uuid.NewV7().String()
	cookieHash := sha256.Sum256([]byte(cookieValue))
	cookieHashBytes := cookieHash[:]
	nonMemberExpiry := time.Now().Add(10 * time.Minute)
	if _, err := models.OidcSessions.Insert(&models.OidcSessionSetter{TokenHash: &cookieHashBytes, MemberID: &nonMemberUUID, Expiry: &nonMemberExpiry}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(context.Background(), executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(nonMemberUUID)))).Exec(context.Background(), executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(ownerUUID)))).Exec(context.Background(), executor)
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
	purchaseUUID := mustIntegrationUUID(t, purchaseID)
	itemUUID := mustIntegrationUUID(t, itemID)
	jobUUID := mustIntegrationUUID(t, jobID)
	occurredAt := time.Now()
	ruleVersion := int64(1)
	if _, err := models.Purchases.Insert(&models.PurchaseSetter{ID: &purchaseUUID, HouseholdID: &householdUUID, OccurredAt: &occurredAt, AllocationRuleVersion: &ruleVersion, CreatedBy: &ownerUUID}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	itemName := "test item"
	itemAmount := int64(0)
	if _, err := models.PurchaseItems.Insert(&models.PurchaseItemSetter{ID: &itemUUID, HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, Name: &itemName, AmountYen: &itemAmount}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	verification := ingestion.Verification{Receipt: ingestion.Receipt{Items: []ingestion.Item{{PrintedName: "printed item", Name: "test item", Region: &ingestion.Region{Top: 0.1, Left: 0.2, Bottom: 0.3, Right: 0.8}}}}}
	payload, err := json.Marshal(verification)
	if err != nil {
		t.Fatal(err)
	}
	jobStatus := "succeeded"
	jobResult := sql.Null[types.JSON[stdjson.RawMessage]]{V: types.JSON[stdjson.RawMessage]{Val: payload}, Valid: true}
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{ID: &jobUUID, HouseholdID: &householdUUID, SourceDocumentID: &documentUUID, Status: &jobStatus, Result: &jobResult}).Exec(ctx, executor); err != nil {
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
	ownerHashBytes := ownerHash[:]
	ownerExpiry := time.Now().Add(10 * time.Minute)
	if _, err := models.OidcSessions.Insert(&models.OidcSessionSetter{TokenHash: &ownerHashBytes, MemberID: &ownerUUID, Expiry: &ownerExpiry}).Exec(ctx, executor); err != nil {
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
	confirmations, err := models.DocumentConfirmations.Query(sm.Where(models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(documentUUID)))).All(ctx, executor)
	if err != nil || len(confirmations) != 0 {
		t.Fatalf("invalid evidence confirmation persisted: %d %v", len(confirmations), err)
	}
	if response := confirm(0); response.Code != http.StatusOK {
		t.Fatalf("evidence link failed: %d %s", response.Code, response.Body.String())
	}
	// 解析結果の更新後も、確認した観測と原本座標は証拠に保存された状態を保つ。
	emptyResult := sql.Null[types.JSON[stdjson.RawMessage]]{V: types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(`{}`)}, Valid: true}
	if _, err := models.ExtractionJobs.Update((models.ExtractionJobSetter{Result: &emptyResult}).UpdateMod(), um.Where(models.ExtractionJobs.Columns.ID.EQ(psql.Arg(jobUUID)))).Exec(ctx, executor); err != nil {
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
