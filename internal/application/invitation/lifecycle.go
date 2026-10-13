package invitation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type InvitationState struct {
	ID          string
	HouseholdID string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	AcceptedAt  sql.Null[time.Time]
	RevokedAt   sql.Null[time.Time]
}

type RevokeCommand struct {
	ActorID      string
	HouseholdID  string
	InvitationID string
}

type AcceptCommand struct {
	MemberID string
	Token    string
}

func (s *Service) Revoke(ctx context.Context, command RevokeCommand) (InvitationState, error) {
	actorID, err := gofrsuuid.FromString(command.ActorID)
	if err != nil {
		return InvitationState{}, apperror.ErrNotFound
	}
	householdID, err := gofrsuuid.FromString(command.HouseholdID)
	if err != nil {
		return InvitationState{}, apperror.ErrNotFound
	}
	invitationID, err := gofrsuuid.FromString(command.InvitationID)
	if err != nil {
		return InvitationState{}, apperror.ErrNotFound
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return InvitationState{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	owner, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
	)), sm.ForShare()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && owner.Role != "OWNER" {
		return InvitationState{}, apperror.ErrNotFound
	}
	if err != nil {
		return InvitationState{}, err
	}
	invitation, err := models.HouseholdInvitations.Query(sm.Where(psql.And(
		models.HouseholdInvitations.Columns.ID.EQ(psql.Arg(invitationID)),
		models.HouseholdInvitations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return InvitationState{}, apperror.ErrNotFound
	}
	if err != nil {
		return InvitationState{}, err
	}
	if invitation.AcceptedAt.Valid {
		return InvitationState{}, apperror.ErrConflict
	}
	if !invitation.RevokedAt.Valid {
		revokedAt := time.Now().UTC()
		invitation, err = models.HouseholdInvitations.Update(
			um.Where(models.HouseholdInvitations.Columns.ID.EQ(psql.Arg(invitation.ID))),
			um.SetCol("revoked_at").To(psql.Arg(revokedAt)),
		).One(ctx, executor)
		if err != nil {
			return InvitationState{}, err
		}
		household, err := models.Households.Update(
			um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))),
			um.SetCol("revision").To(models.Households.Columns.Revision.Plus(psql.Arg(1))),
			um.SetCol("updated_at").To(psql.F("now")),
		).One(ctx, executor)
		if err != nil {
			return InvitationState{}, err
		}
		if err := accounting.RecordEvent(ctx, executor, householdID, invitationID, "household.invitation.revoked", household.Revision, []string{"invitations"}, actorID); err != nil {
			return InvitationState{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return InvitationState{}, err
	}
	return invitationState(invitation), nil
}

func (s *Service) Accept(ctx context.Context, command AcceptCommand) (string, error) {
	memberID, err := gofrsuuid.FromString(command.MemberID)
	if err != nil {
		return "", apperror.ErrNotFound
	}
	rawToken, err := base64.RawURLEncoding.DecodeString(command.Token)
	if err != nil || len(rawToken) != 32 {
		return "", apperror.ErrNotFound
	}
	tokenHash := sha256.Sum256(rawToken)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	invitation, err := models.HouseholdInvitations.Query(
		sm.Where(models.HouseholdInvitations.Columns.TokenHash.EQ(psql.Arg(tokenHash[:]))),
		sm.ForUpdate(),
	).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return "", apperror.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if invitation.RevokedAt.Valid || invitation.AcceptedAt.Valid || !invitation.ExpiresAt.After(time.Now()) {
		return "", apperror.ErrConflict
	}
	_, err = models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(invitation.HouseholdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
	))).One(ctx, executor)
	if err == nil {
		return "", apperror.ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	householdID := invitation.HouseholdID
	role := "MEMBER"
	_, err = models.HouseholdMembers.Insert(
		&models.HouseholdMemberSetter{HouseholdID: &householdID, MemberID: &memberID, Role: &role},
		im.OnConflict("household_id", "member_id").DoNothing(),
	).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return "", apperror.ErrConflict
	}
	if err != nil {
		return "", err
	}
	acceptedAt := time.Now().UTC()
	if _, err := models.HouseholdInvitations.Update(
		um.Where(models.HouseholdInvitations.Columns.ID.EQ(psql.Arg(invitation.ID))),
		um.SetCol("accepted_by").To(psql.Arg(memberID)),
		um.SetCol("accepted_at").To(psql.Arg(acceptedAt)),
	).One(ctx, executor); err != nil {
		return "", err
	}
	household, err := models.Households.Update(
		um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))),
		um.SetCol("revision").To(models.Households.Columns.Revision.Plus(psql.Arg(1))),
		um.SetCol("updated_at").To(psql.F("now")),
	).One(ctx, executor)
	if err != nil {
		return "", err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, memberID, "household.member.joined", household.Revision, []string{"members"}, memberID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return householdID.String(), nil
}

func invitationState(invitation *models.HouseholdInvitation) InvitationState {
	return InvitationState{
		ID: invitation.ID.String(), HouseholdID: invitation.HouseholdID.String(), CreatedAt: invitation.CreatedAt,
		ExpiresAt: invitation.ExpiresAt, AcceptedAt: invitation.AcceptedAt, RevokedAt: invitation.RevokedAt,
	}
}
