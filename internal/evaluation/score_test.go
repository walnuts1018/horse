package evaluation

import (
	"testing"

	"github.com/walnuts1018/horse/internal/ingestion"
)

const (
	appleItemName = "りんご"
	currencyJPY   = "JPY"
)

func TestScoreSeparatesExtractionQualityAndEstimatedCost(t *testing.T) {
	t.Parallel()
	const (
		taxIncludedPriceBasis = "TAX_INCLUDED"
		syntheticDataset      = "synthetic-v1"
	)
	firstRegion := ingestion.Region{Page: 0, Top: 0.1, Left: 0.1, Bottom: 0.3, Right: 0.4}
	secondRegion := ingestion.Region{Page: 1, Top: 0.5, Left: 0.5, Bottom: 0.7, Right: 0.8}
	expected := ingestion.Receipt{
		Currency: currencyJPY, Total: 300,
		Items: []ingestion.Item{
			{PrintedName: appleItemName, Name: appleItemName, PrintedAmount: 100, PriceBasis: taxIncludedPriceBasis, Region: &firstRegion},
			{PrintedName: "水", Name: "水", PrintedAmount: 200, PriceBasis: taxIncludedPriceBasis, Region: &secondRegion},
		},
	}
	wrongRegion := ingestion.Region{Page: 0, Top: 0.5, Left: 0.5, Bottom: 0.7, Right: 0.8}
	actual := ingestion.Receipt{
		Currency: "JPY", Total: 401,
		Items: []ingestion.Item{
			{PrintedName: appleItemName, Name: "リンゴ", PrintedAmount: 100, PriceBasis: taxIncludedPriceBasis, Region: &firstRegion},
			{PrintedName: "水", Name: "水", PrintedAmount: 201, PriceBasis: taxIncludedPriceBasis, Region: &wrongRegion},
			{PrintedName: appleItemName, Name: appleItemName, PrintedAmount: 100, PriceBasis: taxIncludedPriceBasis},
		},
	}
	corrections := 3
	metrics := Score(Dataset{Version: syntheticDataset, Cases: []Case{{
		ID: "case-1", Expected: expected, Actual: actual,
		Runs:                      []ingestion.ExtractionRun{{Model: "gemini-fixture", PriceEstimateStatus: "estimated_paid", EstimatedJPY: 2.5}},
		HumanCorrectionOperations: &corrections,
	}}})
	if metrics.ItemDetectionPrecision != 2.0/3 || metrics.ItemDetectionRecall != 1 || metrics.PrintedAmountExactRate != 0.5 || metrics.TaxConsistentRate != 1 || metrics.ProductNormalizationAccuracy != 0 || metrics.EvidencePrecision != 0.5 || metrics.DuplicatePrecision != 0 || metrics.PredictedDuplicateItems != 1 || metrics.CorrectDuplicateItems != 0 || metrics.AutoConfirmableRate != 0 || metrics.AverageHumanCorrectionOps != 3 || metrics.EstimatedCostJPY != 2.5 || metrics.EstimatedCostJPYPerPricedRun != 2.5 || metrics.UnpricedRuns != 0 {
		t.Fatalf("unexpected evaluation metrics: %+v", metrics)
	}
}

func TestScoreAlignsMisreadNamesWithoutReducingAmountAccuracy(t *testing.T) {
	t.Parallel()
	const datasetVersion = "synthetic-v1"
	expectedRegion := ingestion.Region{Page: 0, Top: 0.2, Left: 0.1, Bottom: 0.3, Right: 0.9}
	actualRegion := expectedRegion
	corrections := 0
	metrics := Score(Dataset{Version: datasetVersion, Cases: []Case{{
		ID:                        "ocr-name-error",
		Expected:                  ingestion.Receipt{Items: []ingestion.Item{{PrintedName: appleItemName, Name: appleItemName, PrintedAmount: 120, Region: &expectedRegion}}},
		Actual:                    ingestion.Receipt{Items: []ingestion.Item{{PrintedName: "りんこ", Name: appleItemName, PrintedAmount: 120, Region: &actualRegion}}},
		HumanCorrectionOperations: &corrections,
	}}})
	if metrics.MatchedItems != 1 || metrics.ItemDetectionRecall != 1 || metrics.PrintedAmountExactRate != 1 || metrics.ProductNormalizationAccuracy != 1 || metrics.EvidencePrecision != 1 || metrics.AutoConfirmableCases != 1 || metrics.AutoConfirmableRate != 1 {
		t.Fatalf("OCR name error was confused with amount accuracy or auto-confirmability: %+v", metrics)
	}
}

