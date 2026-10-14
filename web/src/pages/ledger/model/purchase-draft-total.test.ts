import { describe, expect, it } from "vitest";

import { purchaseDraftTotalYen } from "./purchase-draft-total";

const purchase = {
  items: [
    { id: "item-1", amountYen: 100 },
    { id: "item-2", amountYen: 300 },
  ],
};

describe("purchaseDraftTotalYen", () => {
  it("uses edited amounts while retaining saved amounts for unchanged items", () => {
    expect(purchaseDraftTotalYen(purchase, { "item-1": { amountYen: "120" } })).toBe(420);
  });
});
