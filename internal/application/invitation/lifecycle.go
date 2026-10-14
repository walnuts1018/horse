package invitation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const memberRole = "MEMBER"

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

type RemoveMemberCommand struct {
	ActorID     string
	HouseholdID string
	MemberID    string
}

type LeaveHouseholdCommand struct {
	MemberID    string
	HouseholdID string
}

type MemberChange struct {
	HouseholdID string
}

func (s *Service) RemoveMember(ctx context.Context, command RemoveMemberCommand) (MemberChange, error) {
	return s.changeMembership(ctx, command.ActorID, command.HouseholdID, command.MemberID, false)
}

func (s *Service) LeaveHousehold(ctx context.Context, command LeaveHouseholdCommand) (MemberChange, error) {
	return s.changeMembership(ctx, command.MemberID, command.HouseholdID, command.MemberID, true)
}

func (s *Service) changeMembership(ctx context.Context, actorValue, householdValue, memberValue string, leave bool) (MemberChange, error) {
	actorID, err := gofrsuuid.FromString(actorValue)
	if err != nil {
		return MemberChange{}, apperror.ErrNotFound
	}
	householdID, err := gofrsuuid.FromString(householdValue)
	if err != nil {
		return MemberChange{}, apperror.ErrNotFound
	}
	memberID, err := gofrsuuid.FromString(memberValue)
	if err != nil {
		return MemberChange{}, apperror.ErrNotFound
	}
	if leave && actorID != memberID {
		return MemberChange{}, apperror.ErrNotFound
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return MemberChange{}, err
	}
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); errors.Is(err, sql.ErrNoRows) {
		return MemberChange{}, apperror.ErrNotFound
	} else if err != nil {
		return MemberChange{}, err
	}

	actor, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
		models.HouseholdMembers.Columns.LeftAt.IsNull(),
	)), sm.ForShare()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return MemberChange{}, apperror.ErrNotFound
	}
	if err != nil {
		return MemberChange{}, err
	}
	if !leave && actor.Role != ownerRole {
		return MemberChange{}, apperror.ErrNotFound
	}

	target, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
		models.HouseholdMembers.Columns.LeftAt.IsNull(),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return MemberChange{}, apperror.ErrNotFound
	}
	if err != nil {
		return MemberChange{}, err
	}
	if target.Role == ownerRole {
		members, err := models.HouseholdMembers.Query(sm.Where(psql.And(
			models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.HouseholdMembers.Columns.LeftAt.IsNull(),
		))).All(ctx, executor)
		if err != nil {
			return MemberChange{}, err
		}
		owners := 0
		for _, member := range members {
			if member.Role == ownerRole {
				owners++
			}
		}
		if owners <= 1 {
			return MemberChange{}, apperror.ErrConflict
		}
	}

	if referenced, err := hasActivePoolFundingReference(ctx, executor, householdID, memberID); err != nil {
		return MemberChange{}, err
	} else if referenced {
		return MemberChange{}, apperror.ErrConflict
	}
	if referenced, err := hasActiveFundingScheduleReference(ctx, executor, householdID, memberID); err != nil {
		return MemberChange{}, err
	} else if referenced {
		return MemberChange{}, apperror.ErrConflict
	}
	leftAt := time.Now().UTC()
	if _, err := models.HouseholdMembers.Update(
		um.Where(psql.And(
			models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
			models.HouseholdMembers.Columns.LeftAt.IsNull(),
		)),
		um.SetCol("left_at").To(psql.Arg(leftAt)),
	).One(ctx, executor); err != nil {
		return MemberChange{}, err
	}
	household, err := models.Households.Update(
		um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))),
		um.SetCol("revision").To(models.Households.Columns.Revision.Plus(psql.Arg(1))),
		um.SetCol("updated_at").To(psql.F("now")),
	).One(ctx, executor)
	if err != nil {
		return MemberChange{}, err
	}
	eventType := "household.member.removed"
	if leave {
		eventType = "household.member.left"
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, memberID, eventType, household.Revision, []string{"members"}, actorID); err != nil {
		return MemberChange{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MemberChange{}, err
	}
	return MemberChange{HouseholdID: householdID.String()}, nil
}

func hasActivePoolFundingReference(ctx context.Context, executor bob.Executor, householdID, memberID gofrsuuid.UUID) (bool, error) {
	pools, err := models.Pools.Query(sm.Where(models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)))).All(ctx, executor)
	if err != nil {
		return false, err
	}
	for _, pool := range pools {
		rule, err := models.PoolFundingRules.Query(sm.Where(psql.And(
			models.PoolFundingRules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.PoolFundingRules.Columns.PoolID.EQ(psql.Arg(pool.ID)),
			models.PoolFundingRules.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		if rule.OwnerMemberID.Valid && rule.OwnerMemberID.V == memberID {
			return true, nil
		}
		share, err := models.PoolFundingShares.Query(sm.Where(psql.And(
			models.PoolFundingShares.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.PoolFundingShares.Columns.PoolID.EQ(psql.Arg(pool.ID)),
			models.PoolFundingShares.Columns.Version.EQ(psql.Arg(pool.FundingRuleVersion)),
			models.PoolFundingShares.Columns.MemberID.EQ(psql.Arg(memberID)),
		))).Exists(ctx, executor)
		if err != nil {
			return false, err
		}
		if share {
			return true, nil
		}
	}

	return false, nil
}

func hasActiveFundingScheduleReference(ctx context.Context, executor bob.Executor, householdID, memberID gofrsuuid.UUID) (bool, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)

	return models.FundingSchedules.Query(sm.Where(psql.And(
		models.FundingSchedules.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.FundingSchedules.Columns.MemberID.EQ(psql.Arg(memberID)),
		models.FundingSchedules.Columns.Active.EQ(psql.Arg(true)),
		psql.Or(
			models.FundingSchedules.Columns.EndsOn.IsNull(),
			models.FundingSchedules.Columns.EndsOn.GTE(psql.Arg(today)),
		),
	))).Exists(ctx, executor)
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
	defer txcleanup.Rollback(ctx, tx)
	executor := bobpgx.NewTx(tx, nil)
	owner, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
		models.HouseholdMembers.Columns.LeftAt.IsNull(),
	)), sm.ForShare()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && owner.Role != ownerRole {
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
	defer txcleanup.Rollback(ctx, tx)
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
	householdID := invitation.HouseholdID
	existing, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(invitation.HouseholdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err == nil {
		if !existing.LeftAt.Valid {
			return "", apperror.ErrConflict
		}
		role := memberRole
		joinedAt := time.Now().UTC()
		if _, err := models.HouseholdMembers.Update(
			um.Where(psql.And(
				models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(invitation.HouseholdID)),
				models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(memberID)),
				models.HouseholdMembers.Columns.LeftAt.IsNotNull(),
			)),
			um.SetCol("left_at").To(psql.Arg(nil)),
			um.SetCol("joined_at").To(psql.Arg(joinedAt)),
			um.SetCol("role").To(psql.Arg(role)),
		).One(ctx, executor); err != nil {
			return "", err
		}
	} else {
		role := memberRole
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
