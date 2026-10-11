package ingestion

import (
	"testing"
	"time"
)

func TestReconcile(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name          string
		incoming, old Observation
		state         string
	}{
		{"同原本", Observation{Hash: "abc"}, Observation{Hash: "abc"}, "DUPLICATE"},
		{"分割発送", Observation{OrderID: "order", Amount: 100}, Observation{OrderID: "order", Amount: 200}, "POSSIBLE"},
		{"同額でも別購入を確定統合しない", Observation{Merchant: "店", Amount: 100, OccurredAt: now}, Observation{Merchant: "店", Amount: 100, OccurredAt: now}, "POSSIBLE"},
		{"銀行引落しは支出に統合しない", Observation{Merchant: "店", Amount: 100, OccurredAt: now, SourceKind: "bank"}, Observation{Merchant: "店", Amount: 100, OccurredAt: now, SourceKind: "card"}, ""},
		{"取引番号の金額相違", Observation{Merchant: "店", TransactionID: "tx", Amount: 100}, Observation{Merchant: "店", TransactionID: "tx", Amount: 200}, "CONFLICT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Reconcile(tc.incoming, []Observation{tc.old})
			if tc.state == "" {
				if len(got) != 0 {
					t.Fatalf("unexpected match: %v", got)
				}
				return
			}
			if len(got) != 1 || got[0].State != tc.state {
				t.Fatalf("got %v want %s", got, tc.state)
			}
		})
	}
}
