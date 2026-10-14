package graph

import (
	"context"

	categoryapp "github.com/walnuts1018/horse/internal/application/category"
	"github.com/walnuts1018/horse/internal/graph/model"
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
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	updated, err := categoryapp.NewService(r.DB).SetArchived(ctx, categoryapp.SetArchivedCommand{
		ActorID: user.MemberID, CategoryID: categoryID, Archived: archived,
	})
	if err != nil {
		return nil, err
	}
	return &model.Category{ID: updated.ID, Name: updated.Name, ParentID: updated.ParentID, Archived: updated.Archived}, nil
}

func (r *mutationResolver) updateCategoryName(ctx context.Context, categoryID, name string) (*model.Category, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	updated, err := categoryapp.NewService(r.DB).UpdateName(ctx, categoryapp.UpdateNameCommand{
		ActorID: user.MemberID, CategoryID: categoryID, Name: name,
	})
	if err != nil {
		return nil, err
	}
	return &model.Category{ID: updated.ID, Name: updated.Name, ParentID: updated.ParentID, Archived: updated.Archived}, nil
}
