package graph

import (
	"context"
	"fmt"

	purchaseapp "github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) savePurchaseItemEdits(ctx context.Context, input model.SavePurchaseItemEditsInput) ([]*model.PurchaseItem, error) {
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	command := purchaseapp.SaveItemEditsCommand{
		ActorID: user.MemberID, PurchaseID: input.PurchaseID,
		Items: make([]purchaseapp.UpdateItemCommand, 0, len(input.Items)),
	}
	for _, item := range input.Items {
		if item == nil {
			return nil, fmt.Errorf("%w: item patch is required", ErrBadInput)
		}
		itemCommand, err := purchaseItemUpdateCommand(*item, user.MemberID)
		if err != nil {
			return nil, err
		}
		command.Items = append(command.Items, itemCommand)
	}
	if input.PurchasePatch.IsSet() {
		patch := input.PurchasePatch.Value()
		if patch == nil {
			return nil, fmt.Errorf("%w: purchase patch cannot be null", ErrBadInput)
		}
		updateInput := model.UpdatePurchaseInput{
			PurchaseID: input.PurchaseID, BaseRevision: input.PurchaseBaseRevision, Patch: patch,
		}
		changedFields, err := purchasePatchChangedFieldKeys(patch)
		if err != nil {
			return nil, err
		}
		purchaseUpdate, err := updatePurchaseCommand(updateInput, user.MemberID, changedFields)
		if err != nil {
			return nil, err
		}
		command.PurchaseUpdate = &purchaseUpdate
	}
	if _, err := purchaseapp.NewService(r.DB).SaveItemEdits(ctx, command); err != nil {
		return nil, err
	}
	loaded, err := loadPurchase(ctx, r.DB, input.PurchaseID)
	if err != nil {
		return nil, err
	}
	itemsByID := make(map[string]*model.PurchaseItem, len(loaded.Items))
	for _, item := range loaded.Items {
		itemsByID[item.ID] = item
	}
	items := make([]*model.PurchaseItem, 0, len(input.Items))
	for _, inputItem := range input.Items {
		item := itemsByID[inputItem.ItemID]
		if item == nil {
			return nil, ErrNotFound
		}
		items = append(items, item)
	}
	return items, nil
}

func purchaseItemUpdateCommand(input model.UpdatePurchaseItemInput, actorID string) (purchaseapp.UpdateItemCommand, error) {
	changedFields, err := purchaseItemChangedFieldKeys(input)
	if err != nil {
		return purchaseapp.UpdateItemCommand{}, err
	}
	command := purchaseapp.UpdateItemCommand{
		ActorID: actorID, ItemID: input.ItemID, BaseRevision: int64(input.BaseRevision), ChangedFields: changedFields,
	}
	if input.Name.IsSet() {
		command.Name = input.Name.Value()
	}
	if input.AmountYen.IsSet() && input.AmountYen.Value() != nil {
		amount := int64(*input.AmountYen.Value())
		command.AmountYen = &amount
	}
	if input.Quantity.IsSet() {
		command.QuantitySet = true
		if quantity := input.Quantity.Value(); quantity != nil {
			command.Quantity = &purchaseapp.Measure{Numerator: quantity.Numerator, Denominator: quantity.Denominator, Unit: quantity.Unit}
		}
	}
	if input.PrintedUnitPrice.IsSet() {
		command.PrintedUnitPriceSet = true
		if printedPrice := input.PrintedUnitPrice.Value(); printedPrice != nil {
			if printedPrice.Basis == nil {
				return purchaseapp.UpdateItemCommand{}, fmt.Errorf("%w: printed unit price basis is required", ErrBadInput)
			}
			command.PrintedUnitPrice = &purchaseapp.PrintedUnitPrice{
				AmountYen: int64(printedPrice.AmountYen),
				Basis:     purchaseapp.Measure{Numerator: printedPrice.Basis.Numerator, Denominator: printedPrice.Basis.Denominator, Unit: printedPrice.Basis.Unit},
			}
		}
	}
	if input.CategoryID.IsSet() {
		command.CategoryIDSet = true
		command.CategoryID = input.CategoryID.Value()
	}
	if input.ProductVariantID.IsSet() {
		command.ProductVariantIDSet = true
		command.ProductVariantID = input.ProductVariantID.Value()
	}
	if input.ProductLearning.IsSet() {
		command.ProductLearningSet = true
		configuration := input.ProductLearning.Value()
		if configuration != nil {
			command.ProductLearning = &purchaseapp.ProductLearning{
				ItemOnly: configuration.ItemOnly, VariantName: configuration.VariantName, MerchantAlias: configuration.MerchantAlias,
			}
		}
	}
	if input.Allocations.IsSet() {
		command.AllocationsSet = true
		weights, err := allocationWeights(input.Allocations.Value())
		if err != nil {
			return purchaseapp.UpdateItemCommand{}, err
		}
		command.Allocations = make([]purchaseapp.Allocation, 0, len(weights))
		for _, weight := range weights {
			command.Allocations = append(command.Allocations, purchaseapp.Allocation{PoolID: weight.PoolID, Weight: weight.Value})
		}
	}
	return command, nil
}
