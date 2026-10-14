package graph

import (
	"context"
	"fmt"

	purchaseapp "github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) bulkDeletePurchaseItems(ctx context.Context, input model.BulkDeletePurchaseItemsInput) ([]string, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if len(input.Targets) == 0 || len(input.Targets) > 100 {
		return nil, fmt.Errorf("%w: select between one and one hundred items", ErrBadInput)
	}
	command := purchaseapp.BulkDeleteItemsCommand{
		ActorID: user.MemberID,
		Targets: make([]purchaseapp.BulkItemTarget, 0, len(input.Targets)),
	}
	for _, target := range input.Targets {
		if target == nil {
			return nil, fmt.Errorf("%w: missing item target", ErrBadInput)
		}
		command.Targets = append(command.Targets, purchaseapp.BulkItemTarget{ItemID: target.ItemID, BaseRevision: target.BaseRevision})
	}
	result, err := purchaseapp.NewService(r.DB).BulkDeleteItems(ctx, command)
	if err != nil {
		return nil, err
	}
	return result.DeletedItemIDs, nil
}
