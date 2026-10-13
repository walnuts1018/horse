import type { Settlement } from "../../../shared/api";

export function settlementTotalDue(settlement: Settlement | null): number {
  return settlement?.lines.reduce((total, line) => total + line.amountDueYen, 0) ?? 0;
}

export function settlementPeriodLabel(period: string): string {
  return `${period.replace("-", "年")}月`;
}

export function formatSettlementYen(amount = 0): string {
  return new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
}
