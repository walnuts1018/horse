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

const (
	testFreeGeminiAPIKey = "free-test-key"
	testRateLimitedCode  = "rate_limited"
)

func TestGeminiRouterRetriesQuotaOnFreeRouteBeforeUsingPaid(t *testing.T) {
	t.Parallel()
	freeCalls := 0
	freeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		freeCalls++
		if !strings.Contains(r.URL.Path, ModelReceipt+":generateContent") {
			t.Errorf("receipt quota fallback must keep the receipt model: %s", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != testFreeGeminiAPIKey {
			t.Errorf("free route used unexpected API key")
		}
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"rate limit","details":[{"retryDelay":"3s"}]}}`)
	}))
	defer freeServer.Close()
	var paidCalls int
	paidServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paidCalls++
		if r.Header.Get("x-goog-api-key") != testPaidGeminiAPIKey {
			t.Errorf("paid route used unexpected API key")
		}
		writeGeminiSuccess(w, `{"merchant":"Market","purchasedAt":"","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
	}))
	defer paidServer.Close()
	router := GeminiRouter{
		Free:       Gemini{APIKey: testFreeGeminiAPIKey, Project: "free-id", Endpoint: freeServer.URL + "/v1beta/", HTTPClient: freeServer.Client()},
		Paid:       Gemini{APIKey: testPaidGeminiAPIKey, Project: testPaidProjectID, Endpoint: paidServer.URL + "/v1beta/", HTTPClient: paidServer.Client()},
		USDJPYRate: 158.535,
	}
	_, runs, err := router.Generate(t.Context(), "receipt", Document{MIMEType: mimeTypeJPEG, Data: []byte("not a real image; test transport only")}, nil, GeminiRouteOptions{Purpose: JobPurposeInteractive, Reason: geminiReasonInitialExtraction})
	apiErr, ok := errors.AsType[*GeminiAPIError](err)
	if !ok || apiErr.Kind != GeminiFailureQuota || apiErr.RetryAfter != 3*time.Second {
		t.Fatalf("quota response must preserve its retry delay: error=%v", err)
	}
	if freeCalls != 1 || paidCalls != 0 || len(runs) != 1 {
		t.Fatalf("quota response must not immediately use paid route: free=%d paid=%d runs=%#v", freeCalls, paidCalls, runs)
	}
	if runs[0].Route != ProjectFree || runs[0].ErrorCode != testRateLimitedCode || runs[0].Status != extractionStatusFailed {
		t.Fatalf("run routing metadata is incomplete: %#v", runs)
	}
	if runs[0].PromptVersion != PromptVersion || runs[0].PriceTableVersion != GeminiPriceTableVersion || runs[0].PriceEstimateStatus != "unavailable_failed_run" {
		t.Fatalf("failed free run audit metadata is incomplete: %#v", runs[0])
	}
	if got := retryDelay(err, 1); got != 3*time.Second {
		t.Fatalf("worker retry delay = %s, want Retry-After 3s", got)
	}
}

func TestGeminiRouterSelectedOptionsUseRequestedModelRoute(t *testing.T) {
	t.Parallel()
	router := GeminiRouter{FlashModel: "configured-flash", LiteModel: "configured-lite", ProModel: "configured-pro"}
	tests := []struct {
		name       string
		selection  string
		sourceKind string
		mimeType   string
		wantModel  string
		freeOnly   bool
		paidOnly   bool
	}{
		{name: "free receipt model", selection: ModelSelectionFree, sourceKind: "receipt", mimeType: mimeTypeJPEG, wantModel: "configured-flash", freeOnly: true},
		{name: "free email model", selection: ModelSelectionFree, sourceKind: "email", mimeType: mimeTypeEmail, wantModel: "configured-lite", freeOnly: true},
		{name: "paid pro model", selection: ModelSelectionPro, sourceKind: "receipt", mimeType: mimeTypeJPEG, wantModel: "configured-pro", paidOnly: true},
		{name: "exact receipt model", selection: ModelReceipt, sourceKind: "email", mimeType: mimeTypeEmail, wantModel: ModelReceipt, freeOnly: true},
		{name: "exact 3.5 flash lite model", selection: ModelEmail, sourceKind: "receipt", mimeType: mimeTypeJPEG, wantModel: ModelEmail, freeOnly: true},
		{name: "exact 3.1 flash lite model", selection: ModelLite31, sourceKind: "receipt", mimeType: mimeTypeJPEG, wantModel: ModelLite31, freeOnly: true},
		{name: "exact pro model", selection: ModelPro, sourceKind: "receipt", mimeType: mimeTypeJPEG, wantModel: ModelPro, paidOnly: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options, err := router.selectedOptions(test.selection, test.sourceKind, test.mimeType, JobPurposeInteractive, 3)
			if err != nil {
				t.Fatal(err)
			}
			if options.Model != test.wantModel || !options.ExactModel || options.FreeOnly != test.freeOnly || options.PaidOnly != test.paidOnly || options.Attempt != 3 || options.Reason != geminiReasonInitialExtraction {
				t.Fatalf("selected extraction options = %+v", options)
			}
		})
	}
	if _, err := router.selectedOptions("unknown", "receipt", mimeTypeJPEG, JobPurposeInteractive, 1); err == nil {
		t.Fatal("unknown model selection was accepted")
	}
}

