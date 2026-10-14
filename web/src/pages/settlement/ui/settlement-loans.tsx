import { Check, Plus, Wallet } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type SubmitEvent } from "react";

import { makeIdempotencyKey, type Household, type LoanBalance } from "../../../shared/api";
import { errorMessage } from "../../../shared/lib";
import { InlineError } from "../../../shared/ui";
import { readLoanBalances } from "../api/read-settlement";
import { recordSettlementRepayment } from "../api/settlement-mutations";
import {
  clearTransferIntent,
  getTransferIntent,
  saveTransferIntent,
  type TransferIntent,
} from "../model/pending-ledger-mutations";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const currentTimestamp = () => new Date().toISOString();

export function SettlementLoans({
  household,
  refreshSignal,
  canRecordLoan,
  canManageRepayments,
  onRecordLoan,
  onPendingIntentChange,
  onRecorded,
  onUnsavedChanges,
}: {
  household: Household;
  refreshSignal: number;
  canRecordLoan: boolean;
  canManageRepayments: boolean;
  onRecordLoan: () => void;
  onPendingIntentChange: (pending: boolean) => void;
  onRecorded: () => void;
  onUnsavedChanges: (unsaved: boolean) => void;
}) {
  const [loans, setLoans] = useState<LoanBalance[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [hasNextPage, setHasNextPage] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [selectedLoanId, setSelectedLoanId] = useState("");
  const [amount, setAmount] = useState("");
  const [description, setDescription] = useState("");
  const [paymentAccountId, setPaymentAccountId] = useState("");
  const [destinationAccountId, setDestinationAccountId] = useState("");
  const [pendingIntent, setPendingIntent] = useState<TransferIntent | null>(null);
  const [pending, setPending] = useState(false);
  const [hasUnsavedForm, setHasUnsavedForm] = useState(false);
  const request = useRef(0);
  const openLoans = loans.filter((loan) => loan.balanceYen > 0);
  const selectedLoan = openLoans.find((loan) => loan.id === selectedLoanId);
  const parsedAmount = Number(amount);
  const invalidAmount =
    !Number.isSafeInteger(parsedAmount) ||
    parsedAmount < 1 ||
    (selectedLoan !== undefined && parsedAmount > selectedLoan.balanceYen);

  const load = useCallback(async () => {
    const requestedRefreshSignal = refreshSignal;
    const currentRequest = ++request.current;
    setLoading(true);
    setLoadError("");
    setLoans([]);
    setCursor(null);
    setHasNextPage(false);
    setLoadingMore(false);
    try {
      const result = await readLoanBalances(household.id, { first: 50 });
      if (currentRequest !== request.current || requestedRefreshSignal !== refreshSignal) return;
      setLoans(result.loans);
      setCursor(result.endCursor);
      setHasNextPage(result.hasNextPage);
    } catch (reason) {
      if (currentRequest === request.current) setLoadError(errorMessage(reason));
    } finally {
      if (currentRequest === request.current) setLoading(false);
    }
  }, [household.id, refreshSignal]);

  const loadMore = async () => {
    if (!cursor || loadingMore) return;
    const currentRequest = request.current;
    setLoadingMore(true);
    setLoadError("");
    try {
      const result = await readLoanBalances(household.id, { after: cursor, first: 50 });
      if (currentRequest !== request.current) return;
      setLoans((current) => [...current, ...result.loans]);
      setCursor(result.endCursor);
      setHasNextPage(result.hasNextPage);
    } catch (reason) {
      if (currentRequest === request.current) setLoadError(errorMessage(reason));
    } finally {
      if (currentRequest === request.current) setLoadingMore(false);
    }
  };

  useEffect(() => {
    //読取の開始時に行う同期setStateはeffect直後ではなくマイクロタスクで走らせる
    queueMicrotask(() => void load());
    return () => {
      request.current += 1;
    };
  }, [load]);

  useEffect(() => {
    //保存済み返済意図の復元と同期setStateはeffect直後ではなくマイクロタスクで走らせる
    queueMicrotask(() => {
      const intent = getTransferIntent(household.id);
      if (intent?.input.kind === "REPAYMENT") {
        setPendingIntent(intent);
        onPendingIntentChange(true);
        setSelectedLoanId(intent.input.loanId);
        setAmount(String(intent.input.amountYen));
        setDescription(intent.input.description);
        setPaymentAccountId(intent.input.paymentAccountId ?? "");
        setDestinationAccountId(intent.input.destinationAccountId ?? "");
        setHasUnsavedForm(false);
      } else {
        setPendingIntent(null);
        onPendingIntentChange(false);
        setSelectedLoanId("");
        setAmount("");
        setDescription("");
        setPaymentAccountId("");
        setDestinationAccountId("");
      }
    });
  }, [household.id, onPendingIntentChange]);

  useEffect(() => {
    //未保存状態の親への通知はeffect直後ではなくマイクロタスクで行う
    queueMicrotask(() => onUnsavedChanges(hasUnsavedForm));
    return () => onUnsavedChanges(false);
  }, [hasUnsavedForm, onUnsavedChanges]);

  const changeLoan = (loan: LoanBalance) => {
    setSelectedLoanId(loan.id);
    setAmount("");
    setDescription("");
    setError("");
    setSuccess("");
    setHasUnsavedForm(true);
  };

  const saveRepayment = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (pending) return;
    setPending(true);
    setError("");
    setSuccess("");
    try {
      const existingIntent = getTransferIntent(household.id);
      if (existingIntent && existingIntent.input.kind !== "REPAYMENT") {
        throw new Error("保存待ちの送金を先に完了してください");
      }
      if (!existingIntent && (!selectedLoan || invalidAmount)) return;
      const idempotencyKey = makeIdempotencyKey();
      const intent =
        existingIntent?.input.kind === "REPAYMENT"
          ? existingIntent
          : {
              idempotencyKey,
              input: {
                householdId: household.id,
                fromMemberId: selectedLoan!.borrowerMemberId,
                toMemberId: selectedLoan!.lenderMemberId,
                paymentAccountId: paymentAccountId || null,
                destinationAccountId: destinationAccountId || null,
                amountYen: parsedAmount,
                occurredAt: currentTimestamp(),
                description: description.trim() || "貸付の返済",
                idempotencyKey,
                kind: "REPAYMENT" as const,
                loanId: selectedLoan!.id,
              },
            };
      saveTransferIntent(household.id, intent);
      setPendingIntent(intent);
      onPendingIntentChange(true);
      const input = intent.input;
      if (input.kind !== "REPAYMENT") throw new Error("返済情報が不正です");
      await recordSettlementRepayment({
        householdId: input.householdId,
        fromMemberId: input.fromMemberId,
        toMemberId: input.toMemberId,
        paymentAccountId: input.paymentAccountId,
        destinationAccountId: input.destinationAccountId,
        amountYen: input.amountYen,
        occurredAt: input.occurredAt,
        description: input.description,
        idempotencyKey: input.idempotencyKey,
        loanId: input.loanId,
      });
      clearTransferIntent(household.id);
      setPendingIntent(null);
      onPendingIntentChange(false);
      setAmount("");
      setDescription("");
      setPaymentAccountId("");
      setDestinationAccountId("");
      setSelectedLoanId("");
      setHasUnsavedForm(false);
      setSuccess("返済を記録しました");
      await load();
      onRecorded();
    } catch (reason) {
      setError(errorMessage(reason));
    } finally {
      setPending(false);
    }
  };

  const memberName = (memberId: string, fallback: string) =>
    household.members.find((member) => member.id === memberId)?.displayName ?? fallback;

  return (
    <section className="panel loan-panel">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">貸付と返済</span>
          <h2>貸付残高</h2>
        </div>
        <span className="count-badge">{openLoans.length}</span>
        <button
          className="button button-soft"
          type="button"
          disabled={!canRecordLoan}
          onClick={onRecordLoan}
        >
          <Plus size={15} />
          貸付を記録
        </button>
      </div>
      {error && !(selectedLoan || pendingIntent) && <InlineError message={error} />}
      {success && !(selectedLoan || pendingIntent) && (
        <output className="settlement-confirmation">{success}</output>
      )}
      {loadError && (
        <div className="settlement-load-error">
          <InlineError message={loadError} />
          <button className="text-button" type="button" onClick={() => void load()}>
            再試行
          </button>
        </div>
      )}
      {loading ? (
        <div className="skeleton-list">
          <i />
          <i />
        </div>
      ) : loans.length === 0 ? (
        <div className="empty-state compact">
          <Wallet />
          <span>貸付記録はありません</span>
        </div>
      ) : (
        loans.map((loan) => (
          <div className="loan-balance-row" key={loan.id}>
            <div>
              <b>
                {memberName(loan.borrowerMemberId, "借主")} →{" "}
                {memberName(loan.lenderMemberId, "貸主")}
              </b>
              <small>
                元本 {yen(loan.principalYen)} · 返済済み {yen(loan.repaidYen)}
              </small>
            </div>
            <strong>{yen(loan.balanceYen)}</strong>
            <button
              className="text-button"
              type="button"
              disabled={
                loan.balanceYen <= 0 || pending || Boolean(pendingIntent) || !canManageRepayments
              }
              onClick={() => changeLoan(loan)}
            >
              返済を記録
            </button>
          </div>
        ))
      )}
      {hasNextPage && (
        <button
          className="text-button"
          type="button"
          disabled={loadingMore}
          onClick={() => void loadMore()}
        >
          {loadingMore ? "読み込み中" : "さらに貸付を表示"}
        </button>
      )}
      {(selectedLoan || pendingIntent) && (
        <form className="transfer-form" onSubmit={saveRepayment}>
          {error && <InlineError message={error} />}
          {success && <output className="settlement-confirmation">{success}</output>}
          <label>
            貸付
            <select
              required
              value={selectedLoanId}
              disabled={pending || Boolean(pendingIntent)}
              onChange={(event) => {
                const loan = openLoans.find((row) => row.id === event.target.value);
                if (loan) changeLoan(loan);
              }}
            >
              <option value="">貸付を選択</option>
              {openLoans.map((loan) => (
                <option key={loan.id} value={loan.id}>
                  {memberName(loan.borrowerMemberId, "借主")} →{" "}
                  {memberName(loan.lenderMemberId, "貸主")} {yen(loan.balanceYen)}
                </option>
              ))}
            </select>
          </label>
          <label>
            借主
            <input
              value={selectedLoan ? memberName(selectedLoan.borrowerMemberId, "借主") : ""}
              readOnly
            />
          </label>
          <label>
            貸主
            <input
              value={selectedLoan ? memberName(selectedLoan.lenderMemberId, "貸主") : ""}
              readOnly
            />
          </label>
          <label>
            資金元口座
            <select
              value={paymentAccountId}
              disabled={pending || Boolean(pendingIntent)}
              onChange={(event) => {
                setPaymentAccountId(event.target.value);
                setHasUnsavedForm(true);
              }}
            >
              <option value="">現金など口座外</option>
              {household.accounts.map((account) => (
                <option value={account.id} key={account.id}>
                  {account.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            入金先口座
            <select
              value={destinationAccountId}
              disabled={pending || Boolean(pendingIntent)}
              onChange={(event) => {
                setDestinationAccountId(event.target.value);
                setHasUnsavedForm(true);
              }}
            >
              <option value="">口座外</option>
              {household.accounts
                .filter((account) => account.kind !== "CARD")
                .map((account) => (
                  <option value={account.id} key={account.id}>
                    {account.name}
                  </option>
                ))}
            </select>
          </label>
          <label>
            金額
            <input
              inputMode="numeric"
              value={amount}
              disabled={pending || Boolean(pendingIntent)}
              max={selectedLoan?.balanceYen}
              aria-invalid={Boolean(selectedLoan && parsedAmount > selectedLoan.balanceYen)}
              onChange={(event) => {
                setAmount(event.target.value.replace(/\D/g, ""));
                setHasUnsavedForm(true);
              }}
              required
            />
            {selectedLoan && (
              <small
                className={
                  parsedAmount > selectedLoan.balanceYen ? "payment-unbalanced" : undefined
                }
              >
                {parsedAmount > selectedLoan.balanceYen
                  ? "返済額が残高を超えています"
                  : `返済可能額 ${yen(selectedLoan.balanceYen)}`}
              </small>
            )}
          </label>
          <label className="transfer-description">
            摘要
            <input
              value={description}
              disabled={pending || Boolean(pendingIntent)}
              onChange={(event) => {
                setDescription(event.target.value);
                setHasUnsavedForm(true);
              }}
            />
          </label>
          <button
            className="button button-primary"
            type="submit"
            disabled={
              pending ||
              household.members.length < 2 ||
              (!selectedLoan && !pendingIntent) ||
              (!pendingIntent && invalidAmount)
            }
          >
            <Check size={16} />
            {pendingIntent ? "同じ返済を再試行" : "返済を保存"}
          </button>
        </form>
      )}
    </section>
  );
}
