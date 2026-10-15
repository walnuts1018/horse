package ingestion

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testEmailSource = "email"

type attachmentOnlyStore struct {
	document Document
}

func (s attachmentOnlyStore) Get(context.Context, string, string) (Document, error) {
	return s.document, nil
}

type recordingDocumentStore struct {
	document Document
	keys     []string
	mimeType []string
}

func (s *recordingDocumentStore) Get(_ context.Context, key, mimeType string) (Document, error) {
	s.keys = append(s.keys, key)
	s.mimeType = append(s.mimeType, mimeType)
	document := s.document
	document.MIMEType = mimeType
	return document, nil
}

func TestWorkerExtractionSelectsAnalysisObjectExceptForOriginalBasedCrop(t *testing.T) {
	t.Parallel()
	transform := identityTransform()
	tests := []struct {
		name         string
		request      string
		analysisKey  string
		analysisMIME string
		transform    *[9]float64
		wantKey      string
		wantMIME     string
	}{
		{name: "saved analysis object", request: `{}`, analysisKey: "analysis", analysisMIME: mimeTypePNG, transform: &transform, wantKey: "analysis", wantMIME: mimeTypePNG},
		{name: "manual crop uses original coordinates", request: `{"crop":{"page":0,"top":0.1,"left":0.1,"bottom":0.9,"right":0.9}}`, analysisKey: "analysis", analysisMIME: mimeTypePNG, transform: &transform, wantKey: "original", wantMIME: mimeTypeJPEG},
		{name: "legacy job uses original", request: `{}`, wantKey: "original", wantMIME: mimeTypeJPEG},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := &recordingDocumentStore{document: Document{Data: []byte("invalid image")}}
			worker := Worker{Store: store}
			_, _ = worker.extract(t.Context(), job{
				ObjectKey: "original", MIMEType: mimeTypeJPEG, AnalysisObjectKey: test.analysisKey,
				AnalysisMIMEType: test.analysisMIME, AnalysisTransform: test.transform, SourceKind: "receipt", Request: []byte(test.request),
			})
			if len(store.keys) != 1 || store.keys[0] != test.wantKey || store.mimeType[0] != test.wantMIME {
				t.Fatalf("worker loaded the wrong image: keys=%v mimeTypes=%v", store.keys, store.mimeType)
			}
		})
	}
}

func TestWorkerExtractionSkipsGeminiForAttachmentOnlyEmail(t *testing.T) {
	t.Parallel()
	pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 synthetic receipt"))
	message := []byte("MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=mail\r\n\r\n" +
		"--mail\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=receipt.pdf\r\nContent-Transfer-Encoding: base64\r\n\r\n" + pdf + "\r\n--mail--\r\n")
	worker := Worker{Store: attachmentOnlyStore{document: Document{MIMEType: mimeTypeEmail, Data: message}}}
	result, err := worker.extract(t.Context(), job{ObjectKey: testEmailSource, MIMEType: mimeTypeEmail, SourceKind: testEmailSource, Request: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Balanced || len(result.Issues) != 1 || result.Issues[0] != "メール本文が空のため、添付ファイルを個別に確認してください。" || len(result.Runs) != 0 {
		t.Fatalf("attachment-only email did not produce a review result without extraction runs: %+v", result)
	}
}

func TestWorkerExtractionKeepsAttachmentlessEmptyEmailFailure(t *testing.T) {
	t.Parallel()
	message := []byte("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n")
	worker := Worker{Store: attachmentOnlyStore{document: Document{MIMEType: mimeTypeEmail, Data: message}}}
	_, err := worker.extract(t.Context(), job{ObjectKey: testEmailSource, MIMEType: mimeTypeEmail, SourceKind: testEmailSource, Request: []byte("{}")})
	if err == nil {
		t.Fatal("attachmentless empty email unexpectedly succeeded")
	}
}

func TestWorkerExtractionVerifiesBankCSVMeaning(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		data         string
		wantBalanced bool
		wantError    bool
	}{
		{name: "positive income", data: "date,merchant,amount,transactionKind\n2026-10-01,給与,100000,給与\n", wantBalanced: true},
		{name: "negative income", data: "date,merchant,amount,transactionKind\n2026-10-01,給与,-100000,給与\n", wantError: true},
		{name: "unclassified transaction", data: "date,merchant,amount,transactionKind\n2026-10-01,振込,100000,不明\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			worker := Worker{Store: attachmentOnlyStore{document: Document{MIMEType: mimeTypeCSV, Data: []byte(test.data)}}}
			result, err := worker.extract(t.Context(), job{ObjectKey: "statement.csv", MIMEType: mimeTypeCSV, SourceKind: statementSourceBank, Request: []byte("{}")})
			if (err != nil) != test.wantError {
				t.Fatalf("CSV extraction error = %v, want error %t", err, test.wantError)
			}
			if err != nil {
				return
			}
			if result.Balanced != test.wantBalanced {
				t.Fatalf("CSV balanced = %t, want %t", result.Balanced, test.wantBalanced)
			}
			if test.name == "unclassified transaction" && len(result.Issues) != 1 {
				t.Fatalf("unclassified CSV issues = %v, want one review issue", result.Issues)
			}
		})
	}
}

