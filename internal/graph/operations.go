package graph

import (
	"context"
	"fmt"

	fundingapp "github.com/walnuts1018/horse/internal/application/funding"
	transferapp "github.com/walnuts1018/horse/internal/application/transfer"
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

func (r *mutationResolver) recordTransferCommand(ctx context.Context, input transferCommand) (*model.LedgerEntryData, error) {
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.FromMemberID); err != nil {
		return nil, err
	}
	if err := verifyHouseholdMember(ctx, r.DB, input.HouseholdID, input.ToMemberID); err != nil {
		return nil, err
	}
	if input.AmountYen <= 0 || int64(input.AmountYen) > maxMoney || input.IdempotencyKey == "" || !input.Kind.IsValid() {
		return nil, fmt.Errorf("%w: invalid transfer", ErrBadInput)
	}
	occurredAt, err := parseTime(input.OccurredAt)
	if err != nil {
		return nil, err
	}
	if input.Kind == model.TransferKindRepayment && input.LoanID == nil {
		return nil, fmt.Errorf("%w: repayment requires a loan", ErrBadInput)
	}
	if input.Kind != model.TransferKindRepayment && input.LoanID != nil {
		return nil, fmt.Errorf("%w: loanId is only valid for a repayment", ErrBadInput)
	}
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	result, err := transferapp.New(r.DB).Do(ctx, transferapp.Command{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, FromMemberID: input.FromMemberID,
		ToMemberID: input.ToMemberID, Kind: string(input.Kind), LoanID: input.LoanID,
		PaymentAccountID: input.PaymentAccountID, DestinationAccountID: input.DestinationAccountID,
		AmountYen: int64(input.AmountYen), OccurredAt: occurredAt, Description: input.Description,
		IdempotencyKey: input.IdempotencyKey, RequestFingerprint: fingerprint,
	})
	if err != nil {
		return nil, err
	}
	return loadLedgerEntry(ctx, r.DB, result.LedgerEntryID)
}
