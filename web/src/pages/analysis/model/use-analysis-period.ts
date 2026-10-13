import { useState } from "react";
import { japanMonth } from "../../../shared/lib";
import type { Dimension, ItemMetric } from "./purchase-analytics";

const monthRange = (current: string, count: number) => {
  const [year, month] = current.split("-").map(Number);
  const endIndex = year * 12 + month - 1;
  return Array.from({ length: count }, (_, index) => {
    const serial = endIndex - count + index + 1;
    return `${Math.floor(serial / 12)}-${String((serial % 12) + 1).padStart(2, "0")}`;
  });
};

export function useAnalysisPeriod() {
  const [periodLength, setPeriodLength] = useState(6);
  const [annual, setAnnual] = useState(false);
  const [annualYear, setAnnualYear] = useState(Number(japanMonth(new Date()).slice(0, 4)));
  const [dimension, setDimension] = useState<Dimension>("category");
  const [itemMetric, setItemMetric] = useState<ItemMetric>("amount");
  const currentMonth = japanMonth(new Date());
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
