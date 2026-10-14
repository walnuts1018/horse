package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"image"
	"image/png"
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

const (
	sessionCookieName       = "horse_session"
	itemIDJSONKey           = "itemId"
	documentIDJSONKey       = "documentId"
	observationJobIDJSONKey = "observationJobId"
	sourceItemIndexJSONKey  = "sourceItemIndex"
	testReceiptSourceKind   = "receipt"
)

type previewTestStore struct {
	document        ingestion.Document
	gets            int
	getKeys         []string
	putHouseholdIDs []string
	deletes         []string
	deleteFailures  int
}

func (s *previewTestStore) Put(_ context.Context, householdID, _ string, data []byte) (string, string, error) {
	s.putHouseholdIDs = append(s.putHouseholdIDs, householdID)
	hash := ingestion.DocumentHash(data)
	return householdID + "/originals/" + hash, hash, nil
}

func (s *previewTestStore) PutAnalysis(_ context.Context, householdID, documentID, _ string, data []byte) (string, string, error) {
	s.putHouseholdIDs = append(s.putHouseholdIDs, householdID)
	hash := ingestion.DocumentHash(data)
	return householdID + "/analysis/" + documentID + "/" + hash, hash, nil
}

func (s *previewTestStore) Get(_ context.Context, key, mimeType string) (ingestion.Document, error) {
	s.gets++
	s.getKeys = append(s.getKeys, key)
	s.document.MIMEType = mimeType
	return s.document, nil
}

