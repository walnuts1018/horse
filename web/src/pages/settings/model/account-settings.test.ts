import { describe, expect, it, vi } from "vitest";

import type { Household, Pool, PoolFundingShare } from "../../../shared/api";

import { reconcileFundingDrafts } from "./account-settings";

vi.mock("../../../shared/api", () => ({
  isPersonalPool: (pool: { __typename: string }) =>
    pool.__typename === "PersonalPool" || pool.__typename === "UnconfiguredPersonalPool",
  isSharedPool: (pool: { __typename: string }) =>
    pool.__typename === "SharedPool" || pool.__typename === "UnconfiguredSharedPool",
}));

vi.mock("../api/account-settings", () => ({
  createAccount: vi.fn(),
  createPool: vi.fn(),
  setPoolFundingRule: vi.fn(),
}));

function sharedPool(id: string, fundingShares: PoolFundingShare[]): Pool {
  return {
    __typename: "SharedPool",
    id,
    name: id,
    defaultShareWeight: 1,
    fundingShares,
    settlementPolicy: "BALANCE_TO_STANDARD",
    fundingRuleVersion: 1,
  };
}

function household(id: string, members: string[], pools: Pool[]): Household {
  return {
    id,
    name: id,
    revision: 1,
    members: members.map((memberId) => ({ id: memberId, displayName: memberId, role: "MEMBER" })),
    pools,
    accounts: [],
    categories: [],
    defaultAllocation: [],
  };
}

describe("reconcileFundingDrafts", () => {
  it("preserves edited values on refresh and initializes newly added members", () => {
    const initial = household(
      "household-a",
      ["member-a"],
      [sharedPool("pool-a", [{ memberId: "member-a", memberName: "A", weight: 2 }])],
    );
    const drafts = reconcileFundingDrafts(initial, initial.id, {});
    drafts["pool-a"].weights["member-a"] = "7";
    drafts["pool-a"].policy = "REIMBURSE_PERSONAL";

    const refreshed = household(
      "household-a",
      ["member-a", "member-b"],
      [
        sharedPool("pool-a", [
          { memberId: "member-a", memberName: "A", weight: 2 },
          { memberId: "member-b", memberName: "B", weight: 3 },
        ]),
      ],
    );

    expect(reconcileFundingDrafts(refreshed, initial.id, drafts)).toEqual({
      "pool-a": {
        ownerMemberId: "",
        policy: "REIMBURSE_PERSONAL",
        weights: { "member-a": "7", "member-b": "3" },
      },
    });
  });

  it("resets drafts when switching households", () => {
    const firstHousehold = household(
      "household-a",
      ["member-a"],
      [sharedPool("pool-a", [{ memberId: "member-a", memberName: "A", weight: 2 }])],
    );
    const drafts = reconcileFundingDrafts(firstHousehold, firstHousehold.id, {});
    drafts["pool-a"].weights["member-a"] = "7";

    const nextHousehold = household(
      "household-b",
      ["member-b"],
      [sharedPool("pool-b", [{ memberId: "member-b", memberName: "B", weight: 4 }])],
    );

    expect(reconcileFundingDrafts(nextHousehold, firstHousehold.id, drafts)).toEqual({
      "pool-b": {
        ownerMemberId: "",
        policy: "BALANCE_TO_STANDARD",
        weights: { "member-b": "4" },
      },
    });
  });
});
