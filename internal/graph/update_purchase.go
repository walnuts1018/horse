package graph

import (
	"context"

	"github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) updatePurchase(ctx context.Context, input model.UpdatePurchaseInput) (*model.Purchase, error) {
	changedFields, err := purchasePatchChangedFieldKeys(input.Patch)
	if err != nil {
		return nil, err
	}
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	command, err := updatePurchaseCommand(input, user.MemberID, changedFields)
	if err != nil {
		return nil, err
	}
	result, err := purchase.NewService(r.DB).Update(ctx, command)
	if err != nil {
		return nil, err
	}
	return loadPurchase(ctx, r.DB, result.PurchaseID)
}

func updatePurchaseCommand(input model.UpdatePurchaseInput, actorID string, changedFields []string) (purchase.UpdateCommand, error) {
	command := purchase.UpdateCommand{
		ActorID: actorID, PurchaseID: input.PurchaseID, BaseRevision: int64(input.BaseRevision), ChangedFields: changedFields,
	}
	if input.Patch.OccurredAt.IsSet() {
		occurredAt, err := parseTime(*input.Patch.OccurredAt.Value())
		if err != nil {
			return purchase.UpdateCommand{}, err
		}
		command.OccurredAt = &occurredAt
	}
	if input.Patch.Merchant.IsSet() {
		command.Merchant = input.Patch.Merchant.Value()
	}
	if input.Patch.PaymentAllocations.IsSet() {
		allocations := make([]purchase.Payment, 0, len(input.Patch.PaymentAllocations.Value()))
		for _, allocation := range input.Patch.PaymentAllocations.Value() {
			if allocation == nil {
				allocations = append(allocations, purchase.Payment{})
				continue
			}
			allocations = append(allocations, purchase.Payment{AccountID: allocation.PaymentAccountID, AmountYen: int64(allocation.AmountYen)})
		}
		command.PaymentAllocations = &allocations
	}
	return command, nil
}
