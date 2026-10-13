import "./account-balance-history.css";
import styles from "./settings-page.module.css";
import { useEffect, useRef, useState } from "react";
import { type AccountBalanceAnalysis, type Household } from "../../../shared/api";
import { readAccountBalanceAnalysis } from "../api/read-account-balance-analysis";
import { InlineError } from "../../../shared/ui";

type Account = Household["accounts"][number];

const currentMonth = () =>
  new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Tokyo",
    year: "numeric",
    month: "2-digit",
  }).format(new Date());
const monthIndex = (month: string) => {
  const [year, number] = month.split("-").map(Number);
  return year * 12 + number - 1;
};
const shiftMonth = (month: string, offset: number) => {
  const shifted = monthIndex(month) + offset;
  return `${Math.floor(shifted / 12)}-${String((shifted % 12) + 1).padStart(2, "0")}`;
};
const yen = (amount: number) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const balanceChangeSource = (sourceType: string) =>
  ({
    AccountOpeningBalance: "初期残高",
    Purchase: "購入",
    PurchaseWideCorrection: "訂正",
    PurchaseItemCorrection: "訂正",
    PurchaseLedgerEntry: "購入",
    CorrectionLedgerEntry: "訂正",
    FundingLedgerEntry: "資金拠出",
    AdvanceTransferLedgerEntry: "立替",
    LoanTransferLedgerEntry: "貸付",
    RepaymentTransferLedgerEntry: "返済",
    GiftTransferLedgerEntry: "贈与",
    AccountMovement: "口座振替",
    AccountIncome: "外部入金",
  })[sourceType] ?? "仕訳";

