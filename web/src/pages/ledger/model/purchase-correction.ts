import type { PurchaseItem } from "../../../shared/api";

export function getRemainingRefundAmount(item: PurchaseItem): number {
  return item.remainingRefundableAmountYen;
}
