import { BarChart3 } from "lucide-react";
import { useEffect, useState } from "react";
import {
  readGraphql,
  type Household,
  type Budget,
  type Purchase,
  type RecurringExpense,
  monthlyCategorySpendingQuery,
} from "../../../shared/api";
import { monthlyMemberSpendingQuery } from "../api/operations";
import { budgetsQuery } from "../../../entities/budget";
import { readRecurringExpenses } from "../../../entities/recurring-expense";
import { useHouseholdPurchases } from "../../../entities/purchase";
import { InlineError } from "../../../shared/ui";
import { japanMonth } from "../../../shared/lib";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const sum = (numbers: number[]) => numbers.reduce((total, value) => total + value, 0);
const monthRange = (current: string, count: number) => {
  const [year, month] = current.split("-").map(Number);
  const endIndex = year * 12 + month - 1;
  return Array.from({ length: count }, (_, index) => {
    const serial = endIndex - count + index + 1;
    return `${Math.floor(serial / 12)}-${String((serial % 12) + 1).padStart(2, "0")}`;
  });
};
const nextMonth = (month: string) => {
  const [year, monthNumber] = month.split("-").map(Number);
  const serial = year * 12 + monthNumber;
  return `${Math.floor(serial / 12)}-${String((serial % 12) + 1).padStart(2, "0")}`;
};

type Dimension = "category" | "merchant" | "item" | "pool" | "account" | "member";
type PurchaseDimension = Exclude<Dimension, "category" | "member">;
type ItemMetric = "amount" | "effective" | "printed";
type AnalyticsRow = { id: string; name: string; amount: number; count: number };
type MonthlyEntry = { month: string; id: string; amount: number; occurredAt: string };
const itemAnalysisId = (item: Purchase["items"][number]) =>
  `${item.productVariantId ?? item.name}\u0000${item.quantityUnit ?? ""}`;

function purchaseDimensions(purchases: Purchase[], household: Household) {
  const rows: Record<PurchaseDimension, Map<string, AnalyticsRow>> = {
    merchant: new Map(),
    item: new Map(),
    pool: new Map(),
    account: new Map(),
  };
  const monthly: Record<PurchaseDimension, MonthlyEntry[]> = {
    merchant: [],
    item: [],
    pool: [],
    account: [],
  };
  const add = (
    dimension: PurchaseDimension,
    id: string,
    name: string,
    amount: number,
    month: string,
    occurredAt: string,
  ) => {
    const row = rows[dimension].get(id) ?? { id, name, amount: 0, count: 0 };
    row.amount += amount;
    row.count += 1;
    rows[dimension].set(id, row);
    monthly[dimension].push({ month, id, amount, occurredAt });
  };
  for (const purchase of purchases) {
    if (purchase.status !== "POSTED" && purchase.status !== "CLOSED") continue;
    const month = japanMonth(new Date(purchase.occurredAt));
    add(
      "merchant",
      purchase.merchant || "__unknown",
      purchase.merchant || "店舗未入力",
      purchase.totalYen,
      month,
      purchase.occurredAt,
    );
    for (const item of purchase.items) {
      const itemId = itemAnalysisId(item);
      add(
        "item",
        itemId,
        `${item.name}${item.quantityUnit ? ` · ${item.quantityUnit}` : " · 単位不明"}`,
        item.amountYen,
        month,
        purchase.occurredAt,
      );
      for (const allocation of item.allocations) {
        const amount =
          allocation.amountYen ??
          Math.round(
            (item.amountYen * allocation.weight) /
              Math.max(1, sum(item.allocations.map((entry) => entry.weight))),
          );
        add(
          "pool",
          allocation.poolId,
          allocation.poolName,
          amount,
          month,
          purchase.occurredAt,
        );
      }
    }
    for (const payment of purchase.paymentAllocations) {
      const account = household.accounts.find(
        (candidate) => candidate.id === payment.paymentAccountId,
      );
      add(
        "account",
        payment.paymentAccountId,
        account?.name ?? "決済元",
        payment.amountYen,
        month,
        purchase.occurredAt,
      );
    }
  }
  return { rows, monthly };
}