func TestWorkerExtractUsesGeminiDeveloperAPIAndLocalOCR(t *testing.T) {
	t.Parallel()
	ocrCalls, geminiCalls := 0, 0
	ocr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ocrCalls++
		if r.URL.Path != "/extract" {
			t.Errorf("unexpected OCR path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pageCount":1,"lines":[]}`))
	}))
	defer ocr.Close()
	gemini := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		geminiCalls++
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Errorf("unexpected Gemini path: %s", r.URL.Path)
		}
		writeGeminiSuccess(w, `{"merchant":"Shop","currency":"JPY","items":[{"printedName":"Tea","name":"Tea","printedAmount":100,"discount":0,"priceBasis":"EXEMPT","confidence":1,"region":{"page":0,"top":0.1,"left":0.1,"bottom":0.2,"right":0.8}}],"taxGroups":[],"discount":0,"total":100,"payments":[],"warnings":[]}`)
	}))
	defer gemini.Close()

	worker := Worker{
		Store: attachmentOnlyStore{document: Document{MIMEType: mimeTypeJPEG, Data: []byte("synthetic image fixture")}},
		OCR:   OCR{Endpoint: ocr.URL, HTTPClient: ocr.Client()},
		Router: GeminiRouter{
			Free: Gemini{APIKey: "unpaid-key", Endpoint: gemini.URL + "/v1beta/", HTTPClient: gemini.Client()},
		},
	}
	result, err := worker.extract(t.Context(), job{
		ObjectKey: "synthetic-fixture", MIMEType: mimeTypeJPEG, SourceKind: "receipt", Request: []byte(`{"model":"gemini-3.8-flash"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if ocrCalls != 1 || geminiCalls != 1 || len(result.Runs) != 1 {
		t.Fatalf("worker did not run OCR and Gemini extraction: ocr=%d Gemini=%d result=%+v", ocrCalls, geminiCalls, result)
	}

	interactionCalls := 0
	interaction := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		interactionCalls++
		if !strings.HasSuffix(r.URL.Path, "/interactions") {
			t.Errorf("unexpected interaction path: %s", r.URL.Path)
		}
		output := `{"reply":"確認しました。","receipt":{"merchant":"Shop","purchasedAt":"2026-10-01","datePrecision":"DATE_ONLY","currency":"JPY","items":[{"printedName":"Tea","name":"Tea","printedAmount":100,"discount":0,"priceBasis":"EXEMPT","confidence":1}],"taxGroups":[],"discount":0,"total":100,"payments":[],"warnings":[]}}`
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintln(w, `data: {"event_type":"step.start","index":0,"step":{"type":"model_output","content":[]}}`)
		_, _ = fmt.Fprintf(w, "\ndata: {\"event_type\":\"step.delta\",\"index\":0,\"delta\":{\"type\":\"text\",\"text\":%s}}\n", mustJSONText(output))
		_, _ = io.WriteString(w, "\ndata: {\"event_type\":\"interaction.completed\",\"interaction\":{\"id\":\"worker-interaction\",\"status\":\"completed\",\"usage\":{\"total_input_tokens\":21,\"total_output_tokens\":34}}}\n\ndata: [DONE]\n\n")
	}))
	defer interaction.Close()
	worker.Router.Free.Endpoint = interaction.URL + "/v1beta/"
	conversation, err := worker.extract(t.Context(), job{
		ObjectKey: "synthetic-fixture", MIMEType: mimeTypeJPEG, SourceKind: "receipt",
		Request: []byte(`{"clarification":"confirm merchant","conversationId":"conversation","model":"gemini-3.8-flash","currentExtraction":{"receipt":{}}}`),
	})
	if err != nil || interactionCalls != 1 || conversation.ConversationID != "conversation" || len(conversation.Conversation) != 2 || len(conversation.Runs) != 1 {
		t.Fatalf("worker did not resume conversation processing: calls=%d result=%+v error=%v", interactionCalls, conversation, err)
	}
}

