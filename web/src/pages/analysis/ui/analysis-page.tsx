import { BarChart3 } from "lucide-react";
import { useEffect, useState } from "react";
import {
  monthlyCategorySpendingQuery,
  readGraphql,
  type Household,
  type Purchase,
} from "../../../shared/api";
import { useHouseholdPurchases } from "../../../shared/api";
import { InlineError } from "../../../shared/ui";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const sum = (numbers: number[]) => numbers.reduce((total, value) => total + value, 0);
const japanMonth = (date: Date) =>
  new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Tokyo",
    year: "numeric",
    month: "2-digit",
  }).format(date);
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

type Dimension = "category" | "merchant" | "item" | "pool" | "account";
type AnalyticsRow = { id: string; name: string; amount: number; count: number };
type MonthlyEntry = { month: string; id: string; amount: number };

function purchaseDimensions(purchases: Purchase[], household: Household) {
  const rows: Record<Exclude<Dimension, "category">, Map<string, AnalyticsRow>> = {
    merchant: new Map(),
    item: new Map(),
    pool: new Map(),
    account: new Map(),
  };
  const monthly: Record<Exclude<Dimension, "category">, MonthlyEntry[]> = {
    merchant: [],
    item: [],
    pool: [],
    account: [],
  };
  const add = (
    dimension: Exclude<Dimension, "category">,
    id: string,
    name: string,
    amount: number,
    month: string,
  ) => {
    const row = rows[dimension].get(id) ?? { id, name, amount: 0, count: 0 };
    row.amount += amount;
    row.count += 1;
    rows[dimension].set(id, row);
    monthly[dimension].push({ month, id, amount });
  };
  for (const purchase of purchases) {
    const month = japanMonth(new Date(purchase.occurredAt));
    add(
      "merchant",
      purchase.merchant || "__unknown",
      purchase.merchant || "店舗未入力",
      purchase.totalYen,
      month,
    );
    for (const item of purchase.items) {
      add("item", item.productVariantId ?? item.name, item.name, item.amountYen, month);
      for (const allocation of item.allocations) {
        const pool = household.pools.find((candidate) => candidate.id === allocation.poolId);
        const amount =
          allocation.amountYen ??
          Math.round(
            (item.amountYen * allocation.weight) /
              Math.max(1, sum(item.allocations.map((entry) => entry.weight))),
          );
        add(
          "pool",
          allocation.poolId,
          pool?.name ?? allocation.poolName ?? "プール",
          amount,
          month,
        );
      }
    }
    const payments = purchase.paymentAllocations?.length
      ? purchase.paymentAllocations
      : purchase.paymentAccountId
        ? [{ paymentAccountId: purchase.paymentAccountId, amountYen: purchase.totalYen }]
        : [];
    for (const payment of payments) {
      const account = household.accounts.find(
        (candidate) => candidate.id === payment.paymentAccountId,
      );
      add("account", payment.paymentAccountId, account?.name ?? "決済元", payment.amountYen, month);
    }
  }
  return { rows, monthly };
}

export function AnalysisPage({ household }: { household: Household }) {
  const [periodLength, setPeriodLength] = useState(6);
  const [dimension, setDimension] = useState<Dimension>("category");
  const currentMonth = japanMonth(new Date());
  const months = monthRange(currentMonth, periodLength);
  const from = `${months[0]}-01T00:00:00+09:00`;
  const to = `${nextMonth(currentMonth)}-01T00:00:00+09:00`;
  const { purchases, loading, loadingMore, error, moreError } = useHouseholdPurchases(
    household.id,
    100,
    {
      from,
      to,
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
  useEffect(() => {
    let active = true;
    setSpendingLoading(true);
    setSpendingError("");
    void readGraphql<{ monthlyCategorySpending: typeof spending }>(monthlyCategorySpendingQuery, {
      householdId: household.id,
      from: months[0],
      to: currentMonth,
    })
      .then((result) => active && setSpending(result.monthlyCategorySpending))
      .catch((reason) => active && setSpendingError((reason as Error).message))
      .finally(() => active && setSpendingLoading(false));
    return () => {
      active = false;
    };
  }, [household.id, months[0], currentMonth]);

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
  const selectedRows =
    dimension === "category" ? categories : Array.from(data.rows[dimension].values());
  const rows = selectedRows.sort((a, b) => b.amount - a.amount).slice(0, 8);
  const monthly = months.map((month) => ({
    month,
    amount:
      dimension === "category"
        ? sum(spending.filter((entry) => entry.period === month).map((entry) => entry.amountYen))
        : sum(
            data.monthly[dimension]
              .filter((entry) => entry.month === month)
              .map((entry) => entry.amount),
          ),
  }));
  const total = sum(monthly.map((row) => row.amount));
  const max = Math.max(...monthly.map((row) => row.amount), 1);
  const peakRow = rows[0];
  const labels: Record<Dimension, string> = {
    category: "カテゴリ",
    merchant: "店舗",
    item: "品目",
    pool: "負担先",
    account: "決済元",
  };
  const isLoading = loading || loadingMore || spendingLoading;
  const loadError = error || moreError || spendingError;

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
              value={periodLength}
              onChange={(event) => setPeriodLength(Number(event.target.value))}
            >
              <option value={3}>3か月</option>
              <option value={6}>6か月</option>
              <option value={12}>12か月</option>
            </select>
          </label>
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
          <small>{periodLength}か月</small>
        </section>
        <section className="panel summary-card">
          <span className="eyebrow">月平均</span>
          <h2>{yen(Math.round(total / periodLength))}</h2>
          <small>{periodLength}か月</small>
        </section>
        <section className="panel summary-card">
          <span className="eyebrow">最多の{labels[dimension]}</span>
          <h2>{peakRow?.name ?? "—"}</h2>
          <small>{peakRow ? yen(peakRow.amount) : "記録なし"}</small>
        </section>
      </div>
      <div className="analysis-grid">
        <section className="panel analysis-chart-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">月別推移</span>
              <h2>{labels[dimension]}の支出</h2>
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
              <span className="eyebrow">品目の購入額</span>
              <h2>1回あたりの平均</h2>
            </div>
          </div>
          {rows.slice(0, 5).map((row) =>
            (() => {
              const prices = data.monthly.item
                .filter((entry) => entry.id === row.id)
                .sort((left, right) => left.month.localeCompare(right.month));
              const firstPrice = prices[0]?.amount ?? 0;
              const latestPrice = prices[prices.length - 1]?.amount ?? 0;
              const change =
                firstPrice > 0 ? Math.round(((latestPrice - firstPrice) / firstPrice) * 100) : 0;
              return (
                <div className="price-row" key={row.id}>
                  <span>{row.name}</span>
                  <strong>{yen(Math.round(row.amount / Math.max(1, row.count)))}</strong>
                  <small>{row.count}件</small>
                  {prices.length > 1 && (
                    <small className="price-change">
                      初回価格 {yen(firstPrice)}、最新価格 {yen(latestPrice)}（
                      {change > 0 ? "+" : ""}
                      {change}%）
                    </small>
                  )}
                </div>
              );
            })(),
          )}
        </section>
      )}
    </>
  );
}
