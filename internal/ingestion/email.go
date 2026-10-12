package ingestion

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
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

func parseEmail(data []byte) (emailContent, error) {
	if len(data) == 0 || len(data) > MaxDocumentBytes {
		return emailContent{}, errors.New("invalid email size")
	}
	message, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		if !bytes.ContainsAny(data, "\r\n") && !bytes.Contains(data, []byte(":")) {
			body := strings.TrimSpace(string(data))
			if body != "" {
				return emailContent{Body: body}, nil
			}
		}
		return emailContent{}, errors.New("invalid email headers")
	}
	decoder := &mime.WordDecoder{}
	content := emailContent{}
	if value := message.Header.Get("Subject"); value != "" {
		content.Subject, err = decoder.DecodeHeader(value)
		if err != nil {
			return emailContent{}, errors.New("invalid email subject encoding")
		}
	}
	if value := message.Header.Get("From"); value != "" {
		decoded, decodeErr := decoder.DecodeHeader(value)
		if decodeErr != nil {
			return emailContent{}, errors.New("invalid email sender encoding")
		}
		address, parseErr := mail.ParseAddress(decoded)
		if parseErr == nil {
			content.From = address.Name
		}
	}
	if value := message.Header.Get("Date"); value != "" {
		if date, parseErr := mail.ParseDate(value); parseErr == nil {
			content.Date = date.Format(time.RFC3339)
		}
	}
	parsed, err := parseEmailMIME(textproto.MIMEHeader(message.Header), message.Body, 0, new(int))
	if err != nil {
		return emailContent{}, err
	}
	content.Body = strings.TrimSpace(strings.Join(parsed.Plain, "\n\n"))
	if content.Body == "" && len(parsed.HTML) > 0 {
		var bodies []string
		for _, body := range parsed.HTML {
			text, err := htmlText(body)
			if err != nil {
				return emailContent{}, errors.New("invalid HTML email body")
			}
			if text != "" {
				bodies = append(bodies, text)
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

func parseEmailMIME(header textproto.MIMEHeader, body io.Reader, depth int, parts *int) (emailMIMEContent, error) {
	if depth > maxEmailMIMEDepth {
		return emailMIMEContent{}, errors.New("email MIME nesting is too deep")
	}
	mediaType := strings.TrimSpace(header.Get("Content-Type"))
	var params map[string]string
	if mediaType == "" {
		mediaType = "text/plain"
	} else {
		var err error
		mediaType, params, err = mime.ParseMediaType(mediaType)
		if err != nil {
			return emailMIMEContent{}, errors.New("invalid email MIME content type")
		}
	}
	disposition, dispositionParams, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	if strings.EqualFold(disposition, "attachment") || dispositionParams["filename"] != "" || params["name"] != "" {
		return emailMIMEContent{HasAttachment: true}, nil
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return emailMIMEContent{}, errors.New("email multipart boundary is missing")
		}
		decoded, err := decodeEmailTransfer(body, header.Get("Content-Transfer-Encoding"))
		if err != nil {
			return emailMIMEContent{}, errors.New("invalid email multipart encoding")
		}
		reader := multipart.NewReader(decoded, boundary)
		var children []emailMIMEContent
		for {
			part, nextErr := reader.NextRawPart()
			if errors.Is(nextErr, io.EOF) {
				break
			}
			if nextErr != nil {
				return emailMIMEContent{}, errors.New("invalid email multipart body")
			}
			(*parts)++
			if *parts > maxEmailMIMEParts {
				return emailMIMEContent{}, errors.New("email has too many MIME parts")
			}
			child, childErr := parseEmailMIME(textproto.MIMEHeader(part.Header), part, depth+1, parts)
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
	if mediaType != "text/plain" && mediaType != "text/html" {
		return emailMIMEContent{HasAttachment: true}, nil
	}
	decoded, err := decodeEmailTransfer(body, header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return emailMIMEContent{}, errors.New("invalid email text transfer encoding")
	}
	content, err := io.ReadAll(io.LimitReader(decoded, MaxDocumentBytes+1))
	if err != nil || len(content) > MaxDocumentBytes {
		return emailMIMEContent{}, errors.New("email text part exceeds size limit")
	}
	if label := params["charset"]; label != "" && !strings.EqualFold(label, "utf-8") && !strings.EqualFold(label, "us-ascii") {
		reader, err := charset.NewReaderLabel(label, bytes.NewReader(content))
		if err != nil {
			return emailMIMEContent{}, errors.New("unsupported email text charset")
		}
		content, err = io.ReadAll(io.LimitReader(reader, MaxDocumentBytes+1))
		if err != nil || len(content) > MaxDocumentBytes {
			return emailMIMEContent{}, errors.New("decoded email text exceeds size limit")
		}
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

func decodeEmailTransfer(body io.Reader, encoding string) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "7bit", "8bit", "binary":
		return body, nil
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, body), nil
	case "quoted-printable":
		return quotedprintable.NewReader(body), nil
	default:
		return nil, errors.New("unsupported email transfer encoding")
	}
}

func htmlText(source string) (string, error) {
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return "", err
	}
	var result strings.Builder
	lineStart := true
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch strings.ToLower(node.Data) {
			case "head", "script", "style", "template":
				return
			}
			if isHTMLBlock(node.Data) && result.Len() > 0 && !lineStart {
				result.WriteByte('\n')
				lineStart = true
			}
		}
		if node.Type == html.TextNode {
			text := strings.Join(strings.Fields(node.Data), " ")
			if text != "" {
				if result.Len() > 0 && !lineStart {
					result.WriteByte(' ')
				}
				result.WriteString(text)
				lineStart = false
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
		if node.Type == html.ElementNode && isHTMLBlock(node.Data) && result.Len() > 0 && !lineStart {
			result.WriteByte('\n')
			lineStart = true
		}
	}
	visit(document)
	return strings.TrimSpace(result.String()), nil
}

func isHTMLBlock(name string) bool {
	switch strings.ToLower(name) {
	case "article", "blockquote", "br", "div", "footer", "h1", "h2", "h3", "h4", "h5", "h6", "header", "li", "main", "ol", "p", "section", "table", "td", "th", "tr", "ul":
		return true
	default:
		return false
	}
}
