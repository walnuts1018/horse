package graph

import (
	"context"

	poolapp "github.com/walnuts1018/horse/internal/application/pool"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) createPool(ctx context.Context, input model.CreatePoolInput) (*model.Pool, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	created, err := poolapp.NewService(r.DB).Create(ctx, poolapp.CreateCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, Name: input.Name, Kind: string(input.Kind),
	})
	if err != nil {
		return nil, err
	}
	household, err := loadHousehold(ctx, r.DB, input.HouseholdID, householdFields{pools: true})
	if err != nil {
		return nil, err
	}
	for _, pool := range household.PoolVariants {
		if value := poolData(pool); value != nil && value.ID == created.ID {
			return value, nil
		}
	}
	return nil, ErrNotFound
}
