import { BarChart3 } from "lucide-react";
import { useEffect, useState } from "react";

import type { Household } from "../../../shared/api";
import type { AnalyticsRow } from "../model/purchase-analytics";

import { useHouseholdLedger } from "../../../entities/ledger";
import { InlineError } from "../../../shared/ui";
import { useAnalysisData } from "../model/use-analysis-data";
import { useAnalysisEvidence } from "../model/use-analysis-evidence";
import { useAnalysisPeriod } from "../model/use-analysis-period";
import { AnalysisBreakdown } from "./analysis-breakdown";
import { AnalysisBudgetPanel } from "./analysis-budget-panel";
import { AnalysisControls } from "./analysis-controls";
import { AnalysisEvidencePanel } from "./analysis-evidence-panel";
import { AnalysisLedgerPanel } from "./analysis-ledger-panel";
import { AnalysisSummary } from "./analysis-summary";
import { ItemPricePanel } from "./item-price-panel";
import { RecurringExpensePanel } from "./recurring-expense-panel";

import "./analysis-page.css";

const sum = (numbers: number[]) => numbers.reduce((total, value) => total + value, 0);
const formatTokyoDate = () =>
  new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Tokyo",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date());
const sorted = <T,>(values: T[], compare: (left: T, right: T) => number): T[] => {
  // ES2022の型定義にtoSortedが含まれないため、複製後にsortする。
  // oxlint-disable-next-line unicorn/no-array-sort
  return [...values].sort(compare);
};
export function AnalysisPage({ household }: { household: Household }) {
  const period = useAnalysisPeriod();
  const {
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
  } = period;
  const ledger = useHouseholdLedger(household.id, 100);
  const {
    entries: ledgerEntries,
    hasNextPage: hasMoreLedgerEntries,
    loadingMore: loadingMoreLedgerEntries,
    moreError: ledgerError,
    loadMore: loadMoreLedgerEntries,
  } = ledger;
  const hasOlderLedgerEntry = ledgerEntries.some(
    (entry) => entry.occurredAt.slice(0, 7) < months[0],
  );
  const ledgerHistoryComplete = hasOlderLedgerEntry || !hasMoreLedgerEntries;
  useEffect(() => {
    if (hasMoreLedgerEntries && !loadingMoreLedgerEntries && !ledgerError && !hasOlderLedgerEntry) {
      loadMoreLedgerEntries();
    }
  }, [
    hasOlderLedgerEntry,
    hasMoreLedgerEntries,
    ledgerError,
    loadingMoreLedgerEntries,
    loadMoreLedgerEntries,
  ]);
  const finalMonth = months[months.length - 1];
  const {
    purchaseAnalytics,
    itemPriceObservations,
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
  } = useAnalysisData(household.id, months[0], finalMonth, finalMonth, dimension, itemMetric);
  const evidence = useAnalysisEvidence(household.id, months[0], finalMonth);

  const usesPurchases = dimension !== "category" && dimension !== "member";
  const aggregateError = dimension === "category" ? spendingError : memberSpendingError;
  const aggregateLoading = dimension === "category" ? spendingLoading : memberSpendingLoading;
  const aggregateReady = !aggregateError && !aggregateLoading;
  const data = aggregateReady && usesPurchases ? purchaseAnalytics : null;
  const categories = aggregateReady
    ? sorted(
        Array.from(
          spending
            .reduce((totals, entry) => {
              const id = entry.categoryId ?? "uncategorized";
              const current = totals.get(id) ?? {
                id,
                name: entry.categoryName ?? "未分類",
                amount: 0,
                count: 0,
              };
              current.amount += entry.amountYen;
              totals.set(id, current);
              return totals;
            }, new Map<string, AnalyticsRow>())
            .values(),
        ),
        (a, b) => b.amount - a.amount,
      )
    : [];
  const categorySeries = categories.slice(0, 5).map((category, index) => ({
    id: category.id,
    name: category.name,
    colorIndex: index,
  }));
  const remainingCategories = categories.slice(5);
  if (remainingCategories.length > 0) {
    categorySeries.push({ id: "__other", name: "その他", colorIndex: 5 });
  }
  const categoryAmountsByMonth = new Map<string, Map<string, number>>();
  for (const entry of spending) {
    const amounts = categoryAmountsByMonth.get(entry.period) ?? new Map<string, number>();
    const id = entry.categoryId ?? "uncategorized";
    amounts.set(id, (amounts.get(id) ?? 0) + entry.amountYen);
    categoryAmountsByMonth.set(entry.period, amounts);
  }
  const members = aggregateReady
    ? Array.from(
        memberSpending
          .reduce((totals, entry) => {
            const current = totals.get(entry.memberId) ?? {
              id: entry.memberId,
              name: entry.memberName,
              amount: 0,
              count: 0,
            };
            current.amount += entry.amountYen;
            current.count += 1;
            totals.set(entry.memberId, current);
            return totals;
          }, new Map<string, AnalyticsRow>())
          .values(),
      )
    : [];
  const selectedRows =
    dimension === "category"
      ? categories
      : dimension === "member"
        ? members
        : data
          ? Array.from(data.rows[dimension].values())
          : [];
  const rows = sorted(selectedRows, (a, b) => b.amount - a.amount).slice(0, 8);
  const monthly = aggregateReady
    ? months.map((month) => ({
        month,
        amount:
          dimension === "category"
            ? sum(
                spending.filter((entry) => entry.period === month).map((entry) => entry.amountYen),
              )
            : dimension === "member"
              ? sum(
                  memberSpending
                    .filter((entry) => entry.period === month)
                    .map((entry) => entry.amountYen),
                )
              : sum(
                  data!.monthly[dimension]
                    .filter((entry) => entry.month === month)
                    .map((entry) => entry.amount),
                ),
      }))
    : [];
  const total = aggregateReady ? sum(monthly.map((row) => row.amount)) : 0;
  const displayedPeriodCount = months.length;
  const max = Math.max(...monthly.map((row) => row.amount), 1);
  const categoryMonthTotals = Array.from(categoryAmountsByMonth.values()).map((amounts) =>
    Array.from(amounts.values()).reduce(
      (totals, amount) => {
        if (amount > 0) totals.positive += amount;
        if (amount < 0) totals.negative -= amount;
        return totals;
      },
      { positive: 0, negative: 0 },
    ),
  );
  const maxCategoryPositive = Math.max(...categoryMonthTotals.map(({ positive }) => positive), 1);
  const maxCategoryNegative = Math.max(...categoryMonthTotals.map(({ negative }) => negative), 1);
  const peakRow = rows[0];
  const loadError = budgetError || recurringError;
  const [today, setToday] = useState(formatTokyoDate);
  useEffect(() => {
    const timer = window.setInterval(() => setToday(formatTokyoDate()), 60_000);
    return () => window.clearInterval(timer);
  }, []);
  const activeRecurringExpenses = recurringExpenses.filter(
    (expense) => expense.startsOn <= today && (!expense.endsOn || expense.endsOn >= today),
  );
  const recurringMonthlyAmount = sum(
    activeRecurringExpenses.map((expense) => {
      if (expense.cadence === "WEEKLY") return Math.round((expense.amountYen * 52) / 12);
      if (expense.cadence === "YEARLY") return Math.round(expense.amountYen / 12);
      return expense.amountYen;
    }),
  );

  return (
    <div className="analysis-page">
      <AnalysisControls
        annual={annual}
        setAnnual={setAnnual}
        annualYear={annualYear}
        setAnnualYear={setAnnualYear}
        currentMonth={currentMonth}
        periodLength={periodLength}
        setPeriodLength={setPeriodLength}
        dimension={dimension}
        setDimension={setDimension}
      />
      {loadError && <InlineError message={loadError} />}
      {aggregateError && <InlineError message={aggregateError} />}
      {aggregateLoading && (
        <div className="empty-state compact">
          <BarChart3 />
          <span>集計中</span>
        </div>
      )}
      {aggregateReady && (
        <AnalysisSummary
          annual={annual}
          annualYear={annualYear}
          periodCount={displayedPeriodCount}
          total={total}
          dimension={dimension}
          peakRow={peakRow}
        />
      )}
      <AnalysisBudgetPanel budgets={budgets} household={household} periodMonth={finalMonth} />
      <RecurringExpensePanel
        expenses={activeRecurringExpenses}
        monthlyAmount={recurringMonthlyAmount}
      />
      <AnalysisEvidencePanel {...evidence} />
      <AnalysisLedgerPanel
        entries={ledgerEntries}
        months={months}
        complete={ledgerHistoryComplete}
        error={ledgerError}
      />
      {aggregateReady && (
        <AnalysisBreakdown
          dimension={dimension}
          monthly={monthly}
          categorySeries={categorySeries}
          remainingCategories={remainingCategories}
          categoryAmountsByMonth={categoryAmountsByMonth}
          max={max}
          maxCategoryPositive={maxCategoryPositive}
          maxCategoryNegative={maxCategoryNegative}
          rows={rows}
          hasUnattributedPurchases={hasUnattributedPurchases}
        />
      )}
      {dimension === "item" && (
        <ItemPricePanel
          rows={rows}
          observations={itemPriceObservations}
          itemMetric={itemMetric}
          setItemMetric={setItemMetric}
        />
      )}
    </div>
  );
}
