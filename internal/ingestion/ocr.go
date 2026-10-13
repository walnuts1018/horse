package ingestion

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type OCRLine struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	Region     Region  `json:"region"`
}

type OCR struct {
	Endpoint   string
	HTTPClient *http.Client
}

var printedNumber = regexp.MustCompile(`-?[0-9][0-9,.]*`)
var printedDate = regexp.MustCompile(`[0-9]{1,4}[./-][0-9]{1,2}([./-][0-9]{1,4})?`)
var printedJapaneseDate = regexp.MustCompile(`([0-9]{2,4}年)?[0-9]{1,2}月[0-9]{1,2}日`)

var ErrInvalidCropPage = errors.New("invalid crop page")

const (
	maxOCRPDFPages         = 20
	maxOCRResponseBytes    = 8 << 20
	ocrImageRequestTimeout = 45 * time.Second
	ocrPDFRequestTimeout   = 130 * time.Second
)

func (o OCR) Extract(ctx context.Context, document Document) ([]OCRLine, error) {
	if o.Endpoint == "" {
		return nil, errors.New("OCR is not configured")
	}
	timeout := ocrImageRequestTimeout
	filename := "original"
	if document.MIMEType == mimeTypePDF {
		timeout = ocrPDFRequestTimeout
		filename = "original.pdf"
	} else if !strings.HasPrefix(document.MIMEType, "image/") {
		return nil, errors.New("OCR requires an image or PDF")
	}
	u, err := url.Parse(o.Endpoint)
	if err != nil {
		return nil, errors.New("invalid OCR endpoint")
	}
	u = u.JoinPath("extract")
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", filename)
	if err != nil {
		return nil, fmt.Errorf("create OCR image part: %w", err)
	}
	if _, err := part.Write(document.Data); err != nil {
		return nil, fmt.Errorf("write OCR image part: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("complete OCR form: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), &body)
	if err != nil {
		return nil, fmt.Errorf("create OCR request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client := o.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("OCR request failed")
	}
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, maxOCRResponseBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(payload) > maxOCRResponseBytes {
		return nil, errors.New("read OCR response failed")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OCR returned HTTP %d", response.StatusCode)
	}
	var result struct {
		PageCount int       `json:"pageCount"`
		Lines     []OCRLine `json:"lines"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, errors.New("invalid OCR response")
	}
	if result.PageCount < 1 || result.PageCount > maxOCRPDFPages || (document.MIMEType != mimeTypePDF && result.PageCount != 1) {
		return nil, errors.New("invalid OCR page count")
	}
	for _, line := range result.Lines {
		if !validRegion(line.Region) || line.Region.Page >= result.PageCount || line.Confidence < 0 || line.Confidence > 1 {
			return nil, errors.New("invalid OCR line")
		}
	}
	return result.Lines, nil
}

func (o OCR) Crop(ctx context.Context, document Document, region Region) (Document, [9]float64, error) {
	transform := identityTransform()
	if o.Endpoint == "" || !ValidRegion(region) {
		return Document{}, transform, errors.New("invalid crop request")
	}
	u, err := url.Parse(o.Endpoint)
	if err != nil {
		return Document{}, transform, errors.New("invalid OCR endpoint")
	}
	u = u.JoinPath("crop")
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "original")
	if err != nil {
		return Document{}, transform, fmt.Errorf("create crop document part: %w", err)
	}
	if _, err := part.Write(document.Data); err != nil {
		return Document{}, transform, fmt.Errorf("write crop document part: %w", err)
	}
	for key, value := range map[string]string{
		"page": strconv.Itoa(region.Page), "top": strconv.FormatFloat(region.Top, 'f', -1, 64),
		"left": strconv.FormatFloat(region.Left, 'f', -1, 64), "bottom": strconv.FormatFloat(region.Bottom, 'f', -1, 64),
		"right": strconv.FormatFloat(region.Right, 'f', -1, 64),
	} {
		if err := writer.WriteField(key, value); err != nil {
			return Document{}, transform, fmt.Errorf("write crop region: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return Document{}, transform, fmt.Errorf("complete crop form: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), &body)
	if err != nil {
		return Document{}, transform, fmt.Errorf("create crop request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client := o.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return Document{}, transform, errors.New("image crop request failed")
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, MaxDocumentBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		return Document{}, transform, errors.Join(errors.New("read cropped image failed"), readErr, closeErr)
	}
	if response.StatusCode == http.StatusBadRequest {
		var problem struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(data, &problem) == nil && strings.Contains(problem.Detail, "page is out of range") {
			return Document{}, transform, ErrInvalidCropPage
		}
	}
	if response.StatusCode != http.StatusOK || len(data) == 0 || len(data) > MaxDocumentBytes || response.Header.Get("Content-Type") != mimeTypePNG {
		return Document{}, transform, fmt.Errorf("image crop service returned HTTP %d", response.StatusCode)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return Document{}, transform, errors.New("invalid cropped image")
	}
	var values [9]float64
	if err := json.Unmarshal([]byte(response.Header.Get("X-Coordinate-Transform")), &values); err != nil {
		return Document{}, transform, errors.New("invalid image crop transform")
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return Document{}, transform, errors.New("invalid image crop transform")
		}
	}
	if values[8] == 0 {
		return Document{}, transform, errors.New("invalid image crop transform")
	}
	return Document{MIMEType: mimeTypePNG, Data: data}, values, nil
}

// CompareOCRは独立した読取を照合し、不一致を人間の確認事項に残す。
func CompareOCR(receipt Receipt, lines []OCRLine) []string {
	var issues []string
	regions := make([]Region, 0, len(receipt.Items))
	for i, item := range receipt.Items {
		if item.Region == nil {
			issues = append(issues, fmt.Sprintf("品目%dの画像領域が未確定です。", i+1))
			continue
		}
		regions = append(regions, *item.Region)
		var texts []string
		for _, line := range lines {
			if overlap(*item.Region, line.Region) > 0.2 {
				texts = append(texts, line.Text)
			}
		}
		joined := normalize(strings.Join(texts, ""))
		if joined == "" || !strings.Contains(joined, normalize(item.PrintedName)) {
			issues = append(issues, fmt.Sprintf("品目%dの原文とOCR読取を確認してください。", i+1))
		}
		amountFound := false
		for _, number := range printedNumber.FindAllString(strings.Join(texts, " "), -1) {
			if strings.Contains(number, ".") {
				continue
			}
			amount, err := strconv.ParseInt(strings.ReplaceAll(number, ",", ""), 10, 64)
			if err == nil && amount == item.PrintedAmount {
				amountFound = true
				break
			}
		}
		if !amountFound {
			issues = append(issues, fmt.Sprintf("品目%dの印字金額とOCR読取を確認してください。", i+1))
		}
	}
	if hasUnmatchedReceiptLine(lines, regions) {
		issues = append(issues, "OCRにのみ読み取られた明細候補を確認してください。")
	}
	return issues
}

func hasUnmatchedReceiptLine(lines []OCRLine, regions []Region) bool {
	for _, line := range lines {
		if line.Confidence < 0.5 || !validRegion(line.Region) || !hasPrintedAmount(line.Text) || !hasLetters(line.Text) || isReceiptSummaryLine(line.Text) {
			continue
		}
		matched := false
		for _, region := range regions {
			if overlap(region, line.Region) > 0.2 {
				matched = true
				break
			}
		}
		if !matched {
			return true
		}
	}
	return false
}

func hasPrintedAmount(text string) bool {
	for _, number := range printedNumber.FindAllString(text, -1) {
		if _, err := strconv.ParseInt(strings.ReplaceAll(number, ",", ""), 10, 64); err == nil {
			return true
		}
	}
	return false
}

func hasLetters(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func isReceiptSummaryLine(text string) bool {
	text = strings.ToLower(text)
	for _, marker := range []string{"合計", "小計", "税", "対象", "値引", "割引", "クーポン", "ポイント", "現金", "カード", "お預", "釣銭", "返品", "会計", "領収", "番号", "担当", "電話", "登録", "total", "subtotal", "tax", receiptDiscountKey, "coupon", "payment", "cash", "credit", "change", "receipt", "no."} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return printedDate.MatchString(text) || printedJapaneseDate.MatchString(text)
}

func overlap(a, b Region) float64 {
	if a.Page != b.Page {
		return 0
	}
	width := max(0, min(a.Right, b.Right)-max(a.Left, b.Left))
	height := max(0, min(a.Bottom, b.Bottom)-max(a.Top, b.Top))
	intersection := width * height
	area := min((a.Right-a.Left)*(a.Bottom-a.Top), (b.Right-b.Left)*(b.Bottom-b.Top))
	if area <= 0 {
		return 0
	}
	return intersection / area
}
