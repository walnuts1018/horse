import { describe, expect, it } from "vitest";

import type { PurchaseDraft } from "./purchase-draft";
import type { LedgerItemDraft } from "./purchase-item-draft";

import { isLedgerPurchaseEditorDirty, ledgerPurchaseEditorSnapshot } from "./purchase-editor-dirty";

const purchaseDraft: PurchaseDraft = {
  merchant: "スーパー",
  occurredAt: "2026-10-14T12:00",
  datePrecision: "DATE_TIME",
  paymentAllocations: [{ key: "payment-1", paymentAccountId: "cash", amountYen: "500" }],
};

const itemDraft: LedgerItemDraft = {
  name: "牛乳",
  amountYen: "500",
  quantity: "1",
  quantityUnit: "本",
  printedUnitPriceYen: "",
  printedBasisQuantity: "",
  printedUnitPriceUnit: "",
  categoryId: "food",
  productVariantId: "",
  allocations: [{ poolId: "shared", weight: 1 }],
  productLearning: "ITEM_ONLY",
  variantName: "",
};

describe("ledger purchase editor dirty state", () => {
  it("compares purchase and item drafts with the values at edit start", () => {
    const initial = ledgerPurchaseEditorSnapshot(purchaseDraft, { item: itemDraft });

    expect(isLedgerPurchaseEditorDirty(initial, purchaseDraft, { item: itemDraft })).toBe(false);
    expect(
      isLedgerPurchaseEditorDirty(
        initial,
        { ...purchaseDraft, merchant: "八百屋" },
        { item: itemDraft },
      ),
    ).toBe(true);
    expect(
      isLedgerPurchaseEditorDirty(initial, purchaseDraft, {
        item: { ...itemDraft, allocations: [{ poolId: "shared", weight: 2 }] },
      }),
    ).toBe(true);
    expect(
      isLedgerPurchaseEditorDirty(initial, purchaseDraft, {
        item: { ...itemDraft, name: "別の商品" },
      }),
    ).toBe(true);
    expect(isLedgerPurchaseEditorDirty(initial, null, { item: itemDraft })).toBe(false);
  });
});
