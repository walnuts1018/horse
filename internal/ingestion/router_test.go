package ingestion

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGeminiRouterUsesFreeThenPaidStandardOnInteractiveQuota(t *testing.T) {
	freeCalls := 0
	freeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		freeCalls++
		if r.Header.Get("x-goog-api-key") != "free-test-key" {
			t.Errorf("free route used unexpected API key")
		}
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"rate limit","details":[{"retryDelay":"3s"}]}}`)
	}))
	defer freeServer.Close()
	var paidCalls int
	var paidBody map[string]any
	paidServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paidCalls++
		if r.Header.Get("x-goog-api-key") != "paid-test-key" {
			t.Errorf("paid route used unexpected API key")
		}
		if err := json.Unmarshal(mustReadBody(t, r), &paidBody); err != nil {
			t.Errorf("decode paid request: %v", err)
		}
		writeGeminiSuccess(w, `{"merchant":"Market","purchasedAt":"","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
	}))
	defer paidServer.Close()
	router := GeminiRouter{
		Free:       Gemini{APIKey: "free-test-key", Project: "free-id", Endpoint: freeServer.URL + "/v1beta/", HTTPClient: freeServer.Client()},
		Paid:       Gemini{APIKey: "paid-test-key", Project: "paid-id", Endpoint: paidServer.URL + "/v1beta/", HTTPClient: paidServer.Client()},
		USDJPYRate: 158.535,
	}
	receipt, runs, err := router.Generate(context.Background(), "receipt", Document{MIMEType: "image/jpeg", Data: []byte("not a real image; test transport only")}, nil, GeminiRouteOptions{Purpose: JobPurposeInteractive, Reason: "initial_extraction"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Model != ModelReceipt || freeCalls != 1 || paidCalls != 1 || len(runs) != 2 {
		t.Fatalf("unexpected route result: model=%q free=%d paid=%d runs=%#v", receipt.Model, freeCalls, paidCalls, runs)
	}
	if runs[0].Route != ProjectFree || runs[0].ErrorCode != "rate_limited" || runs[1].Route != ProjectPaid || runs[1].Status != "succeeded" {
		t.Fatalf("run routing metadata is incomplete: %#v", runs)
	}
	if runs[1].InputTokens != 1000 || runs[1].OutputTokens != 275 || runs[1].EstimatedJPY <= 0 || runs[1].LatencyMillis < 0 {
		t.Fatalf("run usage metadata is incomplete: %#v", runs[1])
	}
	if runs[0].PromptVersion != PromptVersion || runs[0].PriceTableVersion != GeminiPriceTableVersion || runs[0].PriceEstimateStatus != "unavailable_failed_run" || runs[1].PromptVersion != PromptVersion || runs[1].PriceTableVersion != GeminiPriceTableVersion || runs[1].PriceEstimateStatus != "estimated_paid" || runs[1].USDJPYRate != 158.535 || runs[1].USDJPYRateSource != "unspecified" || runs[1].PricedAt.IsZero() {
		t.Fatalf("run audit metadata is incomplete: %#v", runs)
	}
	if paidBody["service_tier"] != "standard" {
		t.Fatalf("interactive paid fallback must use Standard: request=%#v", paidBody)
	}
	generationConfig, ok := paidBody["generationConfig"].(map[string]any)
	if !ok || generationConfig["serviceTier"] != nil || generationConfig["maxOutputTokens"] != float64(MaxOutputTokens) {
		t.Fatalf("REST service tier or output token configuration is invalid: %#v", paidBody)
	}
	thinkingConfig, ok := generationConfig["thinkingConfig"].(map[string]any)
	if !ok || thinkingConfig["thinkingLevel"] != "low" {
		t.Fatalf("initial image request must use low thinking: %#v", generationConfig)
	}
}

func TestGeminiServiceTierFollowsJobPurpose(t *testing.T) {
	for _, test := range []struct {
		purpose string
		want    string
	}{
		{purpose: JobPurposeInteractive, want: "standard"},
		{purpose: JobPurposeBackground, want: "flex"},
		{want: "standard"},
	} {
		got, err := defaultServiceTier(test.purpose)
		if err != nil || got != test.want {
			t.Fatalf("defaultServiceTier(%q) = %q, %v; want %q", test.purpose, got, err, test.want)
		}
	}
}

func TestGeminiRouterSelectsEmailLiteAndOnlyRetriesTemporaryErrors(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.Contains(r.URL.Path, ModelEmail+":generateContent") {
			t.Errorf("email route used wrong model path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"status":"UNAVAILABLE"}}`)
	}))
	defer server.Close()
	paidCalls := 0
	paid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		paidCalls++
		writeGeminiSuccess(w, `{"merchant":"Shop","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
	}))
	defer paid.Close()
	router := GeminiRouter{
		Free: Gemini{APIKey: "free", Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()},
		Paid: Gemini{APIKey: "paid", Endpoint: paid.URL + "/v1beta/", HTTPClient: paid.Client()},
	}
	_, runs, err := router.Generate(context.Background(), "email", Document{MIMEType: "message/rfc822", Data: []byte("example")}, nil, GeminiRouteOptions{Purpose: JobPurposeInteractive})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || paidCalls != 1 || len(runs) != 2 || runs[1].ServiceTier != "standard" {
		t.Fatalf("temporary free failure should use same-model paid Standard: free=%d paid=%d runs=%#v", calls, paidCalls, runs)
	}
	if _, _, err := router.Generate(context.Background(), "email", Document{MIMEType: "message/rfc822", Data: []byte("example")}, nil, GeminiRouteOptions{Model: "bad/model"}); err == nil || calls != 1 || paidCalls != 1 {
		t.Fatalf("invalid request should not call providers: free=%d paid=%d error=%v", calls, paidCalls, err)
	}
}

func TestGeminiSpendCapAndMalformedOutputAreTypedAndSanitized(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantKind  GeminiFailureKind
		wantCode  string
		wantSpend bool
	}{
		{name: "spend cap", status: http.StatusTooManyRequests, body: `{"error":{"status":"RESOURCE_EXHAUSTED","message":"project spend cap reached"}}`, wantKind: GeminiFailureSpendCap, wantCode: "spend_cap_reached", wantSpend: true},
		{name: "monthly spending cap", status: http.StatusTooManyRequests, body: `{"error":{"status":"RESOURCE_EXHAUSTED","message":"Your billing account has exceeded its monthly spending cap."}}`, wantKind: GeminiFailureSpendCap, wantCode: "spend_cap_reached", wantSpend: true},
		{name: "structured spend cap reason", status: http.StatusForbidden, body: `{"error":{"status":"PERMISSION_DENIED","details":[{"reason":"SPEND_CAP_ENFORCED"}]}}`, wantKind: GeminiFailureSpendCap, wantCode: "spend_cap_reached", wantSpend: true},
		{name: "malformed provider JSON", status: http.StatusOK, body: `not json`, wantKind: GeminiFailureBadResponse, wantCode: "malformed_provider_json"},
		{name: "broken receipt JSON", status: http.StatusOK, body: `{"candidates":[{"content":{"parts":[{"text":"{broken"}]},"finishReason":"STOP"}]}`, wantKind: GeminiFailureBadResponse, wantCode: "invalid_receipt_json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			router := GeminiRouter{Paid: Gemini{APIKey: "paid", Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()}}
			_, runs, err := router.Generate(context.Background(), "receipt", Document{MIMEType: "application/pdf", Data: []byte("test")}, nil, GeminiRouteOptions{PaidOnly: true})
			if err == nil || len(runs) != 1 {
				t.Fatalf("expected one failed run, got runs=%#v error=%v", runs, err)
			}
			apiErr, ok := errors.AsType[*GeminiAPIError](err)
			if !ok || apiErr.Kind != tt.wantKind || apiErr.Code != tt.wantCode || errors.Is(err, ErrGeminiSpendCap) != tt.wantSpend {
				t.Fatalf("unexpected sanitized error: %#v (%v)", apiErr, err)
			}
			if runs[0].Status != "failed" || runs[0].ErrorCode != tt.wantCode {
				t.Fatalf("failed run metadata missing: %#v", runs[0])
			}
		})
	}
}

func TestSpendCapRetryUsesBillingMonthAndAllowsResetPropagation(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "UTC month changes before billing month",
			now:  time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC),
			want: time.Date(2026, time.November, 1, 9, 0, 0, 0, time.UTC),
		},
		{
			name: "cap remains enforced just after reset",
			now:  time.Date(2026, time.November, 1, 9, 0, 0, 0, time.UTC),
			want: time.Date(2026, time.November, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			name: "December cap resumes in January",
			now:  time.Date(2026, time.December, 31, 23, 0, 0, 0, time.UTC),
			want: time.Date(2027, time.January, 1, 9, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.now.Add(spendCapRetryDelay(tt.now)); !got.Equal(tt.want) {
				t.Fatalf("retry at %s, want %s", got, tt.want)
			}
		})
	}
}

func TestGeminiQualityReviewAddsEvidenceAndChangesRequest(t *testing.T) {
	var mu sync.Mutex
	var prompts []string
	var levels []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Contents []struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
			GenerationConfig struct {
				ThinkingConfig struct {
					Level string `json:"thinkingLevel"`
				} `json:"thinkingConfig"`
			} `json:"generationConfig"`
		}
		body := mustReadBody(t, r)
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		prompts = append(prompts, request.Contents[0].Parts[0].Text)
		levels = append(levels, request.GenerationConfig.ThinkingConfig.Level)
		mu.Unlock()
		writeGeminiSuccess(w, `{"merchant":"Shop","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
	}))
	defer server.Close()
	router := GeminiRouter{Free: Gemini{APIKey: "free", Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()}}
	document := Document{MIMEType: "image/png", Data: []byte("test")}
	first, runs, err := router.Generate(context.Background(), "receipt", document, nil, GeminiRouteOptions{Reason: "initial_extraction"})
	if err != nil || len(runs) != 1 {
		t.Fatalf("initial generation failed: %#v error=%v", runs, err)
	}
	_, _, err = router.Generate(context.Background(), "receipt", document, nil, router.QualityOptions("receipt", runs[0], evidenceGuidance([]string{"税率別小計が一致しません。"}, []OCRLine{{Text: "TOTAL 980", Region: Region{Page: 0, Top: 0.1, Left: 0.1, Bottom: 0.2, Right: 0.9}}})))
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 2 || prompts[0] == prompts[1] || levels[0] != "low" || levels[1] != "medium" {
		t.Fatalf("quality review must use evidence and medium thinking: prompts=%#v levels=%#v first=%#v", prompts, levels, first)
	}
	if !strings.Contains(prompts[1], "税率別小計が一致しません") || !strings.Contains(prompts[1], "TOTAL 980") {
		t.Fatalf("quality review omitted evidence: %q", prompts[1])
	}
}

