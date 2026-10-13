import type { Purchase, PurchaseItem } from "../../../shared/api";

export function getRemainingRefundAmount(purchase: Purchase, item: PurchaseItem): number {
  const previousCorrections = (purchase.corrections ?? [])
    .filter(
      (correction) =>
        correction.__typename === "PurchaseItemCorrection" && correction.itemId === item.id,
    )
    .reduce((sum, correction) => sum + correction.amountDeltaYen, 0);
  return item.amountYen + previousCorrections;
}