func TestWorkerCropContinuesThroughGeminiExtraction(t *testing.T) {
	t.Parallel()
	var cropped bytes.Buffer
	if err := png.Encode(&cropped, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	cropCalls, extractCalls := 0, 0
	ocr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/crop":
			cropCalls++
			w.Header().Set("Content-Type", mimeTypePNG)
			w.Header().Set("X-Coordinate-Transform", `[0.5,0,0.25,0,0.5,0.25,0,0,1]`)
			_, _ = w.Write(cropped.Bytes())
		case "/extract":
			extractCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"pageCount":1,"lines":[]}`)
		default:
			t.Errorf("unexpected OCR path: %s", r.URL.Path)
		}
	}))
	defer ocr.Close()
	gemini := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Errorf("unexpected Gemini path: %s", r.URL.Path)
		}
		writeGeminiSuccess(w, `{"merchant":"Shop","currency":"JPY","items":[{"printedName":"Tea","name":"Tea","printedAmount":100,"discount":0,"priceBasis":"EXEMPT","confidence":1,"region":{"page":0,"top":0.1,"left":0.1,"bottom":0.2,"right":0.8}}],"taxGroups":[],"discount":0,"total":100,"payments":[],"warnings":[]}`)
	}))
	defer gemini.Close()
	worker := Worker{
		Store:  attachmentOnlyStore{document: Document{MIMEType: mimeTypeJPEG, Data: []byte("synthetic original image")}},
		OCR:    OCR{Endpoint: ocr.URL, HTTPClient: ocr.Client()},
		Router: GeminiRouter{Free: Gemini{APIKey: "test-key", Endpoint: gemini.URL + "/v1beta/", HTTPClient: gemini.Client()}},
	}
	result, err := worker.extract(t.Context(), job{
		ObjectKey: "synthetic-fixture", MIMEType: mimeTypeJPEG, SourceKind: "receipt",
		Request: []byte(`{"model":"gemini-3.8-flash","crop":{"page":0,"top":0.1,"left":0.1,"bottom":0.9,"right":0.9}}`),
	})
	if err != nil || cropCalls != 1 || extractCalls != 1 || len(result.Runs) != 1 || result.Transform != [9]float64{0.5, 0, 0.25, 0, 0.5, 0.25, 0, 0, 1} {
		t.Fatalf("cropped document did not complete Gemini extraction: crop=%d OCR=%d result=%+v error=%v", cropCalls, extractCalls, result, err)
	}
}

func TestWorkerExtractionWithoutGeminiKeyReturnsConfigurationError(t *testing.T) {
	t.Parallel()
	worker := Worker{Store: attachmentOnlyStore{document: Document{MIMEType: mimeTypePDF, Data: []byte("synthetic PDF")}}}
	_, err := worker.extract(t.Context(), job{ObjectKey: "synthetic-fixture", MIMEType: mimeTypePDF, SourceKind: "receipt", Request: []byte("{}")})
	if err == nil || !strings.Contains(err.Error(), "Gemini API key is not configured") {
		t.Fatalf("missing Gemini key error = %v, want ordinary configuration failure", err)
	}
}
