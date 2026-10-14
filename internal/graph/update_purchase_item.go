package graph

import (
	"context"

	"github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) updatePurchaseItem(ctx context.Context, input model.UpdatePurchaseItemInput) (*model.PurchaseItem, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	command, err := purchaseItemUpdateCommand(input, user.MemberID)
	if err != nil {
		return nil, err
	}
	result, err := purchase.NewService(r.DB).UpdateItem(ctx, command)
	if err != nil {
		return nil, err
	}
	loaded, err := loadPurchase(ctx, r.DB, result.PurchaseID)
	if err != nil {
		return nil, err
	}
	for _, item := range loaded.Items {
		if item.ID == input.ItemID {
			return item, nil
		}
	}
	return nil, ErrNotFound
}
