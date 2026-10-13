import { useEffect, useState } from "react";
import { BookOpen, ChevronRight, Plus, ReceiptJapaneseYen, Upload, Wallet } from "lucide-react";
import {
  monthlyCategorySpendingQuery,
  readGraphql,
  type Household,
  type Purchase,
  type Settlement,
} from "../../../shared/api";
import { settlementQuery } from "../../../entities/settlement";
import { EmptyState } from "../../../shared/ui";
import { useHouseholdPurchases } from "../../../entities/purchase";
import { japanMonth } from "../../../shared/lib";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const sum = (numbers: number[]) => numbers.reduce((total, value) => total + value, 0);

export function HomePage({
  household,
  onNavigate,
}: {
  household: Household;
  onNavigate: (page: "new" | "ledger" | "analysis" | "imports" | "settlement") => void;
}) {
  const { purchases, totalCount } = useHouseholdPurchases(household.id, 8);
  const currentMonth = japanMonth(new Date());
  const monthPurchases = purchases.filter(
    (item) => japanMonth(new Date(item.occurredAt)) === currentMonth,
  );
  const purchaseRevision = purchases
    .map((purchase) => `${purchase.id}:${purchase.revision}:${purchase.totalYen}`)
    .join("|");
  const [monthTotal, setMonthTotal] = useState<number | null>(null);
  const [settlement, setSettlement] = useState<Settlement | null>(null);
  useEffect(() => {
    let active = true;
    void readGraphql<{
      monthlyCategorySpending: Array<{ period: string; amountYen: number }>;
    }>(monthlyCategorySpendingQuery, {
      householdId: household.id,
      from: currentMonth,
      to: currentMonth,
    })
      .then(({ monthlyCategorySpending }) => {
        if (active)
          setMonthTotal(
            sum(
              monthlyCategorySpending
                .filter((entry) => entry.period === currentMonth)
                .map((entry) => entry.amountYen),
            ),
          );
      })
      .catch(() => active && setMonthTotal(null));
    return () => {
      active = false;
    };
  }, [household.id, currentMonth, totalCount, purchaseRevision]);
  useEffect(() => {
    void readGraphql<{ settlement: Settlement }>(settlementQuery, {
      householdId: household.id,
      period: currentMonth,
    })
      .then((data) => setSettlement(data.settlement))
      .catch(() => setSettlement(null));
  }, [household.id, currentMonth]);
  const nextDue = sum(settlement?.lines?.map((line) => line.amountDueYen) ?? []);
  return (
    <>
      <div className="welcome-row">
        <div>
          <span className="eyebrow">
            {new Date().toLocaleDateString("ja-JP", {
              year: "numeric",
              month: "long",
              day: "numeric",
              weekday: "short",
            })}
          </span>
          <h1>おかえりなさい</h1>
        </div>
        <button className="button button-primary" onClick={() => onNavigate("new")}>
          <Plus size={17} /> 買い物を追加
        </button>
      </div>
      <div className="quick-actions">
        <button className="quick-card quick-main" onClick={() => onNavigate("new")}>
          <span className="quick-icon">
            <ReceiptJapaneseYen />
          </span>
          <span>
            <b>買い物を記録</b>
          </span>
          <ChevronRight />
        </button>
        <button className="quick-card" onClick={() => onNavigate("imports")}>
          <span className="quick-icon quick-amber">
            <Upload />
          </span>
          <span>
            <b>レシートを取り込む</b>
          </span>
          <ChevronRight />
        </button>
      </div>
      <div className="overview-grid">
        <section className="panel spend-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">今月の支出</span>
              <h2>{monthTotal === null ? "—" : yen(monthTotal)}</h2>
            </div>
            <span className="period-chip">{Number(currentMonth.slice(5))}月</span>
          </div>
          <MiniBars purchases={purchases} />
          <div className="spend-footer">
            <span>
              品目{" "}
              <b>{monthPurchases.reduce((count, purchase) => count + purchase.items.length, 0)}</b>
            </span>
            <button className="text-button" onClick={() => onNavigate("analysis")}>
              分析を見る <ChevronRight size={15} />
            </button>
          </div>
        </section>
        <section className="panel settle-card">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">次回の入金</span>
              <h2>{yen(nextDue)}</h2>
            </div>
            <span className="settle-icon">
              <Wallet size={18} />
            </span>
          </div>
          <div className="settle-line">
            <span className="status-dot" />
            {settlement
              ? settlement.status === "CLOSED"
                ? "締め済み"
                : "今月分の精算"
              : "精算情報を取得できません"}
          </div>
          <button className="button button-soft" onClick={() => onNavigate("settlement")}>
            精算を確認 <ChevronRight size={16} />
          </button>
        </section>
      </div>
      <div className="section-heading">
        <div>
          <span className="eyebrow">最近の記録</span>
          <h2>買い物</h2>
        </div>
        <button className="text-button" onClick={() => onNavigate("ledger")}>
          帳簿を開く <ChevronRight size={15} />
        </button>
      </div>
      <section className="panel recent-panel">
        {purchases.length === 0 ? (
          <EmptyState
            icon={<BookOpen />}
            title="買い物を記録"
            action="最初の買い物を追加"
            onClick={() => onNavigate("new")}
          />
        ) : (
          purchases
            .slice(0, 5)
            .map((purchase) => <PurchaseRow key={purchase.id} purchase={purchase} />)
        )}
      </section>
    </>
  );
}

function MiniBars({ purchases }: { purchases: Purchase[] }) {
  const totals = Array.from({ length: 7 }, (_, index) => {
    const date = new Date();
    date.setDate(date.getDate() - (6 - index));
    const day = date.toISOString().slice(0, 10);
    return sum(
      purchases
        .filter((purchase) => purchase.occurredAt.slice(0, 10) === day)
        .map((purchase) => purchase.totalYen),
    );
  });
  const peak = Math.max(...totals, 1);
  return (
    <div className="mini-chart" aria-label="直近7日の支出">
      <div className="mini-bars">
        {totals.map((amount, index) => (
          <div key={index} className="bar-track">
            <i
              style={{ height: `${Math.max(5, (amount / peak) * 100)}%` }}
              className={index === 6 ? "bar-current" : ""}
            />
          </div>
        ))}
      </div>
      <div className="chart-labels">
        {["月", "火", "水", "木", "金", "土", "日"].map((day, index) => (
          <span key={index}>{index === 0 ? "7日前" : index === 6 ? "今日" : day}</span>
        ))}
      </div>
    </div>
  );
}

function PurchaseRow({ purchase }: { purchase: Purchase }) {
  return (
    <div className="purchase-row">
      <div className="merchant-icon">{purchase.merchant?.slice(0, 1) || "買"}</div>
      <div className="purchase-description">
        <b>{purchase.merchant || "店舗未入力"}</b>
        <span>
          {purchase.items
            .slice(0, 2)
            .map((item) => item.name)
            .join("、")}
          {purchase.items.length > 2 ? ` ほか${purchase.items.length - 2}品` : ""}
        </span>
      </div>
      <div className="purchase-meta">
        <b>{yen(purchase.totalYen)}</b>
        <span>
          {new Date(purchase.occurredAt).toLocaleDateString("ja-JP", {
            month: "numeric",
            day: "numeric",
          })}
        </span>
      </div>
    </div>
  );
}
