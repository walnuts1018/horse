package ingestion

import "testing"

func TestInterpretationsKeepAmbiguousObservations(t *testing.T) {
	t.Parallel()
	r := Receipt{
		Currency: currencyJPY, Total: 108,
		Items:     []Item{{PrintedName: "A", PrintedAmount: 108, PriceBasis: priceBasisUnknown, TaxGroup: testTaxGroupID}},
		TaxGroups: []TaxGroup{{ID: testTaxGroupID, Numerator: 8, Denominator: 100, Basis: priceBasisUnknown, Rounding: priceBasisUnknown, PrintedTax: new(int64(8))}},
	}
	for _, rounding := range []string{taxRoundingFloor, "CEIL"} {
		r.Interpretations = append(r.Interpretations, TaxInterpretation{ID: rounding,
			Items:  []ItemInterpretation{{Index: 0, PriceBasis: priceBasisTaxIncluded, TaxGroup: testTaxGroupID}},
			Groups: []GroupInterpretation{{ID: testTaxGroupID, Basis: priceBasisTaxIncluded, Rounding: rounding}},
		})
	}
	r.Interpretations = append(r.Interpretations, TaxInterpretation{ID: "excluded",
		Items:  []ItemInterpretation{{Index: 0, PriceBasis: priceBasisTaxExcluded, TaxGroup: testTaxGroupID}},
		Groups: []GroupInterpretation{{ID: testTaxGroupID, Basis: priceBasisTaxExcluded, Rounding: taxRoundingFloor}},
	})
	got, err := Verify(r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Balanced || got.Receipt.Items[0].PriceBasis != priceBasisUnknown || len(got.Candidates) != 3 {
		t.Fatalf("observation was finalized: %+v", got)
	}
	if !got.Candidates[0].Consistent || !got.Candidates[1].Consistent || got.Candidates[2].Consistent {
		t.Fatalf("candidate constraints not preserved: %+v", got.Candidates)
	}
	if len(got.Candidates[0].ItemInterpretations) != 1 || got.Candidates[0].ItemInterpretations[0].PriceBasis != priceBasisTaxIncluded || len(got.Candidates[0].GroupInterpretations) != 1 || got.Candidates[0].GroupInterpretations[0].Rounding != taxRoundingFloor {
		t.Fatalf("candidate interpretation details missing: %+v", got.Candidates[0])
	}
	if len(got.Candidates[2].ItemInterpretations) != 1 {
		t.Fatalf("unbalanced candidate interpretation was omitted: %+v", got.Candidates[2])
	}
	if r.Items[0].PriceBasis != priceBasisUnknown || r.TaxGroups[0].Rounding != priceBasisUnknown {
		t.Fatal("input observations changed")
	}
}

func TestInterpretationRejectsChangingExplicitFacts(t *testing.T) {
	t.Parallel()
	r := Receipt{Currency: currencyJPY, Total: 100,
		Items:           []Item{{PrintedName: "A", PrintedAmount: 100, PriceBasis: priceBasisTaxIncluded}},
		Interpretations: []TaxInterpretation{{ID: "changed", Items: []ItemInterpretation{{Index: 0, PriceBasis: "EXEMPT"}}}},
	}
	got, err := Verify(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Candidates) != 1 || got.Candidates[0].Consistent || len(got.Candidates[0].Issues) == 0 {
		t.Fatalf("explicit fact override accepted: %+v", got.Candidates)
	}
	if len(got.Candidates[0].ItemInterpretations) > 0 || len(got.Candidates[0].GroupInterpretations) > 0 {
		t.Fatalf("invalid candidate details were exposed: %+v", got.Candidates[0])
	}
}
