package ingestion

import "testing"

func TestInterpretationsKeepAmbiguousObservations(t *testing.T) {
	r := Receipt{
		Currency: "JPY", Total: 108,
		Items:     []Item{{PrintedName: "A", PrintedAmount: 108, PriceBasis: "UNKNOWN", TaxGroup: "tax"}},
		TaxGroups: []TaxGroup{{ID: "tax", Numerator: 8, Denominator: 100, Basis: "UNKNOWN", Rounding: "UNKNOWN", PrintedTax: new(int64(8))}},
	}
	for _, rounding := range []string{"FLOOR", "CEIL"} {
		r.Interpretations = append(r.Interpretations, TaxInterpretation{ID: rounding,
			Items:  []ItemInterpretation{{Index: 0, PriceBasis: "TAX_INCLUDED", TaxGroup: "tax"}},
			Groups: []GroupInterpretation{{ID: "tax", Basis: "TAX_INCLUDED", Rounding: rounding}},
		})
	}
	r.Interpretations = append(r.Interpretations, TaxInterpretation{ID: "excluded",
		Items:  []ItemInterpretation{{Index: 0, PriceBasis: "TAX_EXCLUDED", TaxGroup: "tax"}},
		Groups: []GroupInterpretation{{ID: "tax", Basis: "TAX_EXCLUDED", Rounding: "FLOOR"}},
	})
	got, err := Verify(r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Balanced || got.Receipt.Items[0].PriceBasis != "UNKNOWN" || len(got.Candidates) != 3 {
		t.Fatalf("observation was finalized: %+v", got)
	}
	if !got.Candidates[0].Consistent || !got.Candidates[1].Consistent || got.Candidates[2].Consistent {
		t.Fatalf("candidate constraints not preserved: %+v", got.Candidates)
	}
	if r.Items[0].PriceBasis != "UNKNOWN" || r.TaxGroups[0].Rounding != "UNKNOWN" {
		t.Fatal("input observations changed")
	}
}

func TestInterpretationRejectsChangingExplicitFacts(t *testing.T) {
	r := Receipt{Currency: "JPY", Total: 100,
		Items:           []Item{{PrintedName: "A", PrintedAmount: 100, PriceBasis: "TAX_INCLUDED"}},
		Interpretations: []TaxInterpretation{{ID: "changed", Items: []ItemInterpretation{{Index: 0, PriceBasis: "EXEMPT"}}}},
	}
	got, err := Verify(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Candidates) != 1 || got.Candidates[0].Consistent || len(got.Candidates[0].Issues) == 0 {
		t.Fatalf("explicit fact override accepted: %+v", got.Candidates)
	}
}
