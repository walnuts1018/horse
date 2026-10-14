import type { CSSProperties } from "react";

import { BarChart3 } from "lucide-react";

import type { AnalyticsRow, Dimension } from "../model/purchase-analytics";

import { yen } from "../model/format-yen";
import { analysisDimensionLabels } from "./analysis-dimension-labels";

type MonthTotal = { month: string; amount: number };
type CategorySeries = { id: string; name: string; colorIndex: number };

const sum = (numbers: number[]) => numbers.reduce((total, value) => total + value, 0);
type ChartScaleStyle = CSSProperties & { "--chart-scale": number };

export function AnalysisBreakdown({
  dimension,
  monthly,
  categorySeries,
  remainingCategories,
  categoryAmountsByMonth,
  max,
  maxCategoryPositive,
  maxCategoryNegative,
  rows,
  hasUnattributedPurchases,
}: {
  dimension: Dimension;
  monthly: MonthTotal[];
  categorySeries: CategorySeries[];
  remainingCategories: AnalyticsRow[];
  categoryAmountsByMonth: Map<string, Map<string, number>>;
  max: number;
  maxCategoryPositive: number;
  maxCategoryNegative: number;
  rows: AnalyticsRow[];
  hasUnattributedPurchases: boolean;
}) {
  return (
    <div className="analysis-grid">
      <section className="panel analysis-chart-panel">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">月別推移</span>
            <h2>{analysisDimensionLabels[dimension]}の支出</h2>
            {dimension === "member" && (
              <small>
                購入時の負担スナップショット
                {hasUnattributedPurchases && " · 履歴不明の購入を含みません"}
              </small>
            )}
          </div>
          {dimension === "category" ? (
            <div className="chart-legend category-chart-legend">
              {categorySeries.map((category) => (
                <span key={category.id}>
                  <i className={`swatch-${category.colorIndex}`} />
                  {category.name}
                </span>
              ))}
            </div>
          ) : (
            <span className="chart-legend">
              <i />
              合計
            </span>
          )}
        </div>
        <div className="monthly-chart">
          {monthly.map((row) => {
            if (dimension !== "category") {
              return (
                <div className="month-column" key={row.month}>
                  <b>{yen(row.amount)}</b>
                  <i style={{ height: `${Math.max(3, (row.amount / max) * 100)}%` }} />
                  <span>{Number(row.month.slice(5))}月</span>
                </div>
              );
            }
            const categoryAmounts = categoryAmountsByMonth.get(row.month) ?? new Map();
            const otherAmount = remainingCategories.reduce(
              (amount, category) => amount + (categoryAmounts.get(category.id) ?? 0),
              0,
            );
            const segments = categorySeries.map((category) => ({
              ...category,
              amount:
                category.id === "__other" ? otherAmount : (categoryAmounts.get(category.id) ?? 0),
            }));
            const positiveSegments = segments.filter((segment) => segment.amount > 0);
            const negativeSegments = segments.filter((segment) => segment.amount < 0);
            const positiveTotal = sum(positiveSegments.map((segment) => segment.amount));
            const negativeTotal = sum(negativeSegments.map((segment) => -segment.amount));
            const monthlyBreakdown = segments
              .filter((segment) => segment.amount !== 0)
              .map((segment) => `${segment.name} ${yen(segment.amount)}`)
              .join("、");
            return (
              <figure
                className="month-column category-month-column"
                key={row.month}
                aria-label={`${row.month}: ${monthlyBreakdown}、合計${yen(row.amount)}`}
              >
                <b>{yen(row.amount)}</b>
                <div className="category-stack">
                  <div
                    className="category-positive"
                    style={
                      { "--chart-scale": positiveTotal / maxCategoryPositive } as ChartScaleStyle
                    }
                  >
                    {positiveSegments.map((segment) => (
                      <i
                        className={`swatch-${segment.colorIndex}`}
                        key={segment.id}
                        style={{ flexGrow: segment.amount }}
                      />
                    ))}
                  </div>
                  <div className="category-baseline" />
                  <div
                    className="category-negative"
                    style={
                      { "--chart-scale": negativeTotal / maxCategoryNegative } as ChartScaleStyle
                    }
                  >
                    {negativeSegments.map((segment) => (
                      <i
                        className={`swatch-${segment.colorIndex}`}
                        key={segment.id}
                        style={{ flexGrow: -segment.amount }}
                      />
                    ))}
                  </div>
                </div>
                <span>{Number(row.month.slice(5))}月</span>
              </figure>
            );
          })}
        </div>
      </section>
      <section className="panel category-panel">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">内訳</span>
            <h2>{analysisDimensionLabels[dimension]}別</h2>
          </div>
        </div>
        {rows.length === 0 ? (
          <div className="empty-state compact">
            <BarChart3 />
            <span>支出の記録</span>
          </div>
        ) : (
          rows.map((row, index) => (
            <div className="category-row" key={row.id}>
              <span className={`category-swatch swatch-${index % 6}`} />
              <span>{row.name}</span>
              <i>
                <b
                  style={{ width: `${Math.max(8, (row.amount / (rows[0]?.amount || 1)) * 100)}%` }}
                />
              </i>
              <strong>{yen(row.amount)}</strong>
            </div>
          ))
        )}
      </section>
    </div>
  );
}
