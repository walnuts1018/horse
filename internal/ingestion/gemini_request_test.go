package ingestion

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildGeminiContentsIncludesReviewGuidanceAndTextSource(t *testing.T) {
	t.Parallel()

	contents, err := buildGeminiContents(Document{MIMEType: mimeTypePlainText, Data: []byte("order confirmation")}, []HistoryHint{{Merchant: "Market"}}, GeminiRequestOptions{Guidance: []string{"Verify the printed total", "Keep uncertainty"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 || contents[0].Role != "user" || len(contents[0].Parts) != 2 {
		t.Fatalf("request contents = %#v, want one user message with prompt and source", contents)
	}
	prompt := contents[0].Parts[0].Text
	for _, want := range []string{"History hints are optional normalization candidates", "not included in this text", "Verify the printed total", "Keep uncertainty"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("request prompt does not contain %q", want)
		}
	}
	if contents[0].Parts[1].Text != "order confirmation" {
		t.Errorf("text source = %q, want original document", contents[0].Parts[1].Text)
	}
}

func TestBuildGeminiContentsUsesStatementPromptForBinarySource(t *testing.T) {
	t.Parallel()

	data := []byte("statement bytes")
	contents, err := buildGeminiContents(Document{MIMEType: mimeTypePDF, Data: data}, nil, GeminiRequestOptions{SourceKind: statementSourceCard}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 || len(contents[0].Parts) != 2 {
		t.Fatalf("request contents = %#v, want one user message with prompt and source", contents)
	}
	if prompt := contents[0].Parts[0].Text; !strings.Contains(prompt, "CARD_PURCHASE or REFUND") {
		t.Errorf("statement prompt = %q, want card transaction classification", prompt)
	}
	if blob := contents[0].Parts[1].InlineData; blob == nil || blob.MIMEType != mimeTypePDF || string(blob.Data) != string(data) {
		t.Errorf("binary source = %#v, want original PDF data", blob)
	}
}

func TestBuildGeminiContentsRejectsOversizedHistoryHint(t *testing.T) {
	t.Parallel()

	_, err := buildGeminiContents(Document{}, []HistoryHint{{Merchant: strings.Repeat("x", 1025)}}, GeminiRequestOptions{}, false)
	apiErr, ok := errors.AsType[*GeminiAPIError](err)
	if !ok || apiErr.Kind != GeminiFailureRejected || apiErr.Code != "history_hint_exceeds_limit" {
		t.Fatalf("buildGeminiContents() error = %v, want rejected oversized history hint", err)
	}
}
