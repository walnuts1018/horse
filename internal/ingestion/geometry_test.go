package ingestion

import (
	"math"
	"testing"
)

func TestTransformRegionMapsCropCoordinatesToOriginalPage(t *testing.T) {
	transform := [9]float64{0.6, 0, 0.2, 0, 0.6, 0.1, 0, 0, 1}
	verification := Verification{Receipt: Receipt{Items: []Item{{Region: &Region{Page: 0, Top: 0.25, Left: 0.25, Bottom: 0.75, Right: 0.75}}}}}
	err := transformReceiptRegions(&verification, transform, 3)
	if err != nil {
		t.Fatal(err)
	}
	region := *verification.Receipt.Items[0].Region
	for name, pair := range map[string][2]float64{
		"top": {region.Top, 0.25}, "left": {region.Left, 0.35},
		"bottom": {region.Bottom, 0.55}, "right": {region.Right, 0.65},
	} {
		if math.Abs(pair[0]-pair[1]) > 1e-9 {
			t.Errorf("mapped %s = %v, want %v", name, pair[0], pair[1])
		}
	}
	if region.Page != 3 {
		t.Fatalf("mapped page = %d, want 3", region.Page)
	}
}

func TestTransformRegionRejectsInvalidInputAndOutOfBoundsResult(t *testing.T) {
	if _, err := transformRegion(Region{Top: 0.8, Left: 0.2, Bottom: 0.4, Right: 0.8}, identityTransform()); err == nil {
		t.Fatal("invalid source region was accepted")
	}
	if _, err := transformRegion(Region{Top: 0.2, Left: 0.2, Bottom: 0.8, Right: 0.8}, [9]float64{1, 0, 0.5, 0, 1, 0, 0, 0, 1}); err == nil {
		t.Fatal("out-of-bounds transformed region was accepted")
	}
}
