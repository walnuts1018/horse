package ingestion

import (
	"encoding/base64"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiExtractPreservesExactQuantityAndPrintedUnitPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1beta/models/test-model:generateContent" {
			t.Errorf("unexpected SDK request target: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Errorf("SDK request did not use the configured API key")
		}
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read generation request: %v", err)
		}
		var request struct {
			ServiceTier string `json:"serviceTier"`
			Contents    []struct {
				Parts []struct {
					InlineData struct {
						Data     string `json:"data"`
						MIMEType string `json:"mimeType"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"contents"`
			GenerationConfig struct {
				ResponseMIMEType string `json:"responseMimeType"`
				MaxOutputTokens  int    `json:"maxOutputTokens"`
				ThinkingConfig   struct {
					ThinkingLevel string `json:"thinkingLevel"`
				} `json:"thinkingConfig"`
			} `json:"generationConfig"`
		}
		if err := json.Unmarshal(requestBody, &request); err != nil {
			t.Errorf("decode SDK request: %v", err)
		}
		if request.ServiceTier != "standard" || request.GenerationConfig.ResponseMIMEType != "application/json" || request.GenerationConfig.MaxOutputTokens != MaxOutputTokens || request.GenerationConfig.ThinkingConfig.ThinkingLevel != "LOW" {
			t.Errorf("SDK request omitted generation settings: %#v", request)
		}
		if len(request.Contents) != 1 || len(request.Contents[0].Parts) != 2 || request.Contents[0].Parts[1].InlineData.MIMEType != "image/jpeg" || request.Contents[0].Parts[1].InlineData.Data != base64.StdEncoding.EncodeToString([]byte("synthetic image bytes")) {
			t.Errorf("SDK request changed inline image content: %#v", request.Contents)
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
