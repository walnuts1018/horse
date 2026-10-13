package graph

import (
	"testing"

	"github.com/walnuts1018/horse/internal/graph/model"
)

func TestPurchaseItemMeasureKeepsUnknownAndExactUnitPrices(t *testing.T) {
	t.Parallel()
	three, two := "3", "2"
	unit := "個"
	measure, err := newPurchaseItemMeasure(&three, &two, &unit, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	numerator, denominator := effectiveUnitPrice(100, measure)
	if numerator == nil || denominator == nil || *numerator != "200" || *denominator != "3" {
		t.Fatalf("effective unit price must remain an exact rational: %v/%v", numerator, denominator)
	}

	unknown, err := newPurchaseItemMeasure(nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	numerator, denominator = effectiveUnitPrice(500, unknown)
	if numerator != nil || denominator != nil {
		t.Fatalf("unknown quantity must not produce a unit price: %v/%v", numerator, denominator)
	}
	numerator, denominator = effectiveUnitPrice(-100, measure)
	if numerator != nil || denominator != nil {
		t.Fatalf("refund amounts must not produce a negative market unit price: %v/%v", numerator, denominator)
	}
}

func TestPurchaseItemMeasureRejectsPartialAndNoncanonicalRationals(t *testing.T) {
	t.Parallel()
	three, zero, two := "3", "0", "2"
	unit := "kg"
	price := model.Long(240)
	printedUnit := "kg"
	cases := []struct {
		name                    string
		quantityNumerator       *string
		quantityDenominator     *string
		quantityUnit            *string
		printedPrice            *model.Long
		printedBasisNumerator   *string
		printedBasisDenominator *string
		printedUnit             *string
	}{
		{name: "quantity missing denominator", quantityNumerator: &three, quantityUnit: &unit},
		{name: "zero quantity", quantityNumerator: &zero, quantityDenominator: &two, quantityUnit: &unit},
		{name: "negative quantity", quantityNumerator: new("-1"), quantityDenominator: &two, quantityUnit: &unit},
		{name: "quantity numerator above limit", quantityNumerator: new("1000000000001"), quantityDenominator: new("1"), quantityUnit: &unit},
		{name: "quantity missing unit", quantityNumerator: &three, quantityDenominator: &two},
		{name: "unreduced quantity", quantityNumerator: &two, quantityDenominator: &two, quantityUnit: &unit},
		{name: "noncanonical quantity integer", quantityNumerator: new("03"), quantityDenominator: &two, quantityUnit: &unit},
		{name: "printed price missing basis", printedPrice: &price},
		{name: "printed price missing unit", printedPrice: &price, printedBasisNumerator: &three, printedBasisDenominator: &two},
		{name: "unreduced printed basis", printedPrice: &price, printedBasisNumerator: &two, printedBasisDenominator: &two, printedUnit: &printedUnit},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := newPurchaseItemMeasure(test.quantityNumerator, test.quantityDenominator, test.quantityUnit,
				test.printedPrice, test.printedBasisNumerator, test.printedBasisDenominator, test.printedUnit)
			if err == nil {
				t.Fatal("incomplete or invalid observation was accepted")
			}
		})
	}
}
