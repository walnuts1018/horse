package mailbox

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const (
	defaultMaxMessageBytes = 10 << 20
	defaultMaxMessages     = 100
	defaultTimeout         = 30 * time.Second
	maxMessageBytes        = 25 << 20
	maxMessagesPerPoll     = 1000
	defaultMaxBatchBytes   = 50 << 20
)

var (
	ErrMessageTooLarge = errors.New("mailbox message exceeds size limit")
	ErrInvalidMessage  = errors.New("mailbox message is invalid")
	ErrBatchLimit      = errors.New("mailbox batch size limit reached")
	ErrCursorValidity  = errors.New("mailbox cursor has no UIDVALIDITY")
)

// ConfigはIMAPS接続先と読み取り制限を表す。
type Config struct {
	Address            string
	ServerName         string
	Mailbox            string
	Username           string
	Password           string
	RecipientBase      string
	TLSConfig          *tls.Config
	MaxMessageBytes    int64
	MaxMessagesPerPoll int
	MaxBatchBytes      int64
	Timeout            time.Duration
}

// CursorはIMAP mailbox内の処理済みUID位置を表す。
type Cursor struct {
	UIDValidity uint32
	UID         uint32
}

// MessageはRFC822本文と単一のDelivered-To宛先を表す。
type Message struct {
	UID         uint32
	UIDValidity uint32
	DeliveredTo string
	RFC822      []byte
	Rejected    bool
}

// PollerはIMAPS mailboxからcursorより後のメッセージを取得する。
type Poller struct {
	config Config
}

// NewはTLS必須のIMAP pollerを作成する。
func New(config Config) (*Poller, error) {
	if config.Address == "" || config.Mailbox == "" || config.Username == "" || config.Password == "" {
		return nil, errors.New("mailbox connection settings are incomplete")
	}
	base, err := parsePlainAddress(config.RecipientBase)
	if err != nil || base != config.RecipientBase || !strings.Contains(base, "@") || strings.Contains(strings.SplitN(base, "@", 2)[0], "+") {
		return nil, errors.New("mailbox recipient base must be a plain email address without a plus tag")
	}
	if _, _, err := net.SplitHostPort(config.Address); err != nil {
		return nil, errors.New("mailbox address must include a host and port")
	}
	if config.MaxMessageBytes == 0 {
		config.MaxMessageBytes = defaultMaxMessageBytes
	}
	if config.MaxMessageBytes < 1 || config.MaxMessageBytes > maxMessageBytes {
		return nil, errors.New("mailbox message size limit is invalid")
	}
	if config.MaxMessagesPerPoll == 0 {
		config.MaxMessagesPerPoll = defaultMaxMessages
	}
	if config.MaxMessagesPerPoll < 1 || config.MaxMessagesPerPoll > maxMessagesPerPoll {
		return nil, errors.New("mailbox message count limit is invalid")
	}
	if config.MaxBatchBytes == 0 {
		config.MaxBatchBytes = defaultMaxBatchBytes
	}
	if config.MaxBatchBytes < 1 || config.MaxBatchBytes > defaultMaxBatchBytes {
		return nil, errors.New("mailbox batch size limit is invalid")
	}
	if config.Timeout == 0 {
		config.Timeout = defaultTimeout
	}
	if config.Timeout < 0 {
		return nil, errors.New("mailbox timeout is invalid")
	}
	if config.TLSConfig != nil && config.TLSConfig.InsecureSkipVerify {
		return nil, errors.New("mailbox TLS certificate verification cannot be disabled")
	}
	if config.TLSConfig != nil && config.TLSConfig.MinVersion > 0 && config.TLSConfig.MinVersion < tls.VersionTLS12 {
		return nil, errors.New("mailbox TLS version must be TLS 1.2 or newer")
	}
	if config.ServerName != "" {
		if config.TLSConfig == nil {
			config.TLSConfig = new(tls.Config)
		} else {
			config.TLSConfig = config.TLSConfig.Clone()
		}
		config.TLSConfig.ServerName = config.ServerName
	}

	return &Poller{config: config}, nil
}

type imapSession interface {
	UIDSearch(firstUID, lastUID uint32) ([]uint32, error)
	FetchSize(uid uint32) (int64, error)
	FetchBody(uid uint32, maxBytes int64) ([]byte, error)
}

type imapV2Session struct {
	client *imapclient.Client
}

