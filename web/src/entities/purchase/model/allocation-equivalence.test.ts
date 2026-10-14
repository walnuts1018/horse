import { describe, expect, it } from "vitest";

import { areAllocationsEquivalent } from "./allocation-equivalence";

describe("areAllocationsEquivalent", () => {
  it("compares the same pool ratio regardless of order or scale", () => {
    expect(
      areAllocationsEquivalent(
        [
          { poolId: "a", weight: 3 },
          { poolId: "b", weight: 2 },
        ],
        [
          { poolId: "b", weight: 4 },
          { poolId: "a", weight: 6 },
        ],
      ),
    ).toBe(true);
  });

  it("keeps different pool ratios distinct", () => {
    expect(
      areAllocationsEquivalent(
        [
          { poolId: "a", weight: 3 },
          { poolId: "b", weight: 2 },
        ],
        [
          { poolId: "a", weight: 2 },
          { poolId: "b", weight: 3 },
        ],
      ),
    ).toBe(false);
  });

  it("ignores zero and invalid weights and combines duplicate pool entries", () => {
    expect(
      areAllocationsEquivalent(
        [
          { poolId: "a", weight: 2 },
          { poolId: "a", weight: 1 },
          { poolId: "b", weight: 2 },
        ],
        [
          { poolId: "b", weight: 4 },
          { poolId: "a", weight: 6 },
          { poolId: "unused", weight: 0 },
        ],
      ),
    ).toBe(true);
  });
});
