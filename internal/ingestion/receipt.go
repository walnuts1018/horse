package ingestion

import (
	"cmp"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"
)

const MaxAmount int64 = 1_000_000_000_000
const maxQuantityDenominator int64 = 1_000_000

type Region struct {
	Page   int     `json:"page"`
	Top    float64 `json:"top"`
	Left   float64 `json:"left"`
	Bottom float64 `json:"bottom"`
	Right  float64 `json:"right"`
}

type Item struct {
	PrintedName                         string  `json:"printedName"`
	Name                                string  `json:"name"`
	PrintedAmount                       int64   `json:"printedAmount"`
	Discount                            int64   `json:"discount"`
	PriceBasis                          string  `json:"priceBasis"`
	TaxGroup                            string  `json:"taxGroup"`
	QuantityNumerator                   *int64  `json:"quantityNumerator,omitempty"`
	QuantityDenominator                 *int64  `json:"quantityDenominator,omitempty"`
	QuantityUnit                        string  `json:"quantityUnit,omitempty"`
	PrintedUnitPriceYen                 *int64  `json:"printedUnitPriceYen,omitempty"`
	PrintedUnitPriceQuantityNumerator   *int64  `json:"printedUnitPriceQuantityNumerator,omitempty"`
	PrintedUnitPriceQuantityDenominator *int64  `json:"printedUnitPriceQuantityDenominator,omitempty"`
	PrintedUnitPriceUnit                string  `json:"printedUnitPriceUnit,omitempty"`
	GrossAmount                         int64   `json:"grossAmount"`
	CategoryHint                        string  `json:"categoryHint"`
	Confidence                          float64 `json:"confidence"`
	Region                              *Region `json:"region,omitempty"`
	Verification                        string  `json:"verification"`
}

type TaxGroup struct {
	ID              string `json:"id"`
	Numerator       int64  `json:"numerator"`
	Denominator     int64  `json:"denominator"`
	Basis           string `json:"basis"`
	Rounding        string `json:"rounding"`
	PrintedTax      *int64 `json:"printedTax,omitempty"`
	PrintedSubtotal *int64 `json:"printedSubtotal,omitempty"`
}

type Payment struct {
	Method    string `json:"method"`
	CardLast4 string `json:"cardLast4"`
	Amount    int64  `json:"amount"`
}

type Receipt struct {
	Merchant        string              `json:"merchant"`
	PurchasedAt     string              `json:"purchasedAt"`
	OrderID         string              `json:"orderId"`
	TransactionID   string              `json:"transactionId"`
	Currency        string              `json:"currency"`
	Items           []Item              `json:"items"`
	TaxGroups       []TaxGroup          `json:"taxGroups"`
	Discount        int64               `json:"discount"`
	Total           int64               `json:"total"`
	Payments        []Payment           `json:"payments"`
	Warnings        []string            `json:"warnings"`
	Statements      []StatementEntry    `json:"statements,omitempty"`
	Model           string              `json:"model"`
	PromptVersion   string              `json:"promptVersion"`
	InputTokens     int64               `json:"inputTokens"`
	OutputTokens    int64               `json:"outputTokens"`
	Interpretations []TaxInterpretation `json:"interpretations,omitempty"`
}

type Verification struct {
	Receipt       Receipt                `json:"receipt"`
	Transform     [9]float64             `json:"transform"`
	TransformPage int                    `json:"transformPage"`
	Issues        []string               `json:"issues"`
	ComputedTotal int64                  `json:"computedTotal"`
	Balanced      bool                   `json:"balanced"`
	OCRLines      []OCRLine              `json:"ocrLines"`
	Statements    []StatementEntry       `json:"statements"`
	Runs          []ExtractionRun        `json:"runs"`
	Candidates    []InterpretationResult `json:"candidates"`
}

