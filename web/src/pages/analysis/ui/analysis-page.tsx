import { BarChart3, ChevronDown, Settings2 } from "lucide-react";
import { useEffect, useState } from "react";
import { monthlyCategorySpendingQuery, readGraphql, type Household } from "../../../shared/api";
import { InlineError } from "../../../shared/ui";
import { useHouseholdPurchases } from "../../../shared/api";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const sum = (numbers: number[]) => numbers.reduce((total, value) => total + value, 0);

export function AnalysisPage({ household }: { household: Household }) {
  const { purchases, loading, error } = useHouseholdPurchases(household.id, 300);
  const [spending, setSpending] = useState<
    Array<{
      period: string;
      categoryId?: string | null;
      categoryName?: string | null;
      amountYen: number;
    }>
  >([]);
  const months = Array.from({ length: 6 }, (_, index) => {
    const date = new Date();
    date.setMonth(date.getMonth() - (5 - index));
    return date.toISOString().slice(0, 7);
  });
  const monthly = months.map((month) => ({
    month,
    amount: sum(spending.filter((entry) => entry.period === month).map((entry) => entry.amountYen)),
  }));
  const categories = Array.from(
    spending
      .reduce((totals, entry) => {
        const id = entry.categoryId ?? "uncategorized";
        const current = totals.get(id) ?? { id, name: entry.categoryName ?? "未分類", amount: 0 };
        current.amount += entry.amountYen;
        totals.set(id, current);
        return totals;
      }, new Map<string, { id: string; name: string; amount: number }>())
      .values(),
  ).sort((a, b) => b.amount - a.amount);
  useEffect(() => {
    void readGraphql<{ monthlyCategorySpending: typeof spending }>(monthlyCategorySpendingQuery, {
      householdId: household.id,
      from: months[0],
      to: months[months.length - 1],
    })
      .then((result) => setSpending(result.monthlyCategorySpending))
      .catch(() => setSpending([]));
  }, [household.id, months[0], months[months.length - 1]]);
  const max = Math.max(...monthly.map((row) => row.amount), 1);
  const total = sum(monthly.map((row) => row.amount));
  return (
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">支出の傾向</span>
          <h1>分析</h1>
          <p>カテゴリと月ごとの支出</p>
        </div>
        <button className="button button-soft">
          <span>過去6か月</span>
          <ChevronDown size={15} />
        </button>
      </div>
      {error && <InlineError message={error} />}
      <div className="analysis-summary">
        <section className="panel summary-card">
          <span className="eyebrow">記録期間の合計</span>
          <h2>{yen(total)}</h2>
          <small>過去6か月</small>
        </section>
        <section className="panel summary-card">
          <span className="eyebrow">1回あたり</span>
          <h2>{yen(Math.round(total / 6))}</h2>
          <small>月あたり</small>
        </section>
        <section className="panel summary-card">
          <span className="eyebrow">カテゴリ数</span>
          <h2>{categories.length}</h2>
          <small>支出のあるカテゴリ</small>
        </section>
      </div>
      <div className="analysis-grid">
        <section className="panel analysis-chart-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">月別推移</span>
              <h2>支出</h2>
            </div>
            <span className="chart-legend">
              <i />
              合計
            </span>
          </div>
          {loading ? (
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
              <span className="eyebrow">品目の分類</span>
              <h2>カテゴリ別</h2>
            </div>
            <button className="icon-button" aria-label="カテゴリ設定">
              <Settings2 size={17} />
            </button>
          </div>
          {categories.length === 0 ? (
            <div className="empty-state compact">
              <BarChart3 />
              <span>カテゴリ別の支出はありません</span>
            </div>
          ) : (
            categories.slice(0, 7).map((category, index) => (
              <div className="category-row" key={category.id}>
                <span className={`category-swatch swatch-${index % 6}`} />
                <span>{category.name}</span>
                <i>
                  <b
                    style={{
                      width: `${Math.max(8, (category.amount / (categories[0]?.amount || 1)) * 100)}%`,
                    }}
                  />
                </i>
                <strong>{yen(category.amount)}</strong>
              </div>
            ))
          )}
        </section>
      </div>
      <section className="panel burden-panel">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">負担先</span>
            <h2>プールごとの支出</h2>
          </div>
          <span className="period-chip">記録期間</span>
        </div>
        <div className="burden-grid">
          {household.pools.map((pool, index) => {
            const amount = sum(
              purchases.flatMap((purchase) =>
                purchase.items.flatMap((item) =>
                  item.allocations
                    .filter((allocation) => allocation.poolId === pool.id)
                    .map(
                      (allocation) =>
                        allocation.amountYen ??
                        Math.round(
                          (item.amountYen * allocation.weight) /
                            Math.max(1, sum(item.allocations.map((a) => a.weight))),
                        ),
                    ),
                ),
              ),
            );
            return (
              <div className="burden-card" key={pool.id}>
                <span className={`burden-dot swatch-${index % 6}`} />
                <span>{pool.name}</span>
                <b>{yen(amount)}</b>
              </div>
            );
          })}
        </div>
      </section>
    </>
  );
}
