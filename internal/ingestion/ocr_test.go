package ingestion

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	yogurtName              = "ヨーグルト"
	yogurtReceiptLine       = yogurtName + " 198"
	statementOccurredAt     = "2026-10-14T12:00:00+09:00"
	statementAmountOCRIssue = "取引1の印字金額とOCR読取を確認してください。"
	statementDateOCRIssue   = "取引1の日付とOCR読取を確認してください。"
)

func TestOCRCorroboration(t *testing.T) {
	t.Parallel()
	region := Region{Top: 0.1, Left: 0.1, Bottom: 0.2, Right: 0.9}
	receipt := Receipt{Items: []Item{{PrintedName: yogurtName, PrintedAmount: 198, Region: &region}}}
	if issues := CompareOCR(receipt, []OCRLine{{Text: yogurtReceiptLine, Region: region}}); len(issues) != 0 {
		t.Fatal(issues)
	}
	if issues := CompareOCR(receipt, []OCRLine{{Text: yogurtName + " 196", Region: region}}); len(issues) != 1 {
		t.Fatalf("amount disagreement was not flagged: %v", issues)
	}
	other := Region{Top: 0.8, Left: 0.1, Bottom: 0.9, Right: 0.9}
	if issues := CompareOCR(receipt, []OCRLine{{Text: yogurtReceiptLine, Region: other}}); len(issues) != 2 {
		t.Fatalf("incorrect evidence region was not flagged: %v", issues)
	}
}

func TestOCRCorroborationFindsUnmatchedItemRows(t *testing.T) {
	t.Parallel()
	itemRegion := Region{Top: 0.1, Left: 0.1, Bottom: 0.2, Right: 0.9}
	missedItemRegion := Region{Top: 0.25, Left: 0.1, Bottom: 0.35, Right: 0.9}
	receipt := Receipt{Items: []Item{{PrintedName: yogurtName, PrintedAmount: 198, Region: &itemRegion}}}
	lines := make([]OCRLine, 0, 3)
	lines = append(lines,
		OCRLine{Text: yogurtReceiptLine, Confidence: 0.95, Region: itemRegion},
		OCRLine{Text: "月見バーガー 430", Confidence: 0.92, Region: missedItemRegion},
	)
	issues := CompareOCR(receipt, lines)
	if len(issues) != 1 || issues[0] != "OCRにのみ読み取られた明細候補を確認してください。" {
		t.Fatalf("unmatched item row was not reported: %v", issues)
	}

	lines = append(lines, OCRLine{Text: "小計 366", Confidence: 0.99, Region: Region{Top: 0.8, Left: 0.1, Bottom: 0.9, Right: 0.9}})
	if got := CompareOCR(receipt, lines); len(got) != 1 {
		t.Fatalf("summary line was reported as an unmatched item: %v", got)
	}
	if got := CompareOCR(Receipt{}, lines); len(got) != 1 {
		t.Fatalf("receipt with no recognized items did not report OCR item rows: %v", got)
	}
	lines = append(lines, OCRLine{Text: "2026年10月13日", Confidence: 0.99, Region: Region{Top: 0.95, Left: 0.1, Bottom: 0.99, Right: 0.9}})
	if got := CompareOCR(receipt, lines); len(got) != 1 {
		t.Fatalf("receipt date was reported as an unmatched item: %v", got)
	}
}

