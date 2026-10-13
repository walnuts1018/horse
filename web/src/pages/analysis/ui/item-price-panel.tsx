import type { Dispatch, SetStateAction } from "react";

import type { Purchase } from "../../../shared/api";

import { yen } from "../model/format-yen";
import {
  formatExactYen,
  itemPriceSeries,
  type AnalyticsRow,
  type ItemMetric,
} from "../model/purchase-analytics";

export function ItemPricePanel({
  rows,
  data,
  purchases,
  itemMetric,
  setItemMetric,
}: {
  rows: AnalyticsRow[];
  data: ReturnType<typeof import("../model/purchase-analytics").purchaseDimensions> | null;
  purchases: Purchase[];
  itemMetric: ItemMetric;
  setItemMetric: Dispatch<SetStateAction<ItemMetric>>;
}) {
  if (rows.length === 0) return null;

  return (
    <section className="panel analysis-price-panel">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">品目の推移</span>
          <h2>
            {itemMetric === "amount"
              ? "購入額"
              : itemMetric === "effective"
                ? "実効単価"
                : "印字単価"}
          </h2>
        </div>
        <label className="select-filter">
          <span>価格指標</span>
          <select
            aria-label="価格指標"
            value={itemMetric}
            onChange={(event) => setItemMetric(event.target.value as ItemMetric)}
          >
            <option value="amount">購入額</option>
            <option value="effective">実効単価</option>
            <option value="printed">印字単価</option>
          </select>
        </label>
      </div>
      {rows.slice(0, 5).map((row) => {
        const prices = (data?.monthly.item ?? [])
          .filter((entry) => entry.id === row.id)
          .sort((left, right) => Date.parse(left.occurredAt) - Date.parse(right.occurredAt));
        const trend =
          itemMetric === "amount" ? null : itemPriceSeries(purchases, row.id, itemMetric);
        return (
          <div className="price-row" key={row.id}>
            <span>{row.name}</span>
            {itemMetric === "amount" ? (
              <>
                <strong>{yen(Math.round(row.amount / Math.max(1, row.count)))}</strong>
                <small>{row.count}件</small>
                {prices.length > 1 && (
                  <small className="price-change">
                    初回購入額 {yen(prices[0].amount)}、最新購入額{" "}
                    {yen(prices[prices.length - 1].amount)}
                  </small>
                )}
              </>
            ) : (
              <>
                {trend?.series.map((series) => {
                  const first = series.points[0];
                  const last = series.points[series.points.length - 1];
                  const format = (value: string) => {
                    const [numerator, denominator] = value.split("/");
                    return denominator
                      ? formatExactYen(numerator, denominator)
                      : yen(Number(value));
                  };
                  return (
                    <small className="price-change" key={series.label}>
                      {series.label}: {format(first.value)} → {format(last.value)} ·{" "}
                      {series.points.length}件
                    </small>
                  );
                })}
                {trend?.unknownCount ? <small>単価不明 {trend.unknownCount}件</small> : null}
                {!trend?.series.length && !trend?.unknownCount && <small>記録なし</small>}
              </>
            )}
          </div>
        );
      })}
    </section>
  );
}
