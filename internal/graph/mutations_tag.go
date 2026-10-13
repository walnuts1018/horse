package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *mutationResolver) createTag(ctx context.Context, input model.CreateTagInput) (*model.Tag, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 80 {
		return nil, fmt.Errorf("%w: invalid tag name", ErrBadInput)
	}
	householdID, err := gofrsuuid.FromString(input.HouseholdID)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	if _, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID))), sm.ForUpdate()).One(ctx, executor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	id := generatedUUID(newID())
	revision := int64(1)
	row, err := models.Tags.Insert(&models.TagSetter{ID: &id, HouseholdID: &householdID, Name: &name, Revision: &revision}).One(ctx, executor)
	if err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, input.HouseholdID, "tag.created", id.String(), revision, []string{fieldName}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return tagModel(row), nil
}

func (r *mutationResolver) updateTag(ctx context.Context, input model.UpdateTagInput) (*model.Tag, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := gofrsuuid.FromString(input.TagID)
	if err != nil {
		return nil, ErrNotFound
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 80 {
		return nil, fmt.Errorf("%w: invalid tag name", ErrBadInput)
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	tag, err := models.Tags.Query(sm.Where(models.Tags.Columns.ID.EQ(psql.Arg(id))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := requireMember(ctx, r.DB, tag.HouseholdID.String()); err != nil {
		return nil, err
	}
	if int(tag.Revision) != input.BaseRevision {
		return nil, ErrConflict
	}
	revision := tag.Revision + 1
	if _, err := models.Tags.Update((&models.TagSetter{Name: &name, Revision: &revision}).UpdateMod(), um.Where(models.Tags.Columns.ID.EQ(psql.Arg(id)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, tag.HouseholdID.String(), "tag.updated", input.TagID, revision, []string{fieldName}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tag.Name, tag.Revision = name, revision
	return tagModel(tag), nil
}

func (r *mutationResolver) archiveTag(ctx context.Context, tagID string, baseRevision int, archived bool) (*model.Tag, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := gofrsuuid.FromString(tagID)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	executor := bobpgx.NewTx(tx, nil)
	tag, err := models.Tags.Query(sm.Where(models.Tags.Columns.ID.EQ(psql.Arg(id))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := requireMember(ctx, r.DB, tag.HouseholdID.String()); err != nil {
		return nil, err
	}
	if int(tag.Revision) != baseRevision {
		return nil, ErrConflict
	}
	revision := tag.Revision + 1
	if _, err := models.Tags.Update((&models.TagSetter{Archived: &archived, Revision: &revision}).UpdateMod(), um.Where(models.Tags.Columns.ID.EQ(psql.Arg(id)))).Exec(ctx, executor); err != nil {
		return nil, err
	}
	if err := writeEvent(ctx, tx, tag.HouseholdID.String(), "tag.archived", tagID, revision, []string{fieldArchived}, user.MemberID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tag.Archived, tag.Revision = archived, revision
	return tagModel(tag), nil
}

func tagModel(row *models.Tag) *model.Tag {
	return &model.Tag{ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Name: row.Name, Archived: row.Archived, Revision: int(row.Revision)}
}

func (r *queryResolver) tags(ctx context.Context, householdID string, includeArchived bool) ([]*model.Tag, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	var rows models.TagSlice
	if includeArchived {
		rows, err = models.Tags.Query(sm.Where(models.Tags.Columns.HouseholdID.EQ(psql.Arg(hhID))), sm.OrderBy(models.Tags.Columns.Name)).All(ctx, bobpgx.NewPool(r.DB))
	} else {
		rows, err = models.Tags.Query(sm.Where(psql.And(models.Tags.Columns.HouseholdID.EQ(psql.Arg(hhID)), models.Tags.Columns.Archived.EQ(psql.Arg(false)))), sm.OrderBy(models.Tags.Columns.Name)).All(ctx, bobpgx.NewPool(r.DB))
	}
	if err != nil {
		return nil, err
	}
	result := make([]*model.Tag, 0, len(rows))
	for _, row := range rows {
		result = append(result, tagModel(row))
	}
	return result, nil
}
