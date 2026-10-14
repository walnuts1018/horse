package ingestion

import (
	"context"
	"encoding/base64"
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
