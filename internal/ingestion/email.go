package ingestion

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
	"time"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	messageMail "github.com/emersion/go-message/mail"
	"golang.org/x/net/html"
)

const (
	maxEmailMIMEDepth            = 16
	maxEmailMIMEParts            = 256
	emailAttachmentReviewWarning = "メールに未解析の添付ファイルがあります。添付内容と抽出結果を照合してください。"
)

type emailContent struct {
	From                   string
	Subject                string
	Date                   string
	Body                   string
	HasUnparsedAttachments bool
}

type emailMIMEContent struct {
	Plain         []string
	HTML          []string
	HasAttachment bool
}

type emailMIMEState struct {
	parts int
	size  int
}

func parseEmail(data []byte) (emailContent, error) {
	if len(data) == 0 || len(data) > MaxDocumentBytes {
		return emailContent{}, errors.New("invalid email size")
	}
	root, err := message.Read(bytes.NewReader(data))
	if err != nil {
		if !bytes.ContainsAny(data, "\r\n") && !bytes.Contains(data, []byte(":")) {
			body := strings.TrimSpace(string(data))
			if body != "" {
				return emailContent{Body: body}, nil
			}
		}
		return emailContent{}, errors.New("invalid email headers or encoding")
	}
	content := emailContent{}
	content.Subject, err = root.Header.Text("Subject")
	if err != nil {
		return emailContent{}, errors.New("invalid email subject encoding")
	}
	mailHeader := messageMail.Header{Header: root.Header}
	if addresses, addressErr := mailHeader.AddressList("From"); addressErr == nil && len(addresses) > 0 {
		content.From = addresses[0].Name
	}
	if date, dateErr := mailHeader.Date(); dateErr == nil && !date.IsZero() {
		content.Date = date.Format(time.RFC3339)
	}
	parsed, err := parseEmailMIME(root, 0, new(emailMIMEState))
	if err != nil {
		return emailContent{}, err
	}
	content.Body = strings.TrimSpace(strings.Join(parsed.Plain, "\n\n"))
	if content.Body == "" && len(parsed.HTML) > 0 {
		var bodies []string
		for _, body := range parsed.HTML {
			markdown, convertErr := htmlMarkdown(body)
			if convertErr != nil {
				return emailContent{}, errors.New("invalid HTML email body")
			}
			if markdown != "" {
				bodies = append(bodies, markdown)
			}
		}
		content.Body = strings.TrimSpace(strings.Join(bodies, "\n\n"))
	}
	if content.Body == "" {
		return emailContent{}, errors.New("email has no readable text body")
	}
	content.HasUnparsedAttachments = parsed.HasAttachment
	return content, nil
}

func (e emailContent) text() string {
	var result strings.Builder
	if e.From != "" {
		fmt.Fprintf(&result, "From: %s\n", e.From)
	}
	if e.Subject != "" {
		fmt.Fprintf(&result, "Subject: %s\n", e.Subject)
	}
	if e.Date != "" {
		fmt.Fprintf(&result, "Date: %s\n", e.Date)
	}
	result.WriteString("Body:\n")
	result.WriteString(e.Body)
	return result.String()
}

