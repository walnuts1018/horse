import { describe, expect, it } from "vitest";

import { settlementFundingDue } from "./settlement-view";

describe("settlementFundingDue", () => {
  it("sums required deposits without canceling credits", () => {
    expect(
      settlementFundingDue([{ amountDueYen: 5_000 }, { amountDueYen: -5_000 }]),
    ).toBe(5_000);
  });

  it("returns zero when all balances are credits or settled", () => {
    expect(settlementFundingDue([{ amountDueYen: -500 }, { amountDueYen: 0 }])).toBe(0);
  });
});
