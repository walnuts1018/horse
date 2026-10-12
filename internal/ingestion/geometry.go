package ingestion

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	"image/png"
	"math"
)

const maxCropPixels = 40_000_000

func identityTransform() [9]float64 {
	return [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
}

// CropImageは保存済みの原本を変更せずに画像を切り抜き、処理画像から原本への座標変換を返す。
func CropImage(document Document, region Region) (Document, [9]float64, error) {
	transform := identityTransform()
	if !validRegion(region) || region.Page != 0 {
		return Document{}, transform, errors.New("invalid image crop region")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(document.Data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxCropPixels {
		return Document{}, transform, errors.New("invalid or oversized image")
	}
	source, _, err := image.Decode(bytes.NewReader(document.Data))
	if err != nil {
		return Document{}, transform, errors.New("decode image for crop failed")
	}
	bounds := source.Bounds()
	left := bounds.Min.X + int(math.Floor(region.Left*float64(bounds.Dx())))
	top := bounds.Min.Y + int(math.Floor(region.Top*float64(bounds.Dy())))
	right := bounds.Min.X + int(math.Ceil(region.Right*float64(bounds.Dx())))
	bottom := bounds.Min.Y + int(math.Ceil(region.Bottom*float64(bounds.Dy())))
	left = max(bounds.Min.X, min(left, bounds.Max.X-1))
	top = max(bounds.Min.Y, min(top, bounds.Max.Y-1))
	right = max(left+1, min(right, bounds.Max.X))
	bottom = max(top+1, min(bottom, bounds.Max.Y))
	cropped := image.NewRGBA(image.Rect(0, 0, right-left, bottom-top))
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			cropped.Set(x-left, y-top, source.At(x, y))
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, cropped); err != nil || encoded.Len() > MaxDocumentBytes {
		return Document{}, transform, errors.New("encode cropped image failed")
	}
	width, height := float64(bounds.Dx()), float64(bounds.Dy())
	transform = [9]float64{
		float64(right-left) / width, 0, float64(left-bounds.Min.X) / width,
		0, float64(bottom-top) / height, float64(top-bounds.Min.Y) / height,
		0, 0, 1,
	}
	return Document{MIMEType: "image/png", Data: encoded.Bytes()}, transform, nil
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

func transformVerification(verification *Verification, lines []OCRLine, transform [9]float64) error {
	verification.Transform = transform
	if err := transformReceiptRegions(verification, transform); err != nil {
		return err
	}
	for i := range lines {
		mapped, err := transformRegion(lines[i].Region, transform)
		if err != nil {
			return err
		}
		lines[i].Region = mapped
	}
	return nil
}

func transformReceiptRegions(verification *Verification, transform [9]float64) error {
	verification.Transform = transform
	for i := range verification.Receipt.Items {
		region := verification.Receipt.Items[i].Region
		if region == nil {
			continue
		}
		mapped, err := transformRegion(*region, transform)
		if err != nil {
			return err
		}
		verification.Receipt.Items[i].Region = &mapped
	}
	return nil
}
