export type Dimension = "category" | "merchant" | "item" | "pool" | "account" | "member";
export type PurchaseDimension = Exclude<Dimension, "category" | "member">;
export type ItemMetric = "amount" | "effective" | "printed";
export type AnalyticsRow = { id: string; name: string; amount: number; count: number };
export type PurchaseAnalyticsEntry = {
  dimension: string;
  id: string;
  name: string;
  period: string;
  amountYen: string;
  count: number;
};
export type ItemPriceObservation = {
  itemId: string;
  occurredAt: string;
  amountYen: string;
  effectiveUnitPriceNumeratorYen: string | null;
  effectiveUnitPriceDenominator: string | null;
  printedUnitPriceYen: string | null;
  printedUnitPriceQuantityNumerator: string | null;
  printedUnitPriceQuantityDenominator: string | null;
  printedUnitPriceUnit: string | null;
};
export type MonthlyEntry = { month: string; id: string; amount: number };

export function aggregatePurchaseAnalytics(entries: PurchaseAnalyticsEntry[]) {
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
  const monthlyTotals: Record<PurchaseDimension, Map<string, MonthlyEntry>> = {
    merchant: new Map(),
    item: new Map(),
    pool: new Map(),
    account: new Map(),
  };
  const latestPeriod: Record<PurchaseDimension, Map<string, string>> = {
    merchant: new Map(),
    item: new Map(),
    pool: new Map(),
    account: new Map(),
  };

  for (const entry of entries) {
    if (!(entry.dimension in rows)) continue;
    const dimension = entry.dimension as PurchaseDimension;
    const amount = Number(entry.amountYen);
    const current = rows[dimension].get(entry.id) ?? {
      id: entry.id,
      name: entry.name,
      amount: 0,
      count: 0,
    };
    current.amount += amount;
    current.count += entry.count;
    rows[dimension].set(entry.id, current);
    if (entry.period >= (latestPeriod[dimension].get(entry.id) ?? "")) {
      current.name = entry.name;
      latestPeriod[dimension].set(entry.id, entry.period);
    }
    const key = entry.period;
    const month = monthlyTotals[dimension].get(key) ?? { month: key, id: key, amount: 0 };
    month.amount += amount;
    monthlyTotals[dimension].set(key, month);
  }

  for (const dimension of Object.keys(monthlyTotals) as PurchaseDimension[]) {
    monthly[dimension] = Array.from(monthlyTotals[dimension].values());
  }
  return { rows, monthly };
}

export function itemPriceSeries(
  observations: ItemPriceObservation[],
  itemId: string,
  metric: Exclude<ItemMetric, "amount">,
) {
  const series = new Map<
    string,
    { label: string; points: Array<{ occurredAt: string; value: string }> }
  >();
  let unknownCount = 0;
  for (const observation of observations) {
    if (observation.itemId !== itemId) continue;
    let key: string;
    let label: string;
    let value: string;
    if (metric === "effective") {
      if (!observation.effectiveUnitPriceNumeratorYen || !observation.effectiveUnitPriceDenominator) {
        unknownCount += 1;
        continue;
      }
      const unit = itemId.split("\0").at(-1) || "単位不明";
      key = `effective:${unit}`;
      label = `実効単価 · ${unit}`;
      value = `${observation.effectiveUnitPriceNumeratorYen}/${observation.effectiveUnitPriceDenominator}`;
    } else {
      if (
        observation.printedUnitPriceYen == null ||
        !observation.printedUnitPriceQuantityNumerator ||
        !observation.printedUnitPriceQuantityDenominator ||
        !observation.printedUnitPriceUnit
      ) {
        unknownCount += 1;
        continue;
      }
      key = `printed:${observation.printedUnitPriceQuantityNumerator}/${observation.printedUnitPriceQuantityDenominator}:${observation.printedUnitPriceUnit}`;
      label = `印字単価 · ${observation.printedUnitPriceQuantityNumerator}/${observation.printedUnitPriceQuantityDenominator}${observation.printedUnitPriceUnit}`;
      value = observation.printedUnitPriceYen;
    }
    const current = series.get(key) ?? { label, points: [] };
    current.points.push({ occurredAt: observation.occurredAt, value });
    series.set(key, current);
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
