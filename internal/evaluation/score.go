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
	SourceKind                string                    `json:"sourceKind"`
	Expected                  ingestion.Receipt         `json:"expected"`
	Actual                    ingestion.Receipt         `json:"actual"`
	Runs                      []ingestion.ExtractionRun `json:"runs"`
	HumanCorrectionOperations *int                      `json:"humanCorrectionOperations,omitempty"`
}

type Metrics struct {
	DatasetVersion               string              `json:"datasetVersion"`
	Documents                    int                 `json:"documents"`
	ExpectedItems                int                 `json:"expectedItems"`
	PredictedItems               int                 `json:"predictedItems"`
	MatchedItems                 int                 `json:"matchedItems"`
	ItemDetectionPrecision       float64             `json:"itemDetectionPrecision"`
	ItemDetectionRecall          float64             `json:"itemDetectionRecall"`
	PrintedAmountExactRate       float64             `json:"printedAmountExactRate"`
	TaxConsistentRate            float64             `json:"taxConsistentRate"`
	ProductNormalizationAccuracy float64             `json:"productNormalizationAccuracy"`
	EvidencePrecision            float64             `json:"evidencePrecision"`
	DuplicatePrecision           float64             `json:"duplicatePrecision"`
	PredictedDuplicateItems      int                 `json:"predictedDuplicateItems"`
	CorrectDuplicateItems        int                 `json:"correctDuplicateItems"`
	AutoConfirmableCases         int                 `json:"autoConfirmableCases"`
	AutoConfirmableRate          float64             `json:"autoConfirmableRate"`
	HumanCorrectionCases         int                 `json:"humanCorrectionCases"`
	AverageHumanCorrectionOps    float64             `json:"averageHumanCorrectionOperations"`
	PricedRuns                   int                 `json:"pricedRuns"`
	UnpricedRuns                 int                 `json:"unpricedRuns"`
	EstimatedCostJPY             float64             `json:"estimatedCostJPY"`
	EstimatedCostJPYPerPricedRun float64             `json:"estimatedCostJPYPerPricedRun"`
	Models                       []ModelMetrics      `json:"models"`
	SourceKinds                  []SourceKindMetrics `json:"sourceKinds"`
}

type SourceKindMetrics struct {
	SourceKind string         `json:"sourceKind"`
	Documents  int            `json:"documents"`
	Models     []ModelMetrics `json:"models"`
}

type ModelMetrics struct {
	Model                        string  `json:"model"`
	Runs                         int     `json:"runs"`
	ScoredRuns                   int     `json:"scoredRuns"`
	ExpectedItems                int     `json:"expectedItems"`
	PredictedItems               int     `json:"predictedItems"`
	MatchedItems                 int     `json:"matchedItems"`
	ItemDetectionPrecision       float64 `json:"itemDetectionPrecision"`
	ItemDetectionRecall          float64 `json:"itemDetectionRecall"`
	PrintedAmountExactRate       float64 `json:"printedAmountExactRate"`
	TaxConsistentRate            float64 `json:"taxConsistentRate"`
	ProductNormalizationAccuracy float64 `json:"productNormalizationAccuracy"`
	EvidencePrecision            float64 `json:"evidencePrecision"`
	PricedRuns                   int     `json:"pricedRuns"`
	UnpricedRuns                 int     `json:"unpricedRuns"`
	EstimatedCostJPY             float64 `json:"estimatedCostJPY"`
	EstimatedCostJPYPerPricedRun float64 `json:"estimatedCostJPYPerPricedRun"`
	LatencyRuns                  int     `json:"latencyRuns"`
	AverageLatencyMillis         float64 `json:"averageLatencyMillis"`
}

type modelAccumulator struct {
	metrics           ModelMetrics
	correctAmounts    int
	amountComparisons int
	correctNames      int
	nameComparisons   int
	correctRegions    int
	predictedRegions  int
	taxConsistent     int
	latencyMillis     int64
}

