package graph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	categoryapp "github.com/walnuts1018/horse/internal/application/category"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (r *mutationResolver) createCategory(ctx context.Context, input model.CreateCategoryInput) (*model.Category, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	created, err := categoryapp.NewService(r.DB).Create(ctx, categoryapp.CreateCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, Name: input.Name, ParentID: input.ParentID,
	})
	if err != nil {
		return nil, err
	}
	return &model.Category{ID: created.ID, Name: created.Name, ParentID: created.ParentID}, nil
}

func (r *mutationResolver) setCategoryArchived(ctx context.Context, categoryID string, archived bool) (*model.Category, error) {
	categoryUUID, err := gofrsuuid.FromString(categoryID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid category id", ErrBadInput)
	}
	categoryRow, err := models.Categories.Query(sm.Where(models.Categories.Columns.ID.EQ(psql.Arg(categoryUUID)))).One(ctx, bobpgx.NewPool(r.DB))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	householdID := categoryRow.HouseholdID.String()
	user, err := requireMember(ctx, r.DB, householdID)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	householdUUID := categoryRow.HouseholdID
	categoryRow, err = models.Categories.Query(sm.Where(psql.And(
		models.Categories.Columns.ID.EQ(psql.Arg(categoryUUID)),
		models.Categories.Columns.HouseholdID.EQ(psql.Arg(householdUUID)),
	)), sm.ForUpdate()).One(ctx, bobpgx.NewTx(tx, nil))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	name := categoryRow.Name
	var parentID *string
	if categoryRow.ParentID.Valid {
		parent := categoryRow.ParentID.V.String()
		parentID = &parent
	}
	if categoryRow.Archived != archived {
		if _, err := models.Categories.Update((&models.CategorySetter{Archived: &archived}).UpdateMod(), um.Where(models.Categories.Columns.ID.EQ(psql.Arg(categoryUUID)))).Exec(ctx, bobpgx.NewTx(tx, nil)); err != nil {
			return nil, err
		}
		eventType := "category.unarchived"
		if archived {
			eventType = "category.archived"
		}
		if err := writeEvent(ctx, tx, householdID, eventType, categoryID, 1, []string{fieldArchived}, user.MemberID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &model.Category{ID: categoryID, Name: name, ParentID: parentID, Archived: archived}, nil
}
