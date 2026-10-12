package ingestion

import "testing"

func TestValidateReceiptConfirmation(t *testing.T) {
	verification := Verification{
		Receipt:  Receipt{Total: 30, Items: []Item{{GrossAmount: 10}, {GrossAmount: 20}}},
		Balanced: false,
		Candidates: []InterpretationResult{
			{ID: "consistent", Amounts: []int64{12, 18}, ComputedTotal: 30, Consistent: true},
			{ID: "inconsistent", Amounts: []int64{12, 17}, ComputedTotal: 29},
		},
	}
	tests := []struct {
		name         string
		amounts      []int64
		candidateID  string
		reason       string
		wantOverride bool
		wantError    bool
	}{
		{name: "consistent candidate", amounts: []int64{12, 18}, candidateID: "consistent"},
		{name: "unresolved receipt needs reason", amounts: []int64{10, 20}, wantError: true},
		{name: "changed amounts need reason", amounts: []int64{11, 19}, candidateID: "consistent", wantError: true},
		{name: "manual correction is recorded", amounts: []int64{11, 19}, candidateID: "consistent", reason: "印字額を原本で訂正", wantOverride: true},
		{name: "total mismatch needs reason", amounts: []int64{12, 17}, candidateID: "consistent", wantError: true},
		{name: "total mismatch can be confirmed with reason", amounts: []int64{12, 17}, candidateID: "consistent", reason: "値引きの内訳を確認", wantOverride: true},
		{name: "item count mismatch", amounts: []int64{30}, reason: "原本に合わせて訂正", wantError: true},
		{name: "invalid candidate", amounts: []int64{12, 18}, candidateID: "missing", reason: "原本で確認", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOverride, err := ValidateReceiptConfirmation(verification, tt.amounts, tt.candidateID, tt.reason)
			if (err != nil) != tt.wantError || gotOverride != tt.wantOverride {
				t.Fatalf("got override=%v err=%v", gotOverride, err)
			}
		})
	}
}
