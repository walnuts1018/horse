package ingestion

import "testing"

func TestExtractionRetryPolicyFor(t *testing.T) {
	t.Parallel()
	first := ExtractionRun{Status: extractionStatusSucceeded, Receipt: new(Receipt), Model: "flash"}
	failed := ExtractionRun{Status: extractionStatusFailed, Model: "pro"}
	tests := []struct {
		name         string
		verification Verification
		mimeType     string
		paid         bool
		wantQuality  string
		wantPro      bool
	}{
		{name: "balanced skips retries", verification: Verification{Balanced: true, Runs: []ExtractionRun{first}}, mimeType: mimeTypeJPEG, paid: true},
		{name: "unbalanced without run skips quality", verification: Verification{Issues: []string{"合計が一致しません"}}, mimeType: mimeTypeJPEG, paid: true, wantPro: true},
		{name: "latest successful run enables quality and paid image review", verification: Verification{Issues: []string{"合計が一致しません"}, Runs: []ExtractionRun{first, failed}}, mimeType: mimeTypeJPEG, paid: true, wantQuality: "flash", wantPro: true},
		{name: "failed run does not enable quality but may use pro review", verification: Verification{Issues: []string{"合計が一致しません"}, Runs: []ExtractionRun{failed}}, mimeType: mimeTypeJPEG, paid: true, wantPro: true},
		{name: "unsupported source skips pro review", verification: Verification{Issues: []string{"合計が一致しません"}, Runs: []ExtractionRun{first}}, mimeType: mimeTypeEmail, paid: true, wantQuality: "flash"},
		{name: "unconfigured paid project skips pro review", verification: Verification{Issues: []string{"合計が一致しません"}, Runs: []ExtractionRun{first}}, mimeType: mimeTypePDF, wantQuality: "flash"},
		{name: "non financial issues skip pro review", verification: Verification{Issues: []string{"店舗名を確認してください"}, Runs: []ExtractionRun{first}}, mimeType: mimeTypePDF, paid: true, wantQuality: "flash"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			policy := extractionRetryPolicyFor(test.verification, test.mimeType, test.paid)
			if got := runModel(policy.qualityRun); got != test.wantQuality {
				t.Fatalf("quality run model = %q, want %q", got, test.wantQuality)
			}
			if policy.proReview != test.wantPro {
				t.Fatalf("pro review = %t, want %t", policy.proReview, test.wantPro)
			}
		})
	}
}

func TestSelectVerificationCandidate(t *testing.T) {
	t.Parallel()
	current := Verification{
		Receipt: Receipt{Merchant: "initial"}, Transform: [9]float64{1}, TransformPage: 2,
		Issues: []string{"合計が一致しません"}, ComputedTotal: 100, OCRLines: []OCRLine{{Text: "ocr"}},
		Runs: []ExtractionRun{{Model: "initial"}}, Candidates: []InterpretationResult{{ID: "initial"}},
	}
	tests := []struct {
		name         string
		candidate    Verification
		wantModel    string
		wantIssues   int
		wantBalanced bool
	}{
		{name: "balanced candidate wins", candidate: Verification{Receipt: Receipt{Merchant: "balanced"}, Balanced: true}, wantModel: "balanced", wantBalanced: true},
		{name: "fewer issues wins", candidate: Verification{Receipt: Receipt{Merchant: "fewer"}, Issues: []string{}, ComputedTotal: 80}, wantModel: "fewer", wantBalanced: false},
		{name: "same quality keeps current", candidate: Verification{Receipt: Receipt{Merchant: "tie"}, Issues: []string{"別の問題"}}, wantModel: "initial", wantIssues: 1},
		{name: "more issues keeps current", candidate: Verification{Receipt: Receipt{Merchant: "more"}, Issues: []string{"一", "二"}}, wantModel: "initial", wantIssues: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := selectVerificationCandidate(current, test.candidate)
			if got.Receipt.Merchant != test.wantModel || len(got.Issues) != test.wantIssues || got.Balanced != test.wantBalanced {
				t.Fatalf("selected candidate = merchant %q, issues %d, balanced %t", got.Receipt.Merchant, len(got.Issues), got.Balanced)
			}
			if got.Transform != current.Transform || got.TransformPage != current.TransformPage || got.OCRLines[0].Text != "ocr" || len(got.Runs) != 1 || got.Runs[0].Model != "initial" {
				t.Fatalf("candidate selection changed shared extraction metadata: %+v", got)
			}
		})
	}
}

func runModel(run *ExtractionRun) string {
	if run == nil {
		return ""
	}
	return run.Model
}
