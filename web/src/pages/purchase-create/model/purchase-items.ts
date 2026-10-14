import type { CreatePurchaseInput } from "../../../shared/api";
import type { PurchaseDraft, PurchaseDraftItem } from "../../../shared/lib";

import { parseQuantity } from "../../../shared/lib";

export type PurchaseItemsValue = {
  itemName: string;
  itemAmount: string;
  selectedProductVariantId: string;
  categoryId: string;
  items: PurchaseDraftItem[];
};

export const emptyPurchaseItemsValue = (): PurchaseItemsValue => ({
  itemName: "",
  itemAmount: "",
  selectedProductVariantId: "",
  categoryId: "",
  items: [],
});

export function restorePurchaseItemsValue(
  draft: Pick<
    PurchaseDraft,
    "itemName" | "itemAmount" | "selectedProductVariantId" | "categoryId" | "items"
  >,
  isEnabledCategory: (id: string | null | undefined) => boolean,
): PurchaseItemsValue {
  return {
    itemName: draft.itemName,
    itemAmount: draft.itemAmount,
    selectedProductVariantId: draft.selectedProductVariantId ?? "",
    categoryId: isEnabledCategory(draft.categoryId) ? draft.categoryId : "",
    items: draft.items.map((item) => ({
      ...item,
      categoryId: isEnabledCategory(item.categoryId) ? item.categoryId : "",
    })),
  };
}

export function convertPurchaseItems(items: PurchaseDraftItem[]): CreatePurchaseInput["items"] {
  return items.map((item) => {
    const quantity = parseQuantity(item.quantity ?? "", item.quantityUnit ?? "");
    return {
      name: item.name,
      amountYen: Number(item.amount),
      categoryId: item.categoryId || null,
      allocations: item.allocations.length
        ? item.allocations.filter((allocation) => allocation.weight > 0)
        : null,
      productVariantId: item.productVariantId || null,
      ...(quantity
        ? {
            quantityNumerator: quantity.numerator,
            quantityDenominator: quantity.denominator,
            quantityUnit: quantity.unit,
          }
        : {}),
    };
  });
}
