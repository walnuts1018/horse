package evaluation

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/walnuts1018/horse/internal/ingestion"
)

type Dataset struct {
	Version string `json:"version"`
	Cases   []Case `json:"cases"`
}

type Case struct {
	ID                        string                    `json:"id"`
	Expected                  ingestion.Receipt         `json:"expected"`
	Actual                    ingestion.Receipt         `json:"actual"`
	Runs                      []ingestion.ExtractionRun `json:"runs"`
	HumanCorrectionOperations *int                      `json:"humanCorrectionOperations,omitempty"`
}

type Metrics struct {
	DatasetVersion               string  `json:"datasetVersion"`
	Documents                    int     `json:"documents"`
	ExpectedItems                int     `json:"expectedItems"`
	PredictedItems               int     `json:"predictedItems"`
	MatchedItems                 int     `json:"matchedItems"`
	ItemDetectionPrecision       float64 `json:"itemDetectionPrecision"`
	ItemDetectionRecall          float64 `json:"itemDetectionRecall"`
	PrintedAmountExactRate       float64 `json:"printedAmountExactRate"`
	TaxConsistentRate            float64 `json:"taxConsistentRate"`
	ProductNormalizationAccuracy float64 `json:"productNormalizationAccuracy"`
	EvidencePrecision            float64 `json:"evidencePrecision"`
	DuplicatePrecision           float64 `json:"duplicatePrecision"`
	PredictedDuplicateItems      int     `json:"predictedDuplicateItems"`
	CorrectDuplicateItems        int     `json:"correctDuplicateItems"`
	AutoConfirmableCases         int     `json:"autoConfirmableCases"`
	AutoConfirmableRate          float64 `json:"autoConfirmableRate"`
	HumanCorrectionCases         int     `json:"humanCorrectionCases"`
	AverageHumanCorrectionOps    float64 `json:"averageHumanCorrectionOperations"`
	PricedRuns                   int     `json:"pricedRuns"`
	UnpricedRuns                 int     `json:"unpricedRuns"`
	EstimatedCostJPY             float64 `json:"estimatedCostJPY"`
	EstimatedCostJPYPerPricedRun float64 `json:"estimatedCostJPYPerPricedRun"`
}

// Scoreは実行済みの抽出結果を固定された正解データと照合し、課金実績ではなく推定費用を集計する。
func Score(dataset Dataset) Metrics {
	metrics := Metrics{DatasetVersion: dataset.Version, Documents: len(dataset.Cases)}
	var correctAmounts, correctNames, amountComparisons, nameComparisons, predictedRegions, correctRegions, taxConsistent int
	var correctionOperations int
	for _, testCase := range dataset.Cases {
		expected := testCase.Expected.Items
		actual := testCase.Actual.Items
		metrics.ExpectedItems += len(expected)
		metrics.PredictedItems += len(actual)
		matches := matchItems(expected, actual, true, true)
		metrics.MatchedItems += len(matches)
		amountMatches := matchItems(expected, actual, true, false)
		amountComparisons += len(amountMatches)
		for actualIndex, expectedIndex := range amountMatches {
			actualItem, expectedItem := actual[actualIndex], expected[expectedIndex]
			if actualItem.PrintedAmount == expectedItem.PrintedAmount {
				correctAmounts++
			}
		}
		nameMatches := matchItems(expected, actual, false, true)
		nameComparisons += len(nameMatches)
		for actualIndex, expectedIndex := range nameMatches {
			actualItem, expectedItem := actual[actualIndex], expected[expectedIndex]
			if normalizeName(actualItem.Name) == normalizeName(expectedItem.Name) && normalizeName(expectedItem.Name) != "" {
				correctNames++
			}
		}
		for actualIndex, expectedIndex := range matches {
			actualItem, expectedItem := actual[actualIndex], expected[expectedIndex]
			if actualItem.Region != nil {
				if expectedItem.Region != nil && actualItem.Region.Page == expectedItem.Region.Page && regionIoU(*actualItem.Region, *expectedItem.Region) >= 0.5 {
					correctRegions++
				}
			}
		}
		for _, item := range actual {
			if item.Region != nil {
				predictedRegions++
			}
		}
		if verification, err := ingestion.Verify(testCase.Actual); err == nil && verification.Balanced {
			taxConsistent++
		}
		metrics.PredictedDuplicateItems += duplicateCount(actual)
		metrics.CorrectDuplicateItems += correctDuplicateCount(expected, actual)
		if testCase.HumanCorrectionOperations != nil && *testCase.HumanCorrectionOperations >= 0 {
			metrics.HumanCorrectionCases++
			correctionOperations += *testCase.HumanCorrectionOperations
			if *testCase.HumanCorrectionOperations == 0 {
				metrics.AutoConfirmableCases++
			}
		}
		for _, run := range testCase.Runs {
			if run.PriceEstimateStatus == "estimated_paid" || run.PriceEstimateStatus == "free_tier" {
				metrics.PricedRuns++
				metrics.EstimatedCostJPY += run.EstimatedJPY
			} else if run.Model != "" {
				metrics.UnpricedRuns++
			}
		}
	}
	metrics.ItemDetectionPrecision = ratio(metrics.MatchedItems, metrics.PredictedItems)
	metrics.ItemDetectionRecall = ratio(metrics.MatchedItems, metrics.ExpectedItems)
	metrics.PrintedAmountExactRate = ratio(correctAmounts, amountComparisons)
	metrics.TaxConsistentRate = ratio(taxConsistent, metrics.Documents)
	metrics.ProductNormalizationAccuracy = ratio(correctNames, nameComparisons)
	metrics.EvidencePrecision = ratio(correctRegions, predictedRegions)
	metrics.DuplicatePrecision = ratio(metrics.CorrectDuplicateItems, metrics.PredictedDuplicateItems)
	metrics.AutoConfirmableRate = ratio(metrics.AutoConfirmableCases, metrics.HumanCorrectionCases)
	if metrics.HumanCorrectionCases > 0 {
		metrics.AverageHumanCorrectionOps = float64(correctionOperations) / float64(metrics.HumanCorrectionCases)
	}
	if metrics.PricedRuns > 0 {
		metrics.EstimatedCostJPYPerPricedRun = metrics.EstimatedCostJPY / float64(metrics.PricedRuns)
	}
	return metrics
}

