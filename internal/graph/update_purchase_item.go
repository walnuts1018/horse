package graph

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/walnuts1018/horse/internal/application/purchase"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func (r *mutationResolver) updatePurchaseItem(ctx context.Context, input model.UpdatePurchaseItemInput) (*model.PurchaseItem, error) {
	changedFields, err := purchaseItemChangedFieldKeys(input)
	if err != nil {
		return nil, err
	}
	user, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	command := purchase.UpdateItemCommand{
		ActorID: user.MemberID, ItemID: input.ItemID, BaseRevision: int64(input.BaseRevision), ChangedFields: changedFields,
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
			command.Quantity = &purchase.Measure{Numerator: quantity.Numerator, Denominator: quantity.Denominator, Unit: quantity.Unit}
		}
	}
	if input.PrintedUnitPrice.IsSet() {
		command.PrintedUnitPriceSet = true
		if printedPrice := input.PrintedUnitPrice.Value(); printedPrice != nil {
			if printedPrice.Basis == nil {
				return nil, fmt.Errorf("%w: printed unit price basis is required", ErrBadInput)
			}
			command.PrintedUnitPrice = &purchase.PrintedUnitPrice{
				AmountYen: int64(printedPrice.AmountYen),
				Basis:     purchase.Measure{Numerator: printedPrice.Basis.Numerator, Denominator: printedPrice.Basis.Denominator, Unit: printedPrice.Basis.Unit},
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
			command.ProductLearning = &purchase.ProductLearning{
				ItemOnly: configuration.ItemOnly, VariantName: configuration.VariantName, MerchantAlias: configuration.MerchantAlias,
			}
		}
	}
	if input.Allocations.IsSet() {
		command.AllocationsSet = true
		weights, err := allocationWeights(input.Allocations.Value())
		if err != nil {
			return nil, err
		}
		command.Allocations = make([]purchase.Allocation, 0, len(weights))
		for _, weight := range weights {
			command.Allocations = append(command.Allocations, purchase.Allocation{PoolID: weight.PoolID, Weight: weight.Value})
		}
	}
	result, err := purchase.NewService(r.DB).UpdateItem(ctx, command, purchase.UpdateItemOperations{
		LockAccountingPeriods: lockAccountingPeriods,
		ReplacePayments:       replacePurchasePayments,
		SyncAccounting:        syncPostedPurchaseAccounting,
		UpsertProductAlias:    upsertPurchaseItemProductAlias,
	})
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

func upsertPurchaseItemProductAlias(ctx context.Context, tx pgx.Tx, householdID, variantID, merchantKey, alias, normalizedAlias string) (string, error) {
	var aliasID string
	if err := upsertProductAlias(ctx, tx, householdID, variantID, merchantKey, alias, normalizedAlias, &aliasID); err != nil {
		return "", err
	}
	return aliasID, nil
}
