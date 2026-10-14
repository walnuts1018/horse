package graph

import (
	"context"

	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/walnuts1018/horse/internal/graph/model"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (b *nodeBatch) loadAccountCategoryNodes(ctx context.Context) error {
	if len(b.idsByKind[nodeTypeAccount]) > 0 {
		accounts, err := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.In(b.householdArgs...),
			models.Accounts.Columns.ID.In(b.ids(nodeTypeAccount)...),
		))).All(ctx, b.executor)
		if err != nil {
			return err
		}
		for _, account := range accounts {
			b.set(nodeTypeAccount, account.ID.String(), graphAccount(account))
		}
	}
	if len(b.idsByKind[nodeTypeCategory]) > 0 {
		categories, err := models.Categories.Query(sm.Where(psql.And(
			models.Categories.Columns.HouseholdID.In(b.householdArgs...),
			models.Categories.Columns.ID.In(b.ids(nodeTypeCategory)...),
		))).All(ctx, b.executor)
		if err != nil {
			return err
		}
		for _, category := range categories {
			value := &model.Category{ID: category.ID.String(), Name: category.Name, Archived: category.Archived}
			if category.ParentID.Valid {
				parentID := category.ParentID.V.String()
				value.ParentID = &parentID
			}
			b.set(nodeTypeCategory, value.ID, value)
		}
	}
	return nil
}
