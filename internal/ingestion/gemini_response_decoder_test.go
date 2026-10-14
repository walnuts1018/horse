package ingestion

import (
	"testing"

	"google.golang.org/genai"
)

func TestDecodeGeminiResponseAndUsage(t *testing.T) {
	t.Parallel()
	response := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			FinishReason: genai.FinishReasonStop,
			Content: &genai.Content{Parts: []*genai.Part{
				{Text: "ignored thought", Thought: true},
				{Text: `{"merchant":"Market",`},
				{Text: `"currency":"JPY"}`},
			}},
		}},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     100,
			CandidatesTokenCount: 25,
			ThoughtsTokenCount:   5,
		},
	}

	receipt, err := decodeGeminiResponse(response, "")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Merchant != "Market" || receipt.Currency != "JPY" {
		t.Fatalf("decoded receipt = %#v", receipt)
	}
	usage := decodeGeminiUsage(response)
	if !usage.available || usage.inputTokens != 100 || usage.outputTokens != 30 {
		t.Fatalf("decoded usage = %#v", usage)
	}
}
