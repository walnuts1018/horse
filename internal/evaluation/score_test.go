package evaluation

import (
	"testing"

	"github.com/walnuts1018/horse/internal/ingestion"
)

const appleItemName = "りんご"

func TestScoreSeparatesExtractionQualityAndEstimatedCost(t *testing.T) {
	t.Parallel()
	const (
		taxIncludedPriceBasis = "TAX_INCLUDED"
		syntheticDataset      = "synthetic-v1"
	)
	firstRegion := ingestion.Region{Page: 0, Top: 0.1, Left: 0.1, Bottom: 0.3, Right: 0.4}
	secondRegion := ingestion.Region{Page: 1, Top: 0.5, Left: 0.5, Bottom: 0.7, Right: 0.8}
	expected := ingestion.Receipt{
		Currency: "JPY", Total: 300,
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

func TestDuplicatePrecisionRequiresTheExpectedRepeatedLine(t *testing.T) {
	t.Parallel()
	expected := []ingestion.Item{{PrintedName: appleItemName, PrintedAmount: 100}, {PrintedName: appleItemName, PrintedAmount: 100}}
	actual := []ingestion.Item{{PrintedName: "みかん", PrintedAmount: 90}, {PrintedName: "みかん", PrintedAmount: 90}}
	if got := correctDuplicateCount(expected, actual); got != 0 {
		t.Fatalf("different duplicate lines were counted as correct: %d", got)
	}
}