// Scoreは実行済みの抽出結果を固定された正解データと照合し、課金実績ではなく推定費用を集計する。
func Score(dataset Dataset) Metrics {
	metrics := Metrics{DatasetVersion: dataset.Version, Documents: len(dataset.Cases)}
	models := make(map[string]*modelAccumulator)
	sourceKinds := make(map[string]map[string]*modelAccumulator)
	sourceDocuments := make(map[string]int)
	var correctAmounts, correctNames, amountComparisons, nameComparisons, predictedRegions, correctRegions, taxConsistent int
	var correctionOperations int
	for _, testCase := range dataset.Cases {
		sourceKind := testCase.SourceKind
		if sourceKind == "" {
			sourceKind = "unknown"
		}
		if _, exists := sourceDocuments[sourceKind]; !exists {
			sourceDocuments[sourceKind] = 0
			sourceKinds[sourceKind] = make(map[string]*modelAccumulator)
		}
		sourceDocuments[sourceKind]++
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
			var model *modelAccumulator
			if run.Model != "" {
				model = models[run.Model]
				if model == nil {
					model = &modelAccumulator{metrics: ModelMetrics{Model: run.Model}}
					models[run.Model] = model
				}
				model.metrics.Runs++
				sourceModel := sourceKinds[sourceKind][run.Model]
				if sourceModel == nil {
					sourceModel = &modelAccumulator{metrics: ModelMetrics{Model: run.Model}}
					sourceKinds[sourceKind][run.Model] = sourceModel
				}
				sourceModel.metrics.Runs++
				if run.LatencyMillis > 0 {
					model.metrics.LatencyRuns++
					model.latencyMillis += run.LatencyMillis
					sourceKinds[sourceKind][run.Model].metrics.LatencyRuns++
					sourceKinds[sourceKind][run.Model].latencyMillis += run.LatencyMillis
				}
			}
			if run.PriceEstimateStatus == "estimated_paid" || run.PriceEstimateStatus == "free_tier" {
				metrics.PricedRuns++
				metrics.EstimatedCostJPY += run.EstimatedJPY
				if model != nil {
					model.metrics.PricedRuns++
					model.metrics.EstimatedCostJPY += run.EstimatedJPY
				}
				if sourceModel := sourceKinds[sourceKind][run.Model]; sourceModel != nil {
					sourceModel.metrics.PricedRuns++
					sourceModel.metrics.EstimatedCostJPY += run.EstimatedJPY
				}
			} else if run.Model != "" {
				metrics.UnpricedRuns++
				if model != nil {
					model.metrics.UnpricedRuns++
				}
				if sourceModel := sourceKinds[sourceKind][run.Model]; sourceModel != nil {
					sourceModel.metrics.UnpricedRuns++
				}
			}
			if model != nil && run.Receipt != nil {
				model.metrics.ScoredRuns++
				model.addReceipt(testCase.Expected, *run.Receipt)
				sourceModel := sourceKinds[sourceKind][run.Model]
				sourceModel.metrics.ScoredRuns++
				sourceModel.addReceipt(testCase.Expected, *run.Receipt)
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
	metrics.Models = make([]ModelMetrics, 0, len(models))
	for _, model := range models {
		metrics.Models = append(metrics.Models, model.finish())
	}
	slices.SortFunc(metrics.Models, func(a, b ModelMetrics) int { return cmp.Compare(a.Model, b.Model) })
	for kind, kindModels := range sourceKinds {
		entry := SourceKindMetrics{SourceKind: kind, Documents: sourceDocuments[kind], Models: make([]ModelMetrics, 0, len(kindModels))}
		for _, model := range kindModels {
			entry.Models = append(entry.Models, model.finish())
		}
		slices.SortFunc(entry.Models, func(a, b ModelMetrics) int { return cmp.Compare(a.Model, b.Model) })
		metrics.SourceKinds = append(metrics.SourceKinds, entry)
	}
	slices.SortFunc(metrics.SourceKinds, func(a, b SourceKindMetrics) int { return cmp.Compare(a.SourceKind, b.SourceKind) })
	return metrics
}

func (model *modelAccumulator) addReceipt(expected, actual ingestion.Receipt) {
	model.metrics.ExpectedItems += len(expected.Items)
	model.metrics.PredictedItems += len(actual.Items)
	matches := matchItems(expected.Items, actual.Items, true, true)
	model.metrics.MatchedItems += len(matches)
	amountMatches := matchItems(expected.Items, actual.Items, true, false)
	model.amountComparisons += len(amountMatches)
	for actualIndex, expectedIndex := range amountMatches {
		if actual.Items[actualIndex].PrintedAmount == expected.Items[expectedIndex].PrintedAmount {
			model.correctAmounts++
		}
	}
	nameMatches := matchItems(expected.Items, actual.Items, false, true)
	model.nameComparisons += len(nameMatches)
	for actualIndex, expectedIndex := range nameMatches {
		if normalizeName(actual.Items[actualIndex].Name) == normalizeName(expected.Items[expectedIndex].Name) && normalizeName(expected.Items[expectedIndex].Name) != "" {
			model.correctNames++
		}
	}
	for actualIndex, expectedIndex := range matches {
		actualRegion, expectedRegion := actual.Items[actualIndex].Region, expected.Items[expectedIndex].Region
		if actualRegion != nil && expectedRegion != nil && actualRegion.Page == expectedRegion.Page && regionIoU(*actualRegion, *expectedRegion) >= 0.5 {
			model.correctRegions++
		}
	}
	for _, item := range actual.Items {
		if item.Region != nil {
			model.predictedRegions++
		}
	}
	if verification, err := ingestion.Verify(actual); err == nil && verification.Balanced {
		model.taxConsistent++
	}
}

func (model *modelAccumulator) finish() ModelMetrics {
	metrics := model.metrics
	metrics.ItemDetectionPrecision = ratio(metrics.MatchedItems, metrics.PredictedItems)
	metrics.ItemDetectionRecall = ratio(metrics.MatchedItems, metrics.ExpectedItems)
	metrics.PrintedAmountExactRate = ratio(model.correctAmounts, model.amountComparisons)
	metrics.TaxConsistentRate = ratio(model.taxConsistent, metrics.ScoredRuns)
	metrics.ProductNormalizationAccuracy = ratio(model.correctNames, model.nameComparisons)
	metrics.EvidencePrecision = ratio(model.correctRegions, model.predictedRegions)
	if metrics.PricedRuns > 0 {
		metrics.EstimatedCostJPYPerPricedRun = metrics.EstimatedCostJPY / float64(metrics.PricedRuns)
	}
	if metrics.LatencyRuns > 0 {
		metrics.AverageLatencyMillis = float64(model.latencyMillis) / float64(metrics.LatencyRuns)
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
