package ingestion

import (
	"encoding/json/v2"
	"strings"

	"google.golang.org/genai"
)

type geminiUsage struct {
	inputTokens  int64
	outputTokens int64
	available    bool
}

type statementExtraction struct {
	Transactions []StatementEntry `json:"transactions"`
	Warnings     []string         `json:"warnings"`
}

func decodeGeminiUsage(response *genai.GenerateContentResponse) geminiUsage {
	if response == nil || response.UsageMetadata == nil {
		return geminiUsage{}
	}
	return geminiUsage{
		inputTokens:  int64(response.UsageMetadata.PromptTokenCount),
		outputTokens: int64(response.UsageMetadata.CandidatesTokenCount + response.UsageMetadata.ThoughtsTokenCount),
		available:    true,
	}
}

func decodeGeminiResponse(response *genai.GenerateContentResponse, sourceKind string) (Receipt, error) {
	if response == nil || len(response.Candidates) == 0 || response.Candidates[0] == nil || response.Candidates[0].FinishReason != genai.FinishReasonStop {
		return Receipt{}, &GeminiAPIError{Kind: GeminiFailureBadResponse, Code: "incomplete_generation"}
	}

	var text strings.Builder
	if content := response.Candidates[0].Content; content != nil {
		for _, part := range content.Parts {
			if part != nil && !part.Thought {
				text.WriteString(part.Text)
			}
		}
	}

	var receipt Receipt
	if sourceKind == statementSourceCard || sourceKind == statementSourceBank {
		var extraction statementExtraction
		if err := json.Unmarshal([]byte(text.String()), &extraction); err != nil {
			return Receipt{}, &GeminiAPIError{Kind: GeminiFailureBadResponse, Code: "invalid_statement_json"}
		}
		verified, err := VerifyStatements(sourceKind, extraction.Transactions, extraction.Warnings)
		if err != nil {
			return Receipt{}, &GeminiAPIError{Kind: GeminiFailureBadResponse, Code: "statement_validation_failed"}
		}
		receipt.Statements = verified.Statements
		receipt.Warnings = extraction.Warnings
		return receipt, nil
	}
	if err := json.Unmarshal([]byte(text.String()), &receipt); err != nil {
		return Receipt{}, &GeminiAPIError{Kind: GeminiFailureBadResponse, Code: "invalid_receipt_json"}
	}
	return receipt, nil
}
