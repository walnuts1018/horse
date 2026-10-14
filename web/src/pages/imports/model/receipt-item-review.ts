import type { ImportReceiptItemReviewState } from "../../../shared/lib/purchase-storage";
import type { ExtractedItem } from "./imports";

import { ensureReceiptItemKeys } from "./receipt-review-draft";

export const receiptItemReviewStates = [
  "UNREVIEWED",
  "CONFIRMED",
  "CORRECTED",
  "UNRESOLVED",
] as const satisfies readonly ImportReceiptItemReviewState[];

export type ReceiptItemReviewState = ImportReceiptItemReviewState;

export const receiptItemReviewStateLabels: Record<ReceiptItemReviewState, string> = {
  UNREVIEWED: "未確認",
  CONFIRMED: "確認済み",
  CORRECTED: "修正済み",
  UNRESOLVED: "未解決",
};

export function receiptItemHasInvalidInput(item: ExtractedItem) {
  return (
    !item.name.trim() ||
    !Number.isSafeInteger(item.grossAmount) ||
    item.grossAmount < (item.verification === "MANUAL" ? 1 : 0)
  );
}

export function receiptItemNeedsAttention(item: ExtractedItem) {
  return (
    item.verification === "UNRESOLVED" ||
    (item.verification !== "MANUAL" && (item.confidence < 0.75 || !item.region)) ||
    receiptItemHasInvalidInput(item)
  );
}

export function receiptItemsNeedingReview(
  items: ExtractedItem[],
  states: Record<string, ReceiptItemReviewState> | undefined,
) {
  return ensureReceiptItemKeys(items).flatMap((item, index) => {
    const state = states?.[item.key ?? ""] ?? "UNREVIEWED";
    const needsAttention =
      state === "UNRESOLVED" ||
      receiptItemHasInvalidInput(item) ||
      (state !== "CONFIRMED" && state !== "CORRECTED" && receiptItemNeedsAttention(item));
    return needsAttention ? [{ item, index, state }] : [];
  });
}
