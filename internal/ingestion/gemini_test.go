package ingestion

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiExtractPreservesExactQuantityAndPrintedUnitPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read generation request: %v", err)
		}
		for _, required := range []string{"quantityNumerator", "printedUnitPriceQuantityDenominator", "Never derive printed unit price from line total"} {
			if !strings.Contains(string(requestBody), required) {
				t.Errorf("generation request is missing quantity extraction guidance %q", required)
			}
		}
		writeGeminiSuccess(w, `{"merchant":"Market","purchasedAt":"2026-10-12T12:00:00+09:00","currency":"JPY","items":[{"printedName":"Flour","name":"Flour","printedAmount":240,"discount":0,"priceBasis":"TAX_INCLUDED","taxGroup":"","quantityNumerator":3,"quantityDenominator":2,"quantityUnit":"kg","printedUnitPriceYen":130,"printedUnitPriceQuantityNumerator":1,"printedUnitPriceQuantityDenominator":1,"printedUnitPriceUnit":"kg","confidence":0.99}],"taxGroups":[],"discount":0,"total":240,"payments":[],"warnings":[]}`)
	}))
	defer server.Close()

	gemini := Gemini{APIKey: "test-key", Model: "test-model", Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()}
	receipt, _, err := gemini.ExtractRun(t.Context(), Document{MIMEType: "image/jpeg", Data: []byte("synthetic image bytes")}, nil, GeminiRequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	item := receipt.Items[0]
	if item.QuantityNumerator == nil || *item.QuantityNumerator != 3 || item.QuantityDenominator == nil || *item.QuantityDenominator != 2 || item.QuantityUnit != "kg" ||
		item.PrintedUnitPriceYen == nil || *item.PrintedUnitPriceYen != 130 || item.PrintedUnitPriceUnit != "kg" {
		t.Fatalf("receipt extraction lost rational quantity or printed unit price: %#v", item)
	}
}
