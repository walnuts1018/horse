package ingestion

import (
	"fmt"
	"testing"
)

func TestSelectHistoryHintsUsesOnlyOCRMatchesAndCapsCandidates(t *testing.T) {
	t.Parallel()
	lines := []OCRLine{{Text: "Walnuts Market 商品001 商品002 商品003 商品004 商品005 商品006 商品007 商品008 商品009 商品010 商品011"}}
	candidates := []historyCandidate{{printedName: "未一致商品", normalizedName: "別の商品"}, {printedName: "商品", normalizedName: "商品"}}
	for i := 1; i <= 11; i++ {
		name := fmt.Sprintf("商品%03d", i)
		candidates = append(candidates, historyCandidate{merchant: "Walnuts Market", printedName: name, normalizedName: "正規化" + name, category: "食料品"})
	}
	candidates = append(candidates, historyCandidate{merchant: "Walnuts Market", printedName: "商品001", normalizedName: "正規化商品001"})

	hints := selectHistoryHints(candidates, lines)
	if len(hints) != 10 {
		t.Fatalf("history hints count = %d, want 10", len(hints))
	}
	if hints[0].PrintedName != "商品001" || hints[0].NormalizedName != "正規化商品001" || hints[0].Category != "食料品" {
		t.Fatalf("first relevant history hint = %#v", hints[0])
	}
	for _, hint := range hints {
		if hint.PrintedName == "未一致商品" || hint.PrintedName == "商品" || hint.PrintedName == "商品011" {
			t.Fatalf("irrelevant or over-limit history hint included: %#v", hint)
		}
	}
}