type ExtractionRun struct {
	Route               string    `json:"route"`
	Project             string    `json:"project"`
	Model               string    `json:"model"`
	PromptVersion       string    `json:"promptVersion"`
	ThinkingLevel       string    `json:"thinkingLevel"`
	ServiceTier         string    `json:"serviceTier"`
	InputTokens         int64     `json:"inputTokens"`
	OutputTokens        int64     `json:"outputTokens"`
	EstimatedJPY        float64   `json:"estimatedCostJPY"`
	PricingCurrency     string    `json:"pricingCurrency"`
	InputUSDPerMillion  float64   `json:"inputUsdPerMillionTokens"`
	OutputUSDPerMillion float64   `json:"outputUsdPerMillionTokens"`
	PriceTableVersion   string    `json:"priceTableVersion"`
	PriceEstimateStatus string    `json:"priceEstimateStatus"`
	USDJPYRate          float64   `json:"usdJpyRate"`
	USDJPYRateSource    string    `json:"usdJpyRateSource"`
	USDJPYRateAsOf      string    `json:"usdJpyRateAsOf"`
	PricedAt            time.Time `json:"pricedAt"`
	LatencyMillis       int64     `json:"latencyMillis"`
	Reason              string    `json:"reason"`
	Status              string    `json:"status"`
	ErrorCode           string    `json:"errorCode,omitempty"`
	Receipt             *Receipt  `json:"receipt,omitempty"`
	Issues              []string  `json:"issues,omitempty"`
}

