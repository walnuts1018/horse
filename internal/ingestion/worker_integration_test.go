package ingestion

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

	memberID, householdID := testUUID(t), testUUID(t)
	if _, err := db.Exec(ctx, `INSERT INTO members(id,oidc_subject,display_name) VALUES($1,$2,'Worker test')`, memberID, "worker-test:"+memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO households(id,name) VALUES($1,'Worker integration')`, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO household_members(household_id,member_id,role) VALUES($1,$2,'OWNER')`, householdID, memberID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM households WHERE id=$1`, householdID)
		_, _ = db.Exec(context.Background(), `DELETE FROM members WHERE id=$1`, memberID)
	}()

	store := testDocumentStore{}
	insertJob := func(sourceKind, mimeType, objectKey string, attempt, failureCount int) string {
		t.Helper()
		documentID, jobID := testUUID(t), testUUID(t)
		if _, err := db.Exec(ctx, `INSERT INTO source_documents(id,household_id,sha256,object_key,mime_type,source_kind,created_by) VALUES($1,$2,$3,$4,$5,$6,$7)`, documentID, householdID, testUUID(t), objectKey, mimeType, sourceKind, memberID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO extraction_jobs(id,household_id,source_document_id,status,attempt,failure_count) VALUES($1,$2,$3,'queued',$4,$5)`, jobID, householdID, documentID, attempt, failureCount); err != nil {
			t.Fatal(err)
		}
		return jobID
	}

	free := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"rate limit"}}`)
	}))
	defer free.Close()
	paid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"project spend cap reached"}}`)
	}))
	defer paid.Close()

	spendCapJob := insertJob("receipt", "application/pdf", "spend-cap-document", 9, 4)
	store["spend-cap-document"] = Document{MIMEType: "application/pdf", Data: []byte("test source")}
	worker := Worker{
		DB: db, Store: store, GeminiEnabled: true,
		Router: GeminiRouter{
			Free:       Gemini{APIKey: "test-free", Endpoint: free.URL + "/v1beta/", HTTPClient: free.Client()},
			Paid:       Gemini{APIKey: "test-paid", Endpoint: paid.URL + "/v1beta/", HTTPClient: paid.Client()},
			Background: true,
		},
	}
	for expectedAttempt := 10; expectedAttempt <= 11; expectedAttempt++ {
		processed, err := worker.RunOne(ctx, spendCapJob)
		if err != nil || !processed {
			t.Fatalf("spend-cap job was not processed: processed=%v error=%v", processed, err)
		}
		var status, errorCode string
		var attempt, failureCount int
		var availableInSeconds float64
		if err := db.QueryRow(ctx, `SELECT status,error_code,attempt,failure_count,extract(epoch FROM available_at-now()) FROM extraction_jobs WHERE id=$1`, spendCapJob).Scan(&status, &errorCode, &attempt, &failureCount, &availableInSeconds); err != nil {
			t.Fatal(err)
		}
		if status != "queued" || errorCode != "spend_cap_reached" || attempt != expectedAttempt || failureCount != 4 || availableInSeconds < 5*60*60 {
			t.Fatalf("spend cap must keep the source queued without consuming ordinary failures: status=%q error=%q attempt=%d failures=%d availableIn=%.0fs", status, errorCode, attempt, failureCount, availableInSeconds)
		}
		if expectedAttempt < 11 {
			if _, err := db.Exec(ctx, `UPDATE extraction_jobs SET available_at=now() WHERE id=$1`, spendCapJob); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := worker.RecoverExhausted(ctx); err != nil {
		t.Fatal(err)
	}
	var heldStatus string
	if err := db.QueryRow(ctx, `SELECT status FROM extraction_jobs WHERE id=$1`, spendCapJob).Scan(&heldStatus); err != nil || heldStatus != "queued" {
		t.Fatalf("exhaustion recovery must not fail a queued spend-cap job: status=%q error=%v", heldStatus, err)
	}

	temporary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"status":"UNAVAILABLE"}}`)
	}))
	defer temporary.Close()
	ordinaryFailureJob := insertJob("receipt", "application/pdf", "temporary-error-document", 0, 0)
	store["temporary-error-document"] = Document{MIMEType: "application/pdf", Data: []byte("test source")}
	ordinaryFailureWorker := Worker{DB: db, Store: store, GeminiEnabled: true, Router: GeminiRouter{
		Free: Gemini{APIKey: "test-free", Endpoint: temporary.URL + "/v1beta/", HTTPClient: temporary.Client()},
		Paid: Gemini{APIKey: "test-paid", Endpoint: temporary.URL + "/v1beta/", HTTPClient: temporary.Client()},
	}}
	for attempt := 1; attempt <= 5; attempt++ {
		processed, err := ordinaryFailureWorker.RunOne(ctx, ordinaryFailureJob)
		if err != nil || !processed {
			t.Fatalf("ordinary provider failure was not processed: processed=%v error=%v", processed, err)
		}
		var status string
		var gotAttempt, failureCount int
		if err := db.QueryRow(ctx, `SELECT status,attempt,failure_count FROM extraction_jobs WHERE id=$1`, ordinaryFailureJob).Scan(&status, &gotAttempt, &failureCount); err != nil {
			t.Fatal(err)
		}
		wantStatus := "queued"
		if attempt == 5 {
			wantStatus = "failed"
		}
		if status != wantStatus || gotAttempt != attempt || failureCount != attempt {
			t.Fatalf("ordinary failures must consume the separate retry budget: status=%q attempt=%d failures=%d want=%q/%d", status, gotAttempt, failureCount, wantStatus, attempt)
		}
		if attempt < 5 {
			if _, err := db.Exec(ctx, `UPDATE extraction_jobs SET available_at=now() WHERE id=$1`, ordinaryFailureJob); err != nil {
				t.Fatal(err)
			}
		}
	}

	pausedJob := insertJob("receipt", "application/pdf", "paused-document", 0, 0)
	store["paused-document"] = Document{MIMEType: "application/pdf", Data: []byte("test source")}
	disabledWorker := Worker{DB: db, Store: store}
	processed, err := disabledWorker.RunOne(ctx, pausedJob)
	if err != nil || processed {
		t.Fatalf("disabled worker must leave Gemini documents unclaimed: processed=%v error=%v", processed, err)
	}
	var pausedStatus string
	var pausedAttempt, pausedFailures int
	if err := db.QueryRow(ctx, `SELECT status,attempt,failure_count FROM extraction_jobs WHERE id=$1`, pausedJob).Scan(&pausedStatus, &pausedAttempt, &pausedFailures); err != nil {
		t.Fatal(err)
	}
	if pausedStatus != "queued" || pausedAttempt != 0 || pausedFailures != 0 {
		t.Fatalf("disabled job state changed: status=%q attempt=%d failures=%d", pausedStatus, pausedAttempt, pausedFailures)
	}

	csvJob := insertJob("card", "text/csv", "card-csv-document", 0, 0)
	store["card-csv-document"] = Document{MIMEType: "text/csv", Data: []byte("利用日,利用店名,利用金額,取引番号\n2026/10/12,店,100,tx-1\n")}
	processed, err = disabledWorker.RunOne(ctx, csvJob)
	if err != nil || !processed {
		t.Fatalf("disabled worker must process deterministic CSV imports: processed=%v error=%v", processed, err)
	}
	var csvStatus string
	var csvAttempt int
	var csvResult []byte
	if err := db.QueryRow(ctx, `SELECT status,attempt,result FROM extraction_jobs WHERE id=$1`, csvJob).Scan(&csvStatus, &csvAttempt, &csvResult); err != nil {
		t.Fatal(err)
	}
	if csvStatus != "succeeded" || csvAttempt != 1 || !bytes.Contains(csvResult, []byte(`"transactionId": "tx-1"`)) {
		t.Fatalf("CSV import did not complete deterministically while Gemini was disabled: status=%q attempt=%d result=%s", csvStatus, csvAttempt, csvResult)
	}
}

func testUUID(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV4()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}