func TestGeminiRouterKeepsSelectedEmailModelOnQuota(t *testing.T) {
	t.Parallel()
	freeCalls := 0
	freeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		freeCalls++
		if !strings.Contains(r.URL.Path, "lite-model:generateContent") {
			t.Errorf("selected email model changed during quota handling: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"rate limit"}}`)
	}))
	defer freeServer.Close()
	var paidCalls int
	paidServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paidCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer paidServer.Close()
	router := GeminiRouter{
		Free:      Gemini{APIKey: testFreeGeminiAPIKey, Endpoint: freeServer.URL + "/v1beta/", HTTPClient: freeServer.Client()},
		Paid:      Gemini{APIKey: testPaidGeminiAPIKey, Endpoint: paidServer.URL + "/v1beta/", HTTPClient: paidServer.Client()},
		LiteModel: "lite-model",
	}
	options, err := router.selectedOptions(ModelSelectionFree, "email", mimeTypeEmail, JobPurposeInteractive, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, runs, err := router.Generate(t.Context(), "email", Document{MIMEType: mimeTypeEmail, Data: []byte("message")}, nil, options)
	if err == nil || freeCalls != 1 || paidCalls != 0 || len(runs) != 1 {
		t.Fatalf("selected model was retried through a different model or route: free=%d paid=%d runs=%#v error=%v", freeCalls, paidCalls, runs, err)
	}
}

func TestGeminiCandidatePlanAndFallbackState(t *testing.T) {
	clients := []Gemini{
		{APIKey: testFreeGeminiAPIKey, ProjectRoute: ProjectFree},
		{APIKey: testPaidGeminiAPIKey, ProjectRoute: ProjectPaid},
	}
	candidates := geminiCandidates(clients, ModelEmail, ModelReceipt, true, false)
	if len(candidates) != 4 {
		t.Fatalf("email route has %d candidates, want 4", len(candidates))
	}
	if candidates[0].client.ProjectRoute != ProjectFree || candidates[0].model != ModelEmail || candidates[0].condition != geminiCandidateAlways {
		t.Fatalf("initial email candidate changed: %#v", candidates[0])
	}
	if candidates[1].client.ProjectRoute != ProjectPaid || candidates[1].model != ModelEmail || candidates[1].condition != geminiCandidateAfterInitialFallbackError {
		t.Fatalf("initial paid fallback candidate changed: %#v", candidates[1])
	}
	if candidates[2].client.ProjectRoute != ProjectFree || candidates[2].model != ModelReceipt || candidates[2].condition != geminiCandidateAfterEmailLiteQuota {
		t.Fatalf("free flash candidate changed: %#v", candidates[2])
	}
	if candidates[3].client.ProjectRoute != ProjectPaid || candidates[3].model != ModelReceipt || candidates[3].condition != geminiCandidateAfterEmailFlashFallbackError {
		t.Fatalf("paid flash fallback candidate changed: %#v", candidates[3])
	}

	quota := &GeminiAPIError{Kind: GeminiFailureQuota}
	quotaState := (geminiRouteState{}).afterFailure(quota, false)
	if got := nextGeminiCandidate(candidates, 1, quotaState); got != 2 {
		t.Fatalf("quota should select free flash candidate, got %d", got)
	}
	transientState := (geminiRouteState{}).afterFailure(&GeminiAPIError{Kind: GeminiFailureTemporary}, false)
	if got := nextGeminiCandidate(candidates, 1, transientState); got != 1 {
		t.Fatalf("temporary failure should select paid email candidate, got %d", got)
	}
	flashFallbackState := quotaState.afterFailure(&GeminiAPIError{Kind: GeminiFailureTemporary}, true)
	if got := nextGeminiCandidate(candidates, 3, flashFallbackState); got != 3 {
		t.Fatalf("temporary free flash failure should select paid flash candidate, got %d", got)
	}
}