func TestGeminiRouterModelOverridesAndEmailQualityUpgrade(t *testing.T) {
	router := GeminiRouter{FlashModel: "configured-flash", LiteModel: "configured-lite"}
	if got := router.modelForSource("receipt", "image/jpeg"); got != "configured-flash" {
		t.Fatalf("configured flash model was ignored: %q", got)
	}
	if got := router.modelForSource("email", "message/rfc822"); got != "configured-lite" {
		t.Fatalf("configured lite model was ignored: %q", got)
	}
	quality := router.QualityOptions("email", ExtractionRun{Model: "configured-lite", Route: ProjectFree}, []string{"transaction total conflicts with printed subtotal"})
	if quality.Model != "configured-flash" || quality.ThinkingLevel != "medium" || quality.Reason != "evidence_guided_quality_review" || len(quality.Guidance) != 1 {
		t.Fatalf("email quality review must use a more capable model with evidence: %#v", quality)
	}
	if !supportsProReview("image/jpeg") || !supportsProReview("application/pdf") || supportsProReview("message/rfc822") {
		t.Fatal("Pro review should target scanned purchase documents and skip email")
	}
}

func TestEstimatedGeminiCostUsesProjectTierAndPriceChange(t *testing.T) {
	cutover := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	before := estimateCostJPY(ProjectPaid, ModelReceipt, "standard", 1_000_000, 1_000_000, 100, cutover.Add(-time.Second))
	flex := estimateCostJPY(ProjectPaid, ModelReceipt, "flex", 1_000_000, 1_000_000, 100, cutover.Add(-time.Second))
	after := estimateCostJPY(ProjectPaid, ModelReceipt, "standard", 1_000_000, 1_000_000, 100, cutover)
	proUnderLimit := estimateCostJPY(ProjectPaid, ModelPro, "standard", 200_000, 1_000_000, 100, cutover)
	proOverLimit := estimateCostJPY(ProjectPaid, ModelPro, "standard", 200_001, 1_000_000, 100, cutover)
	if before != 450 || flex != 225 || after != 900 || proUnderLimit != 1240 || proOverLimit != 1880.0004 || estimateCostJPY(ProjectFree, ModelReceipt, "standard", 1_000_000, 1_000_000, 100, cutover) != 0 {
		t.Fatalf("unexpected estimated prices: before=%f flex=%f after=%f", before, flex, after)
	}
}

