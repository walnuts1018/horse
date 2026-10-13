package accounting

import (
	"math"
	"testing"

	gofrsuuid "github.com/gofrs/uuid/v5"
)

func TestBuildEntriesPreservesSidesAndRejectsUnbalancedPostings(t *testing.T) {
	t.Parallel()
	poolID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	accountID, err := gofrsuuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := BuildEntries([]Posting{
		{Side: Debit, PoolID: &poolID, AmountYen: 70, Memo: "allocation"},
		{Side: Debit, PoolID: &accountID, AmountYen: 30, Memo: "allocation"},
		{Side: Credit, AccountID: &accountID, AmountYen: 80, Memo: "payment"},
		{Side: Credit, AmountYen: 20, Memo: "payment"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 || entries[0].LineNo != 1 || entries[3].LineNo != 4 || entries[0].DebitYen != 70 || entries[2].CreditYen != 80 || entries[3].CreditYen != 20 {
		t.Fatalf("journal entries lost their line order or debit/credit side: %#v", entries)
	}
	if !entries[0].PoolID.Valid || entries[0].PoolID.V != poolID || !entries[2].AccountID.Valid || entries[2].AccountID.V != accountID || entries[3].AccountID.Valid {
		t.Fatalf("journal entry account and pool references were not preserved: %#v", entries)
	}
	refund, err := BuildEntries([]Posting{
		{Side: Credit, PoolID: &poolID, AmountYen: 60, Memo: "refund"},
		{Side: Debit, AccountID: &accountID, AmountYen: 60, Memo: "refund"},
	})
	if err != nil || refund[0].CreditYen != 60 || refund[1].DebitYen != 60 {
		t.Fatalf("refund postings should credit the pool and debit the payment account: entries=%#v error=%v", refund, err)
	}

	for _, postings := range [][]Posting{
		{{Side: Debit, AmountYen: 1}},
		{{Side: Debit, AmountYen: 1}, {Side: Credit, AmountYen: 2}},
		{{Side: Debit, AmountYen: 0}, {Side: Credit, AmountYen: 0}},
	} {
		if _, err := BuildEntries(postings); err == nil {
			t.Fatalf("invalid journal postings should fail: %#v", postings)
		}
	}
}

func TestBuildPurchaseEntries(t *testing.T) {
	t.Parallel()
	poolA, err := gofrsuuid.FromString("00000000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	poolB, err := gofrsuuid.FromString("00000000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	accountA, err := gofrsuuid.FromString("00000000-0000-7000-8000-000000000003")
	if err != nil {
		t.Fatal(err)
	}
	accountB, err := gofrsuuid.FromString("00000000-0000-7000-8000-000000000004")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("pool allocations are grouped and payments stay in input order", func(t *testing.T) {
		t.Parallel()
		entries, err := BuildPurchaseEntries(
			[]PurchaseAllocation{
				{PoolID: poolB, AmountYen: 30},
				{PoolID: poolA, AmountYen: 50},
				{PoolID: poolB, AmountYen: 20},
			},
			[]PurchasePayment{{AccountID: accountA, AmountYen: 60}, {AccountID: accountB, AmountYen: 40}},
			100,
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 4 {
			t.Fatalf("BuildPurchaseEntries returned %d entries, want 4", len(entries))
		}
		if entries[0].PoolID.V != poolA || entries[0].DebitYen != 50 || entries[1].PoolID.V != poolB || entries[1].DebitYen != 50 {
			t.Fatalf("pool postings are not grouped in stable order: %#v", entries[:2])
		}
		if entries[2].AccountID.V != accountA || entries[2].CreditYen != 60 || entries[3].AccountID.V != accountB || entries[3].CreditYen != 40 {
			t.Fatalf("payment postings lost their order or amounts: %#v", entries[2:])
		}
	})

	t.Run("purchase without payment accounts uses an unattributed credit", func(t *testing.T) {
		t.Parallel()
		entries, err := BuildPurchaseEntries([]PurchaseAllocation{{PoolID: poolA, AmountYen: 100}}, nil, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 || entries[0].DebitYen != 100 || entries[1].CreditYen != 100 || entries[1].AccountID.Valid {
			t.Fatalf("unattributed payment was not preserved: %#v", entries)
		}
	})

	t.Run("invalid totals are rejected", func(t *testing.T) {
		t.Parallel()
		for _, input := range []struct {
			name        string
			allocations []PurchaseAllocation
			payments    []PurchasePayment
			totalYen    int64
		}{
			{name: "nonpositive total", allocations: []PurchaseAllocation{{PoolID: poolA, AmountYen: 1}}, totalYen: 0},
			{name: "unbalanced", allocations: []PurchaseAllocation{{PoolID: poolA, AmountYen: 90}}, totalYen: 100},
			{name: "payment total mismatch", allocations: []PurchaseAllocation{{PoolID: poolA, AmountYen: 90}}, payments: []PurchasePayment{{AccountID: accountA, AmountYen: 90}}, totalYen: 100},
			{name: "allocation overflow", allocations: []PurchaseAllocation{{PoolID: poolA, AmountYen: math.MaxInt64}, {PoolID: poolA, AmountYen: 1}}, totalYen: 100},
		} {
			t.Run(input.name, func(t *testing.T) {
				t.Parallel()
				if _, err := BuildPurchaseEntries(input.allocations, input.payments, input.totalYen); err == nil {
					t.Fatal("BuildPurchaseEntries accepted invalid purchase entries")
				}
			})
		}
	})
}
