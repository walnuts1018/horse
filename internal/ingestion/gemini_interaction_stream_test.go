package ingestion

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestInteractionReplyPrefixWaitsForCompleteJSONCharacters(t *testing.T) {
	partial := []byte(`{"reply":"東京\uD83D`)
	reply, found, err := interactionReplyPrefix(partial)
	if err != nil || !found || reply != "東京" {
		t.Fatalf("partial reply = %q, found %v, error %v", reply, found, err)
	}
	complete := []byte(`{"reply":"東京\uD83D\uDE00"}`)
	reply, found, err = interactionReplyPrefix(complete)
	if err != nil || !found || reply != "東京😀" {
		t.Fatalf("complete reply = %q, found %v, error %v", reply, found, err)
	}
	partialUTF8 := []byte(`{"reply":"あ`)
	partialUTF8 = partialUTF8[:len(partialUTF8)-1]
	reply, found, err = interactionReplyPrefix(partialUTF8)
	if err != nil || !found || reply != "" {
		t.Fatalf("incomplete UTF-8 reply = %q, found %v, error %v", reply, found, err)
	}
}

func TestGeminiInteractStreamsOnlyReplyText(t *testing.T) {
	output := `{"reply":"確認しました。","receipt":{"merchant":"Shop","purchasedAt":"2026-10-01","datePrecision":"DATE_ONLY","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}}`
	split := strings.Index(output, `,"receipt"`)
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"event_type\":\"step.start\",\"index\":0,\"step\":{\"type\":\"model_output\",\"content\":[]}}\n\n")
		_, _ = fmt.Fprintf(w, "data: {\"event_type\":\"step.delta\",\"index\":0,\"delta\":{\"type\":\"text\",\"text\":%s}}\n\n", mustJSONText(output[:split]))
		_, _ = fmt.Fprintf(w, "data: {\"event_type\":\"step.delta\",\"index\":0,\"delta\":{\"type\":\"text\",\"text\":%s}}\n\n", mustJSONText(output[split:]))
		_, _ = io.WriteString(w, "data: {\"event_type\":\"interaction.completed\",\"interaction\":{\"id\":\"interaction-stream\",\"status\":\"completed\",\"usage\":{\"total_input_tokens\":21,\"total_output_tokens\":34}}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	var previews []string
	gemini := Gemini{APIKey: testGeminiAPIKey, Model: ModelReceipt, Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()}
	receipt, reply, _, err := gemini.InteractStreaming(
		t.Context(), Document{MIMEType: mimeTypePNG, Data: []byte("synthetic png")}, "receipt", []byte(`{"receipt":{}}`), nil,
		"check the total", GeminiRequestOptions{ThinkingLevel: geminiThinkingLevelLow}, func(preview string) error {
			previews = append(previews, preview)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Merchant != "Shop" || reply != "確認しました。" || len(previews) == 0 || previews[len(previews)-1] != reply {
		t.Fatalf("stream result = receipt %#v, reply %q, previews %#v", receipt, reply, previews)
	}
	if request["stream"] != true {
		t.Fatalf("interaction request stream = %#v, want true", request["stream"])
	}
	if slices.ContainsFunc(previews, func(preview string) bool { return strings.ContainsAny(preview, "{}") }) {
		t.Fatalf("reply preview contains partial JSON: %#v", previews)
	}
}

func TestInteractionReplyPrefixRejectsAmbiguousAndInvalidReplies(t *testing.T) {
	for _, test := range []struct {
		input     string
		wantError bool
	}{
		{input: `{"reply":null}`},
		{input: `{"reply":12}`},
		{input: `{"reply":"one","reply":"two"}`, wantError: true},
		{input: "{\"reply\":\"bad\x01text\"}", wantError: true},
	} {
		reply, found, err := interactionReplyPrefix([]byte(test.input))
		if found || reply != "" || (err != nil) != test.wantError {
			t.Fatalf("interactionReplyPrefix(%q) = %q, found %v, error %v", test.input, reply, found, err)
		}
	}
}
