import { fetchQuery } from "react-relay";
import {
  getRelayEnvironment,
  monthlyCategorySpendingQuery,
  type HomeSummaryQuery,
  type HomeSummaryQuery$data,
  type MonthlyCategorySpendingQuery,
  type MonthlyCategorySpendingQuery$data,
  type SettlementQuery,
  type SettlementQuery$data,
} from "../../../shared/api";
import { settlementQuery } from "../../../entities/settlement";
import { homeSummaryQuery } from "./home-summary";

export type HomeSummary = {
  dailySpending: Array<{ date: string; amountYen: number }>;
  monthlyItemCount: number;
};

export type HomeMonthlySpending = { period: string; amountYen: number };
export type HomeSettlement = {
  __typename: "OpenSettlement" | "ClosedSettlement";
  lines: Array<{ amountDueYen: number }>;
};

type HomeSummaryRecord = HomeSummaryQuery$data["homeSummary"];
type MonthlySpendingRecord = MonthlyCategorySpendingQuery$data["monthlyCategorySpending"][number];
type SettlementRecord = SettlementQuery$data["settlement"];

export async function readHomeSummary(householdId: string): Promise<HomeSummary> {
  const data = await fetchQuery<HomeSummaryQuery>(
    getRelayEnvironment(),
    homeSummaryQuery,
    { householdId },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data) throw new Error("ホームの概要を取得できませんでした");
  return mapHomeSummary(data.homeSummary);
}

export async function readHomeMonthlySpending(
  householdId: string,
  period: string,
): Promise<HomeMonthlySpending[]> {
  const data: MonthlyCategorySpendingQuery$data | undefined =
    await fetchQuery<MonthlyCategorySpendingQuery>(
      getRelayEnvironment(),
      monthlyCategorySpendingQuery,
      { householdId, from: period, to: period },
      { fetchPolicy: "network-only" },
    ).toPromise();
  if (!data) throw new Error("月間支出を取得できませんでした");
  return data.monthlyCategorySpending.map(mapMonthlySpending);
}

export async function readHomeSettlement(
  householdId: string,
  period: string,
): Promise<HomeSettlement> {
  const data: SettlementQuery$data | undefined = await fetchQuery<SettlementQuery>(
    getRelayEnvironment(),
    settlementQuery,
    { householdId, period },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data) throw new Error("精算情報を取得できませんでした");
  return mapHomeSettlement(data.settlement);
}

function mapHomeSummary(record: HomeSummaryRecord): HomeSummary {
  return {
    dailySpending: record.dailySpending.map((day) => ({
      date: day.date,
      amountYen: Number(day.amountYen),
    })),
    monthlyItemCount: Number(record.monthlyItemCount),
  };
}

function mapMonthlySpending(record: MonthlySpendingRecord): HomeMonthlySpending {
  return { period: record.period, amountYen: Number(record.amountYen) };
}

function mapHomeSettlement(record: SettlementRecord): HomeSettlement {
  switch (record.__typename) {
    case "OpenSettlement":
    case "ClosedSettlement":
      return {
        __typename: record.__typename,
        lines: record.lines.map((line) => ({ amountDueYen: Number(line.amountDueYen) })),
      };
    default:
      throw new Error(`未対応の精算型です: ${record.__typename}`);
  }
}
