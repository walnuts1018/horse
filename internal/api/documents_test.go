package api

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/stephenafamo/scan"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const testDocumentStore = "Market"

func TestDocumentModelSelectionAcceptsSupportedModels(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value string
		want  string
		valid bool
	}{
		{value: "", want: ingestion.ModelSelectionFree, valid: true},
		{value: ingestion.ModelSelectionFree, want: ingestion.ModelSelectionFree, valid: true},
		{value: ingestion.ModelSelectionPro, want: ingestion.ModelSelectionPro, valid: true},
		{value: ingestion.ModelReceipt, want: ingestion.ModelReceipt, valid: true},
		{value: ingestion.ModelEmail, want: ingestion.ModelEmail, valid: true},
		{value: ingestion.ModelLite31, want: ingestion.ModelLite31, valid: true},
		{value: ingestion.ModelPro, want: ingestion.ModelPro, valid: true},
		{value: "gemini-unsupported", valid: false},
	} {
		got, ok := documentModelSelection(test.value)
		if got != test.want || ok != test.valid || validDocumentModelSelection(test.value) != test.valid {
			t.Errorf("documentModelSelection(%q) = (%q, %t), want (%q, %t)", test.value, got, ok, test.want, test.valid)
		}
	}
}

func TestRateLimitedExtractionCanBeRetriedWithAnotherModel(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"rate_limited", "quota_reservation_deferred"} {
		if !isRateLimitedExtractionError(sql.Null[string]{V: code, Valid: true}) {
			t.Errorf("%q was not recognized as rate limited", code)
		}
	}
	for _, code := range []sql.Null[string]{{}, {V: "provider_unavailable", Valid: true}} {
		if isRateLimitedExtractionError(code) {
			t.Errorf("%#v was incorrectly recognized as rate limited", code)
		}
	}

	request := stdjson.RawMessage(`{"purpose":"interactive","model":"free","crop":{"page":2}}`)
	updated, err := replaceExtractionJobModel(request, ingestion.ModelPro)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Purpose string           `json:"purpose"`
		Model   string           `json:"model"`
		Crop    ingestion.Region `json:"crop"`
	}
	if err := stdjson.Unmarshal(updated, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Purpose != "interactive" || decoded.Model != ingestion.ModelPro || decoded.Crop.Page != 2 {
		t.Fatalf("retry request was not preserved while replacing its model: %#v", decoded)
	}
}

func TestDocumentCursorIsBoundToFilters(t *testing.T) {
	t.Parallel()

	id := gofrsuuid.Must(gofrsuuid.NewV7())
	createdAt := time.Date(2026, time.January, 2, 3, 4, 5, 6, time.UTC)
	confirmed := false
	filters := documentFilters{store: testDocumentStore, sourceKind: testReceiptSourceKind, confirmed: &confirmed}
	encoded := encodeDocumentCursor(documentCursor{id: id, createdAt: createdAt}, filters)
	decoded, err := decodeDocumentCursor(encoded, filters)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.id != id || !decoded.createdAt.Equal(createdAt) {
		t.Fatalf("decoded cursor differs: %#v", decoded)
	}
	filters.store = "Other"
	if _, err := decodeDocumentCursor(encoded, filters); err == nil {
		t.Fatal("cursor accepted a different filter scope")
	}
	filters.store = testDocumentStore
	filters.householdID = gofrsuuid.Must(gofrsuuid.NewV7()).String()
	if _, err := decodeDocumentCursor(encoded, filters); err == nil {
		t.Fatal("cursor accepted a different household")
	}
}

