package ingestion

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiInteractKeepsConversationStatelessAndReturnsStructuredRevision(t *testing.T) {
	t.Parallel()
	output := `{"reply":"税区分を原本の表示に合わせました。","receipt":{"merchant":"Shop","purchasedAt":"2026-10-01","datePrecision":"DATE_ONLY","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}}`
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/interactions") || r.Method != http.MethodPost {
			t.Errorf("request = %s %s, want POST /interactions", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != testGeminiAPIKey {
			t.Errorf("API key header = %q, want configured test key", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"interaction-test","status":"completed","usage":{"total_input_tokens":21,"total_output_tokens":34},"steps":[{"type":"model_output","content":[{"type":"text","text":`+mustJSONText(output)+`}]}]}`)
	}))
	defer server.Close()

	gemini := Gemini{APIKey: testGeminiAPIKey, Model: ModelReceipt, Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()}
	current := []byte(`{"receipt":{"merchant":"Old Shop","items":[]}}`)
	history := []ConversationMessage{{Role: "user", Text: "Use the printed merchant name."}, {Role: "assistant", Text: "I will check the source again."}}
	receipt, reply, usage, err := gemini.Interact(t.Context(), Document{MIMEType: mimeTypePNG, Data: []byte("synthetic png")}, "receipt", current, history, "The printed merchant is Shop.", GeminiRequestOptions{ThinkingLevel: geminiThinkingLevelLow})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Merchant != "Shop" || receipt.Model != ModelReceipt || reply != "税区分を原本の表示に合わせました。" || usage.inputTokens != 21 || usage.outputTokens != 34 {
		t.Fatalf("interaction output = receipt %#v, reply %q, usage %#v", receipt, reply, usage)
	}
	if request["model"] != ModelReceipt || request["store"] != false || request["previous_interaction_id"] != nil {
		t.Fatalf("interaction request changed model selection or storage policy: %#v", request)
	}
	input, ok := request["input"].([]any)
	if !ok || len(input) != 2 {
		t.Fatalf("interaction input = %#v, want conversation text and original image", request["input"])
	}
	textPart, ok := input[0].(map[string]any)
	if !ok || textPart["type"] != "text" || !strings.Contains(textPart["text"].(string), "Use the printed merchant name.") || !strings.Contains(textPart["text"].(string), "The printed merchant is Shop.") {
		t.Fatalf("interaction prompt lost local conversation history: %#v", input[0])
	}
	imagePart, ok := input[1].(map[string]any)
	if !ok || imagePart["type"] != "image" || imagePart["mime_type"] != mimeTypePNG {
		t.Fatalf("interaction source = %#v, want synthetic source image", input[1])
	}
	responseFormat, ok := request["response_format"].(map[string]any)
	if !ok || responseFormat["mime_type"] != "application/json" {
		t.Fatalf("interaction response format = %#v, want structured JSON", request["response_format"])
	}
}

func TestGeminiInteractSettlesReservedQuotaAfterRequestCancellation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":countTokens") {
			t.Errorf("request = %s, want token count before reservation", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"totalTokens":21}`)
	}))
	defer server.Close()

	requestCtx, cancelRequest := context.WithCancel(t.Context())
	defer cancelRequest()
	settled := false
	settledTokens := int64(0)
	reserve := func(_ context.Context, inputTokens int64) (func(context.Context, int64) error, error) {
		if inputTokens != 21 {
			t.Fatalf("reserved token count = %d, want 21", inputTokens)
		}
		cancelRequest()
		return func(ctx context.Context, actualInputTokens int64) error {
			if err := ctx.Err(); err != nil {
				t.Fatalf("quota settlement context is canceled: %v", err)
			}
			settled = true
			settledTokens = actualInputTokens
			return nil
		}, nil
	}
	gemini := Gemini{APIKey: testGeminiAPIKey, Model: ModelReceipt, Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()}
	_, _, _, err := gemini.Interact(requestCtx, Document{MIMEType: mimeTypePNG, Data: []byte("synthetic png")}, "receipt", []byte(`{"receipt":{}}`), nil, "check the total", GeminiRequestOptions{ReserveInputTokens: reserve})
	if err == nil {
		t.Fatal("canceled provider request succeeded")
	}
	if !settled || settledTokens != 21 {
		t.Fatalf("reserved quota was not settled after cancellation: settled=%t tokens=%d", settled, settledTokens)
	}
}
