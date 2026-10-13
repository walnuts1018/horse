package graph

import (
	"context"
	"fmt"

	fundingapp "github.com/walnuts1018/horse/internal/application/funding"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) recordFunding(ctx context.Context, input model.RecordFundingInput) (*model.LedgerEntryData, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	if input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney || input.IdempotencyKey == "" {
		return nil, fmt.Errorf("%w: invalid funding amount or idempotency key", ErrBadInput)
	}
	occurredAt, err := parseTime(input.OccurredAt)
	if err != nil {
		return nil, err
	}
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	result, err := fundingapp.New(r.DB).Do(ctx, fundingapp.Command{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, MemberID: input.MemberID,
		PoolID: input.PoolID, AmountYen: int64(input.AmountYen), OccurredAt: occurredAt,
		IdempotencyKey: input.IdempotencyKey, SourceAccountID: input.SourceAccountID,
		DestinationAccountID: input.DestinationAccountID, RequestFingerprint: fingerprint,
	})
	if err != nil {
		return nil, err
	}
	return loadLedgerEntry(ctx, r.DB, result.LedgerEntryID)
}
