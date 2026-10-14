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
		Kind: input.Kind.String(), CardLast4: input.CardLast4, PoolID: input.PoolID, Opening: opening,
	})
	if err != nil {
		return nil, err
	}
	return &model.Account{ID: created.ID, Name: created.Name, Kind: model.AccountKind(created.Kind), CardLast4: created.CardLast4, PoolID: created.PoolID}, nil
}

func (r *mutationResolver) setAccountCardLast4(ctx context.Context, input model.SetAccountCardLast4Input) (*model.Account, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	updated, err := accountapp.NewService(r.DB).SetCardLast4(ctx, accountapp.SetCardLast4Command{
		ActorID: user.MemberID, AccountID: input.AccountID, CardLast4: input.CardLast4,
	})
	if err != nil {
		return nil, err
	}
	return &model.Account{
		ID: updated.ID, Name: updated.Name, Kind: model.AccountKind(updated.Kind),
		CardLast4: updated.CardLast4, PoolID: updated.PoolID,
	}, nil
}
