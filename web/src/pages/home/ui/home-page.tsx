import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { BookOpen, Camera, ChevronRight, Plus, Upload, Wallet } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useLazyLoadQuery } from "react-relay";

import { accountBalancesQuery } from "../../../entities/ledger";
import { useHouseholdPurchases } from "../../../entities/purchase";
import {
  isSharedPool,
  type AccountBalancesQuery,
  type Household,
  type HomeDashboardQuery,
  type Purchase,
} from "../../../shared/api";
import { japanMonth } from "../../../shared/lib";
import { EmptyState } from "../../../shared/ui";
import { homeDashboardQuery } from "../model/home-summary";
import { pendingImportMessage, readPendingImportStatus } from "../model/pending-imports";
import { summarizeSettlementDues } from "../model/settlement-dues";
import { SettlementStatus } from "./settlement-status";
import { SetupGuidance } from "./setup-guidance";

import "./home-page.css";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const sum = (numbers: number[]) => numbers.reduce((total, value) => total + value, 0);
const millisecondsUntilJapanMidnight = (date: Date) => {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: "Asia/Tokyo",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  }).formatToParts(date);
  const getPart = (type: Intl.DateTimeFormatPartTypes) =>
    Number(parts.find((part) => part.type === type)?.value ?? 0);
  const elapsedToday = getPart("hour") * 60 * 60 + getPart("minute") * 60 + getPart("second");
  return (24 * 60 * 60 - elapsedToday) * 1000 - date.getMilliseconds();
};
type HomeSummary = {
  dailySpending: Array<{ date: string; amountYen: number }>;
  monthlyItemCount: number;
};