func parseEmailMIME(entity *message.Entity, depth int, state *emailMIMEState) (emailMIMEContent, error) {
	if depth > maxEmailMIMEDepth {
		return emailMIMEContent{}, errors.New("email MIME nesting is too deep")
	}
	if err := validateEmailTransferEncoding(entity.Header.Get("Content-Transfer-Encoding")); err != nil {
		return emailMIMEContent{}, err
	}
	mediaType, params, err := parseEmailContentType(entity.Header.Get("Content-Type"))
	if err != nil {
		return emailMIMEContent{}, err
	}
	disposition, dispositionParams, _ := mime.ParseMediaType(entity.Header.Get("Content-Disposition"))
	if strings.EqualFold(disposition, "attachment") || dispositionParams["filename"] != "" || params["name"] != "" {
		if err := consumeEmailBody(entity.Body, state); err != nil {
			return emailMIMEContent{}, err
		}
		return emailMIMEContent{HasAttachment: true}, nil
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		if params["boundary"] == "" {
			return emailMIMEContent{}, errors.New("email multipart boundary is missing")
		}
		reader := entity.MultipartReader()
		if reader == nil {
			return emailMIMEContent{}, errors.New("invalid email multipart body")
		}
		var children []emailMIMEContent
		for {
			part, nextErr := reader.NextPart()
			if errors.Is(nextErr, io.EOF) {
				break
			}
			if nextErr != nil {
				return emailMIMEContent{}, errors.New("invalid email multipart body")
			}
			state.parts++
			if state.parts > maxEmailMIMEParts {
				return emailMIMEContent{}, errors.New("email has too many MIME parts")
			}
			child, childErr := parseEmailMIME(part, depth+1, state)
			if childErr != nil {
				return emailMIMEContent{}, childErr
			}
			children = append(children, child)
		}
		if strings.EqualFold(mediaType, "multipart/alternative") {
			return chooseEmailAlternative(children), nil
		}
		var combined emailMIMEContent
		for _, child := range children {
			combined.Plain = append(combined.Plain, child.Plain...)
			combined.HTML = append(combined.HTML, child.HTML...)
			combined.HasAttachment = combined.HasAttachment || child.HasAttachment
		}
		return combined, nil
	}
	if mediaType != mimeTypePlainText && mediaType != "text/html" {
		if err := consumeEmailBody(entity.Body, state); err != nil {
			return emailMIMEContent{}, err
		}
		return emailMIMEContent{HasAttachment: true}, nil
	}
	content, err := readEmailBody(entity.Body, state)
	if err != nil {
		return emailMIMEContent{}, err
	}
	text := strings.TrimSpace(string(content))
	if text == "" {
		return emailMIMEContent{}, nil
	}
	if mediaType == "text/html" {
		return emailMIMEContent{HTML: []string{text}}, nil
	}
	return emailMIMEContent{Plain: []string{text}}, nil
}

func parseEmailContentType(value string) (string, map[string]string, error) {
	if strings.TrimSpace(value) == "" {
		return mimeTypePlainText, nil, nil
	}
	mediaType, params, err := mime.ParseMediaType(value)
	if err != nil {
		return "", nil, errors.New("invalid email MIME content type")
	}
	return mediaType, params, nil
}

func validateEmailTransferEncoding(encoding string) error {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "7bit", "8bit", "binary", "base64", "quoted-printable":
		return nil
	default:
		return errors.New("invalid email text transfer encoding")
	}
}

func readEmailBody(body io.Reader, state *emailMIMEState) ([]byte, error) {
	remaining := MaxDocumentBytes - state.size
	if remaining < 0 {
		return nil, errors.New("email text exceeds size limit")
	}
	content, err := io.ReadAll(io.LimitReader(body, int64(remaining)+1))
	if err != nil || len(content) > remaining {
		return nil, errors.New("email text exceeds size limit")
	}
	state.size += len(content)
	return content, nil
}

func consumeEmailBody(body io.Reader, state *emailMIMEState) error {
	_, err := readEmailBody(body, state)
	return err
}

func chooseEmailAlternative(children []emailMIMEContent) emailMIMEContent {
	var chosen emailMIMEContent
	var htmlFallback emailMIMEContent
	for _, child := range children {
		chosen.HasAttachment = chosen.HasAttachment || child.HasAttachment
		if len(child.Plain) > 0 {
			chosen.Plain = child.Plain
			chosen.HTML = nil
		} else if len(child.HTML) > 0 && len(chosen.Plain) == 0 {
			htmlFallback.HTML = child.HTML
		}
	}
	if len(chosen.Plain) == 0 {
		chosen.HTML = htmlFallback.HTML
	}
	return chosen
}

func htmlMarkdown(source string) (string, error) {
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return "", err
	}
	removeEmailHTML(document)
	var sanitized bytes.Buffer
	if err := html.Render(&sanitized, document); err != nil {
		return "", err
	}
	conv := converter.NewConverter(converter.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(),
		table.NewTablePlugin(),
	))
	markdown, err := conv.ConvertString(sanitized.String())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(markdown), nil
}

func removeEmailHTML(node *html.Node) {
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		if child.Type == html.ElementNode {
			switch strings.ToLower(child.Data) {
			case "head", "script", "style", "template":
				node.RemoveChild(child)
				child = next
				continue
			}
		}
		if child.Type == html.TextNode {
			child.Data = strings.ReplaceAll(child.Data, "\u00a0", " ")
		}
		removeEmailHTML(child)
		child = next
	}
}
