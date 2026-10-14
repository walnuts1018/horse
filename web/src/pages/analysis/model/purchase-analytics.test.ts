import { describe, expect, it } from "vitest";

import type { Household, Purchase } from "../../../shared/api";

import { purchaseDimensions } from "./purchase-analytics";

describe("purchaseDimensions", () => {
  it("reflects item and purchase-wide corrections in purchase-based totals", () => {
    const purchase: Purchase = {
      id: "purchase-1",
      householdId: "household-1",
      datePrecision: "DATE_TIME",
      origin: "MANUAL",
      hasEvidence: false,
      sourceKinds: [],
      status: "POSTED",
      occurredAt: "2025-04-15T03:00:00.000Z",
      merchant: "店舗",
      revision: 1,
      createdAt: "2025-04-15T03:00:00.000Z",
      updatedAt: "2025-04-15T03:00:00.000Z",
      totalYen: 1000,
      correctionAmountYen: -150,
      correctionCount: 2,
      remainingRefundableAmountYen: 850,
      paymentAllocations: [{ paymentAccountId: "account-1", amountYen: 1000, account: null }],
      items: [
        {
          id: "item-1",
          name: "商品",
          amountYen: 500,
          remainingRefundableAmountYen: 400,
          revision: 1,
          quantityUnit: null,
          productVariantId: null,
          allocations: [{ poolId: "pool-1", poolName: "プール", weight: 1, amountYen: 500 }],
          tags: [],
        },
      ],
    };
    const household: Household = {
      id: "household-1",
      name: "家計",
      revision: 1,
      members: [],
      pools: [],
      accounts: [{ id: "account-1", name: "カード", kind: "CARD" }],
      categories: [],
      defaultAllocation: [],
    };

    const { rows } = purchaseDimensions([purchase], household);

    expect(rows.merchant.get("店舗")?.amount).toBe(850);
    expect(rows.item.get("商品\u0000")?.amount).toBe(400);
    expect(rows.item.get("__correction__")?.amount).toBe(-50);
    expect(rows.pool.get("pool-1")?.amount).toBe(500);
    expect(rows.pool.get("__correction__")?.amount).toBe(-150);
    expect(rows.account.get("account-1")?.amount).toBe(1000);
    expect(rows.account.get("__correction__")?.amount).toBe(-150);
  });
});