func TestParseDocumentFilters(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/documents?store=Market&from=2026-10-01&to=2026-10-03&sourceKind=receipt&confirmed=false", nil)
	filters, err := parseDocumentFilters(request)
	if err != nil {
		t.Fatal(err)
	}
	if filters.store != testDocumentStore || filters.sourceKind != testReceiptSourceKind || filters.confirmed == nil || *filters.confirmed {
		t.Fatalf("unexpected filters: %#v", filters)
	}
	if !filters.from.Equal(time.Date(2026, time.October, 1, 0, 0, 0, 0, time.FixedZone("JST", 9*60*60))) {
		t.Fatalf("unexpected from date: %s", filters.from)
	}
	if !filters.to.Equal(time.Date(2026, time.October, 4, 0, 0, 0, 0, time.FixedZone("JST", 9*60*60))) {
		t.Fatalf("unexpected exclusive to date: %s", filters.to)
	}
	for _, query := range []string{
		"?sourceKind=unknown",
		"?from=2026-10-03&to=2026-10-01",
		"?confirmed=yes",
	} {
		if _, err := parseDocumentFilters(httptest.NewRequest(http.MethodGet, "/documents"+query, nil)); err == nil {
			t.Errorf("accepted invalid filters %s", query)
		}
	}
}

func TestDocumentMIMEUsesContentBeforeTextExtensionFallback(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		filename string
		data     []byte
		want     string
	}{
		{name: "pdf content overrides extension", filename: "receipt.txt", data: []byte("%PDF-1.7\n"), want: documentMIMEPDF},
		{name: "csv text uses extension fallback", filename: "receipt.CSV", data: []byte("date,amount\n2026-10-14,1200\n"), want: "text/csv"},
		{name: "email text uses extension fallback", filename: "receipt.eml", data: []byte("From: shop@example.test\nSubject: receipt\n\nTotal: 1200\n"), want: "message/rfc822"},
		{name: "plain text uses extension fallback", filename: "receipt.txt", data: []byte("receipt total 1200"), want: "text/plain"},
		{name: "unsupported detected content is rejected", filename: "receipt.jpg", data: []byte("GIF89a"), want: ""},
		{name: "unknown binary is not accepted by extension", filename: "receipt.csv", data: []byte{0x00, 0x01, 0x02, 0x03}, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := documentMIME(test.filename, test.data); got != test.want {
				t.Fatalf("documentMIME(%q, %q) = %q, want %q", test.filename, test.data, got, test.want)
			}
		})
	}
}

func TestValidateAnalysisTransform(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		transform [9]float64
		valid     bool
	}{
		{name: "identity", transform: [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}, valid: true},
		{name: "clockwise rotation", transform: [9]float64{0, 1, 0, -1, 0, 1, 0, 0, 1}, valid: true},
		{name: "crop", transform: [9]float64{0.5, 0, 0.25, 0, 0.5, 0.25, 0, 0, 1}, valid: true},
		{name: "singular", transform: [9]float64{0, 0, 0, 0, 1, 0, 0, 0, 1}},
		{name: "outside original", transform: [9]float64{1, 0, 0.1, 0, 1, 0, 0, 0, 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateAnalysisTransform(test.transform)
			if (err == nil) != test.valid {
				t.Fatalf("validateAnalysisTransform(%v) error = %v, valid=%t", test.transform, err, test.valid)
			}
		})
	}
}

func TestDocumentResponsesUseConstantQueryCount(t *testing.T) {
	t.Parallel()

	databaseURL := os.Getenv("HORSE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("HORSE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(t.Context()); err != nil {
			t.Errorf("rollback test transaction: %v", err)
		}
	}()
	executor := bobpgx.NewTx(tx, nil)
	householdID, memberID := gofrsuuid.Must(gofrsuuid.NewV7()), gofrsuuid.Must(gofrsuuid.NewV7())
	householdName, subject := "document response batch", "document-response-batch:"+memberID.String()
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &subject}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &householdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}

	documents := make([]*models.SourceDocument, 50)
	for i := range documents {
		documentID := gofrsuuid.Must(gofrsuuid.NewV7())
		digest, objectKey, mimeType, sourceKind := "document-response:"+documentID.String(), "document-response/"+documentID.String(), documentMIMEJPEG, "receipt"
		createdAt := time.Date(2026, time.January, 1, 0, i, 0, 0, time.UTC)
		stored, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
			ID: &documentID, HouseholdID: &householdID, Sha256: &digest, ObjectKey: &objectKey,
			MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberID, CreatedAt: &createdAt,
		}).One(ctx, executor)
		if err != nil {
			t.Fatal(err)
		}
		documents[i] = stored
	}

	for _, count := range []int{1, len(documents)} {
		countingExecutor := &queryCountingExecutor{Executor: executor}
		responses, err := (&Server{}).documentResponses(ctx, countingExecutor, documents[:count])
		if err != nil {
			t.Fatal(err)
		}
		if len(responses) != count {
			t.Fatalf("got %d document responses, want %d", len(responses), count)
		}
		if countingExecutor.queries != 5 {
			t.Fatalf("document response query count for %d documents = %d, want 5", count, countingExecutor.queries)
		}
		for i, response := range responses {
			if response.ID != documents[i].ID.String() || response.Status != documentStatusQueued || response.Confirmations == nil {
				t.Fatalf("unexpected response at index %d: %#v", i, response)
			}
		}
	}
}

