import { useState } from "react";

import type { Dimension, ItemMetric } from "./purchase-analytics";

import { japanMonth } from "../../../shared/lib";

const monthRange = (current: string, count: number) => {
  const [year, month] = current.split("-").map(Number);
  const endIndex = year * 12 + month - 1;
  return Array.from({ length: count }, (_, index) => {
    const serial = endIndex - count + index + 1;
    return `${Math.floor(serial / 12)}-${String((serial % 12) + 1).padStart(2, "0")}`;
  });
};

export function exclusiveMonthEnd(month: string) {
  const [year, monthNumber] = month.split("-").map(Number);
  const endYear = year + Math.floor(monthNumber / 12);
  const endMonth = (monthNumber % 12) + 1;
  return `${endYear}-${String(endMonth).padStart(2, "0")}-01T00:00:00+09:00`;
}

export function useAnalysisPeriod() {
  const [currentMonth] = useState(() => japanMonth(new Date()));
  const [periodLength, setPeriodLength] = useState(6);
  const [annual, setAnnual] = useState(false);
  const [annualYear, setAnnualYear] = useState(Number(currentMonth.slice(0, 4)));
  const [dimension, setDimension] = useState<Dimension>("category");
  const [itemMetric, setItemMetric] = useState<ItemMetric>("amount");
  const months = annual
    ? monthRange(`${annualYear}-12`, 12)
    : monthRange(currentMonth, periodLength);

  return {
    annual,
    setAnnual,
    annualYear,
    setAnnualYear,
    currentMonth,
    dimension,
    setDimension,
    itemMetric,
    setItemMetric,
    periodLength,
    setPeriodLength,
    months,
  };
}
