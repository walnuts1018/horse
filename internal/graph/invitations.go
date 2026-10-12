package graph

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
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const maxInvitationLifetime = 30 * 24 * time.Hour

func (r *mutationResolver) createHouseholdInvitation(ctx context.Context, input model.CreateHouseholdInvitationInput) (*model.CreatedHouseholdInvitation, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	expiresAt, err := parseTime(input.ExpiresAt)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if !expiresAt.After(now) || expiresAt.After(now.Add(maxInvitationLifetime)) {
		return nil, fmt.Errorf("%w: invitation expiry must be within 30 days", ErrBadInput)
	}
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return nil, fmt.Errorf("generate invitation token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	tokenHash := sha256.Sum256(rawToken)
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := requireHouseholdOwner(ctx, bobpgx.NewTx(tx, nil), input.HouseholdID, user.MemberID); err != nil {
		return nil, err
	}
	id := newID()
	var createdAt, storedExpiresAt time.Time
	idValue, _ := gofrsuuid.FromString(id)
	householdValue, _ := gofrsuuid.FromString(input.HouseholdID)
	createdBy, _ := gofrsuuid.FromString(user.MemberID)
	expiresValue, hashValue := expiresAt, tokenHash[:]
	createdInvitation, err := models.HouseholdInvitations.Insert(&models.HouseholdInvitationSetter{ID: &idValue, HouseholdID: &householdValue, TokenHash: &hashValue, ExpiresAt: &expiresValue, CreatedBy: &createdBy}).One(ctx, bobpgx.NewTx(tx, nil))
	if err != nil {
		return nil, err
	}
	createdAt, storedExpiresAt = createdInvitation.CreatedAt, createdInvitation.ExpiresAt
	household, err := models.Households.Update(
		um.Where(models.Households.Columns.ID.EQ(psql.Arg(input.HouseholdID))),
		um.SetCol("revision").To(models.Households.Columns.Revision.Plus(psql.Arg(1))),
		um.SetCol("updated_at").To(psql.F("now")),
	).One(ctx, bobpgx.NewTx(tx, nil))
	if err != nil {
		return nil, err
	}
	revision := household.Revision
	if err := writeEvent(ctx, tx, input.HouseholdID, "household.invitation.created", id, revision, []string{"invitations"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &model.CreatedHouseholdInvitation{
		Invitation: &model.HouseholdInvitation{ID: id, HouseholdID: input.HouseholdID, Role: "MEMBER", ExpiresAt: timeString(storedExpiresAt), CreatedAt: timeString(createdAt)},
		Token:      token,
	}, nil
}

func (r *queryResolver) householdInvitations(ctx context.Context, householdID string) ([]*model.HouseholdInvitation, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireHouseholdOwner(ctx, bobpgx.NewPool(r.DB), householdID, user.MemberID); err != nil {
		return nil, err
	}
	rows, err := models.HouseholdInvitations.Query(
		sm.Where(models.HouseholdInvitations.Columns.HouseholdID.EQ(psql.Arg(householdID))),
		sm.OrderBy(models.HouseholdInvitations.Columns.CreatedAt).Desc(),
		sm.OrderBy(models.HouseholdInvitations.Columns.ID),
	).All(ctx, bobpgx.NewPool(r.DB))
	if err != nil {
		return nil, err
	}
	invitations := make([]*model.HouseholdInvitation, 0)
	for _, row := range rows {
		invitation := &model.HouseholdInvitation{}
		invitation.ID, invitation.HouseholdID, invitation.Role = row.ID.String(), row.HouseholdID.String(), row.Role
		invitation.ExpiresAt, invitation.CreatedAt = timeString(row.ExpiresAt), timeString(row.CreatedAt)
		invitation.RevokedAt, invitation.AcceptedAt = optionalTimeString(invitationTimePointer(row.RevokedAt)), optionalTimeString(invitationTimePointer(row.AcceptedAt))
		invitations = append(invitations, invitation)
	}
	return invitations, nil
}

func (r *mutationResolver) revokeHouseholdInvitation(ctx context.Context, householdID, invitationID string) (*model.HouseholdInvitation, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := requireHouseholdOwner(ctx, bobpgx.NewTx(tx, nil), householdID, user.MemberID); err != nil {
		return nil, err
	}
	invitationRow, err := models.HouseholdInvitations.Query(
		sm.Where(psql.And(
			models.HouseholdInvitations.Columns.ID.EQ(psql.Arg(invitationID)),
			models.HouseholdInvitations.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		)),
		sm.ForUpdate(),
	).One(ctx, bobpgx.NewTx(tx, nil))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if invitationRow.AcceptedAt.Valid {
		return nil, ErrConflict
	}
	if !invitationRow.RevokedAt.Valid {
		revokedAt := time.Now()
		if _, err := models.HouseholdInvitations.Update(
			um.Where(models.HouseholdInvitations.Columns.ID.EQ(psql.Arg(invitationRow.ID))),
			um.SetCol("revoked_at").To(psql.Arg(revokedAt)),
		).One(ctx, bobpgx.NewTx(tx, nil)); err != nil {
			return nil, err
		}
		household, err := models.Households.Update(
			um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))),
			um.SetCol("revision").To(models.Households.Columns.Revision.Plus(psql.Arg(1))),
			um.SetCol("updated_at").To(psql.F("now")),
		).One(ctx, bobpgx.NewTx(tx, nil))
		if err != nil {
			return nil, err
		}
		if err := writeEvent(ctx, tx, householdID, "household.invitation.revoked", invitationID, household.Revision, []string{"invitations"}, user.MemberID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	invitation := model.HouseholdInvitation{ID: invitationRow.ID.String(), HouseholdID: invitationRow.HouseholdID.String(), Role: invitationRow.Role}
	invitation.ExpiresAt, invitation.CreatedAt = timeString(invitationRow.ExpiresAt), timeString(invitationRow.CreatedAt)
	invitation.RevokedAt, invitation.AcceptedAt = optionalTimeString(invitationTimePointer(invitationRow.RevokedAt)), optionalTimeString(invitationTimePointer(invitationRow.AcceptedAt))
	return &invitation, nil
}

func (r *mutationResolver) acceptHouseholdInvitation(ctx context.Context, token string) (*model.Household, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	rawToken, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(rawToken) != 32 {
		return nil, ErrNotFound
	}
	tokenHash := sha256.Sum256(rawToken)
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	invitationRow, err := models.HouseholdInvitations.Query(
		sm.Where(models.HouseholdInvitations.Columns.TokenHash.EQ(psql.Arg(tokenHash[:]))),
		sm.ForUpdate(),
	).One(ctx, bobpgx.NewTx(tx, nil))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if invitationRow.RevokedAt.Valid || invitationRow.AcceptedAt.Valid || !invitationRow.ExpiresAt.After(time.Now()) {
		return nil, ErrConflict
	}
	householdID := invitationRow.HouseholdID.String()
	_, err = models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(invitationRow.HouseholdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(user.MemberID)),
	))).One(ctx, bobpgx.NewTx(tx, nil))
	if err == nil {
		return nil, ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	memberUUID, _ := gofrsuuid.FromString(user.MemberID)
	householdUUID := invitationRow.HouseholdID
	role := "MEMBER"
	_, err = models.HouseholdMembers.Insert(
		&models.HouseholdMemberSetter{HouseholdID: &householdUUID, MemberID: &memberUUID, Role: &role},
		im.OnConflict("household_id", "member_id").DoNothing(),
	).One(ctx, bobpgx.NewTx(tx, nil))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	acceptedBy, _ := gofrsuuid.FromString(user.MemberID)
	acceptedAt := time.Now()
	if _, err := models.HouseholdInvitations.Update(
		um.Where(models.HouseholdInvitations.Columns.ID.EQ(psql.Arg(invitationRow.ID))),
		um.SetCol("accepted_by").To(psql.Arg(acceptedBy)),
		um.SetCol("accepted_at").To(psql.Arg(acceptedAt)),
	).One(ctx, bobpgx.NewTx(tx, nil)); err != nil {
		return nil, err
	}
	household, err := models.Households.Update(
		um.Where(models.Households.Columns.ID.EQ(psql.Arg(invitationRow.HouseholdID))),
		um.SetCol("revision").To(models.Households.Columns.Revision.Plus(psql.Arg(1))),
		um.SetCol("updated_at").To(psql.F("now")),
	).One(ctx, bobpgx.NewTx(tx, nil))
	if err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, householdID, "household.member.joined", user.MemberID, household.Revision, []string{"members"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadHousehold(ctx, r.DB, householdID)
}

func requireHouseholdOwner(ctx context.Context, db bob.Executor, householdID, memberID string) error {
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
	)), sm.ForShare()).One(ctx, db)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if member.Role != "OWNER" {
		return ErrNotFound
	}
	return nil
}

func invitationTimePointer(value sql.Null[time.Time]) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.V
}

func optionalTimeString(value *time.Time) *string {
	if value == nil {
		return nil
	}
	result := timeString(*value)
	return &result
}
