package graph

import (
	"context"

	invitationapp "github.com/walnuts1018/horse/internal/application/invitation"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) removeHouseholdMember(ctx context.Context, input model.RemoveHouseholdMemberInput) (*model.RemoveHouseholdMemberSuccess, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	change, err := invitationapp.NewService(r.DB).RemoveMember(ctx, invitationapp.RemoveMemberCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, MemberID: input.MemberID,
	})
	if err != nil {
		return nil, err
	}
	return &model.RemoveHouseholdMemberSuccess{HouseholdID: change.HouseholdID}, nil
}

func (r *mutationResolver) leaveHousehold(ctx context.Context, input model.LeaveHouseholdInput) (*model.LeaveHouseholdSuccess, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	change, err := invitationapp.NewService(r.DB).LeaveHousehold(ctx, invitationapp.LeaveHouseholdCommand{
		MemberID: user.MemberID, HouseholdID: input.HouseholdID,
	})
	if err != nil {
		return nil, err
	}
	return &model.LeaveHouseholdSuccess{HouseholdID: change.HouseholdID}, nil
}
