import type { Dispatch, SetStateAction } from "react";
import type { Dimension } from "../model/purchase-analytics";
import { analysisDimensionLabels } from "./analysis-dimension-labels";

type Props = {
  annual: boolean;
  setAnnual: Dispatch<SetStateAction<boolean>>;
  annualYear: number;
  setAnnualYear: Dispatch<SetStateAction<number>>;
  currentMonth: string;
  periodLength: number;
  setPeriodLength: Dispatch<SetStateAction<number>>;
  dimension: Dimension;
  setDimension: Dispatch<SetStateAction<Dimension>>;
};

export function AnalysisControls({
  annual,
  setAnnual,
  annualYear,
  setAnnualYear,
  currentMonth,
  periodLength,
  setPeriodLength,
  dimension,
  setDimension,
}: Props) {
  return (
    <div className="page-heading">
      <div>
        <span className="eyebrow">支出の傾向</span>
        <h1>分析</h1>
      </div>
      <div className="analysis-controls">
        <label className="select-filter">
          <span>期間</span>
          <select
            value={annual ? "annual" : periodLength}
            onChange={(event) => {
              setAnnual(event.target.value === "annual");
              if (event.target.value !== "annual") setPeriodLength(Number(event.target.value));
            }}
          >
            <option value="annual">年次</option>
            <option value={3}>3か月</option>
            <option value={6}>6か月</option>
            <option value={12}>12か月</option>
          </select>
        </label>
        {annual && (
          <label className="select-filter">
            <span>年</span>
            <select
              value={annualYear}
              onChange={(event) => setAnnualYear(Number(event.target.value))}
            >
              {Array.from(
                { length: 10 },
                (_, index) => Number(currentMonth.slice(0, 4)) - index,
              ).map((year) => (
                <option value={year} key={year}>
                  {year}年
                </option>
              ))}
            </select>
          </label>
        )}
        <label className="select-filter">
          <span>集計</span>
          <select
            value={dimension}
            onChange={(event) => setDimension(event.target.value as Dimension)}
          >
            {Object.entries(analysisDimensionLabels).map(([value, label]) => (
              <option value={value} key={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
      </div>
    </div>
  );
}
