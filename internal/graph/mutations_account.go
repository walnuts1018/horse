package graph

import (
	"context"

	accountapp "github.com/walnuts1018/horse/internal/application/account"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) createAccount(ctx context.Context, input model.CreateAccountInput) (*model.Account, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	var opening *accountapp.OpeningBalance
	if input.OpeningBalance != nil {
		openingAt, err := parseTime(input.OpeningBalance.At)
		if err != nil {
			return nil, err
		}
		opening = &accountapp.OpeningBalance{AmountYen: int64(input.OpeningBalance.AmountYen), At: openingAt}
	}
	created, err := accountapp.NewService(r.DB).Create(ctx, accountapp.CreateCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, Name: input.Name,
		Kind: input.Kind.String(), PoolID: input.PoolID, Opening: opening,
	})
	if err != nil {
		return nil, err
	}
	return &model.Account{ID: created.ID, Name: created.Name, Kind: model.AccountKind(created.Kind), PoolID: created.PoolID}, nil
}