type itemMatch struct {
	actual   int
	expected int
	score    float64
}

func matchItems(expected, actual []ingestion.Item, includePrintedName, includePrintedAmount bool) map[int]int {
	candidates := make([]itemMatch, 0, len(expected)*len(actual))
	for actualIndex, actualItem := range actual {
		for expectedIndex, expectedItem := range expected {
			score := 0.0
			if actualItem.Region != nil && expectedItem.Region != nil && actualItem.Region.Page == expectedItem.Region.Page {
				iou := regionIoU(*actualItem.Region, *expectedItem.Region)
				if iou >= 0.5 {
					score += 100 + iou
				}
			}
			if includePrintedAmount && actualItem.PrintedAmount == expectedItem.PrintedAmount {
				score += 20
			}
			if includePrintedName && normalizeName(actualItem.PrintedName) != "" && normalizeName(actualItem.PrintedName) == normalizeName(expectedItem.PrintedName) {
				score += 10
			}
			if score > 0 {
				candidates = append(candidates, itemMatch{actual: actualIndex, expected: expectedIndex, score: score})
			}
		}
	}
	slices.SortFunc(candidates, func(a, b itemMatch) int {
		if a.score != b.score {
			return cmp.Compare(b.score, a.score)
		}
		if a.actual != b.actual {
			return cmp.Compare(a.actual, b.actual)
		}
		return cmp.Compare(a.expected, b.expected)
	})
	matches := make(map[int]int, min(len(actual), len(expected)))
	usedExpected := make(map[int]struct{}, min(len(actual), len(expected)))
	for _, candidate := range candidates {
		if _, used := matches[candidate.actual]; used {
			continue
		}
		if _, used := usedExpected[candidate.expected]; used {
			continue
		}
		matches[candidate.actual] = candidate.expected
		usedExpected[candidate.expected] = struct{}{}
	}
	return matches
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func normalizeName(name string) string {
	var normalized strings.Builder
	for _, character := range strings.ToLower(name) {
		if !unicode.IsSpace(character) && !unicode.IsPunct(character) && !unicode.IsSymbol(character) {
			normalized.WriteRune(character)
		}
	}
	return normalized.String()
}

func duplicateCount(items []ingestion.Item) int {
	counts := duplicateCounts(items)
	duplicates := 0
	for _, count := range counts {
		if count > 1 {
			duplicates += count - 1
		}
	}
	return duplicates
}

func correctDuplicateCount(expected, actual []ingestion.Item) int {
	expectedCounts, actualCounts := duplicateCounts(expected), duplicateCounts(actual)
	correct := 0
	for signature, actualCount := range actualCounts {
		correct += min(max(0, actualCount-1), max(0, expectedCounts[signature]-1))
	}
	return correct
}

func duplicateCounts(items []ingestion.Item) map[string]int {
	counts := make(map[string]int, len(items))
	for _, item := range items {
		signature := normalizeName(item.PrintedName) + ":" + strconv.FormatInt(item.PrintedAmount, 10)
		counts[signature]++
	}
	return counts
}

func regionIoU(a, b ingestion.Region) float64 {
	if !ingestion.ValidRegion(a) || !ingestion.ValidRegion(b) {
		return 0
	}
	width := max(0, min(a.Right, b.Right)-max(a.Left, b.Left))
	height := max(0, min(a.Bottom, b.Bottom)-max(a.Top, b.Top))
	intersection := width * height
	union := (a.Right-a.Left)*(a.Bottom-a.Top) + (b.Right-b.Left)*(b.Bottom-b.Top) - intersection
	if union <= 0 {
		return 0
	}
	return intersection / union
}
