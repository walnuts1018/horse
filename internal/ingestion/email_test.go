package ingestion

import (
	"bytes"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiExtractParsesEmailBodyAndWarnsAboutUnparsedAttachments(t *testing.T) {
	t.Parallel()
	message := syntheticEmail(t, true)
	var requestText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Contents []struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
		}
		if err := json.Unmarshal(mustReadBody(t, r), &request); err != nil {
			t.Errorf("decode generation request: %v", err)
		}
		for _, part := range request.Contents[0].Parts {
			requestText += part.Text
		}
		writeGeminiSuccess(w, `{"merchant":"Example Market","currency":"JPY","items":[],"taxGroups":[],"discount":0,"total":0,"payments":[],"warnings":[]}`)
	}))
	t.Cleanup(server.Close)

	gemini := Gemini{APIKey: testGeminiAPIKey, Model: "test-model", Endpoint: server.URL + "/v1beta/", HTTPClient: server.Client()}
	receipt, _, err := gemini.ExtractRun(t.Context(), Document{MIMEType: mimeTypeEmail, Data: message}, nil, GeminiRequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"From: サンプル店舗", "Subject: 注文内容", "注文番号: ORD-123", "合計: ¥1,234"} {
		if !strings.Contains(requestText, want) {
			t.Errorf("generation text is missing decoded email content %q", want)
		}
	}
	for _, forbidden := range []string{"outer-boundary", "alternative-boundary", "Content-Transfer-Encoding", "invoice.pdf", "JVBERi0xLjQ=", "HTML FALLBACK"} {
		if strings.Contains(requestText, forbidden) {
			t.Errorf("generation text contains MIME data that should have been removed: %q", forbidden)
		}
	}
	if len(receipt.Warnings) != 1 || receipt.Warnings[0] != emailAttachmentReviewWarning {
		t.Fatalf("unparsed attachment warning was not retained: %#v", receipt.Warnings)
	}
}

func TestParseEmailExtractsValidatedAttachments(t *testing.T) {
	t.Parallel()
	attachments, err := ParseEmailAttachments(syntheticEmail(t, true))
	if err != nil {
		t.Fatal(err)
	}
	if len(attachments) != 1 {
		t.Fatalf("attachment count = %d, want 1", len(attachments))
	}
	attachment := attachments[0]
	if attachment.Filename != "invoice.pdf" || attachment.MIMEType != mimeTypePDF || !bytes.HasPrefix(attachment.Data, []byte("%PDF-")) {
		t.Fatalf("unexpected extracted attachment: %#v", attachment)
	}
}

func TestParseEmailAllowsAttachmentOnlyMessagesAndRejectsMIMEMismatch(t *testing.T) {
	t.Parallel()
	pdf := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 synthetic receipt"))
	attachmentOnly := []byte("MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=mail\r\n\r\n" +
		"--mail\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=receipt.pdf\r\nContent-Transfer-Encoding: base64\r\n\r\n" + pdf + "\r\n--mail--\r\n")
	parsed, err := parseEmail(attachmentOnly)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Body != "" || len(parsed.Attachments) != 1 {
		t.Fatalf("attachment-only message was not retained: %#v", parsed)
	}

	mismatch := []byte("MIME-Version: 1.0\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=receipt.pdf\r\n\r\nnot a PDF")
	if _, err := parseEmail(mismatch); err == nil {
		t.Fatal("parseEmail() accepted a PDF MIME type with non-PDF contents")
	}
}

func TestParseEmailRejectsExcessiveAttachmentCount(t *testing.T) {
	t.Parallel()
	var message strings.Builder
	message.WriteString("MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=mail\r\n\r\n")
	for range maxEmailAttachments + 1 {
		message.WriteString("--mail\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=receipt.pdf\r\n\r\n%PDF-1.4\r\n")
	}
	message.WriteString("--mail--\r\n")
	if _, err := parseEmail([]byte(message.String())); err == nil {
		t.Fatal("parseEmail() accepted too many attachments")
	}
}

func TestParseEmailUsesHTMLWhenPlainTextIsUnavailable(t *testing.T) {
	t.Parallel()
	body := base64.StdEncoding.EncodeToString([]byte(`<html><head><title>ignored title</title></head><body><p>注文内容</p><script>ignored script</script><p>合計&nbsp;1,234円</p></body></html>`))
	message := fmt.Sprintf("MIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", body)
	parsed, err := parseEmail([]byte(message))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"注文内容", "合計 1,234円"} {
		if !strings.Contains(parsed.Body, want) {
			t.Errorf("HTML fallback is missing visible text %q: %q", want, parsed.Body)
		}
	}
	for _, forbidden := range []string{"ignored title", "ignored script", "<p>"} {
		if strings.Contains(parsed.Body, forbidden) {
			t.Errorf("HTML fallback retained markup or hidden content %q", forbidden)
		}
	}
}

