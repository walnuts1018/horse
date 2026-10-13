package graph

import (
	"context"

	purchaseapp "github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) bulkUpdatePurchases(ctx context.Context, input model.BulkUpdatePurchasesInput) ([]*model.Purchase, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	command := purchaseapp.BulkUpdateCommand{ActorID: user.MemberID, Targets: make([]purchaseapp.BulkUpdateTarget, 0, len(input.Targets))}
	for _, target := range input.Targets {
		if target == nil || target.Patch == nil {
			return nil, ErrBadInput
		}
		fields, err := bulkPurchasePatchChangedFieldKeys(target.Patch)
		if err != nil {
			return nil, err
		}
		bulkTarget := purchaseapp.BulkUpdateTarget{
			PurchaseID: target.PurchaseID, BaseRevision: int64(target.BaseRevision), ChangedFields: fields,
		}
		if target.Patch.OccurredAt.IsSet() {
			occurredAt, err := parseTime(*target.Patch.OccurredAt.Value())
			if err != nil {
				return nil, err
			}
			bulkTarget.OccurredAt = &occurredAt
		}
		if target.Patch.Merchant.IsSet() {
			bulkTarget.Merchant = target.Patch.Merchant.Value()
		}
		if target.Patch.PaymentAllocations.IsSet() {
			allocations := make([]purchaseapp.Payment, 0, len(target.Patch.PaymentAllocations.Value()))
			for _, allocation := range target.Patch.PaymentAllocations.Value() {
				if allocation == nil {
					allocations = append(allocations, purchaseapp.Payment{})
					continue
				}
				allocations = append(allocations, purchaseapp.Payment{AccountID: allocation.PaymentAccountID, AmountYen: int64(allocation.AmountYen)})
			}
			bulkTarget.PaymentAllocations = &allocations
		}
		command.Targets = append(command.Targets, bulkTarget)
	}
	result, err := purchaseapp.NewService(r.DB).BulkUpdate(ctx, command)
	if err != nil {
		return nil, err
	}
	purchases := make([]*model.Purchase, 0, len(result.PurchaseIDs))
	for _, purchaseID := range result.PurchaseIDs {
		purchase, err := loadPurchase(ctx, r.DB, purchaseID)
		if err != nil {
			return nil, err
		}
		purchases = append(purchases, purchase)
	}
	return purchases, nil
}
