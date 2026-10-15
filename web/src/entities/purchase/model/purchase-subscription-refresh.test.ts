import { describe, expect, it } from "vitest";

import {
  purchaseChangeAffectsMembership,
  type PurchaseChange,
} from "./purchase-subscription-refresh";

const updated = (changedFields: PurchaseChange["changedFields"]) =>
  purchaseChange("UPDATED", changedFields);
const itemUpdated = (changedFields: PurchaseChange["changedFields"]) =>
  purchaseChange("ITEM_UPDATED", changedFields);

describe("purchaseChangeAffectsMembership", () => {
  it("refetches only when changed fields can affect the active purchase list", () => {
    expect(purchaseChangeAffectsMembership(updated(["STATUS"]), {})).toBe(false);
    expect(purchaseChangeAffectsMembership(updated(["OCCURRED_AT"]), {})).toBe(true);
    expect(purchaseChangeAffectsMembership(updated(["MERCHANT"]), {})).toBe(false);
    expect(purchaseChangeAffectsMembership(updated(["MERCHANT"]), { search: "coffee" })).toBe(true);
    expect(purchaseChangeAffectsMembership(itemUpdated(["NAME"]), { search: "coffee" })).toBe(true);
    expect(purchaseChangeAffectsMembership(itemUpdated(["QUANTITY"]), { search: "coffee" })).toBe(
      false,
    );
    expect(
      purchaseChangeAffectsMembership(itemUpdated(["CATEGORY_ID"]), { categoryId: "cat" }),
    ).toBe(true);
    expect(
      purchaseChangeAffectsMembership(itemUpdated(["AMOUNT_YEN"]), { minimumAmount: 100 }),
    ).toBe(true);
  });

  it("refetches for new purchases and item deletion under item filters", () => {
    expect(purchaseChangeAffectsMembership(purchaseChange("CREATED", []), {})).toBe(true);
    expect(purchaseChangeAffectsMembership(purchaseChange("ITEM_DELETED", ["DELETED"]), {})).toBe(
      false,
    );
    expect(
      purchaseChangeAffectsMembership(purchaseChange("ITEM_DELETED", ["DELETED"]), {
        tagId: "tag",
      }),
    ).toBe(true);
  });

  it("does not refetch amount-filtered lists when a purchase correction changes only refund totals", () => {
    const corrected = purchaseChange("CORRECTED", ["CORRECTION"]);

    expect(purchaseChangeAffectsMembership(corrected, { minimumAmount: 100 })).toBe(false);
    expect(purchaseChangeAffectsMembership(corrected, { maximumAmount: 500 })).toBe(false);
  });
});

function purchaseChange(
  kind: PurchaseChange["kind"],
  changedFields: PurchaseChange["changedFields"],
): PurchaseChange {
  return {
    cursor: "1",
    entityId: "id",
    entityKind: kind === "ITEM_UPDATED" || kind === "ITEM_DELETED" ? "PURCHASE_ITEM" : "PURCHASE",
    kind,
    changedFields,
    purchase: {
      correctionAmountYen: 0,
      correctionCount: 0,
      createdAt: "2025-01-01T00:00:00Z",
      datePrecision: "DATE_TIME",
      hasEvidence: false,
      householdId: "household-id",
      id: "purchase-id",
      items: [],
      merchant: "",
      occurredAt: "2025-01-01T00:00:00Z",
      origin: "MANUAL",
      paymentAllocations: [],
      remainingRefundableAmountYen: 0,
      revision: 1,
      sourceKinds: [],
      status: "POSTED",
      totalYen: 0,
      updatedAt: "2025-01-01T00:00:00Z",
    },
  };
}
