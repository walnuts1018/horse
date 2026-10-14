import { Link } from "@tanstack/react-router";
import { BookOpen, ChevronRight, Plus, Upload, Wallet } from "lucide-react";
import { useEffect, useState } from "react";

import { useHouseholdPurchases } from "../../../entities/purchase";
import { type Household, type Purchase } from "../../../shared/api";
import { japanMonth } from "../../../shared/lib";
import { EmptyState } from "../../../shared/ui";
import {
  readHomeMonthlySpending,
  readHomeSettlement,
  readHomeSummary,
  type HomeSettlement,
  type HomeSummary,
} from "../api/read-home-dashboard";
import { SetupGuidance } from "./setup-guidance";

import "./home-page.css";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const sum = (numbers: number[]) => numbers.reduce((total, value) => total + value, 0);

export function HomePage({
  household,
  onNavigateToPurchase,
}: {
  household: Household;
  onNavigateToPurchase: () => void;
}) {
  const { purchases, totalCount } = useHouseholdPurchases(household.id, 8);
  const [currentMonth] = useState(() => japanMonth(new Date()));
  const [today] = useState(() => new Date());
  const [monthTotal, setMonthTotal] = useState<number | null>(null);
  const [homeSummary, setHomeSummary] = useState<HomeSummary | null>(null);
  const [settlement, setSettlement] = useState<HomeSettlement | null>(null);
  useEffect(() => {
    let active = true;
    void readHomeSummary(household.id)
      .then((summary) => {
        if (active) setHomeSummary(summary);
      })
      .catch(() => active && setHomeSummary(null));
    return () => {
      active = false;
    };
  }, [household.id]);
  useEffect(() => {
    let active = true;
    void readHomeMonthlySpending(household.id, currentMonth)
      .then((monthlyCategorySpending) => {
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
  }, [household.id, currentMonth]);
  useEffect(() => {
    let active = true;
    void readHomeSettlement(household.id, currentMonth)
      .then((result) => {
        if (active) setSettlement(result);
      })
      .catch(() => active && setSettlement(null));
    return () => {
      active = false;
    };
  }, [household.id, currentMonth]);
  const nextDue = sum(settlement?.lines.map((line) => line.amountDueYen) ?? []);
  return (
    <div className="home-page">
      <div className="welcome-row">
        <div>
          <span className="eyebrow">
            {today.toLocaleDateString("ja-JP", {
              year: "numeric",
              month: "long",
              day: "numeric",
              weekday: "short",
            })}
          </span>
          <h1>おかえりなさい</h1>
        </div>
        <Link
          className="button button-primary"
          to="/households/$householdId/new"
          params={{ householdId: household.id }}
          search
          onClick={onNavigateToPurchase}
        >
          <Plus size={17} /> 買い物を追加
        </Link>
      </div>
      <SetupGuidance
        household={household}
        purchaseCount={totalCount}
        onNavigateToPurchase={onNavigateToPurchase}
      />
      <div className="overview-grid">
        <section className="panel spend-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">今月の支出</span>
              <h2>{monthTotal === null ? "—" : yen(monthTotal)}</h2>
            </div>
            <span className="period-chip">{Number(currentMonth.slice(5))}月</span>
          </div>
          <MiniBars dailySpending={homeSummary?.dailySpending ?? []} />
          <div className="spend-footer">
            <span>
              品目 <b>{homeSummary?.monthlyItemCount ?? "—"}</b>
            </span>
            <Link
              className="text-button"
              to="/households/$householdId/analysis"
              params={{ householdId: household.id }}
              search
            >
              分析を見る <ChevronRight size={15} />
            </Link>
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
              ? settlement.__typename === "ClosedSettlement"
                ? "締め済み"
                : "今月分の精算"
              : "精算情報を取得できません"}
          </div>
          <Link
            className="button button-soft"
            to="/households/$householdId/settlement"
            params={{ householdId: household.id }}
            search
          >
            精算を確認 <ChevronRight size={16} />
          </Link>
        </section>
      </div>
      <div className="quick-actions">
        <Link
          className="quick-card import-card"
          to="/households/$householdId/imports"
          params={{ householdId: household.id }}
          search
        >
          <span className="quick-icon" aria-hidden="true">
            <Upload />
          </span>
          <span>
            <b>レシートを取り込む</b>
          </span>
          <ChevronRight aria-hidden="true" />
        </Link>
      </div>
      <div className="section-heading">
        <div>
          <span className="eyebrow">最近の記録</span>
          <h2>買い物</h2>
        </div>
        <Link
          className="text-button"
          to="/households/$householdId/ledger"
          params={{ householdId: household.id }}
          search
        >
          帳簿を開く <ChevronRight size={15} />
        </Link>
      </div>
      <section className="panel recent-panel">
        {purchases.length === 0 ? (
          <EmptyState
            icon={<BookOpen />}
            title="買い物を記録"
            action="最初の買い物を追加"
            actionLink={
              <Link
                className="text-button"
                to="/households/$householdId/new"
                params={{ householdId: household.id }}
                search
                onClick={onNavigateToPurchase}
              >
                最初の買い物を追加 <ChevronRight size={15} />
              </Link>
            }
          />
        ) : (
          purchases
            .slice(0, 8)
            .map((purchase) => <PurchaseRow key={purchase.id} purchase={purchase} />)
        )}
      </section>
    </div>
  );
}

function MiniBars({ dailySpending }: { dailySpending: HomeSummary["dailySpending"] }) {
  const totals = dailySpending.map((day) => day.amountYen);
  const peak = Math.max(...totals, 1);
  const spendingSummary = dailySpending
    .map((day) => {
      const date = new Intl.DateTimeFormat("ja-JP", {
        year: "numeric",
        month: "long",
        day: "numeric",
        timeZone: "UTC",
      }).format(new Date(`${day.date}T00:00:00Z`));
      return `${date} ${day.amountYen.toLocaleString("ja-JP")}円`;
    })
    .join("、");
  return (
    <figure className="mini-chart" aria-label={`直近7日の支出。${spendingSummary}`}>
      <div className="mini-bars">
        {dailySpending.map((day, index) => (
          <div key={day.date} className="bar-track" title={`${day.date}: ${yen(day.amountYen)}`}>
            <i
              style={{ height: `${Math.max(5, (day.amountYen / peak) * 100)}%` }}
              className={index === dailySpending.length - 1 ? "bar-current" : ""}
            />
          </div>
        ))}
      </div>
      <div className="chart-labels">
        {dailySpending.map((day) => (
          <span key={day.date} title={day.date}>
            {new Date(`${day.date}T00:00:00Z`).toLocaleDateString("ja-JP", {
              weekday: "short",
              timeZone: "UTC",
            })}
          </span>
        ))}
      </div>
    </figure>
  );
}

function PurchaseRow({ purchase }: { purchase: Purchase }) {
  return (
    <div className="purchase-row">
      <div className="merchant-icon">{purchase.merchant.slice(0, 1) || "買"}</div>
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
