package ingestion

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOCRCorroboration(t *testing.T) {
	region := Region{Top: 0.1, Left: 0.1, Bottom: 0.2, Right: 0.9}
	receipt := Receipt{Items: []Item{{PrintedName: "ヨーグルト", PrintedAmount: 198, Region: &region}}}
	if issues := CompareOCR(receipt, []OCRLine{{Text: "ヨーグルト 198", Region: region}}); len(issues) != 0 {
		t.Fatal(issues)
	}
	if issues := CompareOCR(receipt, []OCRLine{{Text: "ヨーグルト 196", Region: region}}); len(issues) != 1 {
		t.Fatalf("amount disagreement was not flagged: %v", issues)
	}
	other := Region{Top: 0.8, Left: 0.1, Bottom: 0.9, Right: 0.9}
	if issues := CompareOCR(receipt, []OCRLine{{Text: "ヨーグルト 198", Region: other}}); len(issues) != 2 {
		t.Fatalf("incorrect evidence region was not flagged: %v", issues)
	}
}

func TestOCRCropUsesRequestedPageAndValidatesPreview(t *testing.T) {
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
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("X-Coordinate-Transform", `[1,0,0,0,1,0,0,0,1]`)
		_, _ = w.Write(pngData.Bytes())
	}))
	defer server.Close()

	preview, transform, err := (OCR{Endpoint: server.URL}).Crop(context.Background(), Document{MIMEType: "application/pdf", Data: []byte("immutable pdf")}, Region{Page: 2, Top: 0, Left: 0, Bottom: 1, Right: 1})
	if err != nil {
		t.Fatal(err)
	}
	if preview.MIMEType != "image/png" || !bytes.Equal(preview.Data, pngData.Bytes()) || transform != identityTransform() {
		t.Fatalf("unexpected full-page preview: type=%q transform=%v", preview.MIMEType, transform)
	}
}

func TestOCRCropReportsInvalidPageAndRejectsInvalidPreview(t *testing.T) {
	t.Run("invalid page", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"PDF page is out of range"}`))
		}))
		defer server.Close()
		_, _, err := (OCR{Endpoint: server.URL}).Crop(context.Background(), Document{Data: []byte("pdf")}, Region{Page: 3, Top: 0, Left: 0, Bottom: 1, Right: 1})
		if !errors.Is(err, ErrInvalidCropPage) {
			t.Fatalf("expected invalid page error, got %v", err)
		}
	})
	t.Run("invalid image response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("X-Coordinate-Transform", `[1,0,0,0,1,0,0,0,1]`)
			_, _ = w.Write([]byte("not an image"))
		}))
		defer server.Close()
		_, _, err := (OCR{Endpoint: server.URL}).Crop(context.Background(), Document{Data: []byte("image")}, Region{Top: 0, Left: 0, Bottom: 1, Right: 1})
		if err == nil {
			t.Fatal("invalid PNG response was accepted")
		}
	})
}
