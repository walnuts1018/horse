package purchase

import (
	"errors"
	"testing"
)

func TestValidateCorrectionRequest(t *testing.T) {
	const (
		actorID     = "018f47d4-9e1a-7cc0-85b8-4f98c3429472"
		householdID = "018f47d4-9e1a-7cc0-85b8-4f98c3429473"
		purchaseID  = "018f47d4-9e1a-7cc0-85b8-4f98c3429474"
	)
	valid := CorrectionCommand{
		ActorID: actorID, HouseholdID: householdID, PurchaseID: purchaseID,
		AmountDeltaYen: 100, Reason: "price correction", IdempotencyKey: "correction-1",
	}
	tests := []struct {
		name          string
		input         CorrectionCommand
		wantOperation string
		wantErr       bool
	}{
		{name: "manual correction", input: valid, wantOperation: "correctPurchase"},
		{
			name: "statement correction may derive amount from evidence",
			input: func() CorrectionCommand {
				input := valid
				input.AmountDeltaYen = 0
				input.SourceDocumentID = "document"
				input.SourceReference = "row:1"
				return input
			}(),
			wantOperation: "correctPurchaseFromStatement",
		},
		{
			name: "zero manual delta",
			input: func() CorrectionCommand {
				input := valid
				input.AmountDeltaYen = 0
				return input
			}(),
			wantErr: true,
		},
		{
			name: "incomplete statement evidence",
			input: func() CorrectionCommand {
				input := valid
				input.SourceDocumentID = "document"
				return input
			}(),
			wantErr: true,
		},
		{
			name: "amount exceeds limit",
			input: func() CorrectionCommand {
				input := valid
				input.AmountDeltaYen = correctionMaxMoney + 1
				return input
			}(),
			wantErr: true,
		},
		{
			name: "blank reason",
			input: func() CorrectionCommand {
				input := valid
				input.Reason = " \t"
				return input
			}(),
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateCorrectionRequest(test.input)
			if test.wantErr {
				if !errors.Is(err, ErrBadInput) {
					t.Fatalf("validateCorrectionRequest() error = %v, want ErrBadInput", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateCorrectionRequest() error = %v", err)
			}
			if got.operation != test.wantOperation {
				t.Errorf("operation = %q, want %q", got.operation, test.wantOperation)
			}
			if got.delta != test.input.AmountDeltaYen {
				t.Errorf("delta = %d, want %d", got.delta, test.input.AmountDeltaYen)
			}
		})
	}
}
