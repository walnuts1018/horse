import { describe, expect, it } from "vitest";

import { unmatchedObservationPageSize } from "./observation-page-size";

describe("unmatched observation pagination", () => {
  it("queryを表示しない場合もGraphQLが受け付けるページサイズを使う", () => {
    expect(unmatchedObservationPageSize(false)).toBe(1);
  });

  it("ページサイズをGraphQLの許容範囲に収める", () => {
    expect(unmatchedObservationPageSize(true, 0)).toBe(1);
    expect(unmatchedObservationPageSize(true, 101)).toBe(100);
    expect(unmatchedObservationPageSize(true, Number.NaN)).toBe(50);
  });
});
