import { describe, expect, it, vi } from "vitest";

import type { Household } from "../../../shared/api";
import type { CatalogAllocationRule } from "../api/allocation-catalog";

import { createAllocationRuleDraft } from "./allocation-catalog";

vi.mock("../api/allocation-catalog", () => ({
  createCatalogAlias: vi.fn(),
  createCatalogConcept: vi.fn(),
  createCatalogVariant: vi.fn(),
  readAllocationRules: vi.fn(),
  readCatalog: vi.fn(),
  setAllocationRule: vi.fn(),
}));

describe("createAllocationRuleDraft", () => {
  it("preserves an existing rule target and weights for editing", () => {
    const rule: CatalogAllocationRule = {
      id: "rule-1",
      householdId: "household-1",
      target: { __typename: "Category", id: "category-1", name: "食費" },
      revision: 2,
      allocations: [{ poolId: "pool-1", poolName: "共有", weight: 3 }],
    };
    const pools: Household["pools"] = ["pool-1", "pool-2"].map((id) => ({
      __typename: "SharedPool",
      id,
      name: id,
      defaultShareWeight: 1,
      fundingShares: [],
      settlementPolicy: "BALANCE_TO_STANDARD",
      fundingRuleVersion: 1,
    }));

    expect(createAllocationRuleDraft(rule, pools)).toEqual({
      scope: "CATEGORY",
      categoryId: "category-1",
      merchant: "",
      productId: "",
      variantId: "",
      weights: { "pool-1": "3", "pool-2": "0" },
    });
  });
});
