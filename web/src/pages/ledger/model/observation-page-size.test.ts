import { describe, expect, it } from "vitest";

import { unmatchedObservationPageSize } from "./observation-page-size";

describe("unmatched observation pagination", () => {
  it("ページサイズをGraphQLの許容範囲に収める", () => {
    expect(unmatchedObservationPageSize(0)).toBe(1);
    expect(unmatchedObservationPageSize(101)).toBe(100);
    expect(unmatchedObservationPageSize(Number.NaN)).toBe(50);
  });
});
