package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
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

func (r *queryResolver) tagConnection(ctx context.Context, householdID string, includeArchived bool, after, before *string, first, last *int) (*model.TagConnection, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	pageSize, backward, err := pageDirection(first, last)
	if err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	executor := bobpgx.NewPool(r.DB)
	baseFilters := []bob.Expression{models.Tags.Columns.HouseholdID.EQ(psql.Arg(hhID))}
	if !includeArchived {
		baseFilters = append(baseFilters, models.Tags.Columns.Archived.EQ(psql.Arg(false)))
	}
	baseFilter := psql.And(baseFilters...)
	count, err := models.Tags.Query(sm.Where(baseFilter)).Count(ctx, executor)
	if err != nil {
		return nil, err
	}
	filters := append([]bob.Expression(nil), baseFilters...)
	for _, boundary := range []struct {
		cursor   *string
		isBefore bool
	}{{cursor: after}, {cursor: before, isBefore: true}} {
		if boundary.cursor == nil || *boundary.cursor == "" {
			continue
		}
		value, err := decodeCursor(*boundary.cursor, nodeTypeTag)
		if err != nil {
			return nil, err
		}
		name, id, ok := strings.Cut(value, "\x00")
		if !ok || name == "" || id == "" {
			return nil, fmt.Errorf("%w: invalid Tag cursor", ErrBadInput)
		}
		cursorID, err := gofrsuuid.FromString(id)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid Tag cursor", ErrBadInput)
		}
		cursor, err := models.Tags.Query(sm.Where(psql.And(
			models.Tags.Columns.ID.EQ(psql.Arg(cursorID)),
			baseFilter,
		))).One(ctx, executor)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if cursor.Name != name {
			return nil, fmt.Errorf("%w: invalid Tag cursor", ErrBadInput)
		}
		filters = append(filters, tagCursorFilter(cursor, boundary.isBefore))
	}
	var rows []*models.Tag
	if backward {
		rows, err = models.Tags.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.Tags.Columns.Name).Desc(),
			sm.OrderBy(models.Tags.Columns.ID).Desc(),
			sm.Limit(pageSize+1),
		).All(ctx, executor)
	} else {
		rows, err = models.Tags.Query(
			sm.Where(psql.And(filters...)),
			sm.OrderBy(models.Tags.Columns.Name),
			sm.OrderBy(models.Tags.Columns.ID),
			sm.Limit(pageSize+1),
		).All(ctx, executor)
	}
	if err != nil {
		return nil, err
	}
	hasExtra := len(rows) > pageSize
	if hasExtra {
		rows = rows[:pageSize]
	}
	if backward {
		slices.Reverse(rows)
	}
	connection := &model.TagConnection{
		Edges:      make([]*model.TagEdge, 0, len(rows)),
		TotalCount: int(count),
		PageInfo: &model.PageInfo{
			HasNextPage:     backward && before != nil && *before != "" || !backward && hasExtra,
			HasPreviousPage: backward && hasExtra || !backward && after != nil && *after != "",
		},
	}
	for _, row := range rows {
		value := row.Name + "\x00" + row.ID.String()
		connection.Edges = append(connection.Edges, &model.TagEdge{Cursor: encodeCursor(nodeTypeTag, value), Node: tagModel(row)})
	}
	if len(rows) > 0 {
		start := encodeCursor(nodeTypeTag, rows[0].Name+"\x00"+rows[0].ID.String())
		end := encodeCursor(nodeTypeTag, rows[len(rows)-1].Name+"\x00"+rows[len(rows)-1].ID.String())
		connection.PageInfo.StartCursor, connection.PageInfo.EndCursor = &start, &end
	}
	return connection, nil
}

func tagCursorFilter(cursor *models.Tag, isBefore bool) bob.Expression {
	if isBefore {
		return psql.Or(
			models.Tags.Columns.Name.LT(psql.Arg(cursor.Name)),
			psql.And(models.Tags.Columns.Name.EQ(psql.Arg(cursor.Name)), models.Tags.Columns.ID.LT(psql.Arg(cursor.ID))),
		)
	}
	return psql.Or(
		models.Tags.Columns.Name.GT(psql.Arg(cursor.Name)),
		psql.And(models.Tags.Columns.Name.EQ(psql.Arg(cursor.Name)), models.Tags.Columns.ID.GT(psql.Arg(cursor.ID))),
	)
}

func (r *queryResolver) tags(ctx context.Context, householdID string, includeArchived bool) ([]*model.Tag, error) {
	if _, err := requireMember(ctx, r.DB, householdID); err != nil {
		return nil, err
	}
	hhID, err := gofrsuuid.FromString(householdID)
	if err != nil {
		return nil, ErrNotFound
	}
	filters := []bob.Expression{models.Tags.Columns.HouseholdID.EQ(psql.Arg(hhID))}
	if !includeArchived {
		filters = append(filters, models.Tags.Columns.Archived.EQ(psql.Arg(false)))
	}
	rows, err := models.Tags.Query(sm.Where(psql.And(filters...)), sm.OrderBy(models.Tags.Columns.Name), sm.OrderBy(models.Tags.Columns.ID)).All(ctx, bobpgx.NewPool(r.DB))
	if err != nil {
		return nil, err
	}
	result := make([]*model.Tag, 0, len(rows))
	for _, row := range rows {
		result = append(result, tagModel(row))
	}
	return result, nil
}
