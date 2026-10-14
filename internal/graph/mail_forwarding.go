package graph

import (
	"context"

	"github.com/walnuts1018/horse/internal/application/mailforwarding"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) issueHouseholdMailForwarding(ctx context.Context, input model.HouseholdMailForwardingInput) (*model.HouseholdMailForwardingAddress, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	address, err := r.MailForwarding.Issue(ctx, mailforwarding.Command{ActorID: user.MemberID, HouseholdID: input.HouseholdID})
	if err != nil {
		return nil, err
	}
	return &model.HouseholdMailForwardingAddress{
		Address: address.Address,
		Status:  &model.HouseholdMailForwardingStatus{CreatedAt: timeString(address.CreatedAt), Active: true},
	}, nil
}

func (r *mutationResolver) rotateHouseholdMailForwarding(ctx context.Context, input model.HouseholdMailForwardingInput) (*model.HouseholdMailForwardingAddress, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	address, err := r.MailForwarding.Rotate(ctx, mailforwarding.Command{ActorID: user.MemberID, HouseholdID: input.HouseholdID})
	if err != nil {
		return nil, err
	}
	return &model.HouseholdMailForwardingAddress{
		Address: address.Address,
		Status:  &model.HouseholdMailForwardingStatus{CreatedAt: timeString(address.CreatedAt), Active: true},
	}, nil
}

func (r *mutationResolver) revokeHouseholdMailForwarding(ctx context.Context, input model.HouseholdMailForwardingInput) (*model.HouseholdMailForwardingStatus, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	address, err := r.MailForwarding.Revoke(ctx, mailforwarding.Command{ActorID: user.MemberID, HouseholdID: input.HouseholdID})
	if err != nil {
		return nil, err
	}
	return &model.HouseholdMailForwardingStatus{CreatedAt: timeString(address.CreatedAt), Active: false}, nil
}
