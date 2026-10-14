import { useEffect, useMemo, useState } from "react";
import { useLazyLoadQuery, usePaginationFragment } from "react-relay";

import type {
  AnalysisBudgetsFragment$key,
  AnalysisDashboardQuery,
  AnalysisRecurringExpensesFragment$key,
  Budget,
  RecurringExpense,
} from "../../../shared/api";
import type { Dimension } from "./purchase-analytics";

import { useHouseholdPurchases } from "../../../entities/purchase";
import {
  analysisBudgetsFragment,
  analysisDashboardQuery,
  analysisRecurringExpensesFragment,
} from "../api/operations";
import { exclusiveMonthEnd } from "./use-analysis-period";

export function useAnalysisData(
  householdId: string,
  fromMonth: string,
  toMonth: string,
  currentMonth: string,
  dimension: Dimension,
) {
  const variables = useMemo(
    () => ({ householdId, from: fromMonth, to: toMonth, period: currentMonth }),
    [currentMonth, fromMonth, householdId, toMonth],
  );
  const dashboard = useLazyLoadQuery<AnalysisDashboardQuery>(analysisDashboardQuery, variables, {
    fetchPolicy: "network-only",
  });
  const {
    data: budgetData,
    hasNext: hasMoreBudgets,
    isLoadingNext: loadingBudgets,
    loadNext: loadNextBudgets,
  } = usePaginationFragment<AnalysisDashboardQuery, AnalysisBudgetsFragment$key>(
    analysisBudgetsFragment,
    dashboard,
  );
  const {
    data: recurringData,
    hasNext: hasMoreRecurringExpenses,
    isLoadingNext: loadingRecurringExpenses,
    loadNext: loadNextRecurringExpenses,
  } = usePaginationFragment<AnalysisDashboardQuery, AnalysisRecurringExpensesFragment$key>(
    analysisRecurringExpensesFragment,
    dashboard,
  );
  const { purchases, loadingMore, moreError, hasNextPage, loadMore, refresh } =
    useHouseholdPurchases(householdId, 100, {
      from: `${fromMonth}-01T00:00:00+09:00`,
      to: exclusiveMonthEnd(toMonth),
      loadAll: dimension !== "category" && dimension !== "member",
    });
  const requestKey = JSON.stringify(variables);
  const [budgetErrorState, setBudgetErrorState] = useState<{ key: string; message: string } | null>(
    null,
  );
  const [recurringErrorState, setRecurringErrorState] = useState<{
    key: string;
    message: string;
  } | null>(null);
  const budgetError = budgetErrorState?.key === requestKey ? budgetErrorState.message : "";
  const recurringError = recurringErrorState?.key === requestKey ? recurringErrorState.message : "";

  const spending = dashboard.monthlyCategorySpending.map((entry) => ({
    period: entry.period,
    categoryId: entry.categoryId,
    categoryName: entry.categoryName,
    amountYen: Number(entry.amountYen),
  }));
  const memberSpending =
    dimension === "member"
      ? dashboard.monthlyMemberSpending.items.map((entry) => ({
          period: entry.period,
          memberId: entry.memberId,
          memberName: entry.memberName,
          amountYen: Number(entry.amountYen),
        }))
      : [];
  const hasUnattributedPurchases =
    dimension === "member" && dashboard.monthlyMemberSpending.hasUnattributedPurchases;

  useEffect(() => {
    if (!hasMoreBudgets || loadingBudgets || budgetError) return;
    loadNextBudgets(100, {
      onComplete: (error) => {
        if (error)
          setBudgetErrorState({
            key: requestKey,
            message: error.message || "予算を取得できませんでした",
          });
      },
    });
  }, [budgetError, hasMoreBudgets, loadNextBudgets, loadingBudgets, requestKey]);

  useEffect(() => {
    if (!hasMoreRecurringExpenses || loadingRecurringExpenses || recurringError) return;
    loadNextRecurringExpenses(100, {
      onComplete: (error) => {
        if (error)
          setRecurringErrorState({
            key: requestKey,
            message: error.message || "定期支出を取得できませんでした",
          });
      },
    });
  }, [
    hasMoreRecurringExpenses,
    loadNextRecurringExpenses,
    loadingRecurringExpenses,
    recurringError,
    requestKey,
  ]);

  const budgets: Budget[] = budgetData.budgets.edges.map(({ node }) => ({
    id: node.id,
    householdId: node.householdId,
    categoryId: node.categoryId,
    name: node.name,
    amountYen: Number(node.amountYen),
    actualYen: Number(node.actualYen),
    remainingYen: Number(node.remainingYen),
    startsOn: node.startsOn,
    endsOn: node.endsOn,
    archived: node.archived,
    revision: node.revision,
  }));
  const recurringExpenses: RecurringExpense[] = recurringData.recurringExpenses.edges.map(
    ({ node }) => ({
      id: node.id,
      householdId: node.householdId,
      name: node.name,
      merchant: node.merchant,
      amountYen: Number(node.amountYen),
      currency: node.currency,
      categoryId: node.categoryId,
      paymentAccountId: node.paymentAccountId,
      cadence: mapCadence(node.cadence),
      startsOn: node.startsOn,
      nextDueOn: node.nextDueOn,
      endsOn: node.endsOn,
      archived: node.archived,
      revision: node.revision,
    }),
  );

  return {
    purchases,
    loadingMore,
    moreError,
    hasNextPage,
    loadMore,
    refreshPurchases: refresh,
    spending,
    spendingLoading: false,
    spendingError: "",
    memberSpending,
    hasUnattributedPurchases,
    memberSpendingLoading: false,
    memberSpendingError: "",
    budgets,
    budgetError,
    recurringExpenses,
    recurringError,
  };
}

function mapCadence(cadence: string): RecurringExpense["cadence"] {
  switch (cadence) {
    case "WEEKLY":
    case "MONTHLY":
    case "YEARLY":
      return cadence;
    default:
      throw new Error(`未対応の定期支出周期です: ${cadence}`);
  }
}