// Verifyは印字額と解釈を保持したまま税込負担額を導出する。合計一致だけでは曖昧な税解釈を確定しない。
func Verify(receipt Receipt) (Verification, error) {
	r := receipt
	r.Items = slices.Clone(receipt.Items)
	v := Verification{Receipt: r, Transform: identityTransform(), Issues: slices.Clone(r.Warnings)}
	if r.Currency != currencyJPY || len(r.Items) == 0 || len(r.Items) > 1000 || len(r.TaxGroups) > 1000 || len(r.Payments) > 100 || !validAmount(r.Total) || !validAmount(r.Discount) || r.Discount > 0 {
		return v, errors.New("invalid receipt currency, items or amounts")
	}
	groups := map[string]TaxGroup{}
	for _, group := range r.TaxGroups {
		if group.ID == "" || group.Denominator <= 0 || group.Denominator > 1_000_000 || group.Numerator < 0 || group.Numerator > group.Denominator {
			return v, errors.New("invalid tax group")
		}
		if _, exists := groups[group.ID]; exists {
			return v, errors.New("duplicate tax group")
		}
		if group.PrintedTax != nil && !validAmount(*group.PrintedTax) || group.PrintedSubtotal != nil && !validAmount(*group.PrintedSubtotal) {
			return v, errors.New("invalid printed tax group amount")
		}
		groups[group.ID] = group
	}
	indices := map[string][]int{}
	included := map[string][]int{}
	for i, item := range r.Items {
		if item.PrintedName == "" || len(item.PrintedName) > 1024 || len(item.Name) > 1024 || !validAmount(item.PrintedAmount) || !validAmount(item.Discount) || item.Discount > 0 || !validAmount(item.PrintedAmount+item.Discount) || !validItemQuantity(item) || !validPrintedUnitPrice(item) {
			return v, fmt.Errorf("invalid item at index %d", i)
		}
		if item.Region != nil && !validRegion(*item.Region) {
			return v, fmt.Errorf("invalid evidence region at index %d", i)
		}
		if item.Confidence < 0 || item.Confidence > 1 {
			return v, fmt.Errorf("invalid confidence at index %d", i)
		}
		v.Receipt.Items[i].GrossAmount = item.PrintedAmount + item.Discount
		v.Receipt.Items[i].Verification = "DERIVED"
		switch item.PriceBasis {
		case priceBasisTaxIncluded:
			if item.TaxGroup != "" {
				if _, exists := groups[item.TaxGroup]; !exists {
					v.Issues = append(v.Issues, fmt.Sprintf("品目%dの税率が未確定です。", i+1))
				} else {
					included[item.TaxGroup] = append(included[item.TaxGroup], i)
				}
			}
		case "EXEMPT", "NON_TAXABLE":
		case priceBasisTaxExcluded:
			if _, exists := groups[item.TaxGroup]; !exists {
				v.Issues = append(v.Issues, fmt.Sprintf("品目%dの税率が未確定です。", i+1))
				v.Receipt.Items[i].Verification = "UNRESOLVED"
			} else {
				indices[item.TaxGroup] = append(indices[item.TaxGroup], i)
			}
		default:
			v.Issues = append(v.Issues, fmt.Sprintf("品目%dの税込・税抜区分が未確定です。", i+1))
			v.Receipt.Items[i].Verification = "UNRESOLVED"
		}
	}
	groupIDs := make([]string, 0, len(groups))
	for id := range groups {
		groupIDs = append(groupIDs, id)
	}
	slices.Sort(groupIDs)
	for _, id := range groupIDs {
		group := groups[id]
		positions := indices[id]
		if len(included[id]) > 0 {
			if len(positions) > 0 || group.Basis != "" && group.Basis != priceBasisTaxIncluded {
				v.Issues = append(v.Issues, "税率グループの税込・税抜区分が一致しません。")
			}
			var subtotal int64
			for _, pos := range included[id] {
				subtotal += v.Receipt.Items[pos].GrossAmount
			}
			if !validAmount(subtotal) {
				return v, errors.New("tax subtotal exceeds limit")
			}
			if group.PrintedSubtotal != nil && *group.PrintedSubtotal != subtotal {
				v.Issues = append(v.Issues, "税込の税率別小計が一致しません。")
			}
			// 内税は税込小計から抽出するため、税率の分母に分子を加える。
			inclusiveRate := group
			inclusiveRate.Denominator += group.Numerator
			tax, known := roundedTax(subtotal, inclusiveRate)
			if group.PrintedTax != nil {
				if known && tax != *group.PrintedTax {
					v.Issues = append(v.Issues, "印字内税額と計算内税額が一致しません。")
				} else if !known {
					v.Issues = append(v.Issues, "内税の端数処理が未確定です。")
				}
			}
		}
		if len(positions) == 0 {
			if len(included[id]) == 0 && (group.PrintedSubtotal != nil || group.PrintedTax != nil) {
				v.Issues = append(v.Issues, "印字税率グループに対応する品目がありません。")
			}
			continue
		}
		if group.Basis != "" && group.Basis != priceBasisTaxExcluded {
			v.Issues = append(v.Issues, "税率グループの税込・税抜区分が一致しません。")
		}
		var subtotal int64
		weights := make([]int64, len(positions))
		for j, pos := range positions {
			amount := v.Receipt.Items[pos].GrossAmount
			subtotal += amount
			weights[j] = abs(amount)
		}
		if !validAmount(subtotal) {
			return v, errors.New("tax subtotal exceeds limit")
		}
		if group.PrintedSubtotal != nil && *group.PrintedSubtotal != subtotal {
			v.Issues = append(v.Issues, "税率別小計が一致しません。")
		}
		tax, known := roundedTax(subtotal, group)
		if group.PrintedTax != nil {
			if !validAmount(*group.PrintedTax) {
				return v, errors.New("invalid printed tax")
			}
			if known && tax != *group.PrintedTax {
				v.Issues = append(v.Issues, "印字税額と計算税額が一致しません。")
			}
			tax = *group.PrintedTax
		} else if !known {
			v.Issues = append(v.Issues, "税の端数処理が未確定です。")
			continue
		}
		if len(positions) > 1 && slices.ContainsFunc(positions, func(pos int) bool { return v.Receipt.Items[pos].GrossAmount < 0 }) && slices.ContainsFunc(positions, func(pos int) bool { return v.Receipt.Items[pos].GrossAmount > 0 }) {
			v.Issues = append(v.Issues, "購入と返品が混在する税額の配分を確認してください。")
			continue
		}
		shares, err := Distribute(tax, weights)
		if err != nil {
			return v, err
		}
		for j, pos := range positions {
			v.Receipt.Items[pos].GrossAmount += shares[j]
		}
	}
	if r.Discount != 0 {
		weights := make([]int64, len(r.Items))
		for i, item := range v.Receipt.Items {
			if item.GrossAmount < 0 {
				return v, errors.New("receipt discount cannot be applied to a return")
			}
			weights[i] = item.GrossAmount
		}
		shares, err := Distribute(r.Discount, weights)
		if err != nil {
			return v, err
		}
		for i := range v.Receipt.Items {
			v.Receipt.Items[i].GrossAmount += shares[i]
		}
	}
	for _, item := range v.Receipt.Items {
		v.ComputedTotal += item.GrossAmount
	}
	if !validAmount(v.ComputedTotal) {
		return v, errors.New("receipt total exceeds limit")
	}
	if v.ComputedTotal != r.Total {
		v.Issues = append(v.Issues, "品目の合計と印字合計が一致しません。")
	}
	if len(r.Payments) > 0 {
		var paid int64
		for _, payment := range r.Payments {
			if !validAmount(payment.Amount) {
				return v, errors.New("invalid payment amount")
			}
			paid += payment.Amount
		}
		if paid != r.Total {
			v.Issues = append(v.Issues, "決済額の合計と印字合計が一致しません。")
		}
	}
	v.Balanced = len(v.Issues) == 0
	v.Candidates = evaluateInterpretations(r)
	return v, nil
}