func TestScoreDoesNotTreatUnknownCostAsFree(t *testing.T) {
	t.Parallel()
	metrics := Score(Dataset{Version: "synthetic-v1", Cases: []Case{{
		ID: "unpriced", Runs: []ingestion.ExtractionRun{{Model: "future-model", PriceEstimateStatus: "unknown_model_price", EstimatedJPY: 0}},
	}}})
	if metrics.PricedRuns != 0 || metrics.UnpricedRuns != 1 || metrics.EstimatedCostJPY != 0 {
		t.Fatalf("unknown model price was treated as a free run: %+v", metrics)
	}
}

func TestScoreReportsAccuracyCostAndLatencyPerModel(t *testing.T) {
	t.Parallel()
	expected := ingestion.Receipt{
		Currency: currencyJPY, Total: 300,
		Items: []ingestion.Item{
			{PrintedName: appleItemName, Name: appleItemName, PrintedAmount: 100},
			{PrintedName: "牛乳", Name: "牛乳", PrintedAmount: 200},
		},
	}
	complete := expected
	partial := ingestion.Receipt{
		Currency: currencyJPY, Total: 100,
		Items: []ingestion.Item{{PrintedName: appleItemName, Name: appleItemName, PrintedAmount: 100}},
	}
	metrics := Score(Dataset{Version: "synthetic-v1", Cases: []Case{{
		ID:       "model-comparison",
		Expected: expected,
		Runs: []ingestion.ExtractionRun{
			{Model: "model-a", Receipt: &complete, PriceEstimateStatus: "estimated_paid", EstimatedJPY: 1.5, LatencyMillis: 1000},
			{Model: "model-b", Receipt: &partial, PriceEstimateStatus: "free_tier", EstimatedJPY: 0, LatencyMillis: 3000},
			{Model: "model-c", Receipt: &complete},
		},
	}}})
	if len(metrics.Models) != 3 {
		t.Fatalf("expected metrics for three models, got %+v", metrics.Models)
	}
	modelA, modelB, modelC := metrics.Models[0], metrics.Models[1], metrics.Models[2]
	if modelA.Model != "model-a" || modelA.ItemDetectionRecall != 1 || modelA.EstimatedCostJPY != 1.5 || modelA.AverageLatencyMillis != 1000 || modelA.ScoredRuns != 1 {
		t.Fatalf("unexpected model-a metrics: %+v", modelA)
	}
	if modelB.Model != "model-b" || modelB.ItemDetectionPrecision != 1 || modelB.ItemDetectionRecall != 0.5 || modelB.EstimatedCostJPY != 0 || modelB.AverageLatencyMillis != 3000 || modelB.ScoredRuns != 1 {
		t.Fatalf("unexpected model-b metrics: %+v", modelB)
	}
	if modelC.Model != "model-c" || modelC.LatencyRuns != 0 || modelC.AverageLatencyMillis != 0 {
		t.Fatalf("unset latency was included in model-c metrics: %+v", modelC)
	}
}

func TestDuplicatePrecisionRequiresTheExpectedRepeatedLine(t *testing.T) {
	t.Parallel()
	expected := []ingestion.Item{{PrintedName: appleItemName, PrintedAmount: 100}, {PrintedName: appleItemName, PrintedAmount: 100}}
	actual := []ingestion.Item{{PrintedName: "みかん", PrintedAmount: 90}, {PrintedName: "みかん", PrintedAmount: 90}}
	if got := correctDuplicateCount(expected, actual); got != 0 {
		t.Fatalf("different duplicate lines were counted as correct: %d", got)
	}
}
