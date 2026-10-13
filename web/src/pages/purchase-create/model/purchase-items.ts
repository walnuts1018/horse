import type { CreatePurchaseInput } from "../../../shared/api";
import type { PurchaseDraftItem } from "../../../shared/lib";

import { parseQuantity } from "../../../shared/lib";

export type PurchaseItemsValue = {
  itemName: string;
  itemAmount: string;
  categoryId: string;
  items: PurchaseDraftItem[];
};

export const emptyPurchaseItemsValue = (): PurchaseItemsValue => ({
  itemName: "",
  itemAmount: "",
  categoryId: "",
  items: [],
});

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
