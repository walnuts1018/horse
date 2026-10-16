import { useEffect, useMemo, useState } from "react";
import { useLazyLoadQuery, usePaginationFragment } from "react-relay";

import type {
  AnalysisDashboardQuery,
  AnalysisPurchasePriceObservationsQuery,
  AnalysisRecurringExpensesFragment$key,
  Budget,
  RecurringExpense,
} from "../../../shared/api";
import type { Dimension, ItemMetric } from "./purchase-analytics";

import {
  analysisPurchasePriceObservationsQuery,
  analysisDashboardQuery,
  analysisRecurringExpensesFragment,
} from "../api/operations";
import { aggregatePurchaseAnalytics, topPriceItemIDs } from "./purchase-analytics";

export function useAnalysisData(
  householdId: string,
  fromMonth: string,
  toMonth: string,
  dimension: Dimension,
  itemMetric: ItemMetric,
) {
  const variables = useMemo(
    () => ({ householdId, from: fromMonth, to: toMonth }),
    [fromMonth, householdId, toMonth],
  );
  const dashboard = useLazyLoadQuery<AnalysisDashboardQuery>(analysisDashboardQuery, variables, {
    fetchPolicy: "network-only",
  });
  const purchaseAnalytics = useMemo(
    () => aggregatePurchaseAnalytics(dashboard.purchaseAnalytics),
    [dashboard.purchaseAnalytics],
  );
  const priceItemIds = useMemo(
    () => (itemMetric === "amount" ? [] : topPriceItemIDs(purchaseAnalytics.rows.item.values())),
    [itemMetric, purchaseAnalytics.rows.item],
  );
  const priceObservations = useLazyLoadQuery<AnalysisPurchasePriceObservationsQuery>(
    analysisPurchasePriceObservationsQuery,
    { ...variables, itemIds: priceItemIds },
    { fetchPolicy: "network-only" },
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
  const requestKey = JSON.stringify(variables);
  const [recurringErrorState, setRecurringErrorState] = useState<{
    key: string;
    message: string;
  } | null>(null);
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

  const budgetPeriods = dashboard.budgetPeriods.map(({ period, budgets }) => ({
    period,
    budgets: budgets.map((node): Budget => ({
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
    })),
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
    purchaseAnalytics,
    itemPriceObservations: priceObservations.purchasePriceObservations,
    spending,
    spendingLoading: false,
    spendingError: "",
    memberSpending,
    hasUnattributedPurchases,
    memberSpendingLoading: false,
    memberSpendingError: "",
    budgetPeriods,
    budgetError: "",
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
