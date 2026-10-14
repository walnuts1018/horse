import { describe, expect, it } from "vitest";

import { purchaseDateInputValue, purchaseOccurredAtLabel } from "./purchase-date";

describe("purchase date precision", () => {
  it("does not display a representative timestamp as the purchase time", () => {
    expect(purchaseOccurredAtLabel("2026-10-14T03:00:00.000Z", "DATE_ONLY")).toBe("10/14");
    expect(purchaseDateInputValue("2026-10-14T03:00:00.000Z")).toBe("2026-10-14");
  });

  it("displays the time when the purchase timestamp is precise", () => {
    expect(purchaseOccurredAtLabel("2026-10-14T03:00:00.000Z", "DATE_TIME")).toContain("12:00");
  });
});