func (s *previewTestStore) Delete(_ context.Context, key string) error {
	s.deletes = append(s.deletes, key)
	if s.deleteFailures > 0 {
		s.deleteFailures--

		return errors.New("storage unavailable")
	}

	return nil
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
	t.Parallel()
	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx := t.Context()
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
	mimeType, sourceKind := documentMIMEJPEG, testReceiptSourceKind
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
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(t.Context(), executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(nonMemberUUID)))).Exec(t.Context(), executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(ownerUUID)))).Exec(t.Context(), executor)
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
	server := New(db, authService, slog.New(slog.DiscardHandler), Config{PublicOrigin: "http://horse.test", Store: store, OCR: ingestion.OCR{Endpoint: ocrServer.URL}}).Handler()

	householdNodeID := relayid.Encode("Household", householdUUID[:])
	graphqlRequest := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"query($id: ID!) { household(id: $id) { id name } }","variables":{"id":"`+householdNodeID+`"}}`))
	graphqlRequest.Header.Set("Origin", "http://horse.test")
	graphqlRequest.Header.Set("Content-Type", "application/json")
	graphqlRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	graphqlResponse := httptest.NewRecorder()
	server.ServeHTTP(graphqlResponse, graphqlRequest)
	if graphqlResponse.Code != http.StatusOK || !strings.Contains(graphqlResponse.Body.String(), `"household":null`) {
		t.Fatalf("non-member GraphQL request was not rejected: status=%d body=%s", graphqlResponse.Code, graphqlResponse.Body.String())
	}

	contentRequest := httptest.NewRequest(http.MethodGet, "/documents/"+documentID+"/content", nil)
	contentRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	contentResponse := httptest.NewRecorder()
	server.ServeHTTP(contentResponse, contentRequest)
	if contentResponse.Code != http.StatusNotFound {
		t.Fatalf("non-member original document request was not hidden: status=%d body=%s", contentResponse.Code, contentResponse.Body.String())
	}
	previewRequest := httptest.NewRequest(http.MethodGet, "/documents/"+documentID+"/preview", nil)
	previewRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
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
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("non-member source observation exposed: path=%s status=%d", path, response.Code)
		}
	}
	evidenceRequest := httptest.NewRequest(http.MethodGet, "/purchases/"+purchaseNodeID+"/evidence", nil)
	evidenceRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
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
	evidenceRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	evidenceResponse = httptest.NewRecorder()
	server.ServeHTTP(evidenceResponse, evidenceRequest)
	if evidenceResponse.Code != http.StatusOK || strings.TrimSpace(evidenceResponse.Body.String()) != `{"evidence":[]}` {
		t.Fatalf("purchase global ID did not return empty evidence: status=%d body=%s", evidenceResponse.Code, evidenceResponse.Body.String())
	}
	documentsRequest := httptest.NewRequest(http.MethodGet, "/documents?householdId="+householdNodeID, nil)
	documentsRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
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
	if err := uploadForm.WriteField("sourceKind", testReceiptSourceKind); err != nil {
		t.Fatal(err)
	}
	filePart, err := uploadForm.CreateFormFile("file", "receipt.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := filePart.Write(previewPNG.Bytes()); err != nil {
		t.Fatal(err)
	}
	var analysisPNG bytes.Buffer
	if err := png.Encode(&analysisPNG, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	analysisPart, err := uploadForm.CreateFormFile("analysisFile", "receipt-analysis.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := analysisPart.Write(analysisPNG.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := uploadForm.WriteField("analysisTransform", `[0.5,0,0.25,0,0.5,0.25,0,0,1]`); err != nil {
		t.Fatal(err)
	}
	if err := uploadForm.Close(); err != nil {
		t.Fatal(err)
	}
	uploadRequest := httptest.NewRequest(http.MethodPost, "/documents", &uploadBody)
	uploadRequest.Header.Set("Origin", "http://horse.test")
	uploadRequest.Header.Set("Content-Type", uploadForm.FormDataContentType())
	uploadRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	uploadResponse := httptest.NewRecorder()
	server.ServeHTTP(uploadResponse, uploadRequest)
	if uploadResponse.Code != http.StatusAccepted || !strings.Contains(uploadResponse.Body.String(), `"householdId":"`+householdID+`"`) || !strings.Contains(uploadResponse.Body.String(), `"name":"receipt.png"`) {
		t.Fatalf("household global ID document upload failed: status=%d body=%s", uploadResponse.Code, uploadResponse.Body.String())
	}
	if len(store.putHouseholdIDs) != 2 || store.putHouseholdIDs[0] != householdID || store.putHouseholdIDs[1] != householdID {
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
	uploadedUUID := mustIntegrationUUID(t, uploadedID)
	uploadedDocument, err := models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(uploadedUUID)))).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if uploadedDocument.ObjectKey == uploadedDocument.AnalysisObjectKey.V || !strings.HasPrefix(uploadedDocument.AnalysisObjectKey.V, householdID+"/analysis/"+uploadedID+"/") || uploadedDocument.MimeType != "image/png" || !uploadedDocument.AnalysisObjectKey.Valid || uploadedDocument.AnalysisMimeType.V != "image/png" {
		t.Fatalf("original and analysis objects were not stored separately: original=%q/%q analysis=%q/%q", uploadedDocument.ObjectKey, uploadedDocument.MimeType, uploadedDocument.AnalysisObjectKey.V, uploadedDocument.AnalysisMimeType.V)
	}
	interactiveJob, err := models.ExtractionJobs.Query(sm.Where(models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(uploadedUUID)))).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	var interactiveRequest struct {
		Purpose string `json:"purpose"`
	}
	if err := stdjson.Unmarshal(interactiveJob.Request.Val, &interactiveRequest); err != nil || interactiveRequest.Purpose != ingestion.JobPurposeInteractive {
		t.Fatalf("default extraction purpose = %q, error=%v, want interactive", interactiveRequest.Purpose, err)
	}
	uploadedContentRequest := httptest.NewRequest(http.MethodGet, "/documents/"+uploadedID+"/content", nil)
	uploadedContentRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	uploadedContentResponse := httptest.NewRecorder()
	server.ServeHTTP(uploadedContentResponse, uploadedContentRequest)
	if uploadedContentResponse.Code != http.StatusOK || len(store.getKeys) != 1 || store.getKeys[0] != uploadedDocument.ObjectKey {
		t.Fatalf("document content did not read the original object: status=%d keys=%v", uploadedContentResponse.Code, store.getKeys)
	}
	documentsRequest = httptest.NewRequest(http.MethodGet, "/documents?householdId="+householdID, nil)
	documentsRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	documentsResponse = httptest.NewRecorder()
	server.ServeHTTP(documentsResponse, documentsRequest)
	if documentsResponse.Code != http.StatusOK || !strings.Contains(documentsResponse.Body.String(), uploadedID) || !strings.Contains(documentsResponse.Body.String(), `"name":"receipt.png"`) {
		t.Fatalf("raw household UUID compatibility failed: status=%d body=%s", documentsResponse.Code, documentsResponse.Body.String())
	}
	pdfData := []byte("%PDF-1.4 synthetic receipt")
	encodedPDF := base64.StdEncoding.EncodeToString(pdfData)
	emailData := []byte("MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=mail\r\n\r\n" +
		"--mail\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=receipt.pdf\r\nContent-Transfer-Encoding: base64\r\n\r\n" + encodedPDF + "\r\n--mail--\r\n")
	var emailUploadBody bytes.Buffer
	emailUploadForm := multipart.NewWriter(&emailUploadBody)
	if err := emailUploadForm.WriteField("householdId", householdNodeID); err != nil {
		t.Fatal(err)
	}
	if err := emailUploadForm.WriteField("sourceKind", "email"); err != nil {
		t.Fatal(err)
	}
	if err := emailUploadForm.WriteField("purpose", ingestion.JobPurposeBackground); err != nil {
		t.Fatal(err)
	}
	emailFilePart, err := emailUploadForm.CreateFormFile("file", "order.eml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := emailFilePart.Write(emailData); err != nil {
		t.Fatal(err)
	}
	if err := emailUploadForm.Close(); err != nil {
		t.Fatal(err)
	}
	emailUploadRequest := httptest.NewRequest(http.MethodPost, "/documents", &emailUploadBody)
	emailUploadRequest.Header.Set("Origin", "http://horse.test")
	emailUploadRequest.Header.Set("Content-Type", emailUploadForm.FormDataContentType())
	emailUploadRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	emailUploadResponse := httptest.NewRecorder()
	server.ServeHTTP(emailUploadResponse, emailUploadRequest)
	var emailResponse struct {
		ID          string `json:"id"`
		JobID       string `json:"jobId"`
		Attachments []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			MIMEType string `json:"mimeType"`
			Status   string `json:"status"`
			JobID    string `json:"jobId"`
		} `json:"attachments"`
	}
	if emailUploadResponse.Code != http.StatusAccepted {
		t.Fatalf("email upload failed: status=%d body=%s", emailUploadResponse.Code, emailUploadResponse.Body.String())
	}
	if err := stdjson.Unmarshal(emailUploadResponse.Body.Bytes(), &emailResponse); err != nil {
		t.Fatal(err)
	}
	if emailResponse.ID == "" || emailResponse.JobID == "" || len(emailResponse.Attachments) != 1 || emailResponse.Attachments[0].ID == "" || emailResponse.Attachments[0].Filename != "receipt.pdf" || emailResponse.Attachments[0].MIMEType != "application/pdf" || emailResponse.Attachments[0].Status != documentStatusQueued || emailResponse.Attachments[0].JobID == "" {
		t.Fatalf("email upload response omitted its child document: %s", emailUploadResponse.Body.String())
	}
	parentID := mustIntegrationUUID(t, emailResponse.ID)
	childID := mustIntegrationUUID(t, emailResponse.Attachments[0].ID)
	parentDocument, err := models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(parentID)))).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	childDocument, err := models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(childID)))).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if parentDocument.MimeType != "message/rfc822" || parentDocument.SourceKind != "email" || childDocument.MimeType != "application/pdf" || childDocument.SourceKind != testReceiptSourceKind {
		t.Fatalf("email parent or child document was persisted incorrectly: parent=%#v child=%#v", parentDocument, childDocument)
	}
	attachmentRelations, err := models.SourceDocumentAttachments.Query(sm.Where(psql.And(
		models.SourceDocumentAttachments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.SourceDocumentAttachments.Columns.ParentDocumentID.EQ(psql.Arg(parentID)),
	))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(attachmentRelations) != 1 || attachmentRelations[0].ChildDocumentID != childID || attachmentRelations[0].Filename != "receipt.pdf" {
		t.Fatalf("email attachment relation was not persisted: %#v", attachmentRelations)
	}
	jobIDs := make(map[gofrsuuid.UUID]gofrsuuid.UUID, 2)
	for _, documentID := range []gofrsuuid.UUID{parentID, childID} {
		job, err := models.ExtractionJobs.Query(sm.Where(models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(documentID)))).One(ctx, executor)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != documentStatusQueued {
			t.Fatalf("email document job status = %q, want queued", job.Status)
		}
		var request struct {
			Purpose string `json:"purpose"`
		}
		if err := stdjson.Unmarshal(job.Request.Val, &request); err != nil || request.Purpose != ingestion.JobPurposeBackground {
			t.Fatalf("email extraction purpose = %q, error=%v, want background", request.Purpose, err)
		}
		jobIDs[documentID] = job.ID
	}
	if emailResponse.JobID != jobIDs[parentID].String() || emailResponse.Attachments[0].JobID != jobIDs[childID].String() {
		t.Fatalf("email upload response job IDs do not match persisted jobs: response=%s persisted=%v", emailUploadResponse.Body.String(), jobIDs)
	}
	conflictJobID, err := insertDocumentJob(ctx, executor, householdUUID, childID)
	if err != nil {
		t.Fatal(err)
	}
	if conflictJobID != jobIDs[childID] {
		t.Fatalf("conflicting job insert returned %s, want persisted job %s", conflictJobID, jobIDs[childID])
	}
	reclassifyAttachment := func(sourceKind string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]string{"sourceKind": sourceKind})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/documents/"+childID.String()+"/retry", strings.NewReader(string(body)))
		request.Header.Set("Origin", "http://horse.test")
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	queuedReclassification := reclassifyAttachment("card")
	var queuedRetry map[string]string
	if queuedReclassification.Code != http.StatusAccepted || json.Unmarshal(queuedReclassification.Body.Bytes(), &queuedRetry) != nil || queuedRetry[jobIDJSONKey] != jobIDs[childID].String() {
		t.Fatalf("queued attachment was not reclassified with its existing job: status=%d body=%s", queuedReclassification.Code, queuedReclassification.Body.String())
	}
	childDocument, err = models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(childID)))).One(ctx, executor)
	if err != nil || childDocument.SourceKind != "card" {
		t.Fatalf("queued attachment source kind was not updated: document=%#v error=%v", childDocument, err)
	}
	failedStatus := "failed"
	if _, err := models.ExtractionJobs.Update((models.ExtractionJobSetter{Status: &failedStatus}).UpdateMod(), um.Where(models.ExtractionJobs.Columns.ID.EQ(psql.Arg(jobIDs[childID])))).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	completedReclassification := reclassifyAttachment("bank")
	var completedRetry map[string]string
	if completedReclassification.Code != http.StatusAccepted || json.Unmarshal(completedReclassification.Body.Bytes(), &completedRetry) != nil || completedRetry[jobIDJSONKey] == "" || completedRetry[jobIDJSONKey] == jobIDs[childID].String() {
		t.Fatalf("completed attachment was not reprocessed with a new job: status=%d body=%s", completedReclassification.Code, completedReclassification.Body.String())
	}
	childDocument, err = models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(childID)))).One(ctx, executor)
	if err != nil || childDocument.SourceKind != "bank" {
		t.Fatalf("attachment source kind was not updated with its retry: document=%#v error=%v", childDocument, err)
	}
	attachmentJobs, err := models.ExtractionJobs.Query(sm.Where(models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(childID))), sm.OrderBy(models.ExtractionJobs.Columns.Version)).All(ctx, executor)
	if err != nil || len(attachmentJobs) != 2 || attachmentJobs[0].Version != 1 || attachmentJobs[1].Version != 2 || attachmentJobs[1].ID.String() != completedRetry[jobIDJSONKey] {
		t.Fatalf("reclassification retry history is incorrect: jobs=%#v error=%v", attachmentJobs, err)
	}
	store.gets = 0
	invalidPreviewRequest := httptest.NewRequest(http.MethodGet, "/documents/"+documentID+"/preview?page=-1", nil)
	invalidPreviewRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	invalidPreviewResponse := httptest.NewRecorder()
	server.ServeHTTP(invalidPreviewResponse, invalidPreviewRequest)
	if invalidPreviewResponse.Code != http.StatusBadRequest || ocrCalls != 0 {
		t.Fatalf("invalid preview page was not rejected: status=%d OCR calls=%d", invalidPreviewResponse.Code, ocrCalls)
	}
	validPreviewRequest := httptest.NewRequest(http.MethodGet, "/documents/"+documentID+"/preview?page=0", nil)
	validPreviewRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	validPreviewResponse := httptest.NewRecorder()
	server.ServeHTTP(validPreviewResponse, validPreviewRequest)
	if validPreviewResponse.Code != http.StatusOK || validPreviewResponse.Header().Get("Content-Type") != "image/png" || validPreviewResponse.Header().Get("Cache-Control") != "private, no-store" || validPreviewResponse.Header().Get("X-Original-Content-Type") != mimeType || validPreviewResponse.Header().Get("X-Source-Page") != "0" || validPreviewResponse.Header().Get("X-Coordinate-Transform") != `[1,0,0,0,1,0,0,0,1]` || !bytes.Equal(validPreviewResponse.Body.Bytes(), previewPNG.Bytes()) || ocrCalls != 1 || store.gets != 1 {
		t.Fatalf("member preview response incorrect: status=%d headers=%v OCR calls=%d storage reads=%d", validPreviewResponse.Code, validPreviewResponse.Header(), ocrCalls, store.gets)
	}
	confirm := func(index int) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]any{"purchaseId": purchaseNodeID, itemIDJSONKey: itemNodeID, sourceItemIndexJSONKey: index, observationJobIDJSONKey: jobID})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/documents/"+documentID+"/confirm", strings.NewReader(string(body)))
		request.Header.Set("Origin", "http://horse.test")
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
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
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/documents/"+documentID, nil)
	deleteRequest.Header.Set("Origin", "http://horse.test")
	deleteRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	deleteResponse := httptest.NewRecorder()
	server.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusConflict || len(store.deletes) != 0 {
		t.Fatalf("confirmed source document was deleted: status=%d object deletions=%d", deleteResponse.Code, len(store.deletes))
	}
	if _, err := models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(documentUUID)))).One(ctx, executor); err != nil {
		t.Fatalf("confirmed source document metadata was deleted: %v", err)
	}
	unconfirmedDocumentID, unconfirmedJobID := uuid.NewV7().String(), uuid.NewV7().String()
	unconfirmedDocumentUUID := mustIntegrationUUID(t, unconfirmedDocumentID)
	unconfirmedJobUUID := mustIntegrationUUID(t, unconfirmedJobID)
	unconfirmedHash, unconfirmedObjectKey, unconfirmedAnalysisKey := "access-test:"+unconfirmedDocumentID, householdID+"/originals/"+unconfirmedDocumentID, householdID+"/analysis/"+unconfirmedDocumentID
	analysisMIME := "image/png"
	analysisObject := sql.Null[string]{V: unconfirmedAnalysisKey, Valid: true}
	analysisMIMEValue := sql.Null[string]{V: analysisMIME, Valid: true}
	analysisTransform := sql.Null[types.JSON[stdjson.RawMessage]]{V: types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(`[1,0,0,0,1,0,0,0,1]`)}, Valid: true}
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &unconfirmedDocumentUUID, HouseholdID: &householdUUID, Sha256: &unconfirmedHash, ObjectKey: &unconfirmedObjectKey, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &ownerUUID, AnalysisObjectKey: &analysisObject, AnalysisMimeType: &analysisMIMEValue, AnalysisTransform: &analysisTransform}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	unconfirmedStatus := "failed"
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{ID: &unconfirmedJobUUID, HouseholdID: &householdUUID, SourceDocumentID: &unconfirmedDocumentUUID, Status: &unconfirmedStatus}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	store.deleteFailures = 1
	deleteRequest = httptest.NewRequest(http.MethodDelete, "/documents/"+unconfirmedDocumentID, nil)
	deleteRequest.Header.Set("Origin", "http://horse.test")
	deleteRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	deleteResponse = httptest.NewRecorder()
	server.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusBadGateway || len(store.deletes) != 1 || store.deletes[0] != unconfirmedAnalysisKey {
		t.Fatalf("failed object deletion did not preserve retry state: status=%d object deletions=%v body=%s", deleteResponse.Code, store.deletes, deleteResponse.Body.String())
	}
	pendingDocument, err := models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(unconfirmedDocumentUUID)))).One(ctx, executor)
	if err != nil || !documentDeletionPending(pendingDocument) {
		t.Fatalf("failed object deletion did not persist its tombstone: document=%v error=%v", pendingDocument, err)
	}
	deleteRequest = httptest.NewRequest(http.MethodDelete, "/documents/"+unconfirmedDocumentID, nil)
	deleteRequest.Header.Set("Origin", "http://horse.test")
	deleteRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	deleteResponse = httptest.NewRecorder()
	server.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent || len(store.deletes) != 3 || store.deletes[1] != unconfirmedAnalysisKey || store.deletes[2] != unconfirmedObjectKey {
		t.Fatalf("unconfirmed source document deletion failed: status=%d object deletions=%v body=%s", deleteResponse.Code, store.deletes, deleteResponse.Body.String())
	}
	if _, err := models.SourceDocuments.Query(sm.Where(models.SourceDocuments.Columns.ID.EQ(psql.Arg(unconfirmedDocumentUUID)))).One(ctx, executor); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unconfirmed source document metadata remains: %v", err)
	}
	sharedAnalysisKey := householdID + "/originals/shared-analysis-object"
	sharedTargetID, sharedReferenceID, sharedJobID := uuid.NewV7().String(), uuid.NewV7().String(), uuid.NewV7().String()
	sharedTargetUUID, sharedReferenceUUID, sharedJobUUID := mustIntegrationUUID(t, sharedTargetID), mustIntegrationUUID(t, sharedReferenceID), mustIntegrationUUID(t, sharedJobID)
	sharedTargetHash, sharedReferenceHash := "access-test:"+sharedTargetID, "access-test:"+sharedReferenceID
	sharedTargetObjectKey, sharedReferenceObjectKey := householdID+"/originals/"+sharedTargetID, sharedAnalysisKey
	sharedAnalysisObject := sql.Null[string]{V: sharedAnalysisKey, Valid: true}
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &sharedTargetUUID, HouseholdID: &householdUUID, Sha256: &sharedTargetHash, ObjectKey: &sharedTargetObjectKey, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &ownerUUID, AnalysisObjectKey: &sharedAnalysisObject, AnalysisMimeType: &analysisMIMEValue, AnalysisTransform: &analysisTransform}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &sharedReferenceUUID, HouseholdID: &householdUUID, Sha256: &sharedReferenceHash, ObjectKey: &sharedReferenceObjectKey, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &ownerUUID}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{ID: &sharedJobUUID, HouseholdID: &householdUUID, SourceDocumentID: &sharedTargetUUID, Status: &unconfirmedStatus}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	deletesBeforeShared := len(store.deletes)
	deleteRequest = httptest.NewRequest(http.MethodDelete, "/documents/"+sharedTargetID, nil)
	deleteRequest.Header.Set("Origin", "http://horse.test")
	deleteRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	deleteResponse = httptest.NewRecorder()
	server.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent || len(store.deletes) != deletesBeforeShared+1 || store.deletes[deletesBeforeShared] != sharedTargetObjectKey {
		t.Fatalf("document deletion removed a shared object: status=%d object deletions=%v body=%s", deleteResponse.Code, store.deletes[deletesBeforeShared:], deleteResponse.Body.String())
	}
	documentsRequest = httptest.NewRequest(http.MethodGet, "/documents?householdId="+householdID, nil)
	documentsRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	documentsResponse = httptest.NewRecorder()
	server.ServeHTTP(documentsResponse, documentsRequest)
	var listedDocuments struct {
		Documents []documentResponse `json:"documents"`
	}
	if documentsResponse.Code != http.StatusOK || json.Unmarshal(documentsResponse.Body.Bytes(), &listedDocuments) != nil {
		t.Fatalf("document evidence list failed: status=%d body=%s", documentsResponse.Code, documentsResponse.Body.String())
	}
	var foundEvidence bool
	for _, listed := range listedDocuments.Documents {
		if listed.ID == documentID {
			foundEvidence = listed.HasEvidence
			break
		}
	}
	if !foundEvidence {
		t.Fatalf("document list did not report confirmed evidence: %s", documentsResponse.Body.String())
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
		map[string]any{documentIDJSONKey: documentID, itemIDJSONKey: itemNodeID, sourceItemIndexJSONKey: 0, observationJobIDJSONKey: jobID},
		map[string]any{documentIDJSONKey: secondDocumentID, itemIDJSONKey: itemNodeID, sourceItemIndexJSONKey: 0, observationJobIDJSONKey: secondJobID},
	}})
	if err != nil {
		t.Fatal(err)
	}
	groupRequest := httptest.NewRequest(http.MethodPost, "/purchases/"+purchaseNodeID+"/evidence", strings.NewReader(string(groupBody)))
	groupRequest.Header.Set("Origin", "http://horse.test")
	groupRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	groupResponse := httptest.NewRecorder()
	server.ServeHTTP(groupResponse, groupRequest)
	if groupResponse.Code != http.StatusOK || !strings.Contains(groupResponse.Body.String(), `"sourceCount":2`) {
		t.Fatalf("multi-source evidence confirmation failed: %d %s", groupResponse.Code, groupResponse.Body.String())
	}
	rollbackBody, err := json.Marshal(map[string]any{"sources": []any{
		map[string]any{documentIDJSONKey: secondDocumentID, "sourceReference": "rollback"},
		map[string]any{documentIDJSONKey: "invalid-document-id"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	rollbackRequest := httptest.NewRequest(http.MethodPost, "/purchases/"+purchaseNodeID+"/evidence", strings.NewReader(string(rollbackBody)))
	rollbackRequest.Header.Set("Origin", "http://horse.test")
	rollbackRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
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
	evidenceRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	evidenceResponse = httptest.NewRecorder()
	server.ServeHTTP(evidenceResponse, evidenceRequest)
	if evidenceResponse.Code != http.StatusOK || !strings.Contains(evidenceResponse.Body.String(), `"printedName":"printed item"`) || !strings.Contains(evidenceResponse.Body.String(), `"top":0.1`) {
		t.Fatalf("observation snapshot missing: %d %s", evidenceResponse.Code, evidenceResponse.Body.String())
	}
	documentRequest := httptest.NewRequest(http.MethodGet, "/documents/"+documentID, nil)
	documentRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: ownerCookie, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	documentResponse := httptest.NewRecorder()
	server.ServeHTTP(documentResponse, documentRequest)
	if documentResponse.Code != http.StatusOK || !strings.Contains(documentResponse.Body.String(), `"purchaseId":"`+purchaseID+`"`) {
		t.Fatalf("confirmation reference missing: %d %s", documentResponse.Code, documentResponse.Body.String())
	}
}
