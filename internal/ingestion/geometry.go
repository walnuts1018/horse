package ingestion

import (
	"errors"
	"math"
)

func identityTransform() [9]float64 {
	return [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
}

func transformRegion(region Region, transform [9]float64) (Region, error) {
	if !validRegion(region) {
		return Region{}, errors.New("invalid source region")
	}
	points := [][2]float64{{region.Left, region.Top}, {region.Right, region.Top}, {region.Left, region.Bottom}, {region.Right, region.Bottom}}
	left, top := math.Inf(1), math.Inf(1)
	right, bottom := math.Inf(-1), math.Inf(-1)
	for _, point := range points {
		x := transform[0]*point[0] + transform[1]*point[1] + transform[2]
		y := transform[3]*point[0] + transform[4]*point[1] + transform[5]
		w := transform[6]*point[0] + transform[7]*point[1] + transform[8]
		if w == 0 {
			return Region{}, errors.New("invalid coordinate transform")
		}
		x, y = x/w, y/w
		left, top = min(left, x), min(top, y)
		right, bottom = max(right, x), max(bottom, y)
	}
	result := Region{Page: region.Page, Top: top, Left: left, Bottom: bottom, Right: right}
	if !validRegion(result) {
		return Region{}, errors.New("transformed region is outside the original image")
	}
	return result, nil
}

func transformVerification(verification *Verification, lines []OCRLine, transform [9]float64, page int) error {
	verification.Transform = transform
	verification.TransformPage = page
	if err := transformReceiptRegions(verification, transform, page); err != nil {
		return err
	}
	for i := range lines {
		mapped, err := transformRegion(lines[i].Region, transform)
		if err != nil {
			return err
		}
		mapped.Page = page
		lines[i].Region = mapped
	}
	return nil
}

func transformReceiptRegions(verification *Verification, transform [9]float64, page int) error {
	verification.Transform = transform
	verification.TransformPage = page
	for i := range verification.Receipt.Items {
		region := verification.Receipt.Items[i].Region
		if region == nil {
			continue
		}
		mapped, err := transformRegion(*region, transform)
		if err != nil {
			return err
		}
		mapped.Page = page
		verification.Receipt.Items[i].Region = &mapped
	}
	for i := range verification.Statements {
		region := verification.Statements[i].Region
		if region == nil {
			continue
		}
		mapped, err := transformRegion(*region, transform)
		if err != nil {
			return err
		}
		mapped.Page = page
		verification.Statements[i].Region = &mapped
	}
	for i := range verification.Receipt.Statements {
		region := verification.Receipt.Statements[i].Region
		if region == nil {
			continue
		}
		mapped, err := transformRegion(*region, transform)
		if err != nil {
			return err
		}
		mapped.Page = page
		verification.Receipt.Statements[i].Region = &mapped
	}
	return nil
}
