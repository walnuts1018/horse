package mailbox

import (
	"crypto/tls"
	"errors"
	"reflect"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

const (
	testRecipientBase = "horse-mail@walnuts.dev"
	testMailboxName   = "INBOX"
	testMailboxUser   = "account"
	testMailboxSecret = "secret"
)

func TestPollSelectedResetsCursorOnUIDValidityChange(t *testing.T) {
	t.Parallel()
	session := &fakeSession{
		uids: []uint32{2, 1},
		body: []byte("Delivered-To: horse-mail+token@walnuts.dev\r\n\r\nbody"),
	}
	poller := &Poller{config: Config{MaxMessageBytes: 1024, MaxMessagesPerPoll: 2, RecipientBase: testRecipientBase}}

	messages, cursor, err := poller.pollSelected(t.Context(), Cursor{UIDValidity: 12, UID: 900}, session, 14, 10)
	if err != nil {
		t.Fatal(err)
	}
	if session.firstUID != 1 {
		t.Fatalf("UID search started at %d, want 1 after UIDVALIDITY changed", session.firstUID)
	}
	if session.lastUID != 2 {
		t.Fatalf("UID search ended at %d, want bounded UID 2", session.lastUID)
	}
	if got, want := []uint32{messages[0].UID, messages[1].UID}, []uint32{1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fetched UIDs = %v, want %v", got, want)
	}
	if cursor != (Cursor{UIDValidity: 14, UID: 2}) {
		t.Fatalf("cursor = %+v, want UIDVALIDITY 14 and UID 2", cursor)
	}
	if !reflect.DeepEqual(session.fetched, []uint32{1, 2}) {
		t.Fatalf("fetched UIDs = %v, want [1 2]", session.fetched)
	}
}

func TestPollSelectedSkipsSearchWhenUIDNextIsBehindCursor(t *testing.T) {
	t.Parallel()
	session := &fakeSession{uids: []uint32{40, 50}}
	poller := &Poller{config: Config{MaxMessageBytes: 1024, MaxMessagesPerPoll: 2, RecipientBase: testRecipientBase}}
	cursor := Cursor{UIDValidity: 14, UID: 100}

	messages, next, err := poller.pollSelected(t.Context(), cursor, session, 14, 51)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 || next != cursor || session.searchCount != 0 {
		t.Fatalf("messages=%d cursor=%+v searches=%d; want no messages, unchanged cursor, no search", len(messages), next, session.searchCount)
	}
}

func TestPollSelectedAdvancesPastExpungedUIDs(t *testing.T) {
	t.Parallel()
	session := &fakeSession{}
	poller := &Poller{config: Config{MaxMessageBytes: 1024, MaxMessagesPerPoll: 2, RecipientBase: testRecipientBase}}

	messages, next, err := poller.pollSelected(t.Context(), Cursor{UIDValidity: 14, UID: 2}, session, 14, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 || next != (Cursor{UIDValidity: 14, UID: 4}) {
		t.Fatalf("messages=%d cursor=%+v; want no messages and cursor UID 4", len(messages), next)
	}
}

func TestPollSelectedBoundsUIDSearchRange(t *testing.T) {
	t.Parallel()
	session := &fakeSession{}
	poller := &Poller{config: Config{MaxMessageBytes: 1024, MaxMessagesPerPoll: 3, RecipientBase: testRecipientBase}}

	_, next, err := poller.pollSelected(t.Context(), Cursor{UIDValidity: 14, UID: 10}, session, 14, 100_000)
	if err != nil {
		t.Fatal(err)
	}
	if session.firstUID != 11 || session.lastUID != 13 {
		t.Fatalf("UID search range = %d-%d, want 11-13", session.firstUID, session.lastUID)
	}
	if next.UID != 13 {
		t.Fatalf("cursor UID = %d, want 13 after empty bounded range", next.UID)
	}
}

func TestPollSelectedAdvancesPastInvalidRecipient(t *testing.T) {
	t.Parallel()
	session := &fakeSession{
		uids: []uint32{3, 4},
		bodies: map[uint32][]byte{
			3: []byte("To: horse-mail+token@walnuts.dev\r\n\r\nbody"),
			4: []byte("Delivered-To: horse-mail+token@walnuts.dev\r\n\r\nbody"),
		},
	}
	poller := &Poller{config: Config{MaxMessageBytes: 1024, MaxMessagesPerPoll: 10, RecipientBase: testRecipientBase}}

	messages, cursor, err := poller.pollSelected(t.Context(), Cursor{UIDValidity: 20, UID: 2}, session, 20, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || !messages[0].Rejected || messages[1].Rejected {
		t.Fatalf("messages = %+v, want one rejected and one accepted message", messages)
	}
	if cursor != (Cursor{UIDValidity: 20, UID: 4}) {
		t.Fatalf("cursor = %+v, want UID 4", cursor)
	}
}

func TestPollSelectedStopsBeforeBatchByteLimit(t *testing.T) {
	t.Parallel()
	session := &fakeSession{
		uids:  []uint32{3, 4},
		sizes: map[uint32]int64{3: 7, 4: 5},
		bodies: map[uint32][]byte{
			3: []byte("1234567"),
			4: []byte("12345"),
		},
	}
	poller := &Poller{config: Config{
		MaxMessageBytes: 1024, MaxMessagesPerPoll: 10, MaxBatchBytes: 10, RecipientBase: testRecipientBase,
	}}

	messages, cursor, err := poller.pollSelected(t.Context(), Cursor{UIDValidity: 20, UID: 2}, session, 20, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || !messages[0].Rejected {
		t.Fatalf("messages = %+v, want only the first rejected message", messages)
	}
	if cursor != (Cursor{UIDValidity: 20, UID: 3}) {
		t.Fatalf("cursor = %+v, want UID 3 before the message that exceeds the batch limit", cursor)
	}
	if !reflect.DeepEqual(session.bodyFetchedUIDs, []uint32{3}) {
		t.Fatalf("fetched bodies = %v, want only UID 3", session.bodyFetchedUIDs)
	}
}

func TestFetchMessageRejectsOversizeBeforeFetchingBody(t *testing.T) {
	t.Parallel()
	session := &fakeSession{size: 11}
	_, _, err := fetchMessage(session, 3, 20, 10, 10, testRecipientBase)
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("fetchMessage() error = %v, want ErrMessageTooLarge", err)
	}
	if session.bodyFetched {
		t.Fatal("FetchBody was called for a message above the size limit")
	}
}

func TestFetchSizePreservesCollectError(t *testing.T) {
	t.Parallel()
	collectErr := errors.New("temporary IMAP failure")
	if _, err := fetchedMessageSize(7, nil, collectErr); !errors.Is(err, collectErr) {
		t.Fatalf("fetchedMessageSize() error = %v, want original Collect error", err)
	}
	if _, err := fetchedMessageSize(7, []*imapclient.FetchMessageBuffer{{UID: imap.UID(8)}}, nil); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("fetchedMessageSize() error = %v, want ErrInvalidMessage for UID mismatch", err)
	}
}

func TestFetchBodyPreservesCommandErrors(t *testing.T) {
	t.Parallel()
	commandErr := errors.New("temporary IMAP command failure")
	if _, err := fetchedMessageBody(7, nil, 0, false, commandErr, nil, 100); !errors.Is(err, commandErr) {
		t.Fatalf("fetchedMessageBody() error = %v, want original command error", err)
	}
	if _, err := fetchedMessageBody(7, nil, 0, false, nil, commandErr, 100); !errors.Is(err, commandErr) {
		t.Fatalf("fetchedMessageBody() error = %v, want original body read error", err)
	}
	if _, err := fetchedMessageBody(7, nil, 8, true, nil, nil, 100); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("fetchedMessageBody() error = %v, want ErrInvalidMessage for UID mismatch", err)
	}
}

func TestTrustedDeliveredToRequiresOnePlainAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
		fail bool
	}{
		{
			name: "single tagged address",
			raw:  "Delivered-To: horse-mail+token@walnuts.dev\r\nSubject: receipt\r\n\r\nbody",
			want: "horse-mail+token@walnuts.dev",
		},
		{
			name: "repeated header",
			raw:  "Delivered-To: attacker@example.test\r\nDelivered-To: horse-mail+token@walnuts.dev\r\n\r\nbody",
			fail: true,
		},
		{
			name: "bare base address",
			raw:  "Delivered-To: horse-mail@walnuts.dev\r\n\r\nbody",
			fail: true,
		},
		{
			name: "wrong address",
			raw:  "Delivered-To: horse-mail+token@other.test\r\n\r\nbody",
			fail: true,
		},
		{
			name: "display name",
			raw:  "Delivered-To: Receipt <receipt@example.test>\r\n\r\nbody",
			fail: true,
		},
		{
			name: "missing header",
			raw:  "To: horse-mail+token@walnuts.dev\r\nSubject: receipt\r\n\r\nbody",
			fail: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := trustedDeliveredTo([]byte(test.raw), "horse-mail@walnuts.dev")
			if test.fail {
				if !errors.Is(err, ErrInvalidMessage) {
					t.Fatalf("trustedDeliveredTo() error = %v, want ErrInvalidMessage", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("trustedDeliveredTo() = %q, %v; want %q, nil", got, err, test.want)
			}
		})
	}
}

