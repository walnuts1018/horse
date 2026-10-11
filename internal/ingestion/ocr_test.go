package ingestion

import "testing"

func TestOCRCorroboration(t *testing.T) {
	region := Region{Top: 0.1, Left: 0.1, Bottom: 0.2, Right: 0.9}
	receipt := Receipt{Items: []Item{{PrintedName: "ヨーグルト", PrintedAmount: 198, Region: &region}}}
	if issues := CompareOCR(receipt, []OCRLine{{Text: "ヨーグルト 198", Region: region}}); len(issues) != 0 {
		t.Fatal(issues)
	}
	if issues := CompareOCR(receipt, []OCRLine{{Text: "ヨーグルト 196", Region: region}}); len(issues) != 1 {
		t.Fatalf("amount disagreement was not flagged: %v", issues)
	}
	other := Region{Top: 0.8, Left: 0.1, Bottom: 0.9, Right: 0.9}
	if issues := CompareOCR(receipt, []OCRLine{{Text: "ヨーグルト 198", Region: other}}); len(issues) != 2 {
		t.Fatalf("incorrect evidence region was not flagged: %v", issues)
	}
}
