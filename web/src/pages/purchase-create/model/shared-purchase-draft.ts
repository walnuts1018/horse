import { isRecord, type PurchaseDraft, type PurchaseDraftItem } from "../../../shared/lib";

function isDraftItem(value: unknown): value is PurchaseDraftItem {
  return (
    isRecord(value) &&
    typeof value.key === "string" &&
    typeof value.name === "string" &&
    typeof value.amount === "string" &&
    (value.quantity === undefined || typeof value.quantity === "string") &&
    (value.quantityUnit === undefined || typeof value.quantityUnit === "string") &&
    typeof value.categoryId === "string" &&
    (value.productVariantId === undefined || typeof value.productVariantId === "string") &&
    Array.isArray(value.allocations) &&
    value.allocations.every(
      (allocation) =>
        isRecord(allocation) &&
        typeof allocation.poolId === "string" &&
        typeof allocation.weight === "number",
    )
  );
}

export function decodeSharedPurchaseDraft(payload: string): PurchaseDraft {
  let value: unknown;
  try {
    value = JSON.parse(payload);
  } catch (cause) {
    throw new Error("共有下書きを読み込めません", { cause });
  }
  if (!isSharedPurchaseDraft(value)) {
    throw new Error("共有下書きを読み込めません");
  }
  return value;
}

function isSharedPurchaseDraft(value: unknown): value is PurchaseDraft {
  return (
    isRecord(value) &&
    typeof value.merchant === "string" &&
    typeof value.occurredAt === "string" &&
    typeof value.itemName === "string" &&
    typeof value.itemAmount === "string" &&
    typeof value.categoryId === "string" &&
    Array.isArray(value.items) &&
    value.items.every(isDraftItem) &&
    (value.paymentAccountId === undefined || typeof value.paymentAccountId === "string") &&
    (value.selectedProductVariantId === undefined ||
      typeof value.selectedProductVariantId === "string") &&
    (value.paymentAutoTotal === undefined || typeof value.paymentAutoTotal === "boolean") &&
    (value.paymentAmountManuallyEdited === undefined ||
      typeof value.paymentAmountManuallyEdited === "boolean") &&
    (value.paymentAllocations === undefined ||
      (Array.isArray(value.paymentAllocations) &&
        value.paymentAllocations.every(
          (allocation) =>
            isRecord(allocation) &&
            typeof allocation.paymentAccountId === "string" &&
            typeof allocation.amountYen === "string",
        )))
  );
}

export function encodeSharedPurchaseDraft(draft: PurchaseDraft) {
  return JSON.stringify(draft);
}
