import type { Household, Purchase } from "../../../shared/api";
import { japanMonth } from "../../../shared/lib";

export type Dimension = "category" | "merchant" | "item" | "pool" | "account" | "member";
export type PurchaseDimension = Exclude<Dimension, "category" | "member">;
export type ItemMetric = "amount" | "effective" | "printed";
export type AnalyticsRow = { id: string; name: string; amount: number; count: number };
export type MonthlyEntry = { month: string; id: string; amount: number; occurredAt: string };

const itemAnalysisId = (item: Purchase["items"][number]) =>
  `${item.productVariantId ?? item.name}\u0000${item.quantityUnit ?? ""}`;

export function purchaseDimensions(purchases: Purchase[], household: Household) {
  const rows: Record<PurchaseDimension, Map<string, AnalyticsRow>> = {
    merchant: new Map(),
    item: new Map(),
    pool: new Map(),
    account: new Map(),
  };
  const monthly: Record<PurchaseDimension, MonthlyEntry[]> = {
    merchant: [],
    item: [],
    pool: [],
    account: [],
  };
  const add = (
    dimension: PurchaseDimension,
    id: string,
    name: string,
    amount: number,
    month: string,
    occurredAt: string,
  ) => {
    const row = rows[dimension].get(id) ?? { id, name, amount: 0, count: 0 };
    row.amount += amount;
    row.count += 1;
    rows[dimension].set(id, row);
    monthly[dimension].push({ month, id, amount, occurredAt });
  };
  for (const purchase of purchases) {
    if (purchase.status !== "POSTED" && purchase.status !== "CLOSED") continue;
    const month = japanMonth(new Date(purchase.occurredAt));
    add(
      "merchant",
      purchase.merchant || "__unknown",
      purchase.merchant || "店舗未入力",
      purchase.totalYen,
      month,
      purchase.occurredAt,
    );
    for (const item of purchase.items) {
      const itemId = itemAnalysisId(item);
      add(
        "item",
        itemId,
        `${item.name}${item.quantityUnit ? ` · ${item.quantityUnit}` : " · 単位不明"}`,
        item.amountYen,
        month,
        purchase.occurredAt,
      );
      for (const allocation of item.allocations) {
        add(
          "pool",
          allocation.poolId,
          allocation.poolName,
          allocation.amountYen,
          month,
          purchase.occurredAt,
        );
      }
    }
    for (const payment of purchase.paymentAllocations) {
      const account = household.accounts.find(
        (candidate) => candidate.id === payment.paymentAccountId,
      );
      add(
        "account",
        payment.paymentAccountId,
        account?.name ?? "決済元",
        payment.amountYen,
        month,
        purchase.occurredAt,
      );
    }
  }
  return { rows, monthly };
}

export function itemPriceSeries(
  purchases: Purchase[],
  itemId: string,
  metric: Exclude<ItemMetric, "amount">,
) {
  const series = new Map<
    string,
    { label: string; points: Array<{ occurredAt: string; value: string }> }
  >();
  let unknownCount = 0;
  for (const purchase of purchases) {
    if (purchase.status !== "POSTED" && purchase.status !== "CLOSED") continue;
    for (const item of purchase.items) {
      if (itemAnalysisId(item) !== itemId) continue;
      let key: string;
      let label: string;
      let value: string;
      if (metric === "effective") {
        if (!item.effectiveUnitPriceNumeratorYen || !item.effectiveUnitPriceDenominator) {
          unknownCount += 1;
          continue;
        }
        key = `effective:${item.quantityUnit}`;
        label = `実効単価 · ${item.quantityUnit}`;
        value = `${item.effectiveUnitPriceNumeratorYen}/${item.effectiveUnitPriceDenominator}`;
      } else {
        if (
          item.printedUnitPriceYen == null ||
          !item.printedUnitPriceQuantityNumerator ||
          !item.printedUnitPriceQuantityDenominator ||
          !item.printedUnitPriceUnit
        ) {
          unknownCount += 1;
          continue;
        }
        key = `printed:${item.printedUnitPriceQuantityNumerator}/${item.printedUnitPriceQuantityDenominator}:${item.printedUnitPriceUnit}`;
        label = `印字単価 · ${item.printedUnitPriceQuantityNumerator}/${item.printedUnitPriceQuantityDenominator}${item.printedUnitPriceUnit}`;
        value = String(item.printedUnitPriceYen);
      }
      const current = series.get(key) ?? { label, points: [] };
      current.points.push({ occurredAt: purchase.occurredAt, value });
      series.set(key, current);
    }
  }
  for (const current of series.values())
    current.points.sort(
      (left, right) => Date.parse(left.occurredAt) - Date.parse(right.occurredAt),
    );
  return { series: Array.from(series.values()), unknownCount };
}

export const formatExactYen = (numerator: string, denominator: string) => {
  const amount = new Intl.NumberFormat("ja-JP").format(BigInt(numerator));
  return denominator === "1" ? `¥${amount}` : `¥${amount}/${denominator}`;
};
