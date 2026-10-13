import type { PurchaseDraftItem } from "../../../shared/lib";

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