func validItemQuantity(item Item) bool {
	if item.QuantityNumerator == nil && item.QuantityDenominator == nil && item.QuantityUnit == "" {
		return true
	}
	return validReceiptRational(item.QuantityNumerator, item.QuantityDenominator) &&
		strings.TrimSpace(item.QuantityUnit) != "" && len(item.QuantityUnit) <= 32
}

func validPrintedUnitPrice(item Item) bool {
	if item.PrintedUnitPriceYen == nil && item.PrintedUnitPriceQuantityNumerator == nil && item.PrintedUnitPriceQuantityDenominator == nil && item.PrintedUnitPriceUnit == "" {
		return true
	}
	return item.PrintedUnitPriceYen != nil && *item.PrintedUnitPriceYen >= 0 && *item.PrintedUnitPriceYen <= MaxAmount &&
		validReceiptRational(item.PrintedUnitPriceQuantityNumerator, item.PrintedUnitPriceQuantityDenominator) &&
		strings.TrimSpace(item.PrintedUnitPriceUnit) != "" && len(item.PrintedUnitPriceUnit) <= 32
}

func validReceiptRational(numerator, denominator *int64) bool {
	if numerator == nil || denominator == nil || *numerator <= 0 || *numerator > MaxAmount || *denominator <= 0 || *denominator > maxQuantityDenominator {
		return false
	}
	gcd := new(big.Int).GCD(nil, nil, big.NewInt(*numerator), big.NewInt(*denominator))
	return gcd.Int64() == 1
}

func validRegion(r Region) bool {
	return r.Page >= 0 && r.Top >= 0 && r.Left >= 0 && r.Bottom <= 1 && r.Right <= 1 && r.Top < r.Bottom && r.Left < r.Right
}

// ValidRegionは正規化座標の領域が有効かを返す。
func ValidRegion(r Region) bool {
	return validRegion(r)
}

func validAmount(value int64) bool { return value >= -MaxAmount && value <= MaxAmount }
func abs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func roundedTax(subtotal int64, g TaxGroup) (int64, bool) {
	n := new(big.Int).Mul(big.NewInt(abs(subtotal)), big.NewInt(g.Numerator))
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(n, big.NewInt(g.Denominator), rem)
	switch g.Rounding {
	case taxRoundingFloor:
	case "CEIL":
		if rem.Sign() > 0 {
			q.Add(q, big.NewInt(1))
		}
	case "NEAREST":
		if rem.Mul(rem, big.NewInt(2)).Cmp(big.NewInt(g.Denominator)) >= 0 {
			q.Add(q, big.NewInt(1))
		}
	default:
		return 0, false
	}
	result := q.Int64()
	if subtotal < 0 {
		result = -result
	}
	return result, true
}

// Distributeは同率の端数を原本の行順で配賦し、再解析時にも同じ金額を返す。
func Distribute(amount int64, weights []int64) ([]int64, error) {
	if !validAmount(amount) || len(weights) == 0 || len(weights) > 1000 {
		return nil, errors.New("invalid allocation")
	}
	var sum int64
	for _, w := range weights {
		if w < 0 || w > MaxAmount {
			return nil, errors.New("invalid weight")
		}
		sum += w
	}
	if sum == 0 {
		if amount == 0 {
			return make([]int64, len(weights)), nil
		}
		return nil, errors.New("zero allocation weights")
	}
	type remainder struct {
		index int
		value int64
	}
	result := make([]int64, len(weights))
	remainders := make([]remainder, len(weights))
	var allocated int64
	for i, weight := range weights {
		product := new(big.Int).Mul(big.NewInt(abs(amount)), big.NewInt(weight))
		q, rem := new(big.Int), new(big.Int)
		q.QuoRem(product, big.NewInt(sum), rem)
		result[i] = q.Int64()
		allocated += result[i]
		remainders[i] = remainder{i, rem.Int64()}
	}
	slices.SortFunc(remainders, func(a, b remainder) int { return cmp.Or(cmp.Compare(b.value, a.value), cmp.Compare(a.index, b.index)) })
	for i := range abs(amount) - allocated {
		result[remainders[i].index]++
	}
	if amount < 0 {
		for i := range result {
			result[i] = -result[i]
		}
	}
	return result, nil
}
