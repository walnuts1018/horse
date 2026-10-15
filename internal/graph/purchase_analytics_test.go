package graph

import (
	"testing"
	"uuid"
)

func TestAnalyticsItemIDRoundTrip(t *testing.T) {
	variantID := uuid.NewV7().String()
	for _, rawID := range []string{variantID, "牛乳"} {
		databaseID := rawID + "\x1f個"
		graphqlID, err := encodeAnalyticsID("item", databaseID)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeAnalyticsItemID(graphqlID)
		if err != nil {
			t.Fatal(err)
		}
		if got != databaseID {
			t.Fatalf("item ID round trip = %q, want %q", got, databaseID)
		}
	}
}
