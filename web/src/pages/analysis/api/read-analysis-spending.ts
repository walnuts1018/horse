import { fetchQuery } from "react-relay";
import {
  getRelayEnvironment,
  monthlyCategorySpendingQuery,
  type MonthlyCategorySpendingQuery,
  type MonthlyCategorySpendingQuery$data,
  type MonthlyMemberSpendingQuery,
  type MonthlyMemberSpendingQuery$data,
} from "../../../shared/api";
import { monthlyMemberSpendingQuery } from "./operations";

export type AnalysisCategorySpending = {
  period: string;
  categoryId?: string | null;
  categoryName?: string | null;
  amountYen: number;
};

export type AnalysisMemberSpending = {
  period: string;
  memberId: string;
  memberName: string;
  amountYen: number;
};

type CategorySpendingRecord = MonthlyCategorySpendingQuery$data["monthlyCategorySpending"][number];
type MemberSpendingRecord =
  MonthlyMemberSpendingQuery$data["monthlyMemberSpending"]["items"][number];

export async function readAnalysisCategorySpending(
  householdId: string,
  from: string,
  to: string,
): Promise<AnalysisCategorySpending[]> {
  const data = await fetchQuery<MonthlyCategorySpendingQuery>(
    getRelayEnvironment(),
    monthlyCategorySpendingQuery,
    { householdId, from, to },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data) throw new Error("カテゴリ別支出を取得できませんでした");
  return data.monthlyCategorySpending.map(mapCategorySpending);
}

export async function readAnalysisMemberSpending(
  householdId: string,
  from: string,
  to: string,
): Promise<{ items: AnalysisMemberSpending[]; hasUnattributedPurchases: boolean }> {
  const data = await fetchQuery<MonthlyMemberSpendingQuery>(
    getRelayEnvironment(),
    monthlyMemberSpendingQuery,
    { householdId, from, to },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data) throw new Error("メンバー別支出を取得できませんでした");
  return {
    items: data.monthlyMemberSpending.items.map(mapMemberSpending),
    hasUnattributedPurchases: data.monthlyMemberSpending.hasUnattributedPurchases,
  };
}

function mapCategorySpending(record: CategorySpendingRecord): AnalysisCategorySpending {
  return {
    period: record.period,
    categoryId: record.categoryId,
    categoryName: record.categoryName,
    amountYen: Number(record.amountYen),
  };
}

function mapMemberSpending(record: MemberSpendingRecord): AnalysisMemberSpending {
  return {
    period: record.period,
    memberId: record.memberId,
    memberName: record.memberName,
    amountYen: Number(record.amountYen),
  };
}
