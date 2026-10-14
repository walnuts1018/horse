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

export function receiptItemsNeedingReview(
  items: ExtractedItem[],
  states: Record<string, ReceiptItemReviewState> | undefined,
) {
  return ensureReceiptItemKeys(items).flatMap((item, index) => {
    const state = states?.[item.key ?? ""] ?? "UNREVIEWED";
    return state === "UNREVIEWED" || state === "UNRESOLVED" ? [{ item, index, state }] : [];
  });
}
