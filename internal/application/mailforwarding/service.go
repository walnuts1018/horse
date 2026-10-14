package mailforwarding

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const ownerRole = "OWNER"

type Service struct {
	db              *pgxpool.Pool
	mailboxDomain   string
	localPartPrefix string
}

type Config struct {
	MailboxDomain          string
	AddressLocalPartPrefix string
}

func NewService(db *pgxpool.Pool, config Config) *Service {
	return &Service{db: db, mailboxDomain: config.MailboxDomain, localPartPrefix: config.AddressLocalPartPrefix}
}

type Command struct {
	ActorID     string
	HouseholdID string
}

type ForwardingAddress struct {
	ID        string
	Address   string
	CreatedAt time.Time
}

type Status struct {
	ID        string
	CreatedAt time.Time
}

func (s *Service) Active(ctx context.Context, command Command) (Status, error) {
	actorID, householdID, err := parseIDs(command)
	if err != nil {
		return Status{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Status{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForShare()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return Status{}, apperror.ErrNotFound
	} else if err != nil {
		return Status{}, err
	}
	if err := requireOwner(ctx, executor, householdID, actorID); err != nil {
		return Status{}, err
	}
	active, err := models.HouseholdMailForwardings.Query(sm.Where(psql.And(
		models.HouseholdMailForwardings.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMailForwardings.Columns.RevokedAt.IsNull(),
	))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return Status{}, apperror.ErrNotFound
	}
	if err != nil {
		return Status{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Status{}, err
	}
	return Status{ID: active.ID.String(), CreatedAt: active.CreatedAt}, nil
}

func (s *Service) Issue(ctx context.Context, command Command) (ForwardingAddress, error) {
	return s.create(ctx, command, false)
}

func (s *Service) Rotate(ctx context.Context, command Command) (ForwardingAddress, error) {
	return s.create(ctx, command, true)
}

func (s *Service) create(ctx context.Context, command Command, rotate bool) (ForwardingAddress, error) {
	actorID, householdID, err := parseIDs(command)
	if err != nil {
		return ForwardingAddress{}, err
	}
	if s.mailboxDomain == "" || s.localPartPrefix == "" {
		return ForwardingAddress{}, apperror.Wrap(apperror.KindBadInput, errors.New("mailbox domain and address local-part prefix are required"))
	}
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return ForwardingAddress{}, fmt.Errorf("generate forwarding token: %w", err)
	}
	token := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(rawToken))
	address := s.localPartPrefix + token + "@" + s.mailboxDomain
	parsedAddress, err := mail.ParseAddress(address)
	if err != nil || parsedAddress.Address != address || parsedAddress.Name != "" {
		return ForwardingAddress{}, apperror.Wrap(apperror.KindBadInput, errors.New("invalid mailbox domain"))
	}
	tokenHash := sha256.Sum256(rawToken)
	forwardingID, err := gofrsuuid.NewV7()
	if err != nil {
		return ForwardingAddress{}, fmt.Errorf("generate forwarding address id: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ForwardingAddress{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return ForwardingAddress{}, apperror.ErrNotFound
	} else if err != nil {
		return ForwardingAddress{}, err
	}
	if err := requireOwner(ctx, executor, householdID, actorID); err != nil {
		return ForwardingAddress{}, err
	}
	active, err := models.HouseholdMailForwardings.Query(sm.Where(psql.And(
		models.HouseholdMailForwardings.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMailForwardings.Columns.RevokedAt.IsNull(),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		if rotate {
			return ForwardingAddress{}, apperror.ErrNotFound
		}
	} else if err != nil {
		return ForwardingAddress{}, err
	} else {
		if !rotate {
			return ForwardingAddress{}, apperror.ErrConflict
		}
		revokedAt := time.Now().UTC()
		if _, err := models.HouseholdMailForwardings.Update(
			um.Where(models.HouseholdMailForwardings.Columns.ID.EQ(psql.Arg(active.ID))),
			um.SetCol("revoked_at").To(psql.Arg(revokedAt)),
		).One(ctx, executor); err != nil {
			return ForwardingAddress{}, err
		}
	}

	tokenHashBytes := tokenHash[:]
	created, err := models.HouseholdMailForwardings.Insert(&models.HouseholdMailForwardingSetter{
		ID: &forwardingID, HouseholdID: &householdID, TokenHash: &tokenHashBytes, CreatedBy: &actorID,
	}).One(ctx, executor)
	if err != nil {
		return ForwardingAddress{}, err
	}
	household, err := models.Households.Update(
		um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))),
		um.SetCol("revision").To(models.Households.Columns.Revision.Plus(psql.Arg(1))),
		um.SetCol("updated_at").To(psql.F("now")),
	).One(ctx, executor)
	if err != nil {
		return ForwardingAddress{}, err
	}
	eventType := "household.mail_forwarding.issued"
	if rotate {
		eventType = "household.mail_forwarding.rotated"
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, forwardingID, eventType, household.Revision, []string{"mailForwardingAddress"}, actorID); err != nil {
		return ForwardingAddress{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ForwardingAddress{}, err
	}
	return ForwardingAddress{ID: forwardingID.String(), Address: address, CreatedAt: created.CreatedAt}, nil
}

func (s *Service) Revoke(ctx context.Context, command Command) (ForwardingAddress, error) {
	actorID, householdID, err := parseIDs(command)
	if err != nil {
		return ForwardingAddress{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ForwardingAddress{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return ForwardingAddress{}, apperror.ErrNotFound
	} else if err != nil {
		return ForwardingAddress{}, err
	}
	if err := requireOwner(ctx, executor, householdID, actorID); err != nil {
		return ForwardingAddress{}, err
	}
	active, err := models.HouseholdMailForwardings.Query(sm.Where(psql.And(
		models.HouseholdMailForwardings.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMailForwardings.Columns.RevokedAt.IsNull(),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return ForwardingAddress{}, apperror.ErrNotFound
	}
	if err != nil {
		return ForwardingAddress{}, err
	}
	revokedAt := time.Now().UTC()
	if _, err := models.HouseholdMailForwardings.Update(
		um.Where(models.HouseholdMailForwardings.Columns.ID.EQ(psql.Arg(active.ID))),
		um.SetCol("revoked_at").To(psql.Arg(revokedAt)),
	).One(ctx, executor); err != nil {
		return ForwardingAddress{}, err
	}
	household, err := models.Households.Update(
		um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))),
		um.SetCol("revision").To(models.Households.Columns.Revision.Plus(psql.Arg(1))),
		um.SetCol("updated_at").To(psql.F("now")),
	).One(ctx, executor)
	if err != nil {
		return ForwardingAddress{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, active.ID, "household.mail_forwarding.revoked", household.Revision, []string{"mailForwardingAddress"}, actorID); err != nil {
		return ForwardingAddress{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ForwardingAddress{}, err
	}
	return ForwardingAddress{ID: active.ID.String(), CreatedAt: active.CreatedAt}, nil
}

func parseIDs(command Command) (gofrsuuid.UUID, gofrsuuid.UUID, error) {
	actorID, err := gofrsuuid.FromString(command.ActorID)
	if err != nil {
		return gofrsuuid.Nil, gofrsuuid.Nil, apperror.ErrNotFound
	}
	householdID, err := gofrsuuid.FromString(command.HouseholdID)
	if err != nil {
		return gofrsuuid.Nil, gofrsuuid.Nil, apperror.ErrNotFound
	}
	return actorID, householdID, nil
}

func requireOwner(ctx context.Context, executor bob.Executor, householdID, actorID gofrsuuid.UUID) error {
	owner, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
	)), sm.ForShare()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && owner.Role != ownerRole {
		return apperror.ErrNotFound
	}
	return err
}