func TestParseEmailConvertsHTMLTablesAndListsToMarkdown(t *testing.T) {
	t.Parallel()
	message := "MIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		`<html><head><title>ignored title</title></head><body><table><thead><tr><th>商品</th><th>金額</th></tr></thead><tbody><tr><td>りんご</td><td>100円</td></tr></tbody></table><ul><li>袋</li><li>割引</li></ul><script>ignored script</script></body></html>`
	parsed, err := parseEmail([]byte(message))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| 商品", "| 金額", "| りんご", "100円", "- 袋", "- 割引"} {
		if !strings.Contains(parsed.Body, want) {
			t.Errorf("HTML Markdown is missing %q: %q", want, parsed.Body)
		}
	}
	for _, forbidden := range []string{"ignored title", "ignored script", "<td>"} {
		if strings.Contains(parsed.Body, forbidden) {
			t.Errorf("HTML Markdown retained hidden content or markup %q: %q", forbidden, parsed.Body)
		}
	}
}

func TestParseEmailDecodesLegacyCharset(t *testing.T) {
	t.Parallel()
	message := "MIME-Version: 1.0\r\nContent-Type: text/plain; charset=iso-8859-1\r\n\r\nCaf\xe9"
	parsed, err := parseEmail([]byte(message))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Body != "Café" {
		t.Fatalf("legacy charset was not decoded: %q", parsed.Body)
	}
}

func TestParseEmailRejectsUnsafeMIMEInputs(t *testing.T) {
	t.Parallel()
	var tooManyParts strings.Builder
	tooManyParts.WriteString("MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=part\r\n\r\n")
	for range maxEmailMIMEParts + 1 {
		tooManyParts.WriteString("--part\r\nContent-Type: text/plain\r\n\r\nitem\r\n")
	}
	tooManyParts.WriteString("--part--\r\n")
	tooDeep := "Content-Type: text/plain\r\n\r\nitem"
	for depth := range maxEmailMIMEDepth + 1 {
		boundary := fmt.Sprintf("nested-%d", depth)
		tooDeep = fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n\r\n--%s\r\n%s\r\n--%s--\r\n", boundary, boundary, tooDeep, boundary)
	}

	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "oversized message",
			data: bytes.Repeat([]byte("x"), MaxDocumentBytes+1),
		},
		{
			name: "too many MIME parts",
			data: []byte(tooManyParts.String()),
		},
		{
			name: "unsupported transfer encoding",
			data: []byte("MIME-Version: 1.0\r\nContent-Type: text/plain\r\nContent-Transfer-Encoding: x-rot13\r\n\r\nabc"),
		},
		{
			name: "malformed base64 transfer encoding",
			data: []byte("MIME-Version: 1.0\r\nContent-Type: text/plain\r\nContent-Transfer-Encoding: base64\r\n\r\n%%%"),
		},
		{
			name: "excessive MIME nesting",
			data: []byte(tooDeep),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parseEmail(tt.data); err == nil {
				t.Fatal("parseEmail() succeeded for unsafe MIME input")
			}
		})
	}
}

func TestParseEmailOmitsSenderAddressWithoutDisplayName(t *testing.T) {
	t.Parallel()
	parsed, err := parseEmail([]byte("From: privacy@example.test\r\nSubject: 注文\r\n\r\n注文内容"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(parsed.text(), "privacy@example.test") || strings.Contains(parsed.text(), "From:") {
		t.Fatalf("email address without a display name was included in model input: %q", parsed.text())
	}
}

func syntheticEmail(t *testing.T, withAttachment bool) []byte {
	t.Helper()
	var plain bytes.Buffer
	writer := quotedprintable.NewWriter(&plain)
	if _, err := io.WriteString(writer, "注文番号: ORD-123\r\n合計: ¥1,234"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	htmlBody := base64.StdEncoding.EncodeToString([]byte(`<html><body>HTML FALLBACK</body></html>`))
	subject := mime.BEncoding.Encode("UTF-8", "注文内容")
	from := mime.BEncoding.Encode("UTF-8", "サンプル店舗") + " <orders@example.test>"
	email := fmt.Sprintf("From: %s\r\nSubject: %s\r\nDate: Fri, 16 Oct 2026 10:00:00 +0900\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=outer-boundary\r\n\r\n--outer-boundary\r\nContent-Type: multipart/alternative; boundary=alternative-boundary\r\n\r\n--alternative-boundary\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n%s\r\n--alternative-boundary\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n--alternative-boundary--\r\n", from, subject, plain.String(), htmlBody)
	if withAttachment {
		attachment := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 synthetic attachment"))
		email += fmt.Sprintf("--outer-boundary\r\nContent-Type: application/pdf; name=invoice.pdf\r\nContent-Disposition: attachment; filename=invoice.pdf\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", attachment)
	}
	email += "--outer-boundary--\r\n"
	return []byte(email)
}