export function HomePage({
  household,
  onNavigateToPurchase,
}: {
  household: Household;
  onNavigateToPurchase: () => void;
}) {
  const { purchases, totalCount } = useHouseholdPurchases(household.id, 8);
  const [today, setToday] = useState(() => new Date());
  const [refreshKey, setRefreshKey] = useState(0);
  const currentMonth = japanMonth(today);
  useEffect(() => {
    let midnightTimer: number;
    function refresh() {
      setToday(new Date());
      setRefreshKey((key) => key + 1);
      window.clearTimeout(midnightTimer);
      midnightTimer = window.setTimeout(refresh, millisecondsUntilJapanMidnight(new Date()));
    }
    const handleVisibilityChange = () => {
      if (document.visibilityState === "visible") refresh();
    };
    midnightTimer = window.setTimeout(refresh, millisecondsUntilJapanMidnight(new Date()));
    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => {
      window.clearTimeout(midnightTimer);
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    };
  }, []);
  const dashboardVariables = useMemo(
    () => ({
      householdId: household.id,
      from: currentMonth,
      to: currentMonth,
      period: currentMonth,
    }),
    [currentMonth, household.id],
  );
  const dashboard = useLazyLoadQuery<HomeDashboardQuery>(homeDashboardQuery, dashboardVariables, {
    fetchPolicy: "network-only",
    fetchKey: refreshKey,
  });
  const accountBalances = useLazyLoadQuery<AccountBalancesQuery>(
    accountBalancesQuery,
    { householdId: household.id },
    { fetchPolicy: "network-only", fetchKey: refreshKey },
  );
  const pendingImports = useQuery({
    queryKey: ["home-pending-imports", household.id],
    queryFn: ({ signal }) => readPendingImportStatus(household.id, signal),
    refetchInterval: 60_000,
    staleTime: 30_000,
  });
  const pendingImportStatusMessage = pendingImports.data
    ? pendingImportMessage(pendingImports.data)
    : undefined;
  const homeSummary = {
    dailySpending: dashboard.homeSummary.dailySpending.map((day) => ({
      date: day.date,
      amountYen: Number(day.amountYen),
    })),
    monthlyItemCount: Number(dashboard.homeSummary.monthlyItemCount),
  };
  const monthTotal = sum(
    dashboard.monthlyCategorySpending
      .filter((entry) => entry.period === currentMonth)
      .map((entry) => Number(entry.amountYen)),
  );
  const settlement = dashboard.settlement;
  const settlementDues = summarizeSettlementDues(
    settlement.lines.map((line) => ({ amountDueYen: Number(line.amountDueYen) })),
  );
  const accountBalanceById = new Map(
    accountBalances.accountBalances.map((balance) => [
      balance.accountId,
      Number(balance.balanceYen),
    ]),
  );
  const sharedPoolBalances = household.pools.filter(isSharedPool).map((pool) => {
    const accounts = household.accounts.filter((account) => account.poolId === pool.id);
    return {
      id: pool.id,
      name: pool.name,
      balanceYen: accounts.reduce(
        (total, account) => total + (accountBalanceById.get(account.id) ?? 0),
        0,
      ),
      hasAccounts: accounts.length > 0,
    };
  });
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
              timeZone: "Asia/Tokyo",
            })}
          </span>
          <h1>おかえりなさい</h1>
        </div>
        <Link
          className="button button-primary"
          to="/households/$householdId/new"
          params={{ householdId: household.id }}
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
      <section className="panel overview-grid" aria-label="今月の家計">
        <section className="spend-panel">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">今月の支出</span>
              <h2>{yen(monthTotal)}</h2>
            </div>
            <span className="period-chip">{Number(currentMonth.slice(5))}月</span>
          </div>
          <MiniBars dailySpending={homeSummary.dailySpending} />
          <div className="spend-footer">
            <span>
              品目 <b>{homeSummary.monthlyItemCount}</b>
            </span>
            <Link
              className="text-button"
              to="/households/$householdId/analysis"
              params={{ householdId: household.id }}
            >
              分析を見る <ChevronRight size={15} />
            </Link>
          </div>
        </section>
        <section className="settle-card">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">次回の入金</span>
              <h2>{yen(settlementDues.fundingDueYen)}</h2>
            </div>
            <span className="settle-icon">
              <Wallet size={18} />
            </span>
          </div>
          {settlementDues.creditYen > 0 && (
            <p className="settle-credit-summary">過払い・控除 {yen(settlementDues.creditYen)}</p>
          )}
          <ul className="settle-members" aria-label="メンバー別の入金額">
            {settlement.lines.map((line) => (
              <li key={line.memberId}>
                <span>{line.memberName}</span>
                <b>{yen(Number(line.amountDueYen))}</b>
              </li>
            ))}
          </ul>
          {sharedPoolBalances.length > 0 && (
            <div className="shared-pool-balances" aria-label="共用口座残高">
              <span className="eyebrow">共用口座残高</span>
              <ul>
                {sharedPoolBalances.map((pool) => (
                  <li key={pool.id}>
                    <span>{pool.name}</span>
                    <b>{pool.hasAccounts ? yen(pool.balanceYen) : "口座未設定"}</b>
                  </li>
                ))}
              </ul>
              <Link
                className="text-button"
                to="/households/$householdId/settings"
                params={{ householdId: household.id }}
                search={{ tab: "accounts" }}
              >
                口座の残高履歴 <ChevronRight size={15} />
              </Link>
            </div>
          )}
          <SettlementStatus
            state={settlement.__typename === "ClosedSettlement" ? "closed" : "open"}
          />
          <Link
            className="button button-soft"
            to="/households/$householdId/settlement"
            params={{ householdId: household.id }}
          >
            精算を確認 <ChevronRight size={16} />
          </Link>
        </section>
      </section>
      <div className="quick-actions">
        <Link
          className="quick-card"
          to="/households/$householdId/imports"
          params={{ householdId: household.id }}
          search={{ capture: "1" }}
        >
          <span className="quick-icon" aria-hidden="true">
            <Camera />
          </span>
          <span>
            <b>撮影する</b>
          </span>
          <ChevronRight aria-hidden="true" />
        </Link>
        <Link
          className="quick-card"
          to="/households/$householdId/imports"
          params={{ householdId: household.id }}
        >
          <span className="quick-icon" aria-hidden="true">
            <Upload />
          </span>
          <span>
            <b>ファイルを取り込む</b>
            {pendingImportStatusMessage && <small>{pendingImportStatusMessage}</small>}
          </span>
          <ChevronRight aria-hidden="true" />
        </Link>
      </div>
      <div className="section-heading">
        <div>
          <h2>最近の買い物</h2>
        </div>
        <Link
          className="text-button"
          to="/households/$householdId/ledger"
          params={{ householdId: household.id }}
        >
          帳簿を見る <ChevronRight size={15} />
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
                onClick={onNavigateToPurchase}
              >
                最初の買い物を追加 <ChevronRight size={15} />
              </Link>
            }
          />
        ) : (
          purchases
            .slice(0, 8)
            .map((purchase) => (
              <PurchaseRow key={purchase.id} householdId={household.id} purchase={purchase} />
            ))
        )}
      </section>
    </div>
  );
}

function MiniBars({ dailySpending }: { dailySpending: HomeSummary["dailySpending"] }) {
  const totals = dailySpending.map((day) => day.amountYen);
  const peak = Math.max(...totals, 1);
  const hasSpending = totals.some((amount) => amount > 0);
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
      {!hasSpending && <figcaption className="mini-chart-empty">支出なし</figcaption>}
      <div className="mini-bars" aria-hidden="true">
        {dailySpending.map((day, index) => (
          <div key={day.date} className="bar-track" title={`${day.date}: ${yen(day.amountYen)}`}>
            <i
              style={{
                height: day.amountYen > 0 ? `${Math.max(5, (day.amountYen / peak) * 100)}%` : 0,
              }}
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

function PurchaseRow({ householdId, purchase }: { householdId: string; purchase: Purchase }) {
  return (
    <Link
      className="purchase-row"
      to="/households/$householdId/ledger"
      params={{ householdId }}
      search={{ purchaseId: purchase.id, recordMode: "purchases", viewMode: "purchases" }}
    >
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
            timeZone: "Asia/Tokyo",
          })}
        </span>
      </div>
    </Link>
  );
}
