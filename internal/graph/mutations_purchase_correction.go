package graph

import (
	"context"
	"fmt"

	"github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) correctPurchase(ctx context.Context, input model.CorrectPurchaseInput) (*model.Purchase, error) {
	purchaseID, itemID, err := correctionTarget(input.Target)
	if err != nil {
		return nil, err
	}
	user, err := requireMember(ctx, r.DB, input.HouseholdID)
	if err != nil {
		return nil, err
	}
	fingerprint, err := mutationRequestFingerprint(input)
	if err != nil {
		return nil, err
	}
	command := purchase.CorrectionCommand{
		ActorID: user.MemberID, HouseholdID: input.HouseholdID, PurchaseID: purchaseID, ItemID: itemID,
		AmountDeltaYen: int64(input.AmountDeltaYen), Reason: input.Reason, IdempotencyKey: input.IdempotencyKey,
		Allocations:        make([]purchase.Allocation, 0, len(input.Allocations)),
		PaymentAllocations: make([]purchase.Payment, 0, len(input.PaymentAllocations)),
	}
	for _, allocation := range input.Allocations {
		if allocation == nil {
			command.Allocations = append(command.Allocations, purchase.Allocation{})
			continue
		}
		command.Allocations = append(command.Allocations, purchase.Allocation{PoolID: allocation.PoolID, Weight: int64(allocation.Weight)})
	}
	for _, payment := range input.PaymentAllocations {
		if payment == nil {
			command.PaymentAllocations = append(command.PaymentAllocations, purchase.Payment{})
			continue
		}
		command.PaymentAllocations = append(command.PaymentAllocations, purchase.Payment{AccountID: payment.PaymentAccountID, AmountYen: int64(payment.AmountYen)})
	}
	result, err := purchase.NewService(r.DB).Correct(ctx, command, fingerprint)
	if err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, result.PurchaseID)
}

func sameAllocationWeights(inputs []*model.AllocationInput, saved []domain.Weight) bool {
	allocations := make([]purchase.Allocation, 0, len(inputs))
	for _, input := range inputs {
		if input == nil {
			allocations = append(allocations, purchase.Allocation{})
			continue
		}
		allocations = append(allocations, purchase.Allocation{PoolID: input.PoolID, Weight: int64(input.Weight)})
	}
	return purchase.SameAllocationWeights(allocations, saved)
}

func correctionTarget(target *model.CorrectPurchaseTargetInput) (string, *string, error) {
	if target == nil {
		return "", nil, fmt.Errorf("%w: correction target is required", ErrBadInput)
	}
	switch {
	case target.Purchase != nil && target.Item == nil:
		return target.Purchase.PurchaseID, nil, nil
	case target.Item != nil && target.Purchase == nil:
		itemID := target.Item.ItemID
		return target.Item.PurchaseID, &itemID, nil
	default:
		return "", nil, fmt.Errorf("%w: exactly one correction target is required", ErrBadInput)
	}
}