function itemPriceSeries(
  purchases: Purchase[],
  itemId: string,
  metric: Exclude<ItemMetric, "amount">,
) {
  const series = new Map<
    string,
    { label: string; points: Array<{ occurredAt: string; value: string }> }
  >();
  let unknownCount = 0;
  for (const purchase of purchases) {
    if (purchase.status !== "POSTED" && purchase.status !== "CLOSED") continue;
    for (const item of purchase.items) {
      if (itemAnalysisId(item) !== itemId) continue;
      let key: string;
      let label: string;
      let value: string;
      if (metric === "effective") {
        if (!item.effectiveUnitPriceNumeratorYen || !item.effectiveUnitPriceDenominator) {
          unknownCount += 1;
          continue;
        }
        key = `effective:${item.quantityUnit}`;
        label = `実効単価 · ${item.quantityUnit}`;
        value = `${item.effectiveUnitPriceNumeratorYen}/${item.effectiveUnitPriceDenominator}`;
      } else {
        if (
          item.printedUnitPriceYen == null ||
          !item.printedUnitPriceQuantityNumerator ||
          !item.printedUnitPriceQuantityDenominator ||
          !item.printedUnitPriceUnit
        ) {
          unknownCount += 1;
          continue;
        }
        key = `printed:${item.printedUnitPriceQuantityNumerator}/${item.printedUnitPriceQuantityDenominator}:${item.printedUnitPriceUnit}`;
        label = `印字単価 · ${item.printedUnitPriceQuantityNumerator}/${item.printedUnitPriceQuantityDenominator}${item.printedUnitPriceUnit}`;
        value = String(item.printedUnitPriceYen);
      }
      const current = series.get(key) ?? { label, points: [] };
      current.points.push({ occurredAt: purchase.occurredAt, value });
      series.set(key, current);
    }
  }
  for (const current of series.values())
    current.points.sort(
      (left, right) => Date.parse(left.occurredAt) - Date.parse(right.occurredAt),
    );
  return { series: Array.from(series.values()), unknownCount };
}

const formatExactYen = (numerator: string, denominator: string) => {
  const amount = new Intl.NumberFormat("ja-JP").format(BigInt(numerator));
  return denominator === "1" ? `¥${amount}` : `¥${amount}/${denominator}`;
};

