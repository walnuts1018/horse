package query

import (
	"slices"
	"testing"
)

func TestCompareProductSuggestionsUsesPurchaseStatsOnlyAfterTextScore(t *testing.T) {
	items := []rankedProductSuggestion{
		{score: 95, purchaseCount: 0, conceptName: "a", variantName: "new"},
		{score: 90, purchaseCount: 100, sameMerchant: true, conceptName: "b", variantName: "popular"},
		{score: 95, purchaseCount: 1, conceptName: "c", variantName: "less popular"},
		{score: 95, purchaseCount: 1, sameMerchant: true, conceptName: "d", variantName: "same store"},
		{score: 95, purchaseCount: 1, lastPurchasedAt: 2, conceptName: "e", variantName: "recent"},
		{score: 95, purchaseCount: 1, lastPurchasedAt: 1, conceptName: "f", variantName: "older"},
	}

	slices.SortFunc(items, compareProductSuggestions)

	want := []string{"same store", "recent", "older", "less popular", "new", "popular"}
	for index, item := range items {
		if item.variantName != want[index] {
			t.Fatalf("rank %d = %q, want %q", index, item.variantName, want[index])
		}
	}
}

func TestNormalizeSuggestionMerchant(t *testing.T) {
	if got, want := normalizeSuggestionMerchant("  Mart  East\t "), "mart east"; got != want {
		t.Fatalf("normalized merchant = %q, want %q", got, want)
	}
}
