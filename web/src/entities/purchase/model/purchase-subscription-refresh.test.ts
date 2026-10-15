import { describe, expect, it } from "vitest";

import {
  purchaseChangeAffectsMembership,
  type PurchaseChange,
} from "./purchase-subscription-refresh";

describe("purchaseChangeAffectsMembership", () => {
  it("refetches only when changed fields can affect the active purchase list", () => {
    const updated = (changedFields: string[]) => purchaseChange("UPDATED", changedFields);
    const itemUpdated = (changedFields: string[]) => purchaseChange("ITEM_UPDATED", changedFields);

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

function purchaseChange(kind: PurchaseChange["kind"], changedFields: string[]): PurchaseChange {
  return {
    cursor: "1",
    entityId: "id",
    entityKind: kind === "ITEM_UPDATED" || kind === "ITEM_DELETED" ? "PURCHASE_ITEM" : "PURCHASE",
    kind,
    changedFields: changedFields as PurchaseChange["changedFields"],
    purchase: {} as PurchaseChange["purchase"],
  };
}