func TestDocumentMerchantFilterMatchesLatestMerchantOnly(t *testing.T) {
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
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()
	executor := bobpgx.NewTx(tx, nil)
	householdID, memberID := gofrsuuid.Must(gofrsuuid.NewV7()), gofrsuuid.Must(gofrsuuid.NewV7())
	subject := "document-merchant-filter:" + memberID.String()
	displayName, householdName := subject, "document merchant filter"
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &displayName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &householdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	type documentFixture struct {
		name      string
		results   []string
		wantMatch bool
	}
	fixtures := []documentFixture{
		{name: "kind is not merchant", results: []string{`{"statements":[{"merchant":"A Store","kind":"EXTERNAL_INCOME"}]}`}},
		{name: "merchant matches", results: []string{`{"statements":[{"merchant":"Income Market","kind":"PAYMENT"}]}`}, wantMatch: true},
		{name: "latest result wins", results: []string{`{"statements":[{"merchant":"Income Market","kind":"PAYMENT"}]}`, `{"statements":[{"merchant":"A Store","kind":"INCOME"}]}`}},
	}
	want := make(map[string]bool, len(fixtures))
	for _, fixture := range fixtures {
		documentID := gofrsuuid.Must(gofrsuuid.NewV7())
		digest, objectKey, mimeType, sourceKind := "merchant-filter:"+documentID.String(), "merchant-filter/"+documentID.String(), documentMIMEPDF, "bank"
		if _, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
			ID: &documentID, HouseholdID: &householdID, Sha256: &digest, ObjectKey: &objectKey,
			MimeType: &mimeType, SourceKind: &sourceKind, CreatedBy: &memberID,
		}).Exec(ctx, executor); err != nil {
			t.Fatal(err)
		}
		for index, result := range fixture.results {
			jobID := gofrsuuid.Must(gofrsuuid.NewV7())
			version, status := int32(index+1), "succeeded"
			request := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(`{}`)}
			jobResult := sql.Null[types.JSON[stdjson.RawMessage]]{
				V: types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(result)}, Valid: true,
			}
			if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
				ID: &jobID, HouseholdID: &householdID, SourceDocumentID: &documentID, Version: &version,
				Status: &status, Request: &request, Result: &jobResult,
			}).Exec(ctx, executor); err != nil {
				t.Fatal(err)
			}
		}
		want[documentID.String()] = fixture.wantMatch
	}
	merchantFilter, err := documentMerchantExists(householdID, "income")
	if err != nil {
		t.Fatal(err)
	}
	documents, err := models.SourceDocuments.Query(
		sm.Where(models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(householdID))),
		merchantFilter,
	).All(ctx, executor)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool, len(documents))
	for _, document := range documents {
		got[document.ID.String()] = true
	}
	for documentID, wantMatch := range want {
		if got[documentID] != wantMatch {
			t.Fatalf("document %s match = %t, want %t", documentID, got[documentID], wantMatch)
		}
	}
}

type queryCountingExecutor struct {
	bob.Executor
	queries int
}

func (e *queryCountingExecutor) QueryContext(ctx context.Context, query string, args ...any) (scan.Rows, error) {
	e.queries++
	return e.Executor.QueryContext(ctx, query, args...)
}