func TestNewRejectsDisabledTLSVerification(t *testing.T) {
	t.Parallel()
	unsafeTLSConfig := new(tls.Config)
	unsafeTLSConfig.InsecureSkipVerify = true
	_, err := New(Config{
		Address:       "imap.example.test:993",
		Mailbox:       testMailboxName,
		Username:      testMailboxUser,
		Password:      testMailboxSecret,
		RecipientBase: testRecipientBase,
		TLSConfig:     unsafeTLSConfig,
	})
	if err == nil {
		t.Fatal("New() accepted TLS configuration with certificate verification disabled")
	}
}

func TestNewRejectsUnboundedFetchLimits(t *testing.T) {
	t.Parallel()
	_, err := New(Config{
		Address:            "imap.example.test:993",
		Mailbox:            testMailboxName,
		Username:           testMailboxUser,
		Password:           testMailboxSecret,
		RecipientBase:      testRecipientBase,
		MaxMessageBytes:    maxMessageBytes + 1,
		MaxMessagesPerPoll: 1,
	})
	if err == nil {
		t.Fatal("New() accepted a message limit above the configured maximum")
	}
}

func TestNewRejectsBatchLimitBelowMessageLimit(t *testing.T) {
	t.Parallel()
	_, err := New(Config{
		Address:            "imap.example.test:993",
		Mailbox:            testMailboxName,
		Username:           testMailboxUser,
		Password:           testMailboxSecret,
		RecipientBase:      testRecipientBase,
		MaxMessageBytes:    1024,
		MaxBatchBytes:      1023,
		MaxMessagesPerPoll: 1,
	})
	if err == nil {
		t.Fatal("New() accepted a batch size smaller than the maximum accepted message size")
	}
}

