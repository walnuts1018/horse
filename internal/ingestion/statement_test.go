package ingestion

import "testing"

const (
	testStatementRow2Reference = "row:2"
	testStatementRow3Reference = "row:3"
	testStatementMerchant      = "Market"
)

func TestStatementCSV(t *testing.T) {
	t.Parallel()
	data := []byte("\ufeff利用日,利用店名,利用金額,取引番号,取引種別\n2026/10/12,お店,\"1,234円\",tx1,商品購入\n2026/10/13,返金,-100,tx2,返金\n")
	for _, kind := range []string{statementSourceCard, statementSourceBank} {
		entries, err := ParseStatementCSV(data, kind)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 || entries[0].Amount != 1234 || entries[1].Amount != -100 || entries[0].TransactionID != "tx1" {
			t.Fatalf("unexpected statement: %+v", entries)
		}
		want := []string{statementKindCardPurchase, statementKindCardPurchase}
		if kind == statementSourceBank {
			want = []string{statementKindPayment, statementKindRefund}
		}
		for i, entry := range entries {
			if entry.Kind != want[i] {
				t.Fatalf("got kind %s want %s", entry.Kind, want[i])
			}
		}
	}
	unknown, err := ParseStatementCSV([]byte("date,merchant,amount,type\n2026-10-12,店,100,不明な取引\n"), statementSourceBank)
	if err != nil || len(unknown) != 1 || unknown[0].Kind != statementKindUnclassified {
		t.Fatalf("unclassified bank transaction was not preserved: entries=%+v err=%v", unknown, err)
	}
	for _, data := range []string{"date,merchant,amount\n2026-10-12,店,1.5\n", "date,merchant,amount\n不明,店,100\n", "date,merchant\n2026-10-12,店\n"} {
		if _, err := ParseStatementCSV([]byte(data), "card"); err == nil {
			t.Fatalf("invalid statement accepted: %s", data)
		}
	}
}

func TestStatementSourceReferencesAreStableAndUnique(t *testing.T) {
	t.Parallel()
	data := []byte("date,merchant,amount,transactionId\n2026-10-12,店,100,duplicate\n2026-10-12,店,100,duplicate\n")
	first, err := ParseStatementCSV(data, statementSourceCard)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ParseStatementCSV(data, statementSourceCard)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].SourceReference != testStatementRow2Reference || first[1].SourceReference != testStatementRow3Reference {
		t.Fatalf("duplicate transaction IDs did not receive unique row references: %+v", first)
	}
	if first[0].SourceReference != second[0].SourceReference || first[1].SourceReference != second[1].SourceReference {
		t.Fatalf("reparsing changed row references: first=%+v second=%+v", first, second)
	}
	observations := SourceObservations("document", "hash", statementSourceCard, Verification{Statements: first})
	if len(observations) != 2 || observations[0].SourceReference != first[0].SourceReference || observations[1].SourceReference != first[1].SourceReference {
		t.Fatalf("reconciliation references differed from parsed references: %+v", observations)
	}
}

func TestVerifyStatementsPreservesSignedTransactionsAndFailsClosed(t *testing.T) {
	t.Parallel()
	region := Region{Page: 1, Top: 0.1, Left: 0.1, Bottom: 0.2, Right: 0.9}
	entry := StatementEntry{Kind: statementKindCardPurchase, OccurredAt: "2026-10-12T12:00:00+09:00", Merchant: testStatementMerchant, Amount: -1234, Region: &region}
	verified, err := VerifyStatements(statementSourceCard, []StatementEntry{entry}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(verified.Statements) != 1 || verified.Statements[0].Amount != -1234 || verified.Statements[0].Row != 1 || verified.Statements[0].SourceReference != "row:1" {
		t.Fatalf("verification changed transaction evidence: %+v", verified.Statements)
	}
	if _, err := VerifyStatements(statementSourceCard, []StatementEntry{entry, entry}, nil); err == nil {
		t.Fatal("overlapping transaction regions were accepted")
	}
	for _, invalid := range []StatementEntry{
		{Kind: statementKindCardPurchase, OccurredAt: entry.OccurredAt, Merchant: entry.Merchant, Amount: entry.Amount},
		{Kind: statementKindCardPurchase, OccurredAt: "2026-10-12", Merchant: entry.Merchant, Amount: entry.Amount, Region: &region},
		{Kind: "ACCOUNT_TRANSFER", OccurredAt: entry.OccurredAt, Merchant: entry.Merchant, Amount: entry.Amount, Region: &region},
	} {
		if _, err := VerifyStatements(statementSourceCard, []StatementEntry{invalid}, nil); err == nil {
			t.Fatalf("invalid transaction accepted: %+v", invalid)
		}
	}
}
