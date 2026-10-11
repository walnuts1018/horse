package ingestion

import "testing"

func TestStatementCSV(t *testing.T) {
	data := []byte("\ufeff利用日,利用店名,利用金額,取引番号\n2026/10/12,お店,\"1,234円\",tx1\n2026/10/13,返金,-100,tx2\n")
	for _, kind := range []string{"card", "bank"} {
		entries, err := ParseStatementCSV(data, kind)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 || entries[0].Amount != 1234 || entries[1].Amount != -100 || entries[0].TransactionID != "tx1" {
			t.Fatalf("unexpected statement: %+v", entries)
		}
		want := "CARD_PURCHASE"
		if kind == "bank" {
			want = "BANK_MOVEMENT"
		}
		if entries[0].Kind != want {
			t.Fatalf("got kind %s want %s", entries[0].Kind, want)
		}
	}
	for _, data := range []string{"date,merchant,amount\n2026-10-12,店,1.5\n", "date,merchant,amount\n不明,店,100\n", "date,merchant\n2026-10-12,店\n"} {
		if _, err := ParseStatementCSV([]byte(data), "card"); err == nil {
			t.Fatalf("invalid statement accepted: %s", data)
		}
	}
}
