import type { AnalyticsRow, Dimension } from "../model/purchase-analytics";

import { yen } from "../model/format-yen";
import { analysisDimensionLabels } from "./analysis-dimension-labels";

export function AnalysisSummary({
  annual,
  annualYear,
  periodCount,
  total,
  dimension,
  peakRow,
}: {
  annual: boolean;
  annualYear: number;
  periodCount: number;
  total: number;
  dimension: Dimension;
  peakRow?: AnalyticsRow;
}) {
  return (
    <div className="analysis-summary">
      <section className="panel summary-card">
        <span className="eyebrow">期間合計</span>
        <h2>{yen(total)}</h2>
        <small>{annual ? `${annualYear}年` : `${periodCount}か月`}</small>
      </section>
      <section className="panel summary-card">
        <span className="eyebrow">月平均</span>
        <h2>{yen(Math.round(total / periodCount))}</h2>
        <small>{periodCount}か月</small>
      </section>
      <section className="panel summary-card">
        <span className="eyebrow">最多の{analysisDimensionLabels[dimension]}</span>
        <h2>{peakRow?.name ?? "—"}</h2>
        <small>{peakRow ? yen(peakRow.amount) : "記録なし"}</small>
      </section>
    </div>
  );
}
