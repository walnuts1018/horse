import type { Dispatch, SetStateAction } from "react";

import { isOneOf } from "../../../shared/lib";
import { yen } from "../model/format-yen";
import {
  formatExactYen,
  itemPriceSeries,
  type AnalyticsRow,
  type ItemPriceObservation,
  type ItemMetric,
} from "../model/purchase-analytics";

const format = (value: string) => {
  const [numerator, denominator] = value.split("/");
  return denominator ? formatExactYen(numerator, denominator) : yen(Number(value));
};
export function ItemPricePanel({
  rows,
  observations,
  itemMetric,
  setItemMetric,
}: {
  rows: AnalyticsRow[];
  observations: readonly ItemPriceObservation[];
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
            onChange={(event) => {
              if (isOneOf(event.target.value, ["amount", "effective", "printed"] as const)) {
                setItemMetric(event.target.value);
              }
            }}
          >
            <option value="amount">購入額</option>
            <option value="effective">実効単価</option>
            <option value="printed">印字単価</option>
          </select>
        </label>
      </div>
      {rows.filter((row) => row.id.includes("\0")).slice(0, 5).map((row) => {
        const matchingPrices = observations.filter((observation) => observation.itemId === row.id);
        // oxlint-disable-next-line unicorn/no-array-sort
        const prices = [...matchingPrices].sort(
          (left, right) => Date.parse(left.occurredAt) - Date.parse(right.occurredAt),
        );
        const trend =
          itemMetric === "amount" ? null : itemPriceSeries(observations, row.id, itemMetric);
        return (
          <div className="price-row" key={row.id}>
            <span>{row.name}</span>
            {itemMetric === "amount" ? (
              <>
                <strong>{yen(Math.round(row.amount / Math.max(1, row.count)))}</strong>
                <small>{row.count}件</small>
                {prices.length > 1 && (
                  <small className="price-change">
                    初回購入額 {yen(Number(prices[0].remainingRefundableAmountYen))}、最新購入額{" "}
                    {yen(Number(prices[prices.length - 1].remainingRefundableAmountYen))}
                  </small>
                )}
              </>
            ) : (
              <>
                {trend?.series.map((series) => {
                  const first = series.points[0];
                  const last = series.points[series.points.length - 1];
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
