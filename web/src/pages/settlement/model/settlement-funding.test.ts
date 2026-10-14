import { describe, expect, it, vi } from "vitest";

import type { Household } from "../../../shared/api";

vi.mock("../../../shared/api", () => ({
  isSharedPool: (pool: { __typename: string }) =>
    pool.__typename === "SharedPool" || pool.__typename === "UnconfiguredSharedPool",
  makeIdempotencyKey: () => "idempotency-key",
}));
vi.mock("../../../shared/lib", () => ({ errorMessage: String, japanMonth: String }));
vi.mock("../api/settlement-mutations", () => ({ recordSettlementFunding: vi.fn() }));
vi.mock("./pending-ledger-mutations", () => ({
  clearFundingIntent: vi.fn(),
  getFundingAccountDefaults: vi.fn(),
  getFundingIntent: vi.fn(),
  saveFundingAccountDefaults: vi.fn(),
  saveFundingIntent: vi.fn(),
}));
vi.mock("./settlement-view", () => ({ formatSettlementYen: String }));

import { resolveFundingTarget } from "./settlement-funding";

const household = {
  pools: [
    {
      __typename: "SharedPool",
      id: "shared-1",
      name: "共用1",
      defaultShareWeight: 1,
      fundingShares: [],
      settlementPolicy: "BALANCE_TO_STANDARD",
      fundingRuleVersion: 1,
    },
    {
      __typename: "SharedPool",
      id: "shared-2",
      name: "共用2",
      defaultShareWeight: 1,
      fundingShares: [],
      settlementPolicy: "BALANCE_TO_STANDARD",
      fundingRuleVersion: 1,
    },
  ],
  accounts: [
    { id: "account-1", name: "口座1", kind: "BANK", poolId: "shared-1" },
    { id: "account-2", name: "口座2", kind: "BANK", poolId: "shared-2" },
    { id: "personal-account", name: "個人口座", kind: "BANK", poolId: "personal" },
  ],
} satisfies Pick<Household, "pools" | "accounts">;

describe("resolveFundingTarget", () => {
  it("uses the shared pool that owns the selected destination account", () => {
    expect(resolveFundingTarget(household, "account-2")).toEqual({
      poolId: "shared-2",
      destinationAccountId: "account-2",
    });
  });

  it("uses the first shared pool when no destination account is selected", () => {
    expect(resolveFundingTarget(household, "")).toEqual({
      poolId: "shared-1",
      destinationAccountId: undefined,
    });
  });

  it("rejects stale and non-shared destination accounts", () => {
    expect(() => resolveFundingTarget(household, "deleted-account")).toThrow();
    expect(() => resolveFundingTarget(household, "personal-account")).toThrow();
  });
});
