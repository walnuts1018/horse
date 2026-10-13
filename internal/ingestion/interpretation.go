package ingestion

import "slices"

// TaxInterpretationは印字された金額や税率を変更せず、未確定の計算区分だけを候補として保持する。
type TaxInterpretation struct {
	ID     string                `json:"id"`
	Items  []ItemInterpretation  `json:"items"`
	Groups []GroupInterpretation `json:"groups"`
}

type ItemInterpretation struct {
	Index      int    `json:"index"`
	PriceBasis string `json:"priceBasis"`
	TaxGroup   string `json:"taxGroup"`
}

type GroupInterpretation struct {
	ID       string `json:"id"`
	Basis    string `json:"basis"`
	Rounding string `json:"rounding"`
}

type InterpretationResult struct {
	ID            string   `json:"id"`
	Amounts       []int64  `json:"amounts"`
	ComputedTotal int64    `json:"computedTotal"`
	Consistent    bool     `json:"consistent"`
	Issues        []string `json:"issues"`
}

// 成立した候補も観測事実の確定値へ上書きしない。合計が等しい複数の計算方式を保持する。
func evaluateInterpretations(observed Receipt) []InterpretationResult {
	if len(observed.Interpretations) == 0 {
		return nil
	}
	if len(observed.Interpretations) > 16 {
		return []InterpretationResult{{Issues: []string{"税解釈候補が上限を超えています。"}}}
	}
	results := make([]InterpretationResult, 0, len(observed.Interpretations))
	seenIDs := map[string]bool{}
	for _, candidate := range observed.Interpretations {
		result := InterpretationResult{ID: candidate.ID}
		r := observed
		r.Interpretations = nil
		r.Items = slices.Clone(observed.Items)
		r.TaxGroups = slices.Clone(observed.TaxGroups)
		valid := candidate.ID != "" && len(candidate.ID) <= 128 && !seenIDs[candidate.ID] && len(candidate.Items) <= len(r.Items) && len(candidate.Groups) <= len(r.TaxGroups)
		seenIDs[candidate.ID] = true
		seenItems := map[int]bool{}
		for _, item := range candidate.Items {
			if item.Index < 0 || item.Index >= len(r.Items) || seenItems[item.Index] {
				valid = false
				break
			}
			seenItems[item.Index] = true
			original := r.Items[item.Index]
			if !canInterpret(original.PriceBasis, item.PriceBasis) || original.TaxGroup != "" && original.TaxGroup != item.TaxGroup || item.PriceBasis != priceBasisTaxIncluded && item.PriceBasis != priceBasisTaxExcluded && item.PriceBasis != "EXEMPT" && item.PriceBasis != "NON_TAXABLE" {
				valid = false
				break
			}
			r.Items[item.Index].PriceBasis = item.PriceBasis
			r.Items[item.Index].TaxGroup = item.TaxGroup
		}
		seenGroups := map[string]bool{}
		for _, group := range candidate.Groups {
			index := slices.IndexFunc(r.TaxGroups, func(g TaxGroup) bool { return g.ID == group.ID })
			if index < 0 || seenGroups[group.ID] || group.Basis != priceBasisTaxIncluded && group.Basis != priceBasisTaxExcluded || group.Rounding != taxRoundingFloor && group.Rounding != "CEIL" && group.Rounding != "NEAREST" {
				valid = false
				break
			}
			seenGroups[group.ID] = true
			if !canInterpret(r.TaxGroups[index].Basis, group.Basis) || !canInterpret(r.TaxGroups[index].Rounding, group.Rounding) {
				valid = false
				break
			}
			r.TaxGroups[index].Basis = group.Basis
			r.TaxGroups[index].Rounding = group.Rounding
		}
		if !valid {
			result.Issues = []string{"税解釈候補が観測事実と矛盾しています。"}
		} else if verified, err := Verify(r); err != nil {
			result.Issues = []string{"税解釈候補を計算できません。"}
		} else {
			result.ComputedTotal = verified.ComputedTotal
			result.Consistent = verified.Balanced
			result.Issues = verified.Issues
			for _, item := range verified.Receipt.Items {
				result.Amounts = append(result.Amounts, item.GrossAmount)
			}
		}
		results = append(results, result)
	}
	return results
}

func canInterpret(observed, proposed string) bool {
	return observed == "" || observed == priceBasisUnknown || observed == proposed
}