func TestNewSetsExplicitTLSServerName(t *testing.T) {
	t.Parallel()
	poller, err := New(Config{
		Address:       "stalwart.stalwart.svc.cluster.local:993",
		ServerName:    "mx.walnuts.dev",
		Mailbox:       testMailboxName,
		Username:      testMailboxUser,
		Password:      testMailboxSecret,
		RecipientBase: testRecipientBase,
	})
	if err != nil {
		t.Fatal(err)
	}
	if poller.config.TLSConfig == nil || poller.config.TLSConfig.ServerName != "mx.walnuts.dev" {
		t.Fatalf("TLS server name = %q, want mx.walnuts.dev", poller.config.TLSConfig.ServerName)
	}
}

type fakeSession struct {
	uids            []uint32
	firstUID        uint32
	lastUID         uint32
	searchCount     int
	fetched         []uint32
	size            int64
	sizes           map[uint32]int64
	body            []byte
	bodies          map[uint32][]byte
	bodyFetched     bool
	bodyFetchedUIDs []uint32
}

func (s *fakeSession) UIDSearch(firstUID, lastUID uint32) ([]uint32, error) {
	s.firstUID = firstUID
	s.lastUID = lastUID
	s.searchCount++
	return append([]uint32(nil), s.uids...), nil
}

func (s *fakeSession) FetchSize(uid uint32) (int64, error) {
	s.fetched = append(s.fetched, uid)
	if size, ok := s.sizes[uid]; ok {
		return size, nil
	}
	if s.size != 0 {
		return s.size, nil
	}
	return int64(len(s.body)), nil
}

func (s *fakeSession) FetchBody(uid uint32, _ int64) ([]byte, error) {
	s.bodyFetched = true
	s.bodyFetchedUIDs = append(s.bodyFetchedUIDs, uid)
	if body, ok := s.bodies[uid]; ok {
		return append([]byte(nil), body...), nil
	}
	return append([]byte(nil), s.body...), nil
}