func TestGeminiFallbackReason(t *testing.T) {
	tests := []struct {
		name      string
		email     bool
		initial   string
		flash     string
		candidate geminiCandidate
		next      geminiCandidate
		err       error
		want      string
	}{
		{
			name:      "email quota switches model",
			email:     true,
			initial:   ModelEmail,
			flash:     ModelReceipt,
			candidate: geminiCandidate{model: ModelEmail},
			next:      geminiCandidate{model: ModelReceipt},
			err:       &GeminiAPIError{Kind: GeminiFailureQuota},
			want:      "free_quota_model_fallback",
		},
		{
			name: "quota retains generic fallback reason",
			err:  &GeminiAPIError{Kind: GeminiFailureQuota},
			want: "free_quota_fallback",
		},
		{
			name: "temporary failure",
			err:  &GeminiAPIError{Kind: GeminiFailureTemporary},
			want: "free_transient_fallback",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := geminiFallbackReason(test.email, test.initial, test.flash, test.candidate, test.next, test.err)
			if got != test.want {
				t.Fatalf("geminiFallbackReason() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestGeminiRouterReservesCountedInputTokens(t *testing.T) {
	t.Parallel()

	countCalls, generateCalls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, ":countTokens"):
			countCalls++
			_, _ = io.WriteString(w, `{"totalTokens":7}`)
		case strings.Contains(r.URL.Path, ":generateContent"):
			generateCalls++
			writeGeminiSuccess(w, `{"merchant":"Market","purchasedAt":"2026-10-14T10:00:00+09:00","currency":"JPY","items":[{"name":"Tea","printedAmount":100,"grossAmount":100,"confidence":1}],"taxGroups":[],"discount":0,"total":100,"payments":[],"warnings":[]}`)
		default:
			t.Errorf("unexpected Gemini endpoint %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	rateControl := &recordingGeminiRateControl{}
	router := GeminiRouter{
		Free:        Gemini{APIKey: testFreeGeminiAPIKey, Project: "free-id", Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()},
		RateControl: rateControl,
		QuotaLimits: map[GeminiQuotaKey]GeminiQuotaLimit{
			{ProjectRoute: ProjectFree, Model: ModelReceipt}: {RequestsPerMinute: 5, InputTokensMinute: 100, RequestsPerDay: 20},
		},
	}
	_, runs, err := router.Generate(t.Context(), "receipt", Document{MIMEType: mimeTypeJPEG, Data: []byte("synthetic image")}, nil, GeminiRouteOptions{Purpose: JobPurposeInteractive})
	if err != nil {
		t.Fatalf("generate with quota reservation: %v (%#v)", err, runs)
	}
	if countCalls != 1 || generateCalls != 1 || rateControl.reservedInputTokens < 7 || rateControl.settledInputTokens != 1000 {
		t.Fatalf("count=%d generate=%d reserved tokens=%d settled tokens=%d", countCalls, generateCalls, rateControl.reservedInputTokens, rateControl.settledInputTokens)
	}
}

type recordingGeminiRateControl struct {
	reservedInputTokens int64
	settledInputTokens  int64
}

func (*recordingGeminiRateControl) Wait(context.Context, string, string) error { return nil }

func (*recordingGeminiRateControl) RecordCooldown(context.Context, string, string, time.Duration) error {
	return nil
}

func (c *recordingGeminiRateControl) Reserve(_ context.Context, _, _ string, inputTokens int64, _ GeminiQuotaLimit) (string, time.Duration, error) {
	c.reservedInputTokens = inputTokens
	return "reservation", 0, nil
}

func (c *recordingGeminiRateControl) Settle(_ context.Context, _ string, actualInputTokens int64) error {
	c.settledInputTokens = actualInputTokens
	return nil
}

func TestGeminiRouterEmailLiteQuotaUsesFreeFlashBeforePaid(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		flashStatus     int
		wantPaidCalls   int
		wantRuns        int
		wantErrorKind   GeminiFailureKind
		wantRetryAfter  time.Duration
		wantFinalRoute  string
		wantFinalModel  string
		wantFinalReason string
	}{
		{name: "free flash succeeds", flashStatus: http.StatusOK, wantRuns: 2, wantFinalRoute: ProjectFree, wantFinalModel: ModelReceipt, wantFinalReason: geminiReasonInitialExtraction},
		{name: "free flash quota retries without paid", flashStatus: http.StatusTooManyRequests, wantRuns: 2, wantErrorKind: GeminiFailureQuota, wantRetryAfter: 11 * time.Second, wantFinalRoute: ProjectFree, wantFinalModel: ModelReceipt, wantFinalReason: "free_flash_quota_retry"},
		{name: "free flash temporary failure falls back to paid flash", flashStatus: http.StatusServiceUnavailable, wantPaidCalls: 1, wantRuns: 3, wantFinalRoute: ProjectPaid, wantFinalModel: ModelReceipt, wantFinalReason: geminiReasonInitialExtraction},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			freeLiteCalls, freeFlashCalls, paidCalls := 0, 0, 0
			free := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, ModelEmail+":generateContent"):
					freeLiteCalls++
					w.Header().Set("Retry-After", "2")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"lite quota"}}`)
				case strings.Contains(r.URL.Path, ModelReceipt+":generateContent"):
					freeFlashCalls++
					if test.flashStatus == http.StatusOK {
						writeGeminiSuccess(w, `{"merchant":"Shop","purchasedAt":"","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
						return
					}
					if test.flashStatus == http.StatusTooManyRequests {
						w.Header().Set("Retry-After", "11")
					}
					w.WriteHeader(test.flashStatus)
					_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"flash unavailable"}}`)
				default:
					t.Errorf("unexpected free model path: %s", r.URL.Path)
					http.Error(w, "unexpected model", http.StatusBadRequest)
				}
			}))
			defer free.Close()
			paid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paidCalls++
				if !strings.Contains(r.URL.Path, ModelReceipt+":generateContent") {
					t.Errorf("paid fallback used unexpected model: %s", r.URL.Path)
				}
				writeGeminiSuccess(w, `{"merchant":"Shop","purchasedAt":"","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
			}))
			defer paid.Close()
			router := GeminiRouter{
				Free:       Gemini{APIKey: testFreeGeminiAPIKey, Endpoint: free.URL + "/v1beta/", HTTPClient: free.Client()},
				Paid:       Gemini{APIKey: testPaidGeminiAPIKey, Project: testPaidProjectID, Endpoint: paid.URL + "/v1beta/", HTTPClient: paid.Client()},
				USDJPYRate: 150,
			}
			receipt, runs, err := router.Generate(t.Context(), "order_email", Document{MIMEType: mimeTypePlainText, Data: []byte("Order confirmation")}, nil, GeminiRouteOptions{Purpose: JobPurposeInteractive, Reason: geminiReasonInitialExtraction})
			if freeLiteCalls != 1 || freeFlashCalls != 1 || paidCalls != test.wantPaidCalls || len(runs) != test.wantRuns {
				t.Fatalf("unexpected route attempts: lite=%d free-flash=%d paid=%d runs=%#v", freeLiteCalls, freeFlashCalls, paidCalls, runs)
			}
			if runs[0].Route != ProjectFree || runs[0].Model != ModelEmail || runs[0].ErrorCode != testRateLimitedCode || runs[0].Reason != "free_quota_model_fallback" {
				t.Fatalf("lite quota run metadata is wrong: %#v", runs[0])
			}
			last := runs[len(runs)-1]
			if last.Route != test.wantFinalRoute || last.Model != test.wantFinalModel || last.Reason != test.wantFinalReason {
				t.Fatalf("final run metadata is wrong: %#v", last)
			}
			if test.wantErrorKind != "" {
				apiErr, ok := errors.AsType[*GeminiAPIError](err)
				if !ok || apiErr.Kind != test.wantErrorKind || apiErr.RetryAfter != test.wantRetryAfter || last.ErrorCode != testRateLimitedCode {
					t.Fatalf("flash quota error must preserve its retry delay: run=%#v error=%v", last, err)
				}
				return
			}
			if err != nil || receipt.Model != ModelReceipt || last.Status != extractionStatusSucceeded {
				t.Fatalf("fallback extraction failed or used wrong model: receipt=%#v run=%#v error=%v", receipt, last, err)
			}
			if test.wantFinalRoute == ProjectFree {
				if last.PriceEstimateStatus != "free_tier" || last.EstimatedJPY != 0 {
					t.Fatalf("free fallback was recorded as paid: %#v", last)
				}
			} else if last.PriceEstimateStatus != "estimated_paid" || last.EstimatedJPY <= 0 {
				t.Fatalf("paid fallback cost was not recorded: %#v", last)
			}
		})
	}
}

