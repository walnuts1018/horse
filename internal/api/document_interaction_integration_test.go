package api

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
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
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/auth"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
	"uuid"
)

func TestDocumentInteractionBranchesFromSelectedExtractionVersion(t *testing.T) {
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
	executor := bobpgx.NewPool(db)
	memberID := mustIntegrationUUID(t, uuid.NewV7().String())
	householdID := mustIntegrationUUID(t, uuid.NewV7().String())
	documentID := mustIntegrationUUID(t, uuid.NewV7().String())
	memberSubject, householdName := "document-interaction:"+memberID.String(), "Document interaction test"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &memberSubject, DisplayName: &memberSubject}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &householdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	role := "OWNER"
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberID, Role: &role}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	documentHash, objectKey, mimeType, sourceKind := "document-interaction:"+documentID.String(), householdID.String()+"/originals/"+documentID.String(), documentMIMEJPEG, testReceiptSourceKind
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{ID: &documentID, HouseholdID: &householdID, Sha256: &documentHash, ObjectKey: &objectKey, MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberID}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(context.Background(), executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberID)))).Exec(context.Background(), executor)
	}()

	firstJobID := mustIntegrationUUID(t, uuid.NewV7().String())
	secondJobID := mustIntegrationUUID(t, uuid.NewV7().String())
	model, succeeded := ingestion.ModelReceipt, "succeeded"
	for index, fixture := range []struct {
		id           gofrsuuid.UUID
		conversation string
		item         string
	}{
		{id: firstJobID, conversation: "old-conversation", item: "Old item"},
		{id: secondJobID, conversation: "latest-conversation", item: "Latest item"},
	} {
		version := int32(index + 1)
		request := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(`{"model":"` + model + `"}`)}
		resultJSON, err := stdjson.Marshal(ingestion.Verification{
			Receipt:        ingestion.Receipt{Merchant: "Test store", Total: int64(100 + index), Items: []ingestion.Item{{Name: fixture.item, PrintedName: fixture.item, PrintedAmount: int64(100 + index), GrossAmount: int64(100 + index)}}},
			ConversationID: fixture.conversation,
			Conversation:   []ingestion.ConversationMessage{{Role: "user", Text: fixture.item + " question"}, {Role: "assistant", Text: fixture.item + " answer"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		result := sql.Null[types.JSON[stdjson.RawMessage]]{V: types.JSON[stdjson.RawMessage]{Val: resultJSON}, Valid: true}
		if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
			ID: &fixture.id, HouseholdID: &householdID, SourceDocumentID: &documentID, Version: &version, Status: &succeeded, Request: &request, Result: &result,
		}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
	}

	var issuerURL string
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.MarshalWrite(w, map[string]any{
			"issuer": issuerURL, "authorization_endpoint": "http://" + r.Host + "/authorize",
			"token_endpoint": "http://" + r.Host + "/token", "jwks_uri": "http://" + r.Host + "/keys",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	defer issuer.Close()
	issuerURL = issuer.URL
	authService, err := auth.New(ctx, db, auth.Config{IssuerURL: issuerURL, ClientID: "document-interaction", RedirectURL: "http://horse.test/auth/callback", PublicOrigin: "http://horse.test"})
	if err != nil {
		t.Fatal(err)
	}
	cookieValue := "document-interaction-session-" + uuid.NewV7().String()
	cookieHash := sha256.Sum256([]byte(cookieValue))
	cookieHashBytes := cookieHash[:]
	expiresAt := time.Now().Add(10 * time.Minute)
	if _, err := models.OidcSessions.Insert(&models.OidcSessionSetter{TokenHash: &cookieHashBytes, MemberID: &memberID, Expiry: &expiresAt}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	unauthorizedMemberID := mustIntegrationUUID(t, uuid.NewV7().String())
	unauthorizedSubject := "document-interaction-outsider:" + unauthorizedMemberID.String()
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &unauthorizedMemberID, OidcSubject: &unauthorizedSubject, DisplayName: &unauthorizedSubject}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	unauthorizedCookieValue := "document-interaction-outsider-session-" + uuid.NewV7().String()
	unauthorizedCookieHash := sha256.Sum256([]byte(unauthorizedCookieValue))
	unauthorizedCookieHashBytes := unauthorizedCookieHash[:]
	if _, err := models.OidcSessions.Insert(&models.OidcSessionSetter{TokenHash: &unauthorizedCookieHashBytes, MemberID: &unauthorizedMemberID, Expiry: &expiresAt}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(unauthorizedMemberID)))).Exec(context.Background(), executor)
	}()
	server := New(db, authService, slog.New(slog.DiscardHandler), Config{PublicOrigin: "http://horse.test", Store: &previewTestStore{}}).Handler()
	request := httptest.NewRequest(http.MethodGet, "/documents/"+documentID.String()+"/extractions", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"version":1`) || !strings.Contains(response.Body.String(), `"version":2`) {
		t.Fatalf("extraction version list failed: status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/documents/"+documentID.String()+"/extractions/"+firstJobID.String(), nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue})
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Old item") {
		t.Fatalf("historical extraction lookup failed: status=%d body=%s", response.Code, response.Body.String())
	}

	interactionBody := `{"message":"Recheck the old result","baseJobId":"` + firstJobID.String() + `"}`
	request = httptest.NewRequest(http.MethodPost, "/documents/"+documentID.String()+"/interaction", strings.NewReader(interactionBody))
	request.Header.Set("Origin", "http://horse.test")
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue})
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("historical extraction interaction failed: status=%d body=%s", response.Code, response.Body.String())
	}
	var accepted struct {
		JobID string `json:"jobId"`
	}
	if err := stdjson.Unmarshal(response.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	createdJobID := mustIntegrationUUID(t, accepted.JobID)
	createdJob, err := models.ExtractionJobs.Query(sm.Where(models.ExtractionJobs.Columns.ID.EQ(psql.Arg(createdJobID)))).One(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	var createdRequest struct {
		ConversationID    string                          `json:"conversationId"`
		Clarification     string                          `json:"clarification"`
		BaseJobID         string                          `json:"baseJobId"`
		Conversation      []ingestion.ConversationMessage `json:"conversation"`
		CurrentExtraction struct {
			Receipt ingestion.Receipt `json:"receipt"`
		} `json:"currentExtraction"`
	}
	if err := stdjson.Unmarshal(createdJob.Request.Val, &createdRequest); err != nil {
		t.Fatal(err)
	}
	if createdJob.Version != 3 || createdRequest.Clarification != "Recheck the old result" || createdRequest.BaseJobID != firstJobID.String() || createdRequest.ConversationID == "old-conversation" || createdRequest.ConversationID == "latest-conversation" || len(createdRequest.Conversation) != 2 || createdRequest.Conversation[0].Text != "Old item question" || len(createdRequest.CurrentExtraction.Receipt.Items) != 1 || createdRequest.CurrentExtraction.Receipt.Items[0].Name != "Old item" {
		t.Fatalf("new job did not branch from the selected extraction: version=%d request=%#v", createdJob.Version, createdRequest)
	}
	assertPendingInteraction := func(wantStatus string) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/documents/"+documentID.String(), nil)
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue})
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		var document struct {
			Status             string `json:"status"`
			PendingInteraction *struct {
				JobID     string `json:"jobId"`
				Status    string `json:"status"`
				Message   string `json:"message"`
				Model     string `json:"model"`
				BaseJobID string `json:"baseJobId"`
			} `json:"pendingInteraction"`
		}
		if response.Code != http.StatusOK || stdjson.Unmarshal(response.Body.Bytes(), &document) != nil {
			t.Fatalf("document interaction state lookup failed: status=%d body=%s", response.Code, response.Body.String())
		}
		if document.Status != wantStatus || document.PendingInteraction == nil || document.PendingInteraction.JobID != createdJobID.String() || document.PendingInteraction.Status != wantStatus || document.PendingInteraction.Message != "Recheck the old result" || document.PendingInteraction.Model != ingestion.ModelReceipt || document.PendingInteraction.BaseJobID != firstJobID.String() {
			t.Fatalf("document interaction state was not restored: %#v", document)
		}
	}
	assertPendingInteraction("queued")
	runningStatus := "running"
	replyPreview := "原本の金額を確認しました。"
	if err := createdJob.Update(ctx, executor, &models.ExtractionJobSetter{Status: &runningStatus, InteractionReplyPreview: &replyPreview}); err != nil {
		t.Fatal(err)
	}
	assertSSE := func(cookie string, wantCode int, wantStatus, wantReply string) {
		t.Helper()
		requestCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		streamServer := httptest.NewServer(server)
		defer streamServer.Close()
		streamRequest, err := http.NewRequestWithContext(requestCtx, http.MethodGet, streamServer.URL+"/documents/"+documentID.String()+"/interactions/"+createdJobID.String()+"/events", nil)
		if err != nil {
			t.Fatal(err)
		}
		streamRequest.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
		streamResponse, err := http.DefaultClient.Do(streamRequest)
		if err != nil {
			t.Fatal(err)
		}
		defer streamResponse.Body.Close()
		if streamResponse.StatusCode != wantCode {
			t.Fatalf("interaction stream status = %d, want %d", streamResponse.StatusCode, wantCode)
		}
		if wantCode != http.StatusOK {
			return
		}
		line, err := bufio.NewReader(streamResponse.Body).ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "data: ") {
			t.Fatalf("interaction stream event = %q, error %v", line, err)
		}
		var event documentInteractionEvent
		if err := stdjson.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &event); err != nil {
			t.Fatal(err)
		}
		if event.Status != wantStatus || event.Reply != wantReply {
			t.Fatalf("interaction stream event = %#v, want status %q and reply %q", event, wantStatus, wantReply)
		}
	}
	assertSSE(cookieValue, http.StatusOK, "running", replyPreview)
	assertSSE(unauthorizedCookieValue, http.StatusNotFound, "", "")
	otherJobID := mustIntegrationUUID(t, uuid.NewV7().String())
	request = httptest.NewRequest(http.MethodGet, "/documents/"+documentID.String()+"/interactions/"+otherJobID.String()+"/events", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue})
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unrelated interaction stream status = %d, want not found", response.Code)
	}
	succeededStatus := "succeeded"
	if err := createdJob.Update(ctx, executor, &models.ExtractionJobSetter{Status: &succeededStatus}); err != nil {
		t.Fatal(err)
	}
	assertSSE(cookieValue, http.StatusOK, "succeeded", "")
	failedStatus := "failed"
	errorCode := sql.Null[string]{V: "provider_unavailable", Valid: true}
	emptyReplyPreview := ""
	if err := createdJob.Update(ctx, executor, &models.ExtractionJobSetter{Status: &failedStatus, ErrorCode: &errorCode, InteractionReplyPreview: &emptyReplyPreview}); err != nil {
		t.Fatal(err)
	}
	assertPendingInteraction("failed")
	request = httptest.NewRequest(http.MethodGet, "/documents?householdId="+householdID.String(), nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookieValue})
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	var documentsResponse struct {
		Documents []struct {
			ID                 string `json:"id"`
			PendingInteraction *struct {
				JobID     string `json:"jobId"`
				Status    string `json:"status"`
				Message   string `json:"message"`
				Model     string `json:"model"`
				BaseJobID string `json:"baseJobId"`
			} `json:"pendingInteraction"`
		} `json:"documents"`
	}
	if response.Code != http.StatusOK || stdjson.Unmarshal(response.Body.Bytes(), &documentsResponse) != nil || len(documentsResponse.Documents) != 1 {
		t.Fatalf("document list interaction state lookup failed: status=%d body=%s", response.Code, response.Body.String())
	}
	listed := documentsResponse.Documents[0]
	if listed.ID != documentID.String() || listed.PendingInteraction == nil || listed.PendingInteraction.JobID != createdJobID.String() || listed.PendingInteraction.Status != "failed" || listed.PendingInteraction.Message != "Recheck the old result" || listed.PendingInteraction.Model != ingestion.ModelReceipt || listed.PendingInteraction.BaseJobID != firstJobID.String() {
		t.Fatalf("document list did not restore the failed interaction: %#v", listed)
	}
}
