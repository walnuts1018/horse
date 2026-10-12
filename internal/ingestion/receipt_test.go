package ingestion

import (
	"reflect"
	"testing"
)

func TestDistribute(t *testing.T) {
	for _, tc := range []struct {
		name    string
		amount  int64
		weights []int64
		want    []int64
	}{
		{"3対2", 500, []int64{3, 2}, []int64{300, 200}},
		{"端数", 101, []int64{1, 1, 1}, []int64{34, 34, 33}},
		{"返品", -101, []int64{1, 1, 1}, []int64{-34, -34, -33}},
		{"乗算オーバーフロー防止", MaxAmount, []int64{MaxAmount, MaxAmount}, []int64{MaxAmount / 2, MaxAmount / 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Distribute(tc.amount, tc.weights)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestVerifyReceipt(t *testing.T) {
	for _, tc := range []struct {
		name     string
		receipt  Receipt
		total    int64
		balanced bool
		amounts  []int64
	}{
		{"税率混在", Receipt{Currency: "JPY", Total: 328, Items: []Item{{PrintedName: "A", PrintedAmount: 100, PriceBasis: "TAX_EXCLUDED", TaxGroup: "reduced"}, {PrintedName: "B", PrintedAmount: 200, PriceBasis: "TAX_EXCLUDED", TaxGroup: "standard"}}, TaxGroups: []TaxGroup{{ID: "reduced", Numerator: 8, Denominator: 100, Rounding: "FLOOR"}, {ID: "standard", Numerator: 10, Denominator: 100, Rounding: "FLOOR"}}}, 328, true, []int64{108, 220}},
		{"税額をグループで丸めて配賦", Receipt{Currency: "JPY", Total: 111, Items: []Item{{PrintedName: "A", PrintedAmount: 51, PriceBasis: "TAX_EXCLUDED", TaxGroup: "tax"}, {PrintedName: "B", PrintedAmount: 50, PriceBasis: "TAX_EXCLUDED", TaxGroup: "tax"}}, TaxGroups: []TaxGroup{{ID: "tax", Numerator: 10, Denominator: 100, Rounding: "FLOOR"}}}, 111, true, []int64{56, 55}},
		{"ポイント払いは値引きではない", Receipt{Currency: "JPY", Total: 300, Items: []Item{{PrintedName: "A", PrintedAmount: 300, PriceBasis: "TAX_INCLUDED"}}, Payments: []Payment{{Method: "points", Amount: 100}, {Method: "card", Amount: 200}}}, 300, true, []int64{300}},
		{"税込区分不明は合計一致でも要確認", Receipt{Currency: "JPY", Total: 100, Items: []Item{{PrintedName: "A", PrintedAmount: 100, PriceBasis: "UNKNOWN"}}}, 100, false, []int64{100}},
		{"全体値引き", Receipt{Currency: "JPY", Total: 199, Discount: -1, Items: []Item{{PrintedName: "A", PrintedAmount: 100, PriceBasis: "TAX_INCLUDED"}, {PrintedName: "B", PrintedAmount: 100, PriceBasis: "TAX_INCLUDED"}}}, 199, true, []int64{99, 100}},
		{"返金", Receipt{Currency: "JPY", Total: -108, Items: []Item{{PrintedName: "A", PrintedAmount: -100, PriceBasis: "TAX_EXCLUDED", TaxGroup: "tax"}}, TaxGroups: []TaxGroup{{ID: "tax", Numerator: 8, Denominator: 100, Rounding: "FLOOR"}}}, -108, true, []int64{-108}},
		{"内税額の整合", Receipt{Currency: "JPY", Total: 108, Items: []Item{{PrintedName: "A", PrintedAmount: 108, PriceBasis: "TAX_INCLUDED", TaxGroup: "tax"}}, TaxGroups: []TaxGroup{{ID: "tax", Numerator: 8, Denominator: 100, Basis: "TAX_INCLUDED", Rounding: "FLOOR", PrintedTax: new(int64(8)), PrintedSubtotal: new(int64(108))}}}, 108, true, []int64{108}},
		{"合計が一致しても内税額の矛盾を検出", Receipt{Currency: "JPY", Total: 108, Items: []Item{{PrintedName: "A", PrintedAmount: 108, PriceBasis: "TAX_INCLUDED", TaxGroup: "tax"}}, TaxGroups: []TaxGroup{{ID: "tax", Numerator: 8, Denominator: 100, Basis: "TAX_INCLUDED", Rounding: "FLOOR", PrintedTax: new(int64(10))}}}, 108, false, []int64{108}},
		{"税率グループの区分矛盾", Receipt{Currency: "JPY", Total: 108, Items: []Item{{PrintedName: "A", PrintedAmount: 100, PriceBasis: "TAX_EXCLUDED", TaxGroup: "tax"}}, TaxGroups: []TaxGroup{{ID: "tax", Numerator: 8, Denominator: 100, Basis: "TAX_INCLUDED", Rounding: "FLOOR"}}}, 108, false, []int64{108}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Verify(tc.receipt)
			if err != nil {
				t.Fatal(err)
			}
			if got.ComputedTotal != tc.total || got.Balanced != tc.balanced {
				t.Fatalf("got total=%d balanced=%v issues=%v", got.ComputedTotal, got.Balanced, got.Issues)
			}
			for i, want := range tc.amounts {
				if got.Receipt.Items[i].GrossAmount != want {
					t.Errorf("item %d: got %d want %d", i, got.Receipt.Items[i].GrossAmount, want)
				}
			}
		})
	}
}

func TestVerifyKeepsQuantityAndPrintedUnitPriceSeparate(t *testing.T) {
	quantityNumerator, quantityDenominator := int64(2), int64(1)
	printedPrice, printedBasisNumerator, printedBasisDenominator := int64(130), int64(1), int64(1)
	verification, err := Verify(Receipt{Currency: "JPY", Total: 240, Items: []Item{{
		PrintedName: "小麦粉", PrintedAmount: 240, PriceBasis: "TAX_INCLUDED",
		QuantityNumerator: &quantityNumerator, QuantityDenominator: &quantityDenominator, QuantityUnit: "kg",
		PrintedUnitPriceYen: &printedPrice, PrintedUnitPriceQuantityNumerator: &printedBasisNumerator,
		PrintedUnitPriceQuantityDenominator: &printedBasisDenominator, PrintedUnitPriceUnit: "kg",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	item := verification.Receipt.Items[0]
	if item.QuantityNumerator == nil || *item.QuantityNumerator != 2 || item.QuantityUnit != "kg" || item.GrossAmount != 240 || item.PrintedUnitPriceYen == nil || *item.PrintedUnitPriceYen != 130 {
		t.Fatalf("verification changed or conflated observed quantity and printed price: %#v", item)
	}
	if _, err := Verify(Receipt{Currency: "JPY", Total: 240, Items: []Item{{
		PrintedName: "小麦粉", PrintedAmount: 240, PriceBasis: "TAX_INCLUDED", QuantityNumerator: &quantityNumerator, QuantityUnit: "kg",
	}}}); err == nil {
		t.Fatal("partial quantity observation was accepted")
	}
	unreducedNumerator, unreducedDenominator := int64(2), int64(2)
	if _, err := Verify(Receipt{Currency: "JPY", Total: 240, Items: []Item{{
		PrintedName: "小麦粉", PrintedAmount: 240, PriceBasis: "TAX_INCLUDED", QuantityNumerator: &unreducedNumerator,
		QuantityDenominator: &unreducedDenominator, QuantityUnit: "kg",
	}}}); err == nil {
		t.Fatal("unreduced quantity rational was accepted")
	}
}

func FuzzDistributeConservation(f *testing.F) {
	f.Add(int64(101), uint32(3), uint32(2))
	f.Fuzz(func(t *testing.T, amount int64, a, b uint32) {
		amount %= MaxAmount
		weights := []int64{int64(a) + 1, int64(b) + 1}
		result, err := Distribute(amount, weights)
		if err != nil {
			t.Fatal(err)
		}
		if result[0]+result[1] != amount {
			t.Fatalf("allocation lost money: %v amount %d", result, amount)
		}
	})
}
