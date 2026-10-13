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
