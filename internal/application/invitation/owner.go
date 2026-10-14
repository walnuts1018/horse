package invitation

import (
	"context"
	"database/sql"
	"errors"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const householdMembersField = "members"

type TransferOwnerCommand struct {
	ActorID     string
	HouseholdID string
	NewOwnerID  string
}

func (s *Service) TransferOwner(ctx context.Context, command TransferOwnerCommand) (MemberChange, error) {
	actorID, err := gofrsuuid.FromString(command.ActorID)
	if err != nil {
		return MemberChange{}, apperror.ErrNotFound
	}
	householdID, err := gofrsuuid.FromString(command.HouseholdID)
	if err != nil {
		return MemberChange{}, apperror.ErrNotFound
	}
	newOwnerID, err := gofrsuuid.FromString(command.NewOwnerID)
	if err != nil {
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
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && actor.Role != ownerRole {
		return MemberChange{}, apperror.ErrNotFound
	}
	if err != nil {
		return MemberChange{}, err
	}
	if actorID == newOwnerID {
		return MemberChange{}, apperror.ErrConflict
	}

	newOwner, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(newOwnerID)),
	)), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return MemberChange{}, apperror.ErrNotFound
	}
	if err != nil {
		return MemberChange{}, err
	}

	memberRole := "MEMBER"
	if _, err := models.HouseholdMembers.Update(
		um.Where(psql.And(
			models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
		)),
		um.SetCol("role").To(psql.Arg(memberRole)),
	).One(ctx, executor); err != nil {
		return MemberChange{}, err
	}
	if newOwner.Role != ownerRole {
		ownerRoleValue := ownerRole
		if _, err := models.HouseholdMembers.Update(
			um.Where(psql.And(
				models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
				models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(newOwnerID)),
			)),
			um.SetCol("role").To(psql.Arg(ownerRoleValue)),
		).One(ctx, executor); err != nil {
			return MemberChange{}, err
		}
	}

	household, err := models.Households.Update(
		um.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))),
		um.SetCol("revision").To(models.Households.Columns.Revision.Plus(psql.Arg(1))),
		um.SetCol("updated_at").To(psql.F("now")),
	).One(ctx, executor)
	if err != nil {
		return MemberChange{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, householdID, newOwnerID, "household.owner.transferred", household.Revision, []string{householdMembersField, "owner"}, actorID); err != nil {
		return MemberChange{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MemberChange{}, err
	}
	return MemberChange{HouseholdID: householdID.String()}, nil
}
