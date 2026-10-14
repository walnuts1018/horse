import { describe, expect, it } from "vitest";

import { pendingPurchaseEntryAmount } from "./purchase-entry-total";

describe("pendingPurchaseEntryAmount", () => {
  it("includes only a named positive integer entry", () => {
    expect(pendingPurchaseEntryAmount(" ヨーグルト ", "180")).toBe(180);
    expect(pendingPurchaseEntryAmount("", "180")).toBe(0);
    expect(pendingPurchaseEntryAmount("ヨーグルト", "0")).toBe(0);
    expect(pendingPurchaseEntryAmount("ヨーグルト", "18円")).toBe(0);
  });
});
