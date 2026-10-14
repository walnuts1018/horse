package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/scan"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const testDocumentStore = "Market"

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

func TestDocumentResponsesUseConstantQueryCount(t *testing.T) {
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
	executor := bobpgx.NewPool(db)
	householdID, memberID := gofrsuuid.Must(gofrsuuid.NewV7()), gofrsuuid.Must(gofrsuuid.NewV7())
	householdName, subject := "document response batch", "document-response-batch:"+memberID.String()
	if _, err := models.Members.Insert(&models.MemberSetter{ID: &memberID, OidcSubject: &subject, DisplayName: &subject}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Members.Delete(dm.Where(models.Members.Columns.ID.EQ(psql.Arg(memberID)))).Exec(t.Context(), executor)
	}()
	if _, err := models.Households.Insert(&models.HouseholdSetter{ID: &householdID, Name: &householdName}).Exec(ctx, executor); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = models.Households.Delete(dm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exec(t.Context(), executor)
	}()

	documents := make([]*models.SourceDocument, 50)
	for i := range documents {
		documentID := gofrsuuid.Must(gofrsuuid.NewV7())
		digest, objectKey, mimeType, sourceKind := "document-response:"+documentID.String(), "document-response/"+documentID.String(), "image/jpeg", "receipt"
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

type queryCountingExecutor struct {
	bob.Executor
	queries int
}

func (e *queryCountingExecutor) QueryContext(ctx context.Context, query string, args ...any) (scan.Rows, error) {
	e.queries++
	return e.Executor.QueryContext(ctx, query, args...)
}
