package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	documentapp "github.com/walnuts1018/horse/internal/application/document"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/ingestion/mailbox"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const mailboxCursorName = "forwarding"

var errRejectedMailboxMessage = errors.New("mailbox message was rejected")

type MailboxConfig struct {
	Address            string
	ServerName         string
	Mailbox            string
	Username           string
	Password           string
	RecipientBase      string
	PollInterval       time.Duration
	MaxMessageBytes    int64
	MaxMessagesPerPoll int
}

type mailboxIngestor struct {
	db       *pgxpool.Pool
	store    DocumentStore
	queue    *ingestion.Queue
	logger   *slog.Logger
	poller   *mailbox.Poller
	base     string
	interval time.Duration
}

func NewMailboxIngestor(db *pgxpool.Pool, store DocumentStore, queue *ingestion.Queue, logger *slog.Logger, config MailboxConfig) (*mailboxIngestor, error) {
	if config.Address == "" || config.ServerName == "" || config.Mailbox == "" || config.Username == "" || config.Password == "" || config.RecipientBase == "" || db == nil || store == nil {
		return nil, nil
	}
	poller, err := mailbox.New(mailbox.Config{
		Address: config.Address, ServerName: config.ServerName, Mailbox: config.Mailbox, Username: config.Username,
		Password: config.Password, RecipientBase: config.RecipientBase,
		MaxMessageBytes: config.MaxMessageBytes, MaxMessagesPerPoll: config.MaxMessagesPerPoll,
	})
	if err != nil {
		return nil, err
	}
	if config.PollInterval <= 0 {
		config.PollInterval = time.Minute
	}
	return &mailboxIngestor{db: db, store: store, queue: queue, logger: logger, poller: poller, base: config.RecipientBase, interval: config.PollInterval}, nil
}

func (p *mailboxIngestor) Run(ctx context.Context) {
	if p == nil {
		return
	}
	if err := p.poll(ctx); err != nil && ctx.Err() == nil {
		p.logger.WarnContext(ctx, "mailbox poll failed", "error_type", errorType(err))
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.poll(ctx); err != nil && ctx.Err() == nil {
				p.logger.WarnContext(ctx, "mailbox poll failed", "error_type", errorType(err))
			}
		}
	}
}

func (p *mailboxIngestor) poll(ctx context.Context) error {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			p.logger.WarnContext(ctx, "mailbox transaction rollback failed", "error_type", errorType(err))
		}
	}()
	executor := bobpgx.NewTx(tx, nil)
	cursorRow, err := models.MailboxCursors.Query(sm.Where(models.MailboxCursors.Columns.Mailbox.EQ(psql.Arg(mailboxCursorName))), sm.ForUpdate()).One(ctx, executor)
	if err != nil {
		return err
	}
	if cursorRow.UIDValidity < 0 || cursorRow.UIDValidity > int64(^uint32(0)) || cursorRow.UID < 0 || cursorRow.UID > int64(^uint32(0)) {
		return errors.New("mailbox cursor is outside the IMAP UID range")
	}
	cursor := mailbox.Cursor{UIDValidity: uint32(cursorRow.UIDValidity), UID: uint32(cursorRow.UID)}
	messages, next, err := p.poller.Poll(ctx, cursor)
	if err != nil {
		return err
	}
	for _, message := range messages {
		if message.Rejected {
			continue
		}
		forwarding, err := activeForwarding(ctx, executor, message.DeliveredTo, p.base)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		jobIDs, err := p.registerMessage(ctx, forwarding, message.RFC822)
		if errors.Is(err, errRejectedMailboxMessage) {
			continue
		}
		if err != nil {
			return err
		}
		p.publishDocumentJobs(ctx, jobIDs)
	}
	validity, uid := int64(next.UIDValidity), int64(next.UID)
	updatedAt := time.Now().UTC()
	if err := cursorRow.Update(ctx, executor, &models.MailboxCursorSetter{UIDValidity: &validity, UID: &uid, UpdatedAt: &updatedAt}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func activeForwarding(ctx context.Context, executor bob.Executor, address, base string) (*models.HouseholdMailForwarding, error) {
	hash, ok := forwardingTokenHash(address, base)
	if !ok {
		return nil, sql.ErrNoRows
	}
	return models.HouseholdMailForwardings.Query(sm.Where(psql.And(
		models.HouseholdMailForwardings.Columns.TokenHash.EQ(psql.Arg(hash)),
		models.HouseholdMailForwardings.Columns.RevokedAt.IsNull(),
	)), sm.ForShare()).One(ctx, executor)
}

func forwardingTokenHash(address, base string) ([]byte, bool) {
	local, domain, ok := strings.Cut(address, "@")
	baseLocal, baseDomain, baseOK := strings.Cut(base, "@")
	if !ok || !baseOK || !strings.EqualFold(domain, baseDomain) {
		return nil, false
	}
	tag, ok := strings.CutPrefix(local, baseLocal+"+")
	if !ok || len(tag) != 52 {
		return nil, false
	}
	for _, char := range tag {
		if (char < 'a' || char > 'z') && (char < '2' || char > '7') {
			return nil, false
		}
	}
	encoding := base32.StdEncoding.WithPadding(base32.NoPadding)
	token, err := encoding.DecodeString(strings.ToUpper(tag))
	if err != nil || len(token) != sha256.Size || strings.ToLower(encoding.EncodeToString(token)) != tag {
		return nil, false
	}
	hash := sha256.Sum256(token)
	return hash[:], true
}

func (p *mailboxIngestor) registerMessage(ctx context.Context, forwarding *models.HouseholdMailForwarding, raw []byte) ([]gofrsuuid.UUID, error) {
	attachments, err := ingestion.ParseEmailAttachments(raw)
	if err != nil {
		return nil, errRejectedMailboxMessage
	}
	registration, err := documentapp.NewService(p.db).Register(ctx, p.store, documentapp.RegisterCommand{
		HouseholdID: forwarding.HouseholdID, MemberID: forwarding.CreatedBy, MIMEType: documentMIMEEmail, SourceKind: "email",
		Purpose: ingestion.JobPurposeInteractive, Data: raw, Attachments: attachments,
	})
	if err != nil {
		return nil, err
	}
	return registration.JobIDs, nil
}

func (p *mailboxIngestor) publishDocumentJobs(ctx context.Context, jobIDs []gofrsuuid.UUID) {
	if p.queue == nil {
		return
	}
	for _, jobID := range jobIDs {
		if err := p.queue.Publish(ctx, jobID.String()); err != nil {
			p.logger.WarnContext(ctx, "extraction job notification failed", "job_id", jobID.String(), "error_type", errorType(err))
		}
	}
}

func errorType(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%T", err)
}
