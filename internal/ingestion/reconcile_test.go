package ingestion

import (
	"testing"
	"time"
)

func TestStatementObservationsKeepIndependentReferences(t *testing.T) {
	result := Verification{Statements: []StatementEntry{
		{Row: 2, TransactionID: "card-1", Merchant: "店舗", Amount: 100, OccurredAt: "2026-10-12T00:00:00+09:00"},
		{Row: 3, Merchant: "店舗", Amount: 100, OccurredAt: "2026-10-12T00:00:00+09:00"},
	}}
	observations := SourceObservations("document", "hash", "card", result)
	if len(observations) != 2 || observations[0].SourceReference != "transaction:card-1" || observations[1].SourceReference != "row:3" {
		t.Fatalf("statement references lost: %#v", observations)
	}
	matched := Reconcile(Observation{DocumentID: "new", Merchant: "店舗", Amount: 100, OccurredAt: observations[0].OccurredAt}, observations)
	if len(matched) != 2 || matched[0].SourceReference == matched[1].SourceReference {
		t.Fatalf("equal amount statements were collapsed: %#v", matched)
	}
	if !observations[0].OccurredAt.Equal(time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("statement timezone lost: %v", observations[0].OccurredAt)
	}
}

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
