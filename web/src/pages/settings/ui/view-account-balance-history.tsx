import { useEffect, useRef, useState } from "react";

import "./account-balance-history.css";
import { type AccountBalanceAnalysis, type Household } from "../../../shared/api";
import { errorMessage } from "../../../shared/lib";
import { InlineError } from "../../../shared/ui";
import { readAccountBalanceAnalysis } from "../api/read-account-balance-analysis";

import styles from "./settings-page.module.css";

type Account = Household["accounts"][number];

/** 残高分析の取得結果。keyは結果が有効な取得条件を表し、現在の取得条件と一致しない間は表示内容を導出値で上書きする。 */
type BalanceHistoryState = {
  key: string;
  history: AccountBalanceAnalysis | null;
  error: string;
  loading: boolean;
  loadingMore: boolean;
  moreError: string;
};

/** 残高分析の取得条件を一意に表すキーを作る。古い応答を弾くための識別子として使う。 */
const buildRequestKey = (params: {
  householdId: string;
  accountId: string;
  from: string;
  to: string;
  refreshRevision: number;
}) =>
  `${params.householdId}|${params.accountId}|${params.from}|${params.to}|${params.refreshRevision}`;

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
/** 期間指定が不正なときのエラーメッセージを返す。空文字なら期間指定は正しい。 */
const periodErrorMessage = (from: string, to: string) =>
  !/^\d{4}-\d{2}$/.test(from) ||
  !/^\d{4}-\d{2}$/.test(to) ||
  monthIndex(to) < monthIndex(from) ||
  monthIndex(to) - monthIndex(from) > 120
    ? "期間は前後関係を確認し、10年以内で指定してください"
    : "";

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
  const [selectedAccountId, setSelectedAccountId] = useState(accounts[0]?.id ?? "");
  const [from, setFrom] = useState(() => shiftMonth(currentMonth(), -5));
  const [to, setTo] = useState(currentMonth);
  const [historyState, setHistoryState] = useState<BalanceHistoryState>(() => ({
    key: "",
    history: null,
    error: "",
    loading: false,
    loadingMore: false,
    moreError: "",
  }));
  const activeRequestKey = useRef<string | null>(null);
  const request = useRef(false);

  //選択中の口座が口座一覧に無ければ先頭の口座を使う。stateに書き戻さずレンダー中に導出して口座一覧の変化に追従させる。
  const accountId = accounts.some((account) => account.id === selectedAccountId)
    ? selectedAccountId
    : (accounts[0]?.id ?? "");
  const requestKey = buildRequestKey({ householdId, accountId, from, to, refreshRevision });
  const periodError = periodErrorMessage(from, to);
  //表示する履歴・合計・ページネーションは取得条件と一致する結果が無ければ導出値にする。プロップや入力の変更でstateを書き換えない。
  const view: BalanceHistoryState = !accountId
    ? {
        key: requestKey,
        history: null,
        error: "",
        loading: false,
        loadingMore: false,
        moreError: "",
      }
    : periodError
      ? {
          key: requestKey,
          history: null,
          error: periodError,
          loading: false,
          loadingMore: false,
          moreError: "",
        }
      : historyState.key === requestKey
        ? historyState
        : {
            key: requestKey,
            history: null,
            error: "",
            loading: true,
            loadingMore: false,
            moreError: "",
          };
  const { history: balanceHistory, error, loading, loadingMore, moreError } = view;

  useEffect(() => {
    const key = buildRequestKey({ householdId, accountId, from, to, refreshRevision });
    activeRequestKey.current = key;
    request.current = false;
    if (!accountId || periodErrorMessage(from, to)) return;
    //開始時のstateリセットと取得開始はeffectの同期処理にせず、マイクロタスクへ延期する。
    queueMicrotask(() => {
      if (activeRequestKey.current !== key) return;
      setHistoryState({
        key,
        history: null,
        error: "",
        loading: true,
        loadingMore: false,
        moreError: "",
      });
      void readAccountBalanceAnalysis({
        householdId,
        accountId,
        from,
        to,
        first: 100,
      })
        .then((result) => {
          if (activeRequestKey.current === key)
            setHistoryState((current) =>
              current.key === key ? { ...current, history: result } : current,
            );
        })
        .catch((reason) => {
          if (activeRequestKey.current === key)
            setHistoryState((current) =>
              current.key === key ? { ...current, error: errorMessage(reason) } : current,
            );
        })
        .finally(() => {
          if (activeRequestKey.current === key)
            setHistoryState((current) =>
              current.key === key ? { ...current, loading: false } : current,
            );
        });
    });
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
    if (activeRequestKey.current !== requestKey) return;
    request.current = true;
    setHistoryState((current) =>
      current.key === requestKey ? { ...current, loadingMore: true, moreError: "" } : current,
    );
    try {
      const result = await readAccountBalanceAnalysis({
        householdId,
        accountId,
        from,
        to,
        first: 100,
        after,
      });
      if (activeRequestKey.current !== requestKey) return;
      setHistoryState((current) => {
        if (
          current.key !== requestKey ||
          !current.history ||
          current.history.accountId !== result.accountId
        )
          return current;
        return {
          ...current,
          history: {
            ...current.history,
            changes: {
              ...result.changes,
              edges: [...current.history.changes.edges, ...result.changes.edges],
            },
          },
        };
      });
    } catch (reason) {
      if (activeRequestKey.current === requestKey)
        setHistoryState((current) =>
          current.key === requestKey ? { ...current, moreError: errorMessage(reason) } : current,
        );
    } finally {
      if (activeRequestKey.current === requestKey) {
        request.current = false;
        setHistoryState((current) =>
          current.key === requestKey ? { ...current, loadingMore: false } : current,
        );
      }
    }
  };
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
            onChange={(event) => setSelectedAccountId(event.target.value)}
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
            {balanceHistory.changes.edges.map(({ cursor, node: change }) => (
              <div className="account-balance-change" key={cursor}>
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
