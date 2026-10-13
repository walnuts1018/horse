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

const yogurtReceiptLine = "ヨーグルト 198"

func TestOCRCorroboration(t *testing.T) {
	t.Parallel()
	region := Region{Top: 0.1, Left: 0.1, Bottom: 0.2, Right: 0.9}
	receipt := Receipt{Items: []Item{{PrintedName: "ヨーグルト", PrintedAmount: 198, Region: &region}}}
	if issues := CompareOCR(receipt, []OCRLine{{Text: yogurtReceiptLine, Region: region}}); len(issues) != 0 {
		t.Fatal(issues)
	}
	if issues := CompareOCR(receipt, []OCRLine{{Text: "ヨーグルト 196", Region: region}}); len(issues) != 1 {
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
	receipt := Receipt{Items: []Item{{PrintedName: "ヨーグルト", PrintedAmount: 198, Region: &itemRegion}}}
	lines := make([]OCRLine, 0, 4)
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
	receipt := Receipt{Items: []Item{{PrintedName: "ヨーグルト", PrintedAmount: 198, Region: &region}}}
	t.Run("unavailable", func(t *testing.T) {
		verification := Verification{Balanced: true}
		applyOCRCorroboration(&verification, nil, errors.New("OCR timeout"))
		if verification.Balanced || len(verification.Issues) == 0 {
			t.Fatalf("OCR failure did not require review: %+v", verification)
		}
	})
	t.Run("mismatched page", func(t *testing.T) {
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