func TestStatementOCRCorroborationRequiresMerchantAmountAndAllRows(t *testing.T) {
	t.Parallel()
	first := Region{Top: 0.1, Left: 0.1, Bottom: 0.2, Right: 0.9}
	second := Region{Top: 0.25, Left: 0.1, Bottom: 0.35, Right: 0.9}
	entries := []StatementEntry{
		{OccurredAt: statementOccurredAt, Merchant: "Market", Amount: -1234, Region: &first},
		{OccurredAt: statementOccurredAt, Merchant: "Cafe", Amount: 500, Region: &second},
	}
	lines := make([]OCRLine, 0, 3)
	lines = append(lines,
		OCRLine{Text: "Market 2026/10/14 -1,234", Confidence: 0.95, Region: first},
		OCRLine{Text: "Cafe 2026/10/14 500", Confidence: 0.95, Region: second},
	)
	if issues := CompareStatementOCR(entries, lines); len(issues) != 0 {
		t.Fatalf("matching statement rows were not corroborated: %v", issues)
	}
	lines[1].Text = "Cafe 2026/10/14 501"
	if issues := CompareStatementOCR(entries, lines); len(issues) != 1 {
		t.Fatalf("amount disagreement was not flagged: %v", issues)
	}
	lines[1].Text = "Cafe 2026/10/14 500"
	lines = append(lines, OCRLine{Text: "Bakery 300", Confidence: 0.95, Region: Region{Top: 0.4, Left: 0.1, Bottom: 0.5, Right: 0.9}})
	if issues := CompareStatementOCR(entries, lines); len(issues) != 1 || issues[0] != "OCRにのみ読み取られた取引候補を確認してください。" {
		t.Fatalf("unmatched transaction row was not reported: %v", issues)
	}
}

func TestStatementOCRRequiresMatchingSignAndFullDate(t *testing.T) {
	t.Parallel()
	region := Region{Top: 0.1, Left: 0.1, Bottom: 0.2, Right: 0.9}
	date := statementOccurredAt
	tests := []struct {
		name   string
		amount int64
		text   string
		want   []string
	}{
		{name: "japanese negative marker and date", amount: -1234, text: "Market 2026年10月14日 △1,234"},
		{name: "fullwidth minus and date", amount: -1234, text: "Market 2026/10/14 －1,234"},
		{name: "parenthesized negative", amount: -1234, text: "Market (1,234) 2026/10/14"},
		{name: "unsigned OCR cannot verify negative", amount: -1234, text: "Market 2026-10-14 1,234", want: []string{statementAmountOCRIssue}},
		{name: "wrong sign", amount: 1234, text: "Market 2026-10-14 -1,234", want: []string{statementAmountOCRIssue}},
		{name: "wrong day", amount: 1234, text: "Market 2026/10/15 1,234", want: []string{statementDateOCRIssue}},
		{name: "yearless date", amount: 1234, text: "Market 10/14 1,234", want: []string{statementDateOCRIssue}},
		{name: "ambiguous year-last date", amount: 1234, text: "Market 10/11/2026 1,234", want: []string{statementDateOCRIssue}},
		{name: "unambiguous month-day-year", amount: 1234, text: "Market 10/14/2026 1,234"},
		{name: "date component is not transaction amount", amount: 14, text: "Market 2026-10-14", want: []string{statementAmountOCRIssue}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entry := StatementEntry{OccurredAt: date, Merchant: "Market", Amount: tt.amount, Region: &region}
			got := CompareStatementOCR([]StatementEntry{entry}, []OCRLine{{Text: tt.text, Confidence: 0.95, Region: region}})
			if len(got) != len(tt.want) {
				t.Fatalf("got issues %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("got issues %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestOCRExtractAcceptsLinesFromEachPDFPage(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract" {
			t.Fatalf("unexpected OCR path: %s", r.URL.Path)
		}
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		part, err := reader.NextPart()
		if err != nil || part.FileName() != "original.pdf" {
			t.Fatalf("PDF filename was not sent: part=%v err=%v", part, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pageCount":2,"lines":[{"text":"ヨーグルト 198","confidence":0.95,"region":{"page":0,"top":0.1,"left":0.1,"bottom":0.2,"right":0.9}},{"text":"月見バーガー 430","confidence":0.92,"region":{"page":1,"top":0.1,"left":0.1,"bottom":0.2,"right":0.9}}]}`))
	}))
	defer server.Close()

	lines, err := (OCR{Endpoint: server.URL}).Extract(t.Context(), Document{MIMEType: mimeTypePDF, Data: []byte("pdf")})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0].Region.Page != 0 || lines[1].Region.Page != 1 {
		t.Fatalf("unexpected PDF OCR lines: %+v", lines)
	}
}

func TestOCRExtractRejectsLinesOutsidePDFPageCount(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pageCount":2,"lines":[{"text":"ヨーグルト 198","confidence":0.95,"region":{"page":2,"top":0.1,"left":0.1,"bottom":0.2,"right":0.9}}]}`))
	}))
	defer server.Close()
	if _, err := (OCR{Endpoint: server.URL}).Extract(t.Context(), Document{MIMEType: mimeTypePDF, Data: []byte("pdf")}); err == nil {
		t.Fatal("OCR line outside the PDF page count was accepted")
	}
}

