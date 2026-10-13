package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
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
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
	"github.com/walnuts1018/horse/internal/relayid"
)

type previewTestStore struct {
	document        ingestion.Document
	gets            int
	putHouseholdIDs []string
}

func (s *previewTestStore) Put(_ context.Context, householdID, _ string, data []byte) (string, string, error) {
	s.putHouseholdIDs = append(s.putHouseholdIDs, householdID)
	hash := ingestion.DocumentHash(data)
	return householdID + "/originals/" + hash, hash, nil
}

func (s *previewTestStore) Get(_ context.Context, _ string, mimeType string) (ingestion.Document, error) {
	s.gets++
	s.document.MIMEType = mimeType
	return s.document, nil
}

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
	var previewPNG bytes.Buffer
	if err := png.Encode(&previewPNG, image.NewRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	store := &previewTestStore{document: ingestion.Document{Data: []byte("unchanged original")}}
	ocrCalls := 0
	ocrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ocrCalls++
		if r.URL.Path != "/crop" {
			t.Errorf("unexpected OCR path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Coordinate-Transform", `[1,0,0,0,1,0,0,0,1]`)
		_, _ = w.Write(previewPNG.Bytes())
	}))
	defer ocrServer.Close()
	server := New(db, authService, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{PublicOrigin: "http://horse.test", Store: store, OCR: ingestion.OCR{Endpoint: ocrServer.URL}}).Handler()

	householdNodeID := relayid.Encode("Household", householdUUID[:])
	graphqlRequest := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"query($id: ID!) { household(id: $id) { id name } }","variables":{"id":"`+householdNodeID+`"}}`))
	graphqlRequest.Header.Set("Origin", "http://horse.test")
	graphqlRequest.Header.Set("Content-Type", "application/json")
	graphqlRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: cookieValue, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	graphqlResponse := httptest.NewRecorder()
	server.ServeHTTP(graphqlResponse, graphqlRequest)
	if graphqlResponse.Code != http.StatusOK || !strings.Contains(graphqlResponse.Body.String(), `"household":null`) {
		t.Fatalf("non-member GraphQL request was not rejected: status=%d body=%s", graphqlResponse.Code, graphqlResponse.Body.String())
	}

	contentRequest := httptest.NewRequest(http.MethodGet, "/documents/"+documentID+"/content", nil)
	contentRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: cookieValue, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	contentResponse := httptest.NewRecorder()
	server.ServeHTTP(contentResponse, contentRequest)
	if contentResponse.Code != http.StatusNotFound {
		t.Fatalf("non-member original document request was not hidden: status=%d body=%s", contentResponse.Code, contentResponse.Body.String())
	}
	previewRequest := httptest.NewRequest(http.MethodGet, "/documents/"+documentID+"/preview", nil)
	previewRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: cookieValue, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	previewResponse := httptest.NewRecorder()
	server.ServeHTTP(previewResponse, previewRequest)
	if previewResponse.Code != http.StatusNotFound || ocrCalls != 0 || store.gets != 0 {
		t.Fatalf("non-member preview accessed private evidence: status=%d OCR calls=%d storage reads=%d", previewResponse.Code, ocrCalls, store.gets)
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
	purchaseNodeID := relayid.Encode("Purchase", purchaseUUID[:])
	itemName := "test item"
	itemAmount := int64(0)
	if _, err := models.PurchaseItems.Insert(&models.PurchaseItemSetter{ID: &itemUUID, HouseholdID: &householdUUID, PurchaseID: &purchaseUUID, Name: &itemName, AmountYen: &itemAmount}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	itemNodeID := relayid.Encode("PurchaseItem", itemUUID[:])
	verification := ingestion.Verification{Receipt: ingestion.Receipt{Items: []ingestion.Item{{PrintedName: "printed item", Name: "test item", Region: &ingestion.Region{Top: 0.1, Left: 0.2, Bottom: 0.3, Right: 0.8}}}}, Transform: [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}}
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
	evidenceRequest := httptest.NewRequest(http.MethodGet, "/purchases/"+purchaseNodeID+"/evidence", nil)
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
	evidenceRequest = httptest.NewRequest(http.MethodGet, "/purchases/"+purchaseNodeID+"/evidence", nil)
	evidenceRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	evidenceResponse = httptest.NewRecorder()
	server.ServeHTTP(evidenceResponse, evidenceRequest)
	if evidenceResponse.Code != http.StatusOK || strings.TrimSpace(evidenceResponse.Body.String()) != `{"evidence":[]}` {
		t.Fatalf("purchase global ID did not return empty evidence: status=%d body=%s", evidenceResponse.Code, evidenceResponse.Body.String())
	}
	documentsRequest := httptest.NewRequest(http.MethodGet, "/documents?householdId="+householdNodeID, nil)
	documentsRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	documentsResponse := httptest.NewRecorder()
	server.ServeHTTP(documentsResponse, documentsRequest)
	if documentsResponse.Code != http.StatusOK || !strings.Contains(documentsResponse.Body.String(), documentID) {
		t.Fatalf("household global ID document list failed: status=%d body=%s", documentsResponse.Code, documentsResponse.Body.String())
	}
	var uploadBody bytes.Buffer
	uploadForm := multipart.NewWriter(&uploadBody)
	if err := uploadForm.WriteField("householdId", householdNodeID); err != nil {
		t.Fatal(err)
	}
	if err := uploadForm.WriteField("sourceKind", "receipt"); err != nil {
		t.Fatal(err)
	}
	filePart, err := uploadForm.CreateFormFile("file", "receipt.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := filePart.Write(previewPNG.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := uploadForm.Close(); err != nil {
		t.Fatal(err)
	}
	uploadRequest := httptest.NewRequest(http.MethodPost, "/documents", &uploadBody)
	uploadRequest.Header.Set("Origin", "http://horse.test")
	uploadRequest.Header.Set("Content-Type", uploadForm.FormDataContentType())
	uploadRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	uploadResponse := httptest.NewRecorder()
	server.ServeHTTP(uploadResponse, uploadRequest)
	if uploadResponse.Code != http.StatusAccepted || !strings.Contains(uploadResponse.Body.String(), `"householdId":"`+householdID+`"`) {
		t.Fatalf("household global ID document upload failed: status=%d body=%s", uploadResponse.Code, uploadResponse.Body.String())
	}
	if len(store.putHouseholdIDs) != 1 || store.putHouseholdIDs[0] != householdID {
		t.Fatalf("document storage received non-canonical household ID: %v", store.putHouseholdIDs)
	}
	var uploaded map[string]any
	if err := stdjson.Unmarshal(uploadResponse.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	uploadedID, ok := uploaded["id"].(string)
	if !ok || uploadedID == "" {
		t.Fatalf("uploaded document ID missing: %s", uploadResponse.Body.String())
	}
	documentsRequest = httptest.NewRequest(http.MethodGet, "/documents?householdId="+householdID, nil)
	documentsRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	documentsResponse = httptest.NewRecorder()
	server.ServeHTTP(documentsResponse, documentsRequest)
	if documentsResponse.Code != http.StatusOK || !strings.Contains(documentsResponse.Body.String(), uploadedID) {
		t.Fatalf("raw household UUID compatibility failed: status=%d body=%s", documentsResponse.Code, documentsResponse.Body.String())
	}
	invalidPreviewRequest := httptest.NewRequest(http.MethodGet, "/documents/"+documentID+"/preview?page=-1", nil)
	invalidPreviewRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	invalidPreviewResponse := httptest.NewRecorder()
	server.ServeHTTP(invalidPreviewResponse, invalidPreviewRequest)
	if invalidPreviewResponse.Code != http.StatusBadRequest || ocrCalls != 0 {
		t.Fatalf("invalid preview page was not rejected: status=%d OCR calls=%d", invalidPreviewResponse.Code, ocrCalls)
	}
	validPreviewRequest := httptest.NewRequest(http.MethodGet, "/documents/"+documentID+"/preview?page=0", nil)
	validPreviewRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	validPreviewResponse := httptest.NewRecorder()
	server.ServeHTTP(validPreviewResponse, validPreviewRequest)
	if validPreviewResponse.Code != http.StatusOK || validPreviewResponse.Header().Get("Content-Type") != "image/png" || validPreviewResponse.Header().Get("Cache-Control") != "private, no-store" || validPreviewResponse.Header().Get("X-Original-Content-Type") != mimeType || validPreviewResponse.Header().Get("X-Source-Page") != "0" || validPreviewResponse.Header().Get("X-Coordinate-Transform") != `[1,0,0,0,1,0,0,0,1]` || !bytes.Equal(validPreviewResponse.Body.Bytes(), previewPNG.Bytes()) || ocrCalls != 1 || store.gets != 1 {
		t.Fatalf("member preview response incorrect: status=%d headers=%v OCR calls=%d storage reads=%d", validPreviewResponse.Code, validPreviewResponse.Header(), ocrCalls, store.gets)
	}
	confirm := func(index int) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]any{"purchaseId": purchaseNodeID, "itemId": itemNodeID, "sourceItemIndex": index, "observationJobId": jobID})
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
	secondDocumentID, secondJobID := uuid.NewV7().String(), uuid.NewV7().String()
	secondDocumentUUID, secondJobUUID := mustIntegrationUUID(t, secondDocumentID), mustIntegrationUUID(t, secondJobID)
	secondHash, secondObjectKey, cardSource := "access-test:"+secondDocumentID, householdID+"/originals/"+secondDocumentID, "card"
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &secondDocumentUUID, HouseholdID: &householdUUID, Sha256: &secondHash, ObjectKey: &secondObjectKey, MimeType: &mimeType, SourceKind: &cardSource, CreatedBy: &ownerUUID}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{ID: &secondJobUUID, HouseholdID: &householdUUID, SourceDocumentID: &secondDocumentUUID, Status: &jobStatus, Result: &jobResult}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	groupBody, err := json.Marshal(map[string]any{"sources": []any{
		map[string]any{"documentId": documentID, "itemId": itemNodeID, "sourceItemIndex": 0, "observationJobId": jobID},
		map[string]any{"documentId": secondDocumentID, "itemId": itemNodeID, "sourceItemIndex": 0, "observationJobId": secondJobID},
	}})
	if err != nil {
		t.Fatal(err)
	}
	groupRequest := httptest.NewRequest(http.MethodPost, "/purchases/"+purchaseNodeID+"/evidence", strings.NewReader(string(groupBody)))
	groupRequest.Header.Set("Origin", "http://horse.test")
	groupRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	groupResponse := httptest.NewRecorder()
	server.ServeHTTP(groupResponse, groupRequest)
	if groupResponse.Code != http.StatusOK || !strings.Contains(groupResponse.Body.String(), `"sourceCount":2`) {
		t.Fatalf("multi-source evidence confirmation failed: %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	rollbackBody, err := json.Marshal(map[string]any{"sources": []any{
		map[string]any{"documentId": secondDocumentID, "sourceReference": "rollback"},
		map[string]any{"documentId": "invalid-document-id"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	rollbackRequest := httptest.NewRequest(http.MethodPost, "/purchases/"+purchaseNodeID+"/evidence", strings.NewReader(string(rollbackBody)))
	rollbackRequest.Header.Set("Origin", "http://horse.test")
	rollbackRequest.AddCookie(&http.Cookie{Name: "horse_session", Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	rollbackResponse := httptest.NewRecorder()
	server.ServeHTTP(rollbackResponse, rollbackRequest)
	if rollbackResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid evidence group was accepted: %d %s", rollbackResponse.Code, rollbackResponse.Body.String())
	}
	secondConfirmations, err := models.DocumentConfirmations.Query(sm.Where(models.DocumentConfirmations.Columns.SourceDocumentID.EQ(psql.Arg(secondDocumentUUID)))).All(ctx, executor)
	if err != nil || len(secondConfirmations) != 1 {
		t.Fatalf("failed multi-source confirmation left partial state: %d %v", len(secondConfirmations), err)
	}
	// 解析結果の更新後も、確認した観測と原本座標は証拠に保存された状態を保つ。
	emptyResult := sql.Null[types.JSON[stdjson.RawMessage]]{V: types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(`{}`)}, Valid: true}
	if _, err := models.ExtractionJobs.Update((models.ExtractionJobSetter{Result: &emptyResult}).UpdateMod(), um.Where(models.ExtractionJobs.Columns.ID.EQ(psql.Arg(jobUUID)))).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	evidenceRequest = httptest.NewRequest(http.MethodGet, "/purchases/"+purchaseNodeID+"/evidence", nil)
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
