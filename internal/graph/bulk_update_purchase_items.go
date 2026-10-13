package graph

import (
	"context"
	"fmt"

	purchaseapp "github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/domain"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) bulkUpdatePurchaseItems(ctx context.Context, input model.BulkUpdatePurchaseItemsInput) ([]*model.PurchaseItem, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if len(input.Targets) == 0 || len(input.Targets) > 100 || input.Patch == nil {
		return nil, fmt.Errorf("%w: select between one and one hundred items", ErrBadInput)
	}
	if _, err := bulkPurchaseItemChangedFieldKeys(input.Patch); err != nil {
		return nil, err
	}
	command := purchaseapp.BulkUpdateItemsCommand{
		ActorID: user.MemberID,
		Targets: make([]purchaseapp.BulkItemTarget, 0, len(input.Targets)),
		Patch: purchaseapp.BulkItemPatch{
			CategoryIDSet:       input.Patch.CategoryID.IsSet(),
			CategoryID:          input.Patch.CategoryID.Value(),
			ProductVariantIDSet: input.Patch.ProductVariantID.IsSet(),
			ProductVariantID:    input.Patch.ProductVariantID.Value(),
			AllocationsSet:      input.Patch.Allocations.IsSet(),
			TagIDsSet:           input.Patch.TagIds.IsSet(),
			TagIDs:              input.Patch.TagIds.Value(),
		},
	}
	for _, target := range input.Targets {
		if target == nil {
			return nil, fmt.Errorf("%w: missing item target", ErrBadInput)
		}
		command.Targets = append(command.Targets, purchaseapp.BulkItemTarget{ItemID: target.ItemID, BaseRevision: target.BaseRevision})
	}
	if command.Patch.AllocationsSet {
		weights, err := allocationWeights(input.Patch.Allocations.Value())
		if err != nil {
			return nil, err
		}
		for _, weight := range weights {
			command.Patch.Allocations = append(command.Patch.Allocations, domain.Weight{PoolID: weight.PoolID, Value: weight.Value})
		}
	}
	result, err := purchaseapp.NewService(r.DB).BulkUpdateItems(ctx, command)
	if err != nil {
		return nil, err
	}
	items := make([]*model.PurchaseItem, 0, len(result.Items))
	purchases := make(map[string]*model.Purchase)
	for _, updated := range result.Items {
		purchase, ok := purchases[updated.PurchaseID]
		if !ok {
			purchase, err = loadPurchase(ctx, r.DB, updated.PurchaseID)
			if err != nil {
				return nil, err
			}
			purchases[updated.PurchaseID] = purchase
		}
		for _, item := range purchase.Items {
			if item.ID == updated.ID {
				items = append(items, item)
				break
			}
		}
	}
	return items, nil
}
