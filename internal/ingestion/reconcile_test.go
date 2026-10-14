package ingestion

import (
	"testing"
	"time"
)

const statementTransactionID = "card-1"

func TestStatementObservationsKeepIndependentReferences(t *testing.T) {
	t.Parallel()
	result := Verification{Statements: []StatementEntry{
		{Row: 2, TransactionID: statementTransactionID, Merchant: "店舗", Amount: 100, OccurredAt: "2026-10-12T00:00:00+09:00"},
		{Row: 3, TransactionID: statementTransactionID, Merchant: "店舗", Amount: 100, OccurredAt: "2026-10-12T00:00:00+09:00"},
	}}
	observations := SourceObservations("document", "hash", "card", result)
	if len(observations) != 2 || observations[0].SourceReference != testStatementRow2Reference || observations[1].SourceReference != testStatementRow3Reference {
		t.Fatalf("statement references lost: %#v", observations)
	}
	if observations[0].TransactionID != statementTransactionID || observations[1].TransactionID != statementTransactionID {
		t.Fatalf("transaction identifiers were not retained for matching: %#v", observations)
	}
	matched := Reconcile(Observation{DocumentID: "new", TransactionID: statementTransactionID, Merchant: "店舗", Amount: 100, OccurredAt: observations[0].OccurredAt}, observations)
	if len(matched) != 2 || matched[0].SourceReference != testStatementRow2Reference || matched[1].SourceReference != testStatementRow3Reference || matched[0].State != "IDENTICAL" || matched[1].State != "IDENTICAL" {
		t.Fatalf("same-transaction rows were not preserved as separate match candidates: %#v", matched)
	}
	if !observations[0].OccurredAt.Equal(time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("statement timezone lost: %v", observations[0].OccurredAt)
	}
}

func TestReconcile(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name          string
		incoming, old Observation
		state         string
	}{
		{"同原本", Observation{Hash: "abc"}, Observation{Hash: "abc"}, "DUPLICATE"},
		{"分割発送", Observation{OrderID: "order", Amount: 100}, Observation{OrderID: "order", Amount: 200}, statementMatchPossible},
		{"同額でも別購入を確定統合しない", Observation{Merchant: "店", Amount: 100, OccurredAt: now}, Observation{Merchant: "店", Amount: 100, OccurredAt: now}, statementMatchPossible},
		{"銀行明細とカード利用を直接照合しない", Observation{Merchant: "店", Amount: 100, OccurredAt: now, SourceKind: statementSourceBank, TransactionKind: statementKindPayment}, Observation{Merchant: "店", Amount: 100, OccurredAt: now, SourceKind: statementSourceCard, TransactionKind: statementKindCardPurchase}, ""},
		{"取引番号が一致し店舗情報が欠ける", Observation{TransactionID: "tx", Amount: 100}, Observation{TransactionID: "tx", Merchant: "店", Amount: 100}, "IDENTICAL"},
		{"取引番号が一致して店舗が異なる", Observation{TransactionID: "tx", Merchant: "別の店", Amount: 100}, Observation{TransactionID: "tx", Merchant: "店", Amount: 100}, "CONFLICT"},
		{"取引番号の金額相違", Observation{Merchant: "店", TransactionID: "tx", Amount: 100}, Observation{Merchant: "店", TransactionID: "tx", Amount: 200}, "CONFLICT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
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

func TestReconcileReportsMerchantConflict(t *testing.T) {
	t.Parallel()
	incoming := Observation{TransactionID: "tx", Merchant: "別の店", Amount: 100}
	existing := Observation{TransactionID: "tx", Merchant: "店", Amount: 100}

	got := Reconcile(incoming, []Observation{existing})
	if len(got) != 1 || got[0].State != "CONFLICT" {
		t.Fatalf("got %v, want one conflict", got)
	}
	for _, reason := range got[0].Reasons {
		if reason == "店舗名相違" {
			return
		}
	}
	t.Fatalf("got reasons %v, want 店舗名相違", got[0].Reasons)
}

func TestReconcileBankTransactionsOnlyAgainstReceipts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	receipt := Observation{DocumentID: "receipt", SourceKind: "receipt", TransactionKind: statementKindReceipt, Merchant: "スーパー", Amount: 1000, OccurredAt: now}
	for _, kind := range []string{statementKindPayment, statementKindCardBillPayment, statementKindAccountTransfer, statementKindRefund, statementKindUnclassified} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			bank := Observation{DocumentID: "statement", SourceKind: statementSourceBank, TransactionKind: kind, Merchant: "スーパー", Amount: 1000, OccurredAt: now}
			got := Reconcile(bank, []Observation{receipt})
			if kind == statementKindPayment {
				if len(got) != 1 || got[0].State != statementMatchPossible {
					t.Fatalf("purchase payment was not retained as a receipt candidate: %#v", got)
				}
				return
			}
			if len(got) != 0 {
				t.Fatalf("non-purchase bank transaction became a receipt candidate: %#v", got)
			}
		})
	}
}
