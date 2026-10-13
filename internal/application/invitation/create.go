package invitation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const maxLifetime = 30 * 24 * time.Hour

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

type CreateCommand struct {
	ActorID     string
	HouseholdID string
	ExpiresAt   time.Time
}

type CreatedInvitation struct {
	ID        string
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func (s *Service) Create(ctx context.Context, command CreateCommand) (CreatedInvitation, error) {
	now := time.Now().UTC()
	expiresAt := command.ExpiresAt.UTC()
	if !expiresAt.After(now) || expiresAt.After(now.Add(maxLifetime)) {
		return CreatedInvitation{}, apperror.Wrap(apperror.KindBadInput, errors.New("invitation expiry must be within 30 days"))
	}
	householdID, err := gofrsuuid.FromString(command.HouseholdID)
	if err != nil {
		return CreatedInvitation{}, apperror.Wrap(apperror.KindNotFound, err)
	}
	actorID, err := gofrsuuid.FromString(command.ActorID)
	if err != nil {
		return CreatedInvitation{}, apperror.Wrap(apperror.KindNotFound, err)
	}
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return CreatedInvitation{}, fmt.Errorf("generate invitation token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	tokenHash := sha256.Sum256(rawToken)
	invitationID, err := gofrsuuid.NewV7()
	if err != nil {
		return CreatedInvitation{}, fmt.Errorf("generate invitation id: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreatedInvitation{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return CreatedInvitation{}, apperror.ErrNotFound
	} else if err != nil {
		return CreatedInvitation{}, err
	}
	owner, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
	)), sm.ForShare()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && owner.Role != "OWNER" {
		return CreatedInvitation{}, apperror.ErrNotFound
	}
	if err != nil {
		return CreatedInvitation{}, err
	}

	tokenHashValue := tokenHash[:]
	createdInvitation, err := models.HouseholdInvitations.Insert(&models.HouseholdInvitationSetter{
		ID: &invitationID, HouseholdID: &householdID, TokenHash: &tokenHashValue, ExpiresAt: &expiresAt, CreatedBy: &actorID,
	}).One(ctx, executor)
	if err != nil {
		return CreatedInvitation{}, err
	}
	household, err := models.Households.Update(
		um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))),
		um.SetCol("revision").To(models.Households.Columns.Revision.Plus(psql.Arg(int64(1)))),
		um.SetCol("updated_at").To(psql.F("now")),
	).One(ctx, executor)
	if err != nil {
		return CreatedInvitation{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, invitationID, "household.invitation.created", household.Revision, []string{"invitations"}, actorID); err != nil {
		return CreatedInvitation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreatedInvitation{}, err
	}
	return CreatedInvitation{
		ID: invitationID.String(), Token: token, CreatedAt: createdInvitation.CreatedAt, ExpiresAt: createdInvitation.ExpiresAt,
	}, nil
}