// Pollはread-only mailboxから新しいRFC822メッセージを取得する。
func (p *Poller) Poll(ctx context.Context, cursor Cursor) ([]Message, Cursor, error) {
	if cursor.UIDValidity == 0 && cursor.UID != 0 {
		return nil, cursor, ErrCursorValidity
	}
	if err := ctx.Err(); err != nil {
		return nil, cursor, err
	}
	if p.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.config.Timeout)
		defer cancel()
	}

	conn, err := dialTLS(ctx, p.config.Address, p.config.TLSConfig)
	if err != nil {
		return nil, cursor, errors.New("mailbox TLS connection failed")
	}
	defer conn.Close()
	stopDeadline := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stopDeadline()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, cursor, errors.New("mailbox connection deadline failed")
		}
	}

	imapClient := imapclient.New(conn, nil)
	if err := imapClient.Login(p.config.Username, p.config.Password).Wait(); err != nil {
		return nil, cursor, pollError(ctx, "mailbox authentication failed")
	}
	status, err := imapClient.Select(p.config.Mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil || status == nil || status.UIDValidity == 0 || status.UIDNext == 0 {
		return nil, cursor, pollError(ctx, "mailbox read-only selection failed")
	}
	return p.pollSelected(ctx, cursor, imapV2Session{client: imapClient}, status.UIDValidity, uint32(status.UIDNext))
}

func (p *Poller) pollSelected(ctx context.Context, cursor Cursor, session imapSession, uidValidity, uidNext uint32) ([]Message, Cursor, error) {
	next := cursor
	if uidValidity != cursor.UIDValidity {
		next = Cursor{UIDValidity: uidValidity}
	}
	if next.UID == ^uint32(0) {
		return []Message{}, next, nil
	}
	firstUID := next.UID + 1
	lastUID := uidNext - 1
	if firstUID > lastUID {
		return []Message{}, next, nil
	}
	maxMessages := p.config.MaxMessagesPerPoll
	if maxMessages < 1 {
		maxMessages = defaultMaxMessages
	}
	searchLastUID := uint32(min(uint64(lastUID), uint64(firstUID)+uint64(maxMessages)-1))
	uids, err := session.UIDSearch(firstUID, searchLastUID)
	if err != nil {
		return nil, cursor, pollError(ctx, "mailbox UID search failed")
	}
	slices.Sort(uids)
	for _, uid := range uids {
		if uid < firstUID || uid > searchLastUID {
			return nil, cursor, ErrInvalidMessage
		}
	}
	if len(uids) == 0 {
		next.UID = searchLastUID
		return []Message{}, next, nil
	}

	messages := make([]Message, 0, len(uids))
	batchBytes := int64(0)
	maxBatchBytes := p.config.MaxBatchBytes
	if maxBatchBytes == 0 {
		maxBatchBytes = defaultMaxBatchBytes
	}
	for _, uid := range uids {
		if err := ctx.Err(); err != nil {
			return nil, cursor, err
		}
		message, messageBytes, err := fetchMessage(session, uid, uidValidity, p.config.MaxMessageBytes, maxBatchBytes-batchBytes, p.config.RecipientBase)
		if err != nil {
			if errors.Is(err, ErrBatchLimit) {
				break
			}
			if errors.Is(err, ErrMessageTooLarge) || errors.Is(err, ErrInvalidMessage) {
				messages = append(messages, Message{UID: uid, UIDValidity: uidValidity, Rejected: true})
				batchBytes += messageBytes
				next.UID = uid
				continue
			}
			return nil, cursor, pollError(ctx, "mailbox message fetch failed")
		}
		messages = append(messages, message)
		batchBytes += messageBytes
		next.UID = uid
	}
	return messages, next, nil
}

func (s imapV2Session) UIDSearch(firstUID, lastUID uint32) ([]uint32, error) {
	set := imap.UIDSet(nil)
	set.AddRange(imap.UID(firstUID), imap.UID(lastUID))
	data, err := s.client.UIDSearch(&imap.SearchCriteria{UID: []imap.UIDSet{set}}, nil).Wait()
	if err != nil {
		return nil, err
	}
	uidValues := data.AllUIDs()
	uids := make([]uint32, len(uidValues))
	for i, uid := range uidValues {
		uids[i] = uint32(uid)
	}
	return uids, nil
}

func (s imapV2Session) FetchSize(uid uint32) (int64, error) {
	set := imap.UIDSetNum(imap.UID(uid))
	messages, err := s.client.Fetch(set, &imap.FetchOptions{UID: true, RFC822Size: true}).Collect()
	return fetchedMessageSize(uid, messages, err)
}

func fetchedMessageSize(uid uint32, messages []*imapclient.FetchMessageBuffer, err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	if len(messages) != 1 || uint32(messages[0].UID) != uid {
		return 0, ErrInvalidMessage
	}
	return messages[0].RFC822Size, nil
}

