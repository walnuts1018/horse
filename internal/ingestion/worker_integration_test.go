package ingestion

import (
	"bytes"
	"context"
	"database/sql"
	stdjson "encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type testDocumentStore map[string]Document

func (s testDocumentStore) Get(_ context.Context, key, _ string) (Document, error) {
	document, exists := s[key]
	if !exists {
		return Document{}, os.ErrNotExist
	}
	return document, nil
}

func TestWorkerSpendCapDefersWithoutConsumingFailureBudgetAndDisabledWorkerKeepsJobsQueued(t *testing.T) {
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

	memberID, householdID := testUUID(t), testUUID(t)
	executor := bobpgx.NewPool(db)
	memberUUID, householdUUID := parseTestUUID(t, memberID), parseTestUUID(t, householdID)
	memberSubject, memberName := "worker-test:"+memberID, "Worker test"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberUUID, OidcSubject: &memberSubject, DisplayName: &memberName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	householdName := "Worker integration"
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdUUID, Name: &householdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	ownerRole := "OWNER"
	if _, err := models.HouseholdMembers.Insert(&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &memberUUID, Role: &ownerRole}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdUUID)))).Exec(t.Context(), executor)
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberUUID)))).Exec(t.Context(), executor)
	}()

	store := testDocumentStore{}
	insertJob := func(sourceKind, mimeType, objectKey string, attempt, failureCount int) string {
		t.Helper()
		documentID, jobID := testUUID(t), testUUID(t)
		documentUUID, jobUUID := parseTestUUID(t, documentID), parseTestUUID(t, jobID)
		hash, creatorUUID := testUUID(t), memberUUID
		if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
			ID: &documentUUID, HouseholdID: &householdUUID, Sha256: &hash, ObjectKey: &objectKey,
			MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &creatorUUID,
		}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		status := extractionStatusQueued
		availableAt := time.Now().UTC()
		attemptValue, err := postgresInt32(attempt, "extraction attempt")
		if err != nil {
			t.Fatal(err)
		}
		failureCountValue, err := postgresInt32(failureCount, "extraction failure count")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
			ID: &jobUUID, HouseholdID: &householdUUID, SourceDocumentID: &documentUUID, Status: &status,
			Attempt: &attemptValue, FailureCount: &failureCountValue, AvailableAt: &availableAt,
		}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		return jobID
	}

	analysisDocumentID, analysisJobID := testUUID(t), testUUID(t)
	analysisDocumentUUID, analysisJobUUID := parseTestUUID(t, analysisDocumentID), parseTestUUID(t, analysisJobID)
	analysisHash, originalKey, analysisKey := testUUID(t), "original-image", "analysis-image"
	imageMIME, analysisMIME, sourceKind := mimeTypeJPEG, mimeTypePNG, "receipt"
	analysisTransform := sql.Null[types.JSON[stdjson.RawMessage]]{
		V:     types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(`[0.5,0,0.25,0,0.5,0.25,0,0,1]`)},
		Valid: true,
	}
	analysisObject := sql.Null[string]{V: analysisKey, Valid: true}
	analysisMIMEValue := sql.Null[string]{V: analysisMIME, Valid: true}
	if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
		ID: &analysisDocumentUUID, HouseholdID: &householdUUID, Sha256: &analysisHash, ObjectKey: &originalKey,
		MimeType: &imageMIME, SourceKind: &sourceKind, CreatedBy: &memberUUID,
		AnalysisObjectKey: &analysisObject, AnalysisMimeType: &analysisMIMEValue, AnalysisTransform: &analysisTransform,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	queued := extractionStatusQueued
	analysisAvailableAt := time.Now().UTC()
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
		ID: &analysisJobUUID, HouseholdID: &householdUUID, SourceDocumentID: &analysisDocumentUUID, Status: &queued, AvailableAt: &analysisAvailableAt,
	}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	analysisWorker := Worker{DB: db, GeminiEnabled: true}
	claimedJob, claimed, err := analysisWorker.claim(ctx, analysisJobID)
	if err != nil || !claimed {
		t.Fatalf("camera analysis job was not claimed: claimed=%t error=%v", claimed, err)
	}
	if claimedJob.ObjectKey != originalKey || claimedJob.AnalysisObjectKey != analysisKey || claimedJob.MIMEType != imageMIME || claimedJob.AnalysisMIMEType != analysisMIME || claimedJob.AnalysisTransform == nil || *claimedJob.AnalysisTransform != [9]float64{0.5, 0, 0.25, 0, 0.5, 0.25, 0, 0, 1} {
		t.Fatalf("worker claim mixed or omitted original and analysis metadata: %+v", claimedJob)
	}

	freeCalls := 0
	free := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		freeCalls++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"status":"UNAVAILABLE","message":"temporarily unavailable"}}`)
	}))
	defer free.Close()
	paidCalls := 0
	paid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		paidCalls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"project spend cap reached"}}`)
	}))
	defer paid.Close()

	spendCapJob := insertJob("receipt", mimeTypePDF, "spend-cap-document", 9, 4)
	store["spend-cap-document"] = Document{MIMEType: mimeTypePDF, Data: []byte("test source")}
	worker := Worker{
		DB: db, Store: store, GeminiEnabled: true, allowSyntheticEvaluation: true,
		Router: GeminiRouter{
			Free: Gemini{APIKey: "test-free", Endpoint: free.URL + "/v1beta/", HTTPClient: free.Client()},
			Paid: Gemini{APIKey: "test-paid", Endpoint: paid.URL + "/v1beta/", HTTPClient: paid.Client()},
		},
	}
	for expectedAttempt := 10; expectedAttempt <= 11; expectedAttempt++ {
		processed, err := worker.RunOne(ctx, spendCapJob)
		if err != nil || !processed {
			t.Fatalf("spend-cap job was not processed: processed=%v error=%v", processed, err)
		}
		spendCap, err := loadTestJob(ctx, executor, spendCapJob)
		if err != nil {
			t.Fatal(err)
		}
		status, errorCode := spendCap.Status, spendCap.ErrorCode.V
		attempt, failureCount := int(spendCap.Attempt), int(spendCap.FailureCount)
		availableInSeconds := time.Until(spendCap.AvailableAt).Seconds()
		if status != extractionStatusQueued || errorCode != geminiFailureSpendCap || attempt != expectedAttempt || failureCount != 4 || availableInSeconds < 30*60 {
			t.Fatalf("spend cap must keep the source queued without consuming ordinary failures: status=%q error=%q attempt=%d failures=%d availableIn=%.0fs", status, errorCode, attempt, failureCount, availableInSeconds)
		}
		if expectedAttempt < 11 {
			if err := updateTestJob(ctx, executor, spendCapJob, &models.ExtractionJobSetter{AvailableAt: new(time.Time)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := worker.RecoverExhausted(ctx); err != nil {
		t.Fatal(err)
	}
	held, err := loadTestJob(ctx, executor, spendCapJob)
	if err != nil || held.Status != extractionStatusQueued {
		t.Fatalf("exhaustion recovery must not fail a queued spend-cap job: status=%q error=%v", held.Status, err)
	}

	blockedJob := insertJob("receipt", mimeTypePDF, "blocked-developer-api-document", 3, 2)
	store["blocked-developer-api-document"] = Document{MIMEType: mimeTypePDF, Data: []byte("synthetic source")}
	blockedWorker := Worker{DB: db, Store: store, GeminiEnabled: true, Router: GeminiRouter{
		Free: Gemini{APIKey: "test-free", Endpoint: free.URL + "/v1beta/", HTTPClient: free.Client()},
		Paid: Gemini{APIKey: "test-paid", Endpoint: paid.URL + "/v1beta/", HTTPClient: paid.Client()},
	}}
	beforeFreeCalls, beforePaidCalls := freeCalls, paidCalls
	processed, err := blockedWorker.RunOne(ctx, blockedJob)
	if err != nil || !processed {
		t.Fatalf("Developer API policy hold was not recorded: processed=%t error=%v", processed, err)
	}
	blocked, err := loadTestJob(ctx, executor, blockedJob)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Status != extractionStatusFailed || blocked.ErrorCode.V != geminiFailureDeveloperAPIRestricted || blocked.FailureCount != 2 {
		t.Fatalf("Developer API policy stop must preserve the failure count and stop retrying: status=%q code=%q failures=%d", blocked.Status, blocked.ErrorCode.V, blocked.FailureCount)
	}
	if freeCalls != beforeFreeCalls || paidCalls != beforePaidCalls {
		t.Fatalf("production worker sent a document to Gemini Developer API: free=%d paid=%d", freeCalls-beforeFreeCalls, paidCalls-beforePaidCalls)
	}

	temporary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"status":"UNAVAILABLE"}}`)
	}))
	defer temporary.Close()
	ordinaryFailureJob := insertJob("receipt", mimeTypePDF, "temporary-error-document", 0, 0)
	store["temporary-error-document"] = Document{MIMEType: mimeTypePDF, Data: []byte("test source")}
	ordinaryFailureWorker := Worker{DB: db, Store: store, GeminiEnabled: true, allowSyntheticEvaluation: true, Router: GeminiRouter{
		Free: Gemini{APIKey: "test-free", Endpoint: temporary.URL + "/v1beta/", HTTPClient: temporary.Client()},
		Paid: Gemini{APIKey: "test-paid", Endpoint: temporary.URL + "/v1beta/", HTTPClient: temporary.Client()},
	}}
	for attempt := 1; attempt <= 5; attempt++ {
		processed, err := ordinaryFailureWorker.RunOne(ctx, ordinaryFailureJob)
		if err != nil || !processed {
			t.Fatalf("ordinary provider failure was not processed: processed=%v error=%v", processed, err)
		}
		failedJob, err := loadTestJob(ctx, executor, ordinaryFailureJob)
		if err != nil {
			t.Fatal(err)
		}
		status, gotAttempt, failureCount := failedJob.Status, int(failedJob.Attempt), int(failedJob.FailureCount)
		wantStatus := extractionStatusQueued
		if attempt == 5 {
			wantStatus = extractionStatusFailed
		}
		if status != wantStatus || gotAttempt != attempt || failureCount != attempt {
			t.Fatalf("ordinary failures must consume the separate retry budget: status=%q attempt=%d failures=%d want=%q/%d", status, gotAttempt, failureCount, wantStatus, attempt)
		}
		if attempt < 5 {
			if err := updateTestJob(ctx, executor, ordinaryFailureJob, &models.ExtractionJobSetter{AvailableAt: new(time.Time)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	sourceDocument, err := models.SourceDocuments.Query(sm.Where(psql.And(
		models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
		models.SourceDocuments.Columns.ObjectKey.EQ(psql.Arg("temporary-error-document")),
	))).One(ctx, executor)
	if err != nil {
		t.Fatalf("failed extraction must retain its source document record: %v", err)
	}
	if _, err := store.Get(ctx, sourceDocument.ObjectKey, sourceDocument.MimeType); err != nil {
		t.Fatalf("failed extraction must retain the original object: %v", err)
	}

	pausedJob := insertJob("receipt", mimeTypePDF, "paused-document", 0, 0)
	store["paused-document"] = Document{MIMEType: mimeTypePDF, Data: []byte("test source")}
	disabledWorker := Worker{DB: db, Store: store}
	processed, err = disabledWorker.RunOne(ctx, "")
	if err != nil || processed {
		t.Fatalf("disabled worker must leave Gemini documents unclaimed: processed=%v error=%v", processed, err)
	}
	paused, err := loadTestJob(ctx, executor, pausedJob)
	if err != nil {
		t.Fatal(err)
	}
	pausedStatus, pausedAttempt, pausedFailures := paused.Status, int(paused.Attempt), int(paused.FailureCount)
	if pausedStatus != extractionStatusQueued || pausedAttempt != 0 || pausedFailures != 0 {
		t.Fatalf("disabled job state changed: status=%q attempt=%d failures=%d", pausedStatus, pausedAttempt, pausedFailures)
	}

	csvJob := insertJob("card", "text/csv", "card-csv-document", 0, 0)
	store["card-csv-document"] = Document{MIMEType: "text/csv", Data: []byte("利用日,利用店名,利用金額,取引番号\n2026/10/12,店,100,tx-1\n")}
	processed, err = disabledWorker.RunOne(ctx, "")
	if err != nil || !processed {
		t.Fatalf("disabled worker must process deterministic CSV imports: processed=%v error=%v", processed, err)
	}
	csv, err := loadTestJob(ctx, executor, csvJob)
	if err != nil {
		t.Fatal(err)
	}
	csvResult, err := stdjson.Marshal(csv.Result.V.Val)
	if err != nil {
		t.Fatal(err)
	}
	if csv.Status != extractionStatusSucceeded || csv.Attempt != 1 || !bytes.Contains(csvResult, []byte(`"transactionId":"tx-1"`)) {
		t.Fatalf("CSV import did not complete deterministically while Gemini was disabled: status=%q attempt=%d result=%s", csv.Status, csv.Attempt, csvResult)
	}
	observations, err := models.SourceObservations.Query(sm.Where(models.SourceObservations.Columns.SourceDocumentID.EQ(psql.Arg(csv.SourceDocumentID)))).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 {
		t.Fatalf("successful extraction persisted %d source observations, want 1", len(observations))
	}
	if observation := observations[0]; observation.SourceReference != "row:2" || observation.TransactionID != "tx-1" || observation.SourceHash == "" {
		t.Fatalf("successful extraction persisted an incomplete source observation: reference=%q transaction=%q hash=%q", observation.SourceReference, observation.TransactionID, observation.SourceHash)
	}
}

func parseTestUUID(t *testing.T, value string) uuid.UUID {
	t.Helper()
	parsed, err := uuid.FromString(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func loadTestJob(ctx context.Context, executor bob.Executor, jobID string) (*models.ExtractionJob, error) {
	id, err := uuid.FromString(jobID)
	if err != nil {
		return nil, err
	}
	return models.ExtractionJobs.Query(sm.Where(models.ExtractionJobs.Columns.ID.EQ(psql.Arg(id)))).One(ctx, executor)
}

func updateTestJob(ctx context.Context, executor bob.Executor, jobID string, setter *models.ExtractionJobSetter) error {
	id, err := uuid.FromString(jobID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	setter.AvailableAt = &now
	_, err = models.ExtractionJobs.Update(setter.UpdateMod(), um.Where(models.ExtractionJobs.Columns.ID.EQ(psql.Arg(id)))).Exec(ctx, executor)
	return err
}

func testUUID(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV4()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}
