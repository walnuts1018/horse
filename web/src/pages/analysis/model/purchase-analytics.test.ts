import { describe, expect, it } from "vitest";

import { aggregatePurchaseAnalytics } from "./purchase-analytics";

describe("aggregatePurchaseAnalytics", () => {
  it("combines monthly server totals by dimension and keeps the latest label", () => {
    const result = aggregatePurchaseAnalytics([
      {
        dimension: "merchant",
        id: "store-1",
        name: "旧店舗名",
        period: "2025-04",
        amountYen: "1000",
        count: 2,
      },
      {
        dimension: "merchant",
        id: "store-1",
        name: "店舗名",
        period: "2025-05",
        amountYen: "1500",
        count: 3,
      },
      {
        dimension: "item",
        id: "variant\0個",
        name: "商品 · 個",
        period: "2025-05",
        amountYen: "400",
        count: 1,
      },
    ]);

    expect(result.rows.merchant.get("store-1")).toEqual({
      id: "store-1",
      name: "店舗名",
      amount: 2500,
      count: 5,
    });
    expect(result.monthly.merchant).toEqual([
      { month: "2025-04", id: "2025-04", amount: 1000 },
      { month: "2025-05", id: "2025-05", amount: 1500 },
    ]);
    expect(result.rows.item.get("variant\0個")?.amount).toBe(400);
  });
});