func TestGeminiRouterDoesNotApplyEmailFallbackToOtherSourceKinds(t *testing.T) {
	t.Parallel()
	var calls int
	free := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.Contains(r.URL.Path, ModelEmail+":generateContent") {
			t.Errorf("explicit model override was not preserved: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"rate limit"}}`)
	}))
	defer free.Close()
	var paidCalls int
	paid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		paidCalls++
		writeGeminiSuccess(w, `{"merchant":"Shop","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
	}))
	defer paid.Close()
	router := GeminiRouter{
		Free: Gemini{APIKey: testFreeGeminiAPIKey, Endpoint: free.URL + "/v1beta/", HTTPClient: free.Client()},
		Paid: Gemini{APIKey: testPaidGeminiAPIKey, Project: testPaidProjectID, Endpoint: paid.URL + "/v1beta/", HTTPClient: paid.Client()},
	}
	_, runs, err := router.Generate(t.Context(), "receipt", Document{MIMEType: mimeTypePlainText, Data: []byte("synthetic text")}, nil, GeminiRouteOptions{Model: ModelEmail})
	apiErr, ok := errors.AsType[*GeminiAPIError](err)
	if !ok || apiErr.Kind != GeminiFailureQuota || calls != 1 || paidCalls != 0 || len(runs) != 1 || runs[0].Model != ModelEmail {
		t.Fatalf("email model fallback escaped its source kind: calls=%d paid=%d runs=%#v error=%v", calls, paidCalls, runs, err)
	}
}

func TestGeminiServiceTierFollowsJobPurpose(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		purpose string
		want    string
	}{
		{purpose: JobPurposeInteractive, want: geminiServiceTierStandard},
		{purpose: JobPurposeBackground, want: geminiServiceTierFlex},
		{want: geminiServiceTierStandard},
	} {
		got, err := defaultServiceTier(test.purpose)
		if err != nil || got != test.want {
			t.Fatalf("defaultServiceTier(%q) = %q, %v; want %q", test.purpose, got, err, test.want)
		}
	}
}

func TestGeminiRouterSelectsEmailLiteAndOnlyRetriesTemporaryErrors(t *testing.T) {
	t.Parallel()
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
		Free: Gemini{APIKey: testFreeGeminiAPIKey, Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()},
		Paid: Gemini{APIKey: "paid", Endpoint: paid.URL + "/v1beta/", HTTPClient: paid.Client()},
	}
	_, runs, err := router.Generate(t.Context(), "email", Document{MIMEType: mimeTypeEmail, Data: []byte("example")}, nil, GeminiRouteOptions{Purpose: JobPurposeInteractive})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || paidCalls != 1 || len(runs) != 2 || runs[1].ServiceTier != geminiServiceTierStandard {
		t.Fatalf("temporary free failure should use same-model paid Standard: free=%d paid=%d runs=%#v", calls, paidCalls, runs)
	}
	if _, _, err := router.Generate(t.Context(), "email", Document{MIMEType: mimeTypeEmail, Data: []byte("example")}, nil, GeminiRouteOptions{Model: "bad/model"}); err == nil || calls != 1 || paidCalls != 1 {
		t.Fatalf("invalid request should not call providers: free=%d paid=%d error=%v", calls, paidCalls, err)
	}
}

func TestGeminiSpendCapAndMalformedOutputAreTypedAndSanitized(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		status         int
		body           string
		retryAfter     string
		wantRetryAfter time.Duration
		wantKind       GeminiFailureKind
		wantCode       string
		wantSpend      bool
	}{
		{name: "spend cap", status: http.StatusTooManyRequests, body: `{"error":{"status":"RESOURCE_EXHAUSTED","message":"project spend cap reached"}}`, wantKind: GeminiFailureSpendCap, wantCode: geminiFailureSpendCap, wantSpend: true},
		{name: "monthly spending cap", status: http.StatusTooManyRequests, body: `{"error":{"status":"RESOURCE_EXHAUSTED","message":"Your billing account has exceeded its monthly spending cap."}}`, wantKind: GeminiFailureSpendCap, wantCode: geminiFailureSpendCap, wantSpend: true},
		{name: "structured spend cap reason", status: http.StatusForbidden, body: `{"error":{"status":"PERMISSION_DENIED","details":[{"reason":"SPEND_CAP_ENFORCED"}]}}`, wantKind: GeminiFailureSpendCap, wantCode: geminiFailureSpendCap, wantSpend: true},
		{name: "rate limit retry delay", status: http.StatusTooManyRequests, body: `{"error":{"status":"RESOURCE_EXHAUSTED"}}`, retryAfter: "3", wantRetryAfter: 3 * time.Second, wantKind: GeminiFailureQuota, wantCode: "rate_limited"},
		{name: "malformed provider JSON", status: http.StatusOK, body: `not json`, wantKind: GeminiFailureBadResponse, wantCode: "malformed_provider_json"},
		{name: "broken receipt JSON", status: http.StatusOK, body: `{"candidates":[{"content":{"parts":[{"text":"{broken"}]},"finishReason":"STOP"}]}`, wantKind: GeminiFailureBadResponse, wantCode: "invalid_receipt_json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.retryAfter != "" {
					w.Header().Set("Retry-After", tt.retryAfter)
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			router := GeminiRouter{Paid: Gemini{APIKey: "paid", Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()}}
			_, runs, err := router.Generate(t.Context(), "receipt", Document{MIMEType: mimeTypePDF, Data: []byte("test")}, nil, GeminiRouteOptions{PaidOnly: true})
			if err == nil || len(runs) != 1 {
				t.Fatalf("expected one failed run, got runs=%#v error=%v", runs, err)
			}
			apiErr, ok := errors.AsType[*GeminiAPIError](err)
			if !ok || apiErr.Kind != tt.wantKind || apiErr.Code != tt.wantCode || errors.Is(err, ErrGeminiSpendCap) != tt.wantSpend {
				t.Fatalf("unexpected sanitized error: %#v (%v)", apiErr, err)
			}
			if apiErr.RetryAfter != tt.wantRetryAfter {
				t.Fatalf("unexpected retry delay: got %s, want %s", apiErr.RetryAfter, tt.wantRetryAfter)
			}
			if runs[0].Status != extractionStatusFailed || runs[0].ErrorCode != tt.wantCode {
				t.Fatalf("failed run metadata missing: %#v", runs[0])
			}
		})
	}
}

func TestSpendCapRetryUsesBillingMonthAndAllowsResetPropagation(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			if got := tt.now.Add(spendCapRetryDelay(tt.now)); !got.Equal(tt.want) {
				t.Fatalf("retry at %s, want %s", got, tt.want)
			}
		})
	}
}

func TestGeminiQualityReviewAddsEvidenceAndChangesRequest(t *testing.T) {
	t.Parallel()
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
	document := Document{MIMEType: mimeTypePNG, Data: []byte("test")}
	first, runs, err := router.Generate(t.Context(), "receipt", document, nil, GeminiRouteOptions{Reason: geminiReasonInitialExtraction})
	if err != nil || len(runs) != 1 {
		t.Fatalf("initial generation failed: %#v error=%v", runs, err)
	}
	_, _, err = router.Generate(t.Context(), "receipt", document, nil, router.QualityOptions("receipt", runs[0], evidenceGuidance([]string{"税率別小計が一致しません。"}, []OCRLine{{Text: "TOTAL 980", Region: Region{Page: 0, Top: 0.1, Left: 0.1, Bottom: 0.2, Right: 0.9}}})))
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(prompts) != 2 || prompts[0] == prompts[1] || levels[0] != geminiThinkingLevelLOW || levels[1] != "MEDIUM" {
		t.Fatalf("quality review must use evidence and medium thinking: prompts=%#v levels=%#v first=%#v", prompts, levels, first)
	}
	if !strings.Contains(prompts[1], "税率別小計が一致しません") || !strings.Contains(prompts[1], "TOTAL 980") {
		t.Fatalf("quality review omitted evidence: %q", prompts[1])
	}
}

func TestGeminiQualityReviewDoesNotRepeatPaidRouteAfterFreeFailure(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		status    int
		wantKind  GeminiFailureKind
		wantDelay time.Duration
	}{
		{name: "quota", status: http.StatusTooManyRequests, wantKind: GeminiFailureQuota, wantDelay: 5 * time.Second},
		{name: "temporary failure", status: http.StatusServiceUnavailable, wantKind: GeminiFailureTemporary},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			freeCalls := 0
			free := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				freeCalls++
				if test.status == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "5")
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"provider unavailable"}}`)
			}))
			defer free.Close()
			paidCalls := 0
			paid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				paidCalls++
				writeGeminiSuccess(w, `{"merchant":"Shop","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
			}))
			defer paid.Close()
			router := GeminiRouter{
				Free: Gemini{APIKey: testFreeGeminiAPIKey, Endpoint: free.URL + "/v1beta/", HTTPClient: free.Client()},
				Paid: Gemini{APIKey: testPaidGeminiAPIKey, Project: testPaidProjectID, Endpoint: paid.URL + "/v1beta/", HTTPClient: paid.Client()},
			}
			options := router.QualityOptions("receipt", ExtractionRun{Model: ModelReceipt, Route: ProjectPaid}, []string{"tax total mismatch"})
			_, runs, err := router.Generate(t.Context(), "receipt", Document{MIMEType: mimeTypeJPEG, Data: []byte("synthetic image")}, nil, options)
			apiErr, ok := errors.AsType[*GeminiAPIError](err)
			if !ok || apiErr.Kind != test.wantKind || apiErr.RetryAfter != test.wantDelay {
				t.Fatalf("quality review must preserve the free error: error=%v", err)
			}
			if !options.FreeOnly || options.ProjectPreferred != ProjectFree || freeCalls != 1 || paidCalls != 0 || len(runs) != 1 || runs[0].Route != ProjectFree {
				t.Fatalf("paid initial extraction must not cause a duplicate paid quality call: options=%#v free=%d paid=%d runs=%#v", options, freeCalls, paidCalls, runs)
			}
		})
	}
}

func TestGeminiRouterModelOverridesAndEmailQualityUpgrade(t *testing.T) {
	t.Parallel()
	router := GeminiRouter{FlashModel: testConfiguredFlash, LiteModel: testConfiguredLite}
	if got := router.modelForSource("receipt", mimeTypeJPEG); got != testConfiguredFlash {
		t.Fatalf("configured flash model was ignored: %q", got)
	}
	if got := router.modelForSource("email", mimeTypeEmail); got != testConfiguredLite {
		t.Fatalf("configured lite model was ignored: %q", got)
	}
	quality := router.QualityOptions("email", ExtractionRun{Model: "configured-lite", Route: ProjectFree}, []string{"transaction total conflicts with printed subtotal"})
	if quality.Model != testConfiguredFlash || quality.ThinkingLevel != "medium" || quality.Reason != "evidence_guided_quality_review" || len(quality.Guidance) != 1 {
		t.Fatalf("email quality review must use a more capable model with evidence: %#v", quality)
	}
	if !supportsProReview(mimeTypeJPEG) || !supportsProReview(mimeTypePDF) || supportsProReview(mimeTypeEmail) {
		t.Fatal("Pro review should target scanned purchase documents and skip email")
	}
}

func TestEstimatedGeminiCostUsesProjectTierAndPriceChange(t *testing.T) {
	t.Parallel()
	cutover := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	before := estimateCostJPY(ProjectPaid, ModelReceipt, geminiServiceTierStandard, 1_000_000, 1_000_000, 100, cutover.Add(-time.Second))
	flex := estimateCostJPY(ProjectPaid, ModelReceipt, geminiServiceTierFlex, 1_000_000, 1_000_000, 100, cutover.Add(-time.Second))
	after := estimateCostJPY(ProjectPaid, ModelReceipt, geminiServiceTierStandard, 1_000_000, 1_000_000, 100, cutover)
	proUnderLimit := estimateCostJPY(ProjectPaid, ModelPro, geminiServiceTierStandard, 200_000, 1_000_000, 100, cutover)
	proOverLimit := estimateCostJPY(ProjectPaid, ModelPro, geminiServiceTierStandard, 200_001, 1_000_000, 100, cutover)
	lite31 := estimateCostJPY(ProjectPaid, ModelLite31, geminiServiceTierStandard, 1_000_000, 1_000_000, 100, cutover)
	if before != 450 || flex != 225 || after != 900 || proUnderLimit != 1240 || proOverLimit != 1880.0004 || lite31 != 175 || estimateCostJPY(ProjectFree, ModelReceipt, geminiServiceTierStandard, 1_000_000, 1_000_000, 100, cutover) != 0 {
		t.Fatalf("unexpected estimated prices: before=%f flex=%f after=%f", before, flex, after)
	}
}

func TestGeminiRouterRejectsUnknownPriceBeforePaidRequest(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		preferred     string
		paidOnly      bool
		freeStatus    int
		wantFreeCalls int
		wantRuns      int
	}{
		{name: "paid only", paidOnly: true},
		{name: "paid fallback", preferred: ProjectFree, freeStatus: http.StatusServiceUnavailable, wantFreeCalls: 1, wantRuns: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			freeCalls := 0
			freeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				freeCalls++
				if test.freeStatus != 0 {
					w.WriteHeader(test.freeStatus)
					_, _ = io.WriteString(w, `{"error":{"status":"UNAVAILABLE"}}`)
					return
				}
				writeGeminiSuccess(w, `{"merchant":"Market","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
			}))
			defer freeServer.Close()
			paidCalls := 0
			paidServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				paidCalls++
				writeGeminiSuccess(w, `{"merchant":"Market","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
			}))
			defer paidServer.Close()
			router := GeminiRouter{
				Free:       Gemini{APIKey: testFreeGeminiAPIKey, Endpoint: freeServer.URL + "/v1beta/", HTTPClient: freeServer.Client()},
				Paid:       Gemini{APIKey: testPaidGeminiAPIKey, Project: testPaidProjectID, Endpoint: paidServer.URL + "/v1beta/", HTTPClient: paidServer.Client()},
				USDJPYRate: 150,
			}
			_, runs, err := router.Generate(t.Context(), "receipt", Document{MIMEType: mimeTypeJPEG, Data: []byte("test input")}, nil, GeminiRouteOptions{Model: "future-model", ProjectPreferred: test.preferred, PaidOnly: test.paidOnly})
			apiErr, ok := errors.AsType[*GeminiAPIError](err)
			if !ok || apiErr.Kind != GeminiFailureRejected || apiErr.Code != geminiFailureUnknownModelPrice {
				t.Fatalf("unknown model price should reject paid routing: %v", err)
			}
			if freeCalls != test.wantFreeCalls || paidCalls != 0 || len(runs) != test.wantRuns {
				t.Fatalf("unexpected calls or runs: free=%d paid=%d runs=%#v", freeCalls, paidCalls, runs)
			}
		})
	}
}

func TestGeminiExtractRunDoesNotLabelUnknownModelPriceAsFree(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeGeminiSuccess(w, `{"merchant":"Market","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
	}))
	defer server.Close()
	gemini := Gemini{APIKey: testPaidGeminiAPIKey, Model: "future-model", ProjectRoute: ProjectPaid, Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client(), USDJPYRate: 150}
	_, run, err := gemini.ExtractRun(t.Context(), Document{MIMEType: mimeTypeJPEG, Data: []byte("test input")}, nil, GeminiRequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if run.PriceEstimateStatus != geminiFailureUnknownModelPrice || run.EstimatedJPY != 0 || run.PricedAt.IsZero() {
		t.Fatalf("unknown model was reported as a free run: %#v", run)
	}
}

func TestGeminiRunKeepsPriceSnapshotWhenExchangeRateIsMissing(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeGeminiSuccess(w, `{"merchant":"Market","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
	}))
	defer server.Close()
	router := GeminiRouter{
		Paid: Gemini{APIKey: testPaidGeminiAPIKey, Project: testPaidProjectID, Endpoint: server.URL + "/v1beta/"},
	}
	_, runs, err := router.Generate(t.Context(), "receipt", Document{MIMEType: mimeTypeJPEG, Data: []byte("test input")}, nil, GeminiRouteOptions{PaidOnly: true, ServiceTier: geminiServiceTierFlex})
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
