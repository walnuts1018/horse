package graph

import (
	"encoding/json/v2"
	"fmt"
	"slices"

	"github.com/walnuts1018/horse/internal/graph/model"
)

func setFieldRevisions(current []byte, fields []string, revision int64) ([]byte, error) {
	var revisions map[string]int64
	if len(current) == 0 {
		revisions = make(map[string]int64)
	} else if err := json.Unmarshal(current, &revisions); err != nil {
		return nil, err
	}
	for _, field := range fields {
		revisions[field] = revision
	}
	return json.Marshal(revisions)
}
func purchasePatchChangedFieldKeys(patch *model.PurchasePatch) ([]string, error) {
	if patch == nil {
		return nil, fmt.Errorf("%w: missing purchase patch", ErrBadInput)
	}
	var fields []string
	if patch.OccurredAt.IsSet() {
		if patch.OccurredAt.Value() == nil {
			return nil, fmt.Errorf("%w: occurredAt cannot be null", ErrBadInput)
		}
		fields = append(fields, "occurredAt")
	}
	if patch.Merchant.IsSet() {
		if patch.Merchant.Value() == nil {
			return nil, fmt.Errorf("%w: merchant cannot be null", ErrBadInput)
		}
		fields = append(fields, "merchant")
	}
	if patch.PaymentAllocations.IsSet() {
		if patch.PaymentAllocations.Value() == nil {
			return nil, fmt.Errorf("%w: paymentAllocations cannot be null", ErrBadInput)
		}
		fields = append(fields, "paymentAllocations")
	}
	if err := validateChangedFields(fields, "occurredAt", "merchant", "paymentAllocations"); err != nil {
		return nil, err
	}
	return fields, nil
}
func bulkPurchasePatchChangedFieldKeys(patch *model.BulkPurchasePatch) ([]string, error) {
	if patch == nil {
		return nil, fmt.Errorf("%w: missing purchase patch", ErrBadInput)
	}
	return purchasePatchChangedFieldKeys(&model.PurchasePatch{
		OccurredAt: patch.OccurredAt, Merchant: patch.Merchant, PaymentAllocations: patch.PaymentAllocations,
	})
}
func purchaseItemChangedFieldKeys(input model.UpdatePurchaseItemInput) ([]string, error) {
	var fields []string
	if input.Name.IsSet() {
		if input.Name.Value() == nil {
			return nil, fmt.Errorf("%w: name cannot be null", ErrBadInput)
		}
		fields = append(fields, "name")
	}
	if input.AmountYen.IsSet() {
		if input.AmountYen.Value() == nil {
			return nil, fmt.Errorf("%w: amountYen cannot be null", ErrBadInput)
		}
		fields = append(fields, "amountYen")
	}
	if input.Quantity.IsSet() {
		fields = append(fields, "quantity")
	}
	if input.PrintedUnitPrice.IsSet() {
		fields = append(fields, "printedUnitPrice")
	}
	if input.CategoryID.IsSet() {
		fields = append(fields, "categoryId")
	}
	if input.ProductVariantID.IsSet() {
		fields = append(fields, "productVariantId")
	}
	if input.Allocations.IsSet() {
		if input.Allocations.Value() == nil {
			return nil, fmt.Errorf("%w: allocations cannot be null", ErrBadInput)
		}
		fields = append(fields, "allocations")
	}
	if err := validateChangedFields(fields, "name", "amountYen", "quantity", "printedUnitPrice", "categoryId", "productVariantId", "allocations"); err != nil {
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
		fields = append(fields, "categoryId")
	}
	if patch.ProductVariantID.IsSet() {
		fields = append(fields, "productVariantId")
	}
	if patch.Allocations.IsSet() {
		if patch.Allocations.Value() == nil {
			return nil, fmt.Errorf("%w: allocations cannot be null", ErrBadInput)
		}
		fields = append(fields, "allocations")
	}
	if patch.TagIds.IsSet() {
		if patch.TagIds.Value() == nil {
			return nil, fmt.Errorf("%w: tagIds cannot be null", ErrBadInput)
		}
		fields = append(fields, "tags")
	}
	if err := validateChangedFields(fields, "categoryId", "productVariantId", "allocations", "tags"); err != nil {
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
func valuesConflict(fieldRevisions []byte, base int, fields []string) (bool, error) {
	if base < 1 {
		return true, nil
	}
	var revisions map[string]int64
	if len(fieldRevisions) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(fieldRevisions, &revisions); err != nil {
		return false, err
	}
	for _, field := range fields {
		if revisions[field] > int64(base) {
			return true, nil
		}
	}
	return false, nil
}
