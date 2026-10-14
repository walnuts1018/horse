package graph

import (
	"context"

	invitationapp "github.com/walnuts1018/horse/internal/application/invitation"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) transferHouseholdOwner(ctx context.Context, input model.TransferHouseholdOwnerInput) (*model.TransferHouseholdOwnerSuccess, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	change, err := invitationapp.NewService(r.DB).TransferOwner(ctx, invitationapp.TransferOwnerCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, NewOwnerID: input.NewOwnerID,
	})
	if err != nil {
		return nil, err
	}
	return &model.TransferHouseholdOwnerSuccess{HouseholdID: change.HouseholdID}, nil
}