export function ViewAccountBalanceHistory({
  householdId,
  accounts,
  hidden,
  refreshRevision,
}: {
  householdId: string;
  accounts: Account[];
  hidden: boolean;
  refreshRevision: number;
}) {
  const [accountId, setAccountId] = useState(accounts[0]?.id ?? "");
  const [from, setFrom] = useState(() => shiftMonth(currentMonth(), -5));
  const [to, setTo] = useState(currentMonth);
  const [balanceHistory, setBalanceHistory] = useState<AccountBalanceAnalysis | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState("");
  const [moreError, setMoreError] = useState("");
  const generation = useRef(0);
  const request = useRef(false);

  useEffect(() => {
    if (!accounts.some((account) => account.id === accountId)) {
      setAccountId(accounts[0]?.id ?? "");
    }
  }, [accounts, accountId]);
  useEffect(() => {
    const currentGeneration = ++generation.current;
    request.current = false;
    if (!accountId) {
      setBalanceHistory(null);
      setError("");
      setLoading(false);
      setLoadingMore(false);
      setMoreError("");
      return;
    }
    if (
      !/^\d{4}-\d{2}$/.test(from) ||
      !/^\d{4}-\d{2}$/.test(to) ||
      monthIndex(to) < monthIndex(from) ||
      monthIndex(to) - monthIndex(from) > 120
    ) {
      setBalanceHistory(null);
      setError("期間は前後関係を確認し、10年以内で指定してください");
      setLoading(false);
      setLoadingMore(false);
      setMoreError("");
      return;
    }
    setBalanceHistory(null);
    setLoading(true);
    setLoadingMore(false);
    setError("");
    setMoreError("");
    void readAccountBalanceAnalysis({
      householdId,
      accountId,
      from,
      to,
      first: 100,
    })
      .then((result) => {
        if (generation.current === currentGeneration) setBalanceHistory(result);
      })
      .catch((reason) => {
        if (generation.current === currentGeneration) setError((reason as Error).message);
      })
      .finally(() => {
        if (generation.current === currentGeneration) setLoading(false);
      });
    return () => {
      if (generation.current === currentGeneration) generation.current++;
    };
  }, [householdId, accountId, from, to, refreshRevision]);
  const loadMoreBalanceChanges = async () => {
    const after = balanceHistory?.changes.pageInfo.endCursor;
    if (
      !balanceHistory ||
      !balanceHistory.changes.pageInfo.hasNextPage ||
      !after ||
      request.current
    )
      return;
    const currentGeneration = generation.current;
    request.current = true;
    setLoadingMore(true);
    setMoreError("");
    try {
      const result = await readAccountBalanceAnalysis({
        householdId,
        accountId,
        from,
        to,
        first: 100,
        after,
      });
      if (generation.current !== currentGeneration) return;
      setBalanceHistory((current) => {
        if (!current || current.accountId !== result.accountId) return current;
        return {
          ...current,
          changes: {
            ...result.changes,
            edges: [...current.changes.edges, ...result.changes.edges],
          },
        };
      });
    } catch (reason) {
      if (generation.current === currentGeneration) setMoreError((reason as Error).message);
    } finally {
      if (generation.current === currentGeneration) {
        request.current = false;
        setLoadingMore(false);
      }
    }
  };
  const visibleBalanceChanges = balanceHistory?.changes.edges.map(({ node }) => node) ?? [];
  const maxBalanceChange =
    balanceHistory?.changes.edges.reduce(
      (maximum, { node }) => Math.max(maximum, Math.abs(node.changeYen)),
      1,
    ) ?? 1;

  return (
    <section className={`panel ${styles.settingsSection} account-balance-analysis`} hidden={hidden}>
      <div className={`panel-heading ${styles.panelHeading}`}>
        <div>
          <span className="eyebrow">口座別の仕訳</span>
          <h2>残高の増減</h2>
        </div>
        {balanceHistory?.currency && <span>{balanceHistory.currency}</span>}
      </div>
      <div className="analysis-controls">
        <label className="select-filter">
          <span>口座</span>
          <select
            value={accountId}
            disabled={!accounts.length}
            onChange={(event) => setAccountId(event.target.value)}
          >
            {accounts.map((account) => (
              <option key={account.id} value={account.id}>
                {account.name}
              </option>
            ))}
          </select>
        </label>
        <label className="select-filter">
          <span>開始月</span>
          <input
            aria-label="残高分析の開始月"
            type="month"
            value={from}
            onChange={(event) => setFrom(event.target.value)}
          />
        </label>
        <label className="select-filter">
          <span>終了月</span>
          <input
            aria-label="残高分析の終了月"
            type="month"
            value={to}
            onChange={(event) => setTo(event.target.value)}
          />
        </label>
      </div>
      {error && <InlineError message={error} />}
      {loading ? (
        <p>残高を集計しています</p>
      ) : balanceHistory ? (
        <>
          <div className="account-balance-summary">
            <div>
              <small>期首残高</small>
              <b>{yen(balanceHistory.openingBalanceYen)}</b>
            </div>
            <div>
              <small>期末残高</small>
              <b>{yen(balanceHistory.closingBalanceYen)}</b>
            </div>
          </div>
          {accounts.find((account) => account.id === accountId)?.kind === "CARD" && (
            <small className="account-balance-sign">カード債務の増加はプラス</small>
          )}
          <div className="account-balance-changes">
            {visibleBalanceChanges.map((change, index) => (
              <div
                className="account-balance-change"
                key={`${change.source.id}-${change.occurredAt}-${index}`}
              >
                <div className="account-balance-change-copy">
                  <b>{change.description || balanceChangeSource(change.source.__typename)}</b>
                  <small>
                    {new Date(change.occurredAt).toLocaleString("ja-JP", {
                      month: "numeric",
                      day: "numeric",
                      hour: "2-digit",
                      minute: "2-digit",
                    })}{" "}
                    · {balanceChangeSource(change.source.__typename)}
                  </small>
                </div>
                <div className="account-balance-change-bar">
                  <i
                    className={change.changeYen < 0 ? "account-balance-negative" : ""}
                    style={{
                      width: `${Math.max(2, Math.min(100, (Math.abs(change.changeYen) / maxBalanceChange) * 100))}%`,
                    }}
                  />
                </div>
                <b className={change.changeYen < 0 ? "account-balance-negative-text" : ""}>
                  {change.changeYen > 0 ? "+" : ""}
                  {yen(change.changeYen)}
                </b>
                <small>残高 {yen(change.balanceYen)}</small>
              </div>
            ))}
            {!balanceHistory.changes.edges.length && <p>この期間に仕訳はありません</p>}
            {balanceHistory.changes.pageInfo.hasNextPage && (
              <button
                className="text-button"
                type="button"
                disabled={loadingMore}
                onClick={() => void loadMoreBalanceChanges()}
              >
                {loadingMore ? "読み込み中" : "過去の仕訳を表示"}
              </button>
            )}
            {moreError && <InlineError message={moreError} />}
          </div>
        </>
      ) : null}
    </section>
  );
}
