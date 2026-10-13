package tag

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/application/accounting"
	"github.com/walnuts1018/horse/internal/application/apperror"
	"github.com/walnuts1018/horse/internal/application/inputvalidation"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type UpdateCommand struct {
	ActorID      string
	TagID        string
	BaseRevision int
	Name         string `validate:"notblank,max=80"`
}

type ArchiveCommand struct {
	ActorID      string
	TagID        string
	BaseRevision int
	Archived     bool
}

func (s *Service) Update(ctx context.Context, command UpdateCommand) (Tag, error) {
	command.Name = strings.TrimSpace(command.Name)
	if err := inputvalidation.Check(command); err != nil {
		return Tag{}, err
	}
	name := command.Name
	return s.update(ctx, command.ActorID, command.TagID, command.BaseRevision, "tag.updated", []string{"name"}, func(_ *models.Tag, revision int64) *models.TagSetter {
		return &models.TagSetter{Name: &name, Revision: &revision}
	})
}

func (s *Service) Archive(ctx context.Context, command ArchiveCommand) (Tag, error) {
	return s.update(ctx, command.ActorID, command.TagID, command.BaseRevision, "tag.archived", []string{"archived"}, func(_ *models.Tag, revision int64) *models.TagSetter {
		return &models.TagSetter{Archived: &command.Archived, Revision: &revision}
	})
}

func (s *Service) update(ctx context.Context, actorIDValue, tagIDValue string, baseRevision int, eventType string, changedFields []string, setter func(*models.Tag, int64) *models.TagSetter) (Tag, error) {
	tagID, err := gofrsuuid.FromString(tagIDValue)
	if err != nil {
		return Tag{}, apperror.ErrNotFound
	}
	actorID, err := gofrsuuid.FromString(actorIDValue)
	if err != nil {
		return Tag{}, apperror.ErrNotFound
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Tag{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	row, err := models.Tags.Query(sm.Where(models.Tags.Columns.ID.EQ(psql.Arg(tagID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return Tag{}, apperror.ErrNotFound
	}
	if err != nil {
		return Tag{}, err
	}
	member, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(row.HouseholdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
	))).Exists(ctx, executor)
	if err != nil {
		return Tag{}, err
	}
	if !member {
		return Tag{}, apperror.ErrNotFound
	}
	if int(row.Revision) != baseRevision {
		return Tag{}, apperror.ErrConflict
	}
	revision := row.Revision + 1
	values := setter(row, revision)
	if _, err := models.Tags.Update(values.UpdateMod(), um.Where(models.Tags.Columns.ID.EQ(psql.Arg(tagID)))).Exec(ctx, executor); err != nil {
		return Tag{}, err
	}
	if err := accounting.RecordEvent(ctx, executor, row.HouseholdID, tagID, eventType, revision, changedFields, actorID); err != nil {
		return Tag{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Tag{}, err
	}
	if values.Name != nil {
		row.Name = *values.Name
	}
	if values.Archived != nil {
		row.Archived = *values.Archived
	}
	row.Revision = revision
	return Tag{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, Archived: row.Archived, Revision: row.Revision}, nil
}