func TestGeminiRunDoesNotLabelUnknownModelPriceAsFree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeGeminiSuccess(w, `{"merchant":"Market","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
	}))
	defer server.Close()
	router := GeminiRouter{
		Paid:       Gemini{APIKey: "paid-test-key", Project: "paid-id", Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()},
		USDJPYRate: 150,
	}
	_, runs, err := router.Generate(context.Background(), "receipt", Document{MIMEType: "image/jpeg", Data: []byte("test input")}, nil, GeminiRouteOptions{Model: "future-model", PaidOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].PriceEstimateStatus != "unknown_model_price" || runs[0].EstimatedJPY != 0 || runs[0].PricedAt.IsZero() {
		t.Fatalf("unknown model was reported as a zero-cost run: %#v", runs)
	}
}

func TestGeminiRunKeepsPriceSnapshotWhenExchangeRateIsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeGeminiSuccess(w, `{"merchant":"Market","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
	}))
	defer server.Close()
	router := GeminiRouter{
		Paid: Gemini{APIKey: "paid-test-key", Project: "paid-id", Endpoint: server.URL + "/v1beta/"},
	}
	_, runs, err := router.Generate(context.Background(), "receipt", Document{MIMEType: "image/jpeg", Data: []byte("test input")}, nil, GeminiRouteOptions{PaidOnly: true, ServiceTier: "flex"})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].PriceEstimateStatus != "missing_exchange_rate" || runs[0].InputUSDPerMillion != 0.375 || runs[0].OutputUSDPerMillion != 1.875 || runs[0].EstimatedJPY != 0 {
		t.Fatalf("run should preserve model and tier price snapshot without inventing a converted estimate: %#v", runs)
	}
}

func mustReadBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("read request body: %v", err)
	}
	return body
}

func writeGeminiSuccess(w http.ResponseWriter, receipt string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":`+mustJSONText(receipt)+`}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1000,"candidatesTokenCount":250,"thoughtsTokenCount":25}}`)
}

func mustJSONText(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
