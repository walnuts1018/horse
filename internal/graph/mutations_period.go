package graph

import (
	"context"
	"errors"
	"fmt"

	settlementapp "github.com/walnuts1018/horse/internal/application/settlement"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) closePeriod(ctx context.Context, input model.ClosePeriodInput) (model.SettlementVariant, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	result, err := settlementapp.New(r.DB).Close(ctx, settlementapp.CloseCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, Period: string(input.Period), ExpectedRevision: int64(input.ExpectedRevision),
	})
	if err != nil {
		return nil, mapSettlementError(err)
	}
	return settlementModel(result)
}

func mapSettlementError(err error) error {
	switch {
	case errors.Is(err, settlementapp.ErrBadInput):
		return fmt.Errorf("%w: %w", ErrBadInput, err)
	case errors.Is(err, settlementapp.ErrConflict):
		return fmt.Errorf("%w: %w", ErrConflict, err)
	case errors.Is(err, settlementapp.ErrNotFound):
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	case errors.Is(err, settlementapp.ErrUnattributedPurchasePayment):
		return settlementErrorAlias{cause: err, target: ErrUnattributedPurchasePayment}
	default:
		return err
	}
}

type settlementErrorAlias struct {
	cause  error
	target error
}

func (err settlementErrorAlias) Error() string {
	return err.cause.Error()
}

func (err settlementErrorAlias) Unwrap() []error {
	return []error{err.cause, err.target}
}
