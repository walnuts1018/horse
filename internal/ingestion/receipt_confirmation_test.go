package ingestion

import "testing"

func TestValidateReceiptConfirmation(t *testing.T) {
	t.Parallel()
	verification := Verification{
		Receipt:  Receipt{Total: 30, Items: []Item{{GrossAmount: 10}, {GrossAmount: 20}}},
		Balanced: false,
		Candidates: []InterpretationResult{
			{ID: testConsistentReceipt, Amounts: []int64{12, 18}, ComputedTotal: 30, Consistent: true},
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
		{name: "consistent candidate", amounts: []int64{12, 18}, candidateID: testConsistentReceipt},
		{name: "unresolved items with matching total", amounts: []int64{10, 20}},
		{name: "manual split with matching total", amounts: []int64{5, 10, 15}},
		{name: "unverified extracted amounts with matching total", amounts: []int64{11, 19}, wantError: true},
		{name: "unverified candidate amounts with matching total", amounts: []int64{11, 19}, candidateID: testConsistentReceipt, wantError: true},
		{name: "manual reason permits changed candidate amounts", amounts: []int64{11, 19}, candidateID: testConsistentReceipt, reason: "印字額を原本で訂正", wantOverride: true},
		{name: "total mismatch needs reason", amounts: []int64{12, 17}, candidateID: testConsistentReceipt, wantError: true},
		{name: "total mismatch can be confirmed with reason", amounts: []int64{12, 17}, candidateID: testConsistentReceipt, reason: "値引きの内訳を確認", wantOverride: true},
		{name: "item count can change when total matches", amounts: []int64{30}},
		{name: "invalid candidate", amounts: []int64{12, 18}, candidateID: "missing", reason: "原本で確認", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotOverride, err := ValidateReceiptConfirmation(verification, tt.amounts, tt.candidateID, tt.reason)
			if (err != nil) != tt.wantError || gotOverride != tt.wantOverride {
				t.Fatalf("got override=%v err=%v", gotOverride, err)
			}
		})
	}
}
