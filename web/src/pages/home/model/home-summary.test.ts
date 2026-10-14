import { describe, expect, it } from "vitest";

import { summarizeSettlementDues } from "./settlement-dues";

describe("summarizeSettlementDues", () => {
  it("入金必要額と過払い控除を相殺せずに集計する", () => {
    expect(summarizeSettlementDues([{ amountDueYen: 5000 }, { amountDueYen: -5000 }])).toEqual({
      fundingDueYen: 5000,
      creditYen: 5000,
    });
  });
});
