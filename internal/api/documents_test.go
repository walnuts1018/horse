package api

import (
	"net/http/httptest"
	"testing"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
)

const testDocumentStore = "Market"

func TestDocumentCursorIsBoundToFilters(t *testing.T) {
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
	request := httptest.NewRequest("GET", "/documents?store=Market&from=2026-10-01&to=2026-10-03&sourceKind=receipt&confirmed=false", nil)
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
		if _, err := parseDocumentFilters(httptest.NewRequest("GET", "/documents"+query, nil)); err == nil {
			t.Errorf("accepted invalid filters %s", query)
		}
	}
}
