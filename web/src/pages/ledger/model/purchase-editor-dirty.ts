import type { PurchaseDraft } from "./purchase-draft";
import type { LedgerItemDraft } from "./purchase-item-draft";

export function ledgerPurchaseEditorSnapshot(
  purchaseDraft: PurchaseDraft,
  itemDrafts: Record<string, LedgerItemDraft | undefined>,
) {
  return JSON.stringify([purchaseDraft, itemDrafts]);
}

export function isLedgerPurchaseEditorDirty(
  initialSnapshot: string | null,
  purchaseDraft: PurchaseDraft | null,
  itemDrafts: Record<string, LedgerItemDraft | undefined>,
) {
  return Boolean(
    initialSnapshot &&
    purchaseDraft &&
    ledgerPurchaseEditorSnapshot(purchaseDraft, itemDrafts) !== initialSnapshot,
  );
}
