package graph

import (
	"reflect"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/walnuts1018/horse/internal/graph/model"
)

func omittable[T any](value T) graphql.Omittable[T] {
	return graphql.OmittableOf(value)
}

func TestPurchasePatchChangedFieldKeysUsesPresence(t *testing.T) {
	t.Parallel()
	date := model.DateTime("2026-10-13T12:00:00Z")
	datePrecision := model.PurchaseDatePrecisionDateTime
	merchant := "Store"
	fields, err := purchasePatchChangedFieldKeys(&model.PurchasePatch{
		OccurredAt: omittable(&date), DatePrecision: omittable(&datePrecision), Merchant: omittable(&merchant),
		PaymentAllocations: omittable([]*model.PurchasePaymentInput{}),
	})
	if err != nil || !reflect.DeepEqual(fields, []string{"occurredAt", "merchant", "paymentAllocations"}) {
		t.Fatalf("changed fields = %v, error = %v", fields, err)
	}
	if _, err := purchasePatchChangedFieldKeys(&model.PurchasePatch{Merchant: omittable[*string](nil)}); err == nil {
		t.Fatal("explicit null merchant must be rejected")
	}
	if _, err := purchasePatchChangedFieldKeys(&model.PurchasePatch{DatePrecision: omittable(&datePrecision)}); err == nil {
		t.Fatal("date precision without occurredAt must be rejected")
	}
}

func TestPurchaseItemPatchChangedFieldKeysUsesPresence(t *testing.T) {
	t.Parallel()
	fields, err := purchaseItemChangedFieldKeys(model.UpdatePurchaseItemInput{
		CategoryID: omittable[*string](nil), Quantity: omittable[*model.PurchaseQuantityInput](nil),
		PrintedUnitPrice: omittable[*model.PrintedUnitPriceInput](nil),
	})
	if err != nil || !reflect.DeepEqual(fields, []string{"quantity", "printedUnitPrice", "categoryId"}) {
		t.Fatalf("changed fields = %v, error = %v", fields, err)
	}
	if _, err := purchaseItemChangedFieldKeys(model.UpdatePurchaseItemInput{Name: omittable[*string](nil)}); err == nil {
		t.Fatal("explicit null name must be rejected")
	}
	if _, err := purchaseItemChangedFieldKeys(model.UpdatePurchaseItemInput{Allocations: omittable([]*model.AllocationInput(nil))}); err == nil {
		t.Fatal("explicit null allocations must be rejected")
	}
}

func TestBulkPurchaseItemPatchChangedFieldKeysUsesPresence(t *testing.T) {
	t.Parallel()
	fields, err := bulkPurchaseItemChangedFieldKeys(&model.BulkPurchaseItemPatch{
		CategoryID: omittable[*string](nil), ProductVariantID: omittable[*string](nil),
		Allocations: omittable([]*model.AllocationInput{}), TagIds: omittable([]string{}),
	})
	if err != nil || !reflect.DeepEqual(fields, []string{"categoryId", "productVariantId", "allocations", "tags"}) {
		t.Fatalf("changed fields = %v, error = %v", fields, err)
	}
	if _, err := bulkPurchaseItemChangedFieldKeys(&model.BulkPurchaseItemPatch{TagIds: omittable([]string(nil))}); err == nil {
		t.Fatal("explicit null tagIds must be rejected")
	}
}
