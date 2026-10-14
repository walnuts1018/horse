import { describe, expect, it } from "vitest";

import { exclusiveMonthEnd } from "./use-analysis-period";

describe("exclusiveMonthEnd", () => {
  it("returns the first instant after the selected month", () => {
    expect(exclusiveMonthEnd("2026-03")).toBe("2026-04-01T00:00:00+09:00");
  });

  it("advances the year when the selected month is December", () => {
    expect(exclusiveMonthEnd("2026-12")).toBe("2027-01-01T00:00:00+09:00");
  });
});
