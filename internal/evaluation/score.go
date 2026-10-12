package evaluation

import (
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
	var correctAmounts, correctNames, predictedRegions, correctRegions, taxConsistent int
	var correctionOperations int
	for _, testCase := range dataset.Cases {
		expected := testCase.Expected.Items
		actual := testCase.Actual.Items
		metrics.ExpectedItems += len(expected)
		metrics.PredictedItems += len(actual)
		available := make(map[string][]int, len(expected))
		for index, item := range expected {
			available[normalizeName(item.PrintedName)] = append(available[normalizeName(item.PrintedName)], index)
		}
		matches := make(map[int]int, min(len(actual), len(expected)))
		for actualIndex, item := range actual {
			name := normalizeName(item.PrintedName)
			indexes := available[name]
			if name == "" || len(indexes) == 0 {
				continue
			}
			expectedIndex := indexes[0]
			available[name] = indexes[1:]
			matches[actualIndex] = expectedIndex
		}
		metrics.MatchedItems += len(matches)
		for actualIndex, expectedIndex := range matches {
			actualItem, expectedItem := actual[actualIndex], expected[expectedIndex]
			if actualItem.PrintedAmount == expectedItem.PrintedAmount {
				correctAmounts++
			}
			if normalizeName(actualItem.Name) == normalizeName(expectedItem.Name) && normalizeName(expectedItem.Name) != "" {
				correctNames++
			}
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
	metrics.PrintedAmountExactRate = ratio(correctAmounts, metrics.ExpectedItems)
	metrics.TaxConsistentRate = ratio(taxConsistent, metrics.Documents)
	metrics.ProductNormalizationAccuracy = ratio(correctNames, metrics.ExpectedItems)
	metrics.EvidencePrecision = ratio(correctRegions, predictedRegions)
	metrics.DuplicatePrecision = ratio(metrics.CorrectDuplicateItems, metrics.PredictedDuplicateItems)
	if metrics.HumanCorrectionCases > 0 {
		metrics.AverageHumanCorrectionOps = float64(correctionOperations) / float64(metrics.HumanCorrectionCases)
	}
	if metrics.PricedRuns > 0 {
		metrics.EstimatedCostJPYPerPricedRun = metrics.EstimatedCostJPY / float64(metrics.PricedRuns)
	}
	return metrics
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
