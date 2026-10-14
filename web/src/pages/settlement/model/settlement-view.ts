import type { Settlement } from "../../../shared/api";

export function settlementTotalDue(settlement: Settlement | null): number {
  return settlementFundingDue(settlement?.lines ?? []);
}

export function settlementFundingDue(lines: readonly { amountDueYen: number }[]): number {
  return lines.reduce((total, line) => total + Math.max(line.amountDueYen, 0), 0);
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
