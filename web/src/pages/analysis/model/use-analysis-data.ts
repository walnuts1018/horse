import { useEffect, useState } from "react";

import type { Budget, RecurringExpense } from "../../../shared/api";
import type { Dimension } from "./purchase-analytics";

import { readBudgets } from "../../../entities/budget";
import { useHouseholdPurchases } from "../../../entities/purchase";
import { readRecurringExpenses } from "../../../entities/recurring-expense";
import { errorMessage } from "../../../shared/lib";
import {
  readAnalysisCategorySpending,
  readAnalysisMemberSpending,
} from "../api/read-analysis-spending";
import { exclusiveMonthEnd } from "./use-analysis-period";

export function useAnalysisData(
  householdId: string,
  fromMonth: string,
  toMonth: string,
  currentMonth: string,
  dimension: Dimension,
) {
  const { purchases, loadingMore, moreError, hasNextPage, loadMore } = useHouseholdPurchases(
    householdId,
    100,
    {
      from: `${fromMonth}-01T00:00:00+09:00`,
      to: exclusiveMonthEnd(toMonth),
      loadAll: true,
    },
  );
  const [spending, setSpending] = useState<
    Array<{
      period: string;
      categoryId?: string | null;
      categoryName?: string | null;
      amountYen: number;
    }>
  >([]);
  const [spendingLoading, setSpendingLoading] = useState(true);
  const [spendingError, setSpendingError] = useState("");
  const [memberSpending, setMemberSpending] = useState<
    Array<{ period: string; memberId: string; memberName: string; amountYen: number }>
  >([]);
  const [hasUnattributedPurchases, setHasUnattributedPurchases] = useState(false);
  const [memberSpendingLoading, setMemberSpendingLoading] = useState(false);
  const [memberSpendingError, setMemberSpendingError] = useState("");
  const [budgets, setBudgets] = useState<Budget[]>([]);
  const [budgetError, setBudgetError] = useState("");
  const [recurringExpenses, setRecurringExpenses] = useState<RecurringExpense[]>([]);
  const [recurringError, setRecurringError] = useState("");

  useEffect(() => {
    let active = true;
    //同期setStateはeffect直後ではなくマイクロタスクで走らせ、取得の再開も1tick遅延させる
    queueMicrotask(() => {
      if (!active) return;
      setSpendingLoading(true);
      setSpendingError("");
      void readAnalysisCategorySpending(householdId, fromMonth, toMonth)
        .then((result) => active && setSpending(result))
        .catch((reason) => active && setSpendingError(errorMessage(reason)))
        .finally(() => active && setSpendingLoading(false));
    });
    return () => {
      active = false;
    };
  }, [householdId, fromMonth, toMonth]);

  useEffect(() => {
    let active = true;
    //同期setStateはeffect直後ではなくマイクロタスクで走らせ、取得の再開も1tick遅延させる
    queueMicrotask(() => {
      if (!active) return;
      if (dimension !== "member") {
        setMemberSpendingLoading(false);
        setMemberSpendingError("");
        return;
      }
      setMemberSpendingLoading(true);
      setMemberSpendingError("");
      setHasUnattributedPurchases(false);
      void readAnalysisMemberSpending(householdId, fromMonth, toMonth)
        .then((result) => {
          if (!active) return;
          setMemberSpending(result.items);
          setHasUnattributedPurchases(result.hasUnattributedPurchases);
        })
        .catch((reason) => active && setMemberSpendingError(errorMessage(reason)))
        .finally(() => active && setMemberSpendingLoading(false));
    });
    return () => {
      active = false;
    };
  }, [dimension, householdId, fromMonth, toMonth]);

  useEffect(() => {
    let active = true;
    //同期setStateはeffect直後ではなくマイクロタスクで走らせ、取得の再開も1tick遅延させる
    queueMicrotask(() => {
      if (!active) return;
      setBudgetError("");
      void readBudgets(householdId, currentMonth)
        .then((result) => active && setBudgets(result))
        .catch((reason) => active && setBudgetError(errorMessage(reason)));
    });
    return () => {
      active = false;
    };
  }, [householdId, currentMonth]);

  useEffect(() => {
    let active = true;
    //同期setStateはeffect直後ではなくマイクロタスクで走らせ、取得の再開も1tick遅延させる
    queueMicrotask(() => {
      if (!active) return;
      setRecurringError("");
      void readRecurringExpenses(householdId)
        .then((expenses) => active && setRecurringExpenses(expenses))
        .catch((reason) => active && setRecurringError(errorMessage(reason)));
    });
    return () => {
      active = false;
    };
  }, [householdId]);

  return {
    purchases,
    loadingMore,
    moreError,
    hasNextPage,
    loadMore,
    spending,
    spendingLoading,
    spendingError,
    memberSpending,
    hasUnattributedPurchases,
    memberSpendingLoading,
    memberSpendingError,
    budgets,
    budgetError,
    recurringExpenses,
    recurringError,
  };
}
