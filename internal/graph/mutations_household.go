package graph

import (
	"context"

	householdapp "github.com/walnuts1018/horse/internal/application/household"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) createHousehold(ctx context.Context, input model.CreateHouseholdInput) (*model.Household, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	created, err := householdapp.NewService(r.DB).Create(ctx, householdapp.CreateCommand{
		ActorID:     user.MemberID,
		Name:        input.Name,
		MemberNames: input.MemberNames,
	})
	if err != nil {
		return nil, err
	}
	return loadHousehold(ctx, r.DB, created.ID)
}