func TestOCRCorroborationFailsClosedForUnavailableOrMismatchedPages(t *testing.T) {
	t.Parallel()
	region := Region{Page: 1, Top: 0.1, Left: 0.1, Bottom: 0.2, Right: 0.9}
	receipt := Receipt{Items: []Item{{PrintedName: yogurtName, PrintedAmount: 198, Region: &region}}}
	t.Run("unavailable", func(t *testing.T) {
		t.Parallel()
		verification := Verification{Balanced: true}
		applyOCRCorroboration(&verification, nil, errors.New("OCR timeout"))
		if verification.Balanced || len(verification.Issues) == 0 {
			t.Fatalf("OCR failure did not require review: %+v", verification)
		}
	})
	t.Run("mismatched page", func(t *testing.T) {
		t.Parallel()
		verification := Verification{Receipt: receipt, Balanced: true}
		applyOCRCorroboration(&verification, []OCRLine{{Text: yogurtReceiptLine, Region: Region{Page: 0, Top: 0.1, Left: 0.1, Bottom: 0.2, Right: 0.9}}}, nil)
		if verification.Balanced || len(verification.Issues) == 0 {
			t.Fatalf("page mismatch did not require review: %+v", verification)
		}
	})
}

func TestOCRCropUsesRequestedPageAndValidatesPreview(t *testing.T) {
	t.Parallel()
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crop" {
			t.Fatalf("unexpected OCR path: %s", r.URL.Path)
		}
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || params["boundary"] == "" {
			t.Fatalf("invalid multipart request: %v", err)
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		fields := map[string]string{}
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			if part.FormName() != "image" {
				var value bytes.Buffer
				_, _ = value.ReadFrom(part)
				fields[part.FormName()] = value.String()
			}
			_ = part.Close()
		}
		if fields["page"] != "2" || fields["top"] != "0" || fields["left"] != "0" || fields["bottom"] != "1" || fields["right"] != "1" {
			t.Errorf("unexpected preview crop fields: %v", fields)
		}
		w.Header().Set("Content-Type", mimeTypePNG)
		w.Header().Set("X-Coordinate-Transform", `[1,0,0,0,1,0,0,0,1]`)
		_, _ = w.Write(pngData.Bytes())
	}))
	defer server.Close()

	preview, transform, err := (OCR{Endpoint: server.URL}).Crop(t.Context(), Document{MIMEType: mimeTypePDF, Data: []byte("immutable pdf")}, Region{Page: 2, Top: 0, Left: 0, Bottom: 1, Right: 1})
	if err != nil {
		t.Fatal(err)
	}
	if preview.MIMEType != mimeTypePNG || !bytes.Equal(preview.Data, pngData.Bytes()) || transform != identityTransform() {
		t.Fatalf("unexpected full-page preview: type=%q transform=%v", preview.MIMEType, transform)
	}
}

func TestOCRCropReportsInvalidPageAndRejectsInvalidPreview(t *testing.T) {
	t.Parallel()
	t.Run("invalid page", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"PDF page is out of range"}`))
		}))
		defer server.Close()
		_, _, err := (OCR{Endpoint: server.URL}).Crop(t.Context(), Document{Data: []byte("pdf")}, Region{Page: 3, Top: 0, Left: 0, Bottom: 1, Right: 1})
		if !errors.Is(err, ErrInvalidCropPage) {
			t.Fatalf("expected invalid page error, got %v", err)
		}
	})
	t.Run("invalid image response", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", mimeTypePNG)
			w.Header().Set("X-Coordinate-Transform", `[1,0,0,0,1,0,0,0,1]`)
			_, _ = w.Write([]byte("not an image"))
		}))
		defer server.Close()
		_, _, err := (OCR{Endpoint: server.URL}).Crop(t.Context(), Document{Data: []byte("image")}, Region{Top: 0, Left: 0, Bottom: 1, Right: 1})
		if err == nil {
			t.Fatal("invalid PNG response was accepted")
		}
	})
}
