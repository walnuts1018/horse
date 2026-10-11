package graph

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/walnuts1018/horse/internal/graph/model"
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
	if err := requireHouseholdOwner(ctx, tx, input.HouseholdID, user.MemberID); err != nil {
		return nil, err
	}
	id := newID()
	var createdAt, storedExpiresAt time.Time
	err = tx.QueryRow(ctx, `INSERT INTO household_invitations(id, household_id, token_hash, expires_at, created_by) VALUES ($1, $2, $3, $4, $5) RETURNING created_at, expires_at`, id, input.HouseholdID, tokenHash[:], expiresAt, user.MemberID).Scan(&createdAt, &storedExpiresAt)
	if err != nil {
		return nil, err
	}
	var revision int64
	if err := tx.QueryRow(ctx, `UPDATE households SET revision = revision + 1, updated_at = now() WHERE id = $1 RETURNING revision`, input.HouseholdID).Scan(&revision); err != nil {
		return nil, err
	}
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
	if err := requireHouseholdOwner(ctx, r.DB, householdID, user.MemberID); err != nil {
		return nil, err
	}
	rows, err := r.DB.Query(ctx, `SELECT id::text, household_id::text, role, expires_at, created_at, revoked_at, accepted_at FROM household_invitations WHERE household_id = $1 ORDER BY created_at DESC, id`, householdID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	invitations := make([]*model.HouseholdInvitation, 0)
	for rows.Next() {
		invitation := &model.HouseholdInvitation{}
		var expiresAt, createdAt time.Time
		var revokedAt, acceptedAt *time.Time
		if err := rows.Scan(&invitation.ID, &invitation.HouseholdID, &invitation.Role, &expiresAt, &createdAt, &revokedAt, &acceptedAt); err != nil {
			return nil, err
		}
		invitation.ExpiresAt, invitation.CreatedAt = timeString(expiresAt), timeString(createdAt)
		invitation.RevokedAt, invitation.AcceptedAt = optionalTimeString(revokedAt), optionalTimeString(acceptedAt)
		invitations = append(invitations, invitation)
	}
	if err := rows.Err(); err != nil {
		return nil, err
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
	if err := requireHouseholdOwner(ctx, tx, householdID, user.MemberID); err != nil {
		return nil, err
	}
	var invitation model.HouseholdInvitation
	var expiresAt, createdAt time.Time
	var revokedAt, acceptedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT id::text, household_id::text, role, expires_at, created_at, revoked_at, accepted_at FROM household_invitations WHERE id = $1 AND household_id = $2 FOR UPDATE`, invitationID, householdID).Scan(&invitation.ID, &invitation.HouseholdID, &invitation.Role, &expiresAt, &createdAt, &revokedAt, &acceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if acceptedAt != nil {
		return nil, ErrConflict
	}
	if revokedAt == nil {
		if err := tx.QueryRow(ctx, `UPDATE household_invitations SET revoked_at = now() WHERE id = $1 RETURNING revoked_at`, invitationID).Scan(&revokedAt); err != nil {
			return nil, err
		}
		var revision int64
		if err := tx.QueryRow(ctx, `UPDATE households SET revision = revision + 1, updated_at = now() WHERE id = $1 RETURNING revision`, householdID).Scan(&revision); err != nil {
			return nil, err
		}
		if err := writeEvent(ctx, tx, householdID, "household.invitation.revoked", invitationID, revision, []string{"invitations"}, user.MemberID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	invitation.ExpiresAt, invitation.CreatedAt = timeString(expiresAt), timeString(createdAt)
	invitation.RevokedAt, invitation.AcceptedAt = optionalTimeString(revokedAt), optionalTimeString(acceptedAt)
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
	var invitationID, householdID string
	var isExpired bool
	var revokedAt, acceptedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT id::text, household_id::text, expires_at <= now(), revoked_at, accepted_at FROM household_invitations WHERE token_hash = $1 FOR UPDATE`, tokenHash[:]).Scan(&invitationID, &householdID, &isExpired, &revokedAt, &acceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if revokedAt != nil || acceptedAt != nil || isExpired {
		return nil, ErrConflict
	}
	var alreadyMember bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM household_members WHERE household_id = $1 AND member_id = $2)`, householdID, user.MemberID).Scan(&alreadyMember); err != nil {
		return nil, err
	}
	if alreadyMember {
		return nil, ErrConflict
	}
	var joinedMemberID string
	err = tx.QueryRow(ctx, `INSERT INTO household_members(household_id, member_id, role) VALUES ($1, $2, 'MEMBER') ON CONFLICT (household_id, member_id) DO NOTHING RETURNING member_id::text`, householdID, user.MemberID).Scan(&joinedMemberID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE household_invitations SET accepted_by = $2, accepted_at = now() WHERE id = $1`, invitationID, user.MemberID); err != nil {
		return nil, err
	}
	var revision int64
	if err := tx.QueryRow(ctx, `UPDATE households SET revision = revision + 1, updated_at = now() WHERE id = $1 RETURNING revision`, householdID).Scan(&revision); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, householdID, "household.member.joined", user.MemberID, revision, []string{"members"}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return loadHousehold(ctx, r.DB, householdID)
}

type householdRoleQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func requireHouseholdOwner(ctx context.Context, db householdRoleQuerier, householdID, memberID string) error {
	var role string
	err := db.QueryRow(ctx, `SELECT role FROM household_members WHERE household_id = $1 AND member_id = $2 FOR SHARE`, householdID, memberID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if role != "OWNER" {
		return ErrNotFound
	}
	return nil
}

func optionalTimeString(value *time.Time) *string {
	if value == nil {
		return nil
	}
	result := timeString(*value)
	return &result
}
