import { describe, expect, it } from "vitest";

import {
  aggregatePurchaseAnalytics,
  itemPriceSeries,
  signedBarScale,
  topPriceItemIDs,
} from "./purchase-analytics";

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
      {
        dimension: "pool",
        id: "pool-1",
        name: "A個人",
        period: "2025-05",
        amountYen: "1000",
        count: 1,
      },
      {
        dimension: "pool",
        id: "pool-1",
        name: "A個人",
        period: "2025-05",
        amountYen: "-250",
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
    expect(result.rows.pool.get("pool-1")?.amount).toBe(750);
    expect(result.monthly.pool).toEqual([{ month: "2025-05", id: "2025-05", amount: 750 }]);
  });
});

describe("itemPriceSeries", () => {
  it("keeps the GraphQL item key separator and unit for price observations", () => {
    const itemId = "variant-id\0個";
    const result = itemPriceSeries(
      [
        {
          itemId,
          occurredAt: "2025-05-01T00:00:00Z",
          amountYen: "100",
          remainingRefundableAmountYen: "100",
          effectiveUnitPriceNumeratorYen: "100",
          effectiveUnitPriceDenominator: "1",
          printedUnitPriceYen: null,
          printedUnitPriceQuantityNumerator: null,
          printedUnitPriceQuantityDenominator: null,
          printedUnitPriceUnit: null,
        },
      ],
      itemId,
      "effective",
    );

    expect(result.series[0]?.label).toBe("実効単価 · 個");
    expect(result.series[0]?.points[0]?.value).toBe("100/1");
  });
});

describe("topPriceItemIDs", () => {
  it("selects five real items without letting the correction row consume a slot", () => {
    const ids = topPriceItemIDs([
      { id: "__correction__", name: "訂正", amount: 1000, count: 0 },
      ...Array.from({ length: 6 }, (_, index) => ({
        id: `item-${index}\0個`,
        name: `item ${index}`,
        amount: 100 - index,
        count: 1,
      })),
    ]);

    expect(ids).toEqual(["item-0\0個", "item-1\0個", "item-2\0個", "item-3\0個", "item-4\0個"]);
  });
});

describe("signedBarScale", () => {
  it("scales positive and negative amounts by their magnitude", () => {
    expect(signedBarScale(25, 100)).toBe(0.25);
    expect(signedBarScale(-75, 100)).toBe(0.75);
    expect(signedBarScale(0, 100)).toBe(0);
  });

  it("returns zero when there is no scale", () => {
    expect(signedBarScale(-10, 0)).toBe(0);
  });
});
