package accounting

import (
	"testing"

	gofrsuuid "github.com/gofrs/uuid/v5"
)

func TestBuildEntriesPreservesSidesAndRejectsUnbalancedPostings(t *testing.T) {
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
