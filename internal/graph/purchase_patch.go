package graph

import (
	"fmt"
	"slices"

	"github.com/walnuts1018/horse/internal/graph/model"
)

func purchasePatchChangedFieldKeys(patch *model.PurchasePatch) ([]string, error) {
	if patch == nil {
		return nil, fmt.Errorf("%w: missing purchase patch", ErrBadInput)
	}
	var fields []string
	if patch.OccurredAt.IsSet() {
		if patch.OccurredAt.Value() == nil {
			return nil, fmt.Errorf("%w: occurredAt cannot be null", ErrBadInput)
		}
		fields = append(fields, fieldOccurredAt)
	}
	if patch.DatePrecision.IsSet() {
		if patch.DatePrecision.Value() == nil {
			return nil, fmt.Errorf("%w: datePrecision cannot be null", ErrBadInput)
		}
		if !patch.OccurredAt.IsSet() {
			return nil, fmt.Errorf("%w: datePrecision requires occurredAt", ErrBadInput)
		}
	}
	if patch.Merchant.IsSet() {
		if patch.Merchant.Value() == nil {
			return nil, fmt.Errorf("%w: merchant cannot be null", ErrBadInput)
		}
		fields = append(fields, fieldMerchant)
	}
	if patch.PaymentAllocations.IsSet() {
		if patch.PaymentAllocations.Value() == nil {
			return nil, fmt.Errorf("%w: paymentAllocations cannot be null", ErrBadInput)
		}
		fields = append(fields, fieldPaymentAllocations)
	}
	if err := validateChangedFields(fields, fieldOccurredAt, fieldMerchant, fieldPaymentAllocations); err != nil {
		return nil, err
	}
	return fields, nil
}
func bulkPurchasePatchChangedFieldKeys(patch *model.BulkPurchasePatch) ([]string, error) {
	if patch == nil {
		return nil, fmt.Errorf("%w: missing purchase patch", ErrBadInput)
	}
	return purchasePatchChangedFieldKeys(&model.PurchasePatch{
		OccurredAt: patch.OccurredAt, DatePrecision: patch.DatePrecision, Merchant: patch.Merchant, PaymentAllocations: patch.PaymentAllocations,
	})
}
func purchaseItemChangedFieldKeys(input model.UpdatePurchaseItemInput) ([]string, error) {
	var fields []string
	if input.Name.IsSet() {
		if input.Name.Value() == nil {
			return nil, fmt.Errorf("%w: name cannot be null", ErrBadInput)
		}
		fields = append(fields, fieldName)
	}
	if input.AmountYen.IsSet() {
		if input.AmountYen.Value() == nil {
			return nil, fmt.Errorf("%w: amountYen cannot be null", ErrBadInput)
		}
		fields = append(fields, fieldAmountYen)
	}
	if input.Quantity.IsSet() {
		fields = append(fields, fieldQuantity)
	}
	if input.PrintedUnitPrice.IsSet() {
		fields = append(fields, fieldPrintedUnitPrice)
	}
	if input.CategoryID.IsSet() {
		fields = append(fields, fieldCategoryID)
	}
	if input.ProductVariantID.IsSet() {
		fields = append(fields, fieldProductVariantID)
	}
	if input.Allocations.IsSet() {
		if input.Allocations.Value() == nil {
			return nil, fmt.Errorf("%w: allocations cannot be null", ErrBadInput)
		}
		fields = append(fields, fieldAllocations)
	}
	if err := validateChangedFields(fields, fieldName, fieldAmountYen, fieldQuantity, fieldPrintedUnitPrice, fieldCategoryID, fieldProductVariantID, fieldAllocations); err != nil {
		return nil, err
	}
	return fields, nil
}
func bulkPurchaseItemChangedFieldKeys(patch *model.BulkPurchaseItemPatch) ([]string, error) {
	if patch == nil {
		return nil, fmt.Errorf("%w: missing purchase item patch", ErrBadInput)
	}
	var fields []string
	if patch.CategoryID.IsSet() {
		fields = append(fields, fieldCategoryID)
	}
	if patch.ProductVariantID.IsSet() {
		fields = append(fields, fieldProductVariantID)
	}
	if patch.Allocations.IsSet() {
		if patch.Allocations.Value() == nil {
			return nil, fmt.Errorf("%w: allocations cannot be null", ErrBadInput)
		}
		fields = append(fields, fieldAllocations)
	}
	if patch.TagIds.IsSet() {
		if patch.TagIds.Value() == nil {
			return nil, fmt.Errorf("%w: tagIds cannot be null", ErrBadInput)
		}
		fields = append(fields, fieldTags)
	}
	if err := validateChangedFields(fields, fieldCategoryID, fieldProductVariantID, fieldAllocations, fieldTags); err != nil {
		return nil, err
	}
	return fields, nil
}
func validateChangedFields(fields []string, allowed ...string) error {
	if len(fields) == 0 {
		return fmt.Errorf("%w: patch must contain a field", ErrBadInput)
	}
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !slices.Contains(allowed, field) {
			return fmt.Errorf("%w: invalid changed field", ErrBadInput)
		}
		if _, exists := seen[field]; exists {
			return fmt.Errorf("%w: duplicate changed field", ErrBadInput)
		}
		seen[field] = struct{}{}
	}
	return nil
}