func (s imapV2Session) FetchBody(uid uint32, maxBytes int64) ([]byte, error) {
	set := imap.UIDSetNum(imap.UID(uid))
	section := &imap.FetchItemBodySection{Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: maxBytes + 1}}
	command := s.client.Fetch(set, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{section}})
	var body []byte
	var receivedUID uint32
	var bodyFound bool
	var readErr error
	for data := command.Next(); data != nil; data = command.Next() {
		for item := data.Next(); item != nil; item = data.Next() {
			switch item := item.(type) {
			case imapclient.FetchItemDataUID:
				receivedUID = uint32(item.UID)
			case imapclient.FetchItemDataBodySection:
				if item.Literal == nil {
					continue
				}
				body, readErr = io.ReadAll(io.LimitReader(item.Literal, maxBytes+1))
				bodyFound = true
			}
		}
	}
	return fetchedMessageBody(uid, body, receivedUID, bodyFound, command.Close(), readErr, maxBytes)
}

func fetchedMessageBody(uid uint32, body []byte, receivedUID uint32, bodyFound bool, closeErr, readErr error, maxBytes int64) ([]byte, error) {
	if closeErr != nil {
		return nil, closeErr
	}
	if readErr != nil {
		return nil, readErr
	}
	if !bodyFound || receivedUID != uid {
		return nil, ErrInvalidMessage
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrMessageTooLarge
	}
	return body, nil
}

func dialTLS(ctx context.Context, address string, config *tls.Config) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	dialer := new(net.Dialer)
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	if config != nil {
		tlsConfig = config.Clone()
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = host
		}
		if tlsConfig.MinVersion == 0 || tlsConfig.MinVersion < tls.VersionTLS12 {
			tlsConfig.MinVersion = tls.VersionTLS12
		}
	}
	tlsConn := tls.Client(conn, tlsConfig)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func fetchMessage(session imapSession, uid, uidValidity uint32, maxBytes, remainingBatchBytes int64, recipientBase string) (Message, int64, error) {
	size, err := session.FetchSize(uid)
	if err != nil {
		return Message{}, 0, err
	}
	if size < 0 {
		return Message{}, 0, ErrInvalidMessage
	}
	if size > maxBytes {
		return Message{}, 0, ErrMessageTooLarge
	}
	if size > remainingBatchBytes {
		return Message{}, 0, ErrBatchLimit
	}
	readLimit := min(maxBytes, remainingBatchBytes)
	body, err := session.FetchBody(uid, readLimit)
	if err != nil {
		if errors.Is(err, ErrMessageTooLarge) && remainingBatchBytes < maxBytes {
			return Message{}, 0, ErrBatchLimit
		}
		return Message{}, 0, err
	}
	if int64(len(body)) > maxBytes {
		return Message{}, 0, ErrMessageTooLarge
	}
	if int64(len(body)) > remainingBatchBytes {
		return Message{}, 0, ErrBatchLimit
	}
	deliveredTo, err := trustedDeliveredTo(body, recipientBase)
	if err != nil {
		return Message{UID: uid, UIDValidity: uidValidity}, int64(len(body)), ErrInvalidMessage
	}
	return Message{UID: uid, UIDValidity: uidValidity, DeliveredTo: deliveredTo, RFC822: body}, int64(len(body)), nil
}

func trustedDeliveredTo(raw []byte, recipientBase string) (string, error) {
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "", ErrInvalidMessage
	}
	values := parsed.Header["Delivered-To"]
	if len(values) != 1 {
		return "", ErrInvalidMessage
	}
	value := strings.TrimSpace(values[0])
	address, err := parsePlainAddress(value)
	if err != nil || address != value || !hasPlusRecipient(address, recipientBase) {
		return "", ErrInvalidMessage
	}
	return address, nil
}

func parsePlainAddress(value string) (string, error) {
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address == "" || address.Name != "" {
		return "", ErrInvalidMessage
	}
	return address.Address, nil
}

func hasPlusRecipient(address, base string) bool {
	addressLocal, addressDomain, ok := strings.Cut(address, "@")
	if !ok {
		return false
	}
	baseLocal, baseDomain, ok := strings.Cut(base, "@")
	if !ok || addressLocal == baseLocal {
		return false
	}
	tag, ok := strings.CutPrefix(addressLocal, baseLocal+"+")
	return ok && tag != "" && strings.EqualFold(addressDomain, baseDomain)
}

func pollError(ctx context.Context, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New(message)
}