export function AnalysisPage({ household }: { household: Household }) {
  const [periodLength, setPeriodLength] = useState(6);
  const [annual, setAnnual] = useState(false);
  const [annualYear, setAnnualYear] = useState(Number(japanMonth(new Date()).slice(0, 4)));
  const [dimension, setDimension] = useState<Dimension>("category");
  const [itemMetric, setItemMetric] = useState<ItemMetric>("amount");
  const currentMonth = japanMonth(new Date());
  const months = annual
    ? monthRange(`${annualYear}-12`, 12)
    : monthRange(currentMonth, periodLength);
  const finalMonth = months[months.length - 1];
  const from = `${months[0]}-01T00:00:00+09:00`;
  const to = `${nextMonth(finalMonth)}-01T00:00:00+09:00`;
  const { purchases, loadingMore, moreError } = useHouseholdPurchases(household.id, 100, {
    from,
    to,
    loadAll: true,
  });
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
    setSpendingLoading(true);
    setSpendingError("");
    void readGraphql<{ monthlyCategorySpending: typeof spending }>(monthlyCategorySpendingQuery, {
      householdId: household.id,
      from: months[0],
      to: finalMonth,
    })
      .then((result) => active && setSpending(result.monthlyCategorySpending))
      .catch((reason) => active && setSpendingError((reason as Error).message))
      .finally(() => active && setSpendingLoading(false));
    return () => {
      active = false;
    };
  }, [household.id, months[0], finalMonth]);
  useEffect(() => {
    if (dimension !== "member") {
      setMemberSpendingLoading(false);
      setMemberSpendingError("");
      return;
    }
    let active = true;
    setMemberSpendingLoading(true);
    setMemberSpendingError("");
    setHasUnattributedPurchases(false);
    void readGraphql<{
      monthlyMemberSpending: {
        items: Array<{
          period: string;
          memberId: string;
          memberName: string;
          amountYen: number;
        }>;
        hasUnattributedPurchases: boolean;
      };
    }>(monthlyMemberSpendingQuery, {
      householdId: household.id,
      from: months[0],
      to: finalMonth,
    })
      .then((result) => {
        if (!active) return;
        setMemberSpending(result.monthlyMemberSpending.items);
        setHasUnattributedPurchases(result.monthlyMemberSpending.hasUnattributedPurchases);
      })
      .catch((reason) => active && setMemberSpendingError((reason as Error).message))
      .finally(() => active && setMemberSpendingLoading(false));
    return () => {
      active = false;
    };
  }, [dimension, household.id, months[0], finalMonth]);
  useEffect(() => {
    let active = true;
    setBudgetError("");
    void readGraphql<{ budgets: Budget[] }>(budgetsQuery, {
      householdId: household.id,
      period: currentMonth,
    })
      .then((result) => active && setBudgets(result.budgets))
      .catch((reason) => active && setBudgetError((reason as Error).message));
    return () => {
      active = false;
    };
  }, [household.id, currentMonth]);
  useEffect(() => {
    let active = true;
    setRecurringError("");
    void readRecurringExpenses(household.id)
      .then((expenses) => active && setRecurringExpenses(expenses))
      .catch((reason) => active && setRecurringError((reason as Error).message));
    return () => {
      active = false;
    };
  }, [household.id]);

  const data = purchaseDimensions(purchases, household);
  const categories = Array.from(
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
  ).sort((a, b) => b.amount - a.amount);
  const members = Array.from(
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
  );
  const selectedRows =
    dimension === "category"
      ? categories
      : dimension === "member"
        ? members
        : Array.from(data.rows[dimension].values());
  const rows = selectedRows.sort((a, b) => b.amount - a.amount).slice(0, 8);
  const monthly = months.map((month) => ({
    month,
    amount:
      dimension === "category"
        ? sum(spending.filter((entry) => entry.period === month).map((entry) => entry.amountYen))
        : dimension === "member"
          ? sum(
              memberSpending
                .filter((entry) => entry.period === month)
                .map((entry) => entry.amountYen),
            )
          : sum(
              data.monthly[dimension]
                .filter((entry) => entry.month === month)
                .map((entry) => entry.amount),
            ),
  }));
  const total = sum(monthly.map((row) => row.amount));
  const displayedPeriodCount = months.length;
  const max = Math.max(...monthly.map((row) => row.amount), 1);
  const peakRow = rows[0];
  const labels: Record<Dimension, string> = {
    category: "カテゴリ",
    merchant: "店舗",
    item: "品目",
    pool: "負担先",
    account: "決済元",
    member: "メンバー負担",
  };
  const isLoading =
    loadingMore || spendingLoading || (dimension === "member" && memberSpendingLoading);
  const loadError =
    moreError || spendingError || budgetError || recurringError || memberSpendingError;
  const today = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Tokyo",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date());
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
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">支出の傾向</span>
          <h1>分析</h1>
        </div>
        <div className="analysis-controls">
          <label className="select-filter">
            <span>期間</span>
            <select
              value={annual ? "annual" : periodLength}
              onChange={(event) => {
                setAnnual(event.target.value === "annual");
                if (event.target.value !== "annual") setPeriodLength(Number(event.target.value));
              }}
            >
              <option value="annual">年次</option>
              <option value={3}>3か月</option>
              <option value={6}>6か月</option>
              <option value={12}>12か月</option>
            </select>
          </label>
          {annual && (
            <label className="select-filter">
              <span>年</span>
              <select
                value={annualYear}
                onChange={(event) => setAnnualYear(Number(event.target.value))}
              >
                {Array.from(
                  { length: 10 },
                  (_, index) => Number(currentMonth.slice(0, 4)) - index,
                ).map((year) => (
                  <option value={year} key={year}>
                    {year}年
                  </option>
                ))}
              </select>
            </label>
          )}
          <label className="select-filter">
            <span>集計</span>
            <select
              value={dimension}
              onChange={(event) => setDimension(event.target.value as Dimension)}
            >
              {Object.entries(labels).map(([value, label]) => (
                <option value={value} key={value}>
                  {label}
                </option>
              ))}
            </select>
          </label>
        </div>
      </div>
      {loadError && <InlineError message={loadError} />}
      <div className="analysis-summary">
        <section className="panel summary-card">
          <span className="eyebrow">期間合計</span>
          <h2>{yen(total)}</h2>
          <small>{annual ? `${annualYear}年` : `${displayedPeriodCount}か月`}</small>
        </section>
        <section className="panel summary-card">
          <span className="eyebrow">月平均</span>
          <h2>{yen(Math.round(total / displayedPeriodCount))}</h2>
          <small>{displayedPeriodCount}か月</small>
        </section>
        <section className="panel summary-card">
          <span className="eyebrow">最多の{labels[dimension]}</span>
          <h2>{peakRow?.name ?? "—"}</h2>
          <small>{peakRow ? yen(peakRow.amount) : "記録なし"}</small>
        </section>
      </div>
      {budgets.length > 0 && (
        <section className="panel analysis-budget-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">{currentMonth}の予算</span>
              <h2>予算と実績</h2>
            </div>
          </div>
          {budgets.map((budget) => {
            const ratio =
              budget.amountYen > 0
                ? Math.min(100, Math.max(0, (budget.actualYen / budget.amountYen) * 100))
                : 0;
            const category = household.categories.find(
              (candidate) => candidate.id === budget.categoryId,
            );
            return (
              <div className="analysis-budget-row" key={budget.id}>
                <div>
                  <b>{category?.name ?? budget.name}</b>
                  <small>
                    {yen(budget.actualYen)} / {yen(budget.amountYen)}
                  </small>
                </div>
                <progress max="100" value={ratio} aria-label={`${budget.name}の予算使用率`} />
                <strong className={budget.remainingYen < 0 ? "settlement-credit" : undefined}>
                  {budget.remainingYen < 0
                    ? `${yen(-budget.remainingYen)}超過`
                    : `残り${yen(budget.remainingYen)}`}
                </strong>
              </div>
            );
          })}
        </section>
      )}
      <section className="panel analysis-budget-panel">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">定期支出</span>
            <h2>月額換算 {yen(recurringMonthlyAmount)}</h2>
          </div>
        </div>
        {activeRecurringExpenses.length === 0 ? (
          <div className="empty-state compact">
            <BarChart3 />
            <span>定期支出の記録</span>
          </div>
        ) : (
          activeRecurringExpenses.map((expense) => (
            <div className="analysis-budget-row" key={expense.id}>
              <div>
                <b>{expense.name}</b>
                <small>{expense.merchant || expense.cadence}</small>
              </div>
              <strong>{yen(expense.amountYen)}</strong>
              <small>
                {expense.cadence === "WEEKLY"
                  ? "毎週"
                  : expense.cadence === "YEARLY"
                    ? "毎年"
                    : "毎月"}
              </small>
            </div>
          ))
        )}
      </section>
      <div className="analysis-grid">
        <section className="panel analysis-chart-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">月別推移</span>
              <h2>{labels[dimension]}の支出</h2>
              {dimension === "member" && (
                <small>
                  購入時の負担スナップショット
                  {hasUnattributedPurchases && " · 履歴不明の購入を含みません"}
                </small>
              )}
            </div>
            <span className="chart-legend">
              <i />
              合計
            </span>
          </div>
          {isLoading ? (
            <div className="skeleton-list">
              <i />
              <i />
              <i />
            </div>
          ) : (
            <div className="monthly-chart">
              {monthly.map((row) => (
                <div className="month-column" key={row.month}>
                  <b>{yen(row.amount)}</b>
                  <i style={{ height: `${Math.max(3, (row.amount / max) * 100)}%` }} />
                  <span>{Number(row.month.slice(5))}月</span>
                </div>
              ))}
            </div>
          )}
        </section>
        <section className="panel category-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">内訳</span>
              <h2>{labels[dimension]}別</h2>
            </div>
          </div>
          {rows.length === 0 ? (
            <div className="empty-state compact">
              <BarChart3 />
              <span>支出の記録</span>
            </div>
          ) : (
            rows.map((row, index) => (
              <div className="category-row" key={row.id}>
                <span className={`category-swatch swatch-${index % 6}`} />
                <span>{row.name}</span>
                <i>
                  <b
                    style={{
                      width: `${Math.max(8, (row.amount / (rows[0]?.amount || 1)) * 100)}%`,
                    }}
                  />
                </i>
                <strong>{yen(row.amount)}</strong>
              </div>
            ))
          )}
        </section>
      </div>
      {dimension === "item" && rows.length > 0 && (
        <section className="panel analysis-price-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">品目の推移</span>
              <h2>
                {itemMetric === "amount"
                  ? "購入額"
                  : itemMetric === "effective"
                    ? "実効単価"
                    : "印字単価"}
              </h2>
            </div>
            <label className="select-filter">
              <span>価格指標</span>
              <select
                aria-label="価格指標"
                value={itemMetric}
                onChange={(event) => setItemMetric(event.target.value as ItemMetric)}
              >
                <option value="amount">購入額</option>
                <option value="effective">実効単価</option>
                <option value="printed">印字単価</option>
              </select>
            </label>
          </div>
          {rows.slice(0, 5).map((row) => {
            const prices = data.monthly.item
              .filter((entry) => entry.id === row.id)
              .sort((left, right) => Date.parse(left.occurredAt) - Date.parse(right.occurredAt));
            const trend =
              itemMetric === "amount" ? null : itemPriceSeries(purchases, row.id, itemMetric);
            return (
              <div className="price-row" key={row.id}>
                <span>{row.name}</span>
                {itemMetric === "amount" ? (
                  <>
                    <strong>{yen(Math.round(row.amount / Math.max(1, row.count)))}</strong>
                    <small>{row.count}件</small>
                    {prices.length > 1 && (
                      <small className="price-change">
                        初回購入額 {yen(prices[0].amount)}、最新購入額{" "}
                        {yen(prices[prices.length - 1].amount)}
                      </small>
                    )}
                  </>
                ) : (
                  <>
                    {trend?.series.map((series) => {
                      const first = series.points[0];
                      const last = series.points[series.points.length - 1];
                      const format = (value: string) => {
                        const [numerator, denominator] = value.split("/");
                        return denominator
                          ? formatExactYen(numerator, denominator)
                          : yen(Number(value));
                      };
                      return (
                        <small className="price-change" key={series.label}>
                          {series.label}: {format(first.value)} → {format(last.value)} ·{" "}
                          {series.points.length}件
                        </small>
                      );
                    })}
                    {trend?.unknownCount ? <small>単価不明 {trend.unknownCount}件</small> : null}
                    {!trend?.series.length && !trend?.unknownCount && <small>記録なし</small>}
                  </>
                )}
              </div>
            );
          })}
        </section>
      )}
    </>
  );
}
