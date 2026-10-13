import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  ArrowDownLeft,
  ArrowLeftRight,
  Check,
  ChevronLeft,
  ChevronRight,
  Plus,
  Wallet,
} from "lucide-react";
import {
  makeIdempotencyKey,
  readGraphql,
  type Connection,
  type Household,
  type LoanBalance,
  type Settlement,
} from "../../../shared/api";
import { closePeriodMutation, settlementQuery } from "../../../entities/settlement";
import {
  loanBalancesQuery,
  recordRepaymentMutation,
  recordFundingMutation,
  recordTransferMutation,
  useHouseholdLedger,
} from "../../../entities/ledger";
import { InlineError } from "../../../shared/ui";
import { japanMonth } from "../../../shared/lib";
import {
  clearFundingIntent,
  clearTransferIntent,
  getFundingIntent,
  getTransferIntent,
  saveFundingIntent,
  saveTransferIntent,
  type FundingIntent,
  type TransferIntent,
} from "../model/pending-ledger-mutations";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const sum = (numbers: number[]) => numbers.reduce((total, value) => total + value, 0);
const ledgerKindLabels: Record<string, string> = {
  FUNDING: "入金",
  ADVANCE: "立て替え",
  LOAN: "貸付",
  REPAYMENT: "返済",
  GIFT: "贈与",
};

export function SettlementPage({
  household,
  onUnsavedChanges,
}: {
  household: Household;
  onUnsavedChanges: (unsaved: boolean) => void;
}) {
  const [period, setPeriod] = useState(() => japanMonth(new Date()));
  const [settlementSnapshot, setSettlementSnapshot] = useState<{
    householdId: string;
    period: string;
    value: Settlement;
  } | null>(null);
  const settlement =
    settlementSnapshot?.householdId === household.id && settlementSnapshot.period === period
      ? settlementSnapshot.value
      : null;
  const activePeriod = useRef(period);
  activePeriod.current = period;
  const activeHouseholdId = useRef(household.id);
  activeHouseholdId.current = household.id;
  const loadRequest = useRef(0);
  const {
    entries,
    hasNextPage: ledgerHasNextPage,
    loadingMore: ledgerLoadingMore,
    moreError: ledgerMoreError,
    loadMore: loadMoreLedger,
    refresh: refreshLedger,
  } = useHouseholdLedger(household.id);
  const [loans, setLoans] = useState<LoanBalance[]>([]);
  const [loanCursor, setLoanCursor] = useState<string | null>(null);
  const [loanHasNextPage, setLoanHasNextPage] = useState(false);
  const [loanPageLoading, setLoanPageLoading] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [loadError, setLoadError] = useState("");
  const [pending, setPending] = useState(false);
  const [pendingFunding, setPendingFunding] = useState<Record<string, FundingIntent>>({});
  const [pendingTransfer, setPendingTransfer] = useState<TransferIntent | null>(null);
  const [transferOpen, setTransferOpen] = useState(false);
  const [transferFrom, setTransferFrom] = useState(household.members[0]?.id ?? "");
  const [transferTo, setTransferTo] = useState(household.members[1]?.id ?? "");
  const [transferKind, setTransferKind] = useState<"ADVANCE" | "LOAN" | "REPAYMENT" | "GIFT">(
    "ADVANCE",
  );
  const [transferAmount, setTransferAmount] = useState("");
  const [transferDescription, setTransferDescription] = useState("");
  const [transferLoanId, setTransferLoanId] = useState("");
  const [transferPaymentAccountId, setTransferPaymentAccountId] = useState("");
  const [transferDestinationAccountId, setTransferDestinationAccountId] = useState("");
  const [hasUnsavedTransfer, setHasUnsavedTransfer] = useState(false);
  const [fundingSourceAccounts, setFundingSourceAccounts] = useState<Record<string, string>>({});
  const [fundingDestinationAccountId, setFundingDestinationAccountId] = useState("");
  const [savedFundingSourceAccounts, setSavedFundingSourceAccounts] = useState<
    Record<string, string>
  >({});
  const [savedFundingDestinationAccountId, setSavedFundingDestinationAccountId] = useState("");
  const hasUnsavedFunding =
    household.members.some(
      (member) =>
        (fundingSourceAccounts[member.id] ?? "") !== (savedFundingSourceAccounts[member.id] ?? ""),
    ) || fundingDestinationAccountId !== savedFundingDestinationAccountId;
  useEffect(() => {
    onUnsavedChanges(hasUnsavedTransfer || hasUnsavedFunding);
    return () => onUnsavedChanges(false);
  }, [hasUnsavedFunding, hasUnsavedTransfer, onUnsavedChanges]);
  const markTransferUnsaved = () => setHasUnsavedTransfer(true);
  const load = async () => {
    const request = ++loadRequest.current;
    const requestedPeriod = activePeriod.current;
    const requestedHouseholdId = activeHouseholdId.current;
    setLoadError("");
    try {
      refreshLedger();
      const [settlementData, loanData] = await Promise.all([
        readGraphql<{ settlement: Settlement }>(settlementQuery, {
          householdId: requestedHouseholdId,
          period: requestedPeriod,
        }),
        readGraphql<{ loanBalances: Connection<LoanBalance> }>(loanBalancesQuery, {
          householdId: requestedHouseholdId,
          first: 50,
        }),
      ]);
      if (request !== loadRequest.current) return;
      setSettlementSnapshot({
        householdId: requestedHouseholdId,
        period: requestedPeriod,
        value: settlementData.settlement,
      });
      setLoans(loanData.loanBalances.edges.map((edge) => edge.node));
      setLoanCursor(loanData.loanBalances.pageInfo.endCursor ?? null);
      setLoanHasNextPage(loanData.loanBalances.pageInfo.hasNextPage);
    } catch (reason) {
      if (request === loadRequest.current) setLoadError((reason as Error).message);
    }
  };
  const loadMoreLoans = async () => {
    if (!loanCursor || loanPageLoading) return;
    setLoanPageLoading(true);
    try {
      const result = await readGraphql<{ loanBalances: Connection<LoanBalance> }>(
        loanBalancesQuery,
        {
          householdId: household.id,
          after: loanCursor,
          first: 50,
        },
      );
      setLoans((current) => [...current, ...result.loanBalances.edges.map((edge) => edge.node)]);
      setLoanCursor(result.loanBalances.pageInfo.endCursor ?? null);
      setLoanHasNextPage(result.loanBalances.pageInfo.hasNextPage);
    } catch (reason) {
      setLoadError((reason as Error).message);
    } finally {
      setLoanPageLoading(false);
    }
  };
  useEffect(() => {
    void load();
  }, [household.id, period]);
  useEffect(() => {
    try {
      const funding: Record<string, FundingIntent> = {};
      for (const member of household.members) {
        const intent = getFundingIntent(household.id, member.id);
        if (intent) funding[member.id] = intent;
      }
      setPendingFunding(funding);
      const transfer = getTransferIntent(household.id);
      setPendingTransfer(transfer);
      if (transfer) {
        const input = transfer.input;
        setTransferKind(input.kind);
        setTransferFrom(input.fromMemberId);
        setTransferTo(input.toMemberId);
        setTransferAmount(String(input.amountYen));
        setTransferDescription(input.description);
        setTransferLoanId(input.kind === "REPAYMENT" ? input.loanId : "");
        setTransferPaymentAccountId(input.paymentAccountId ?? "");
        setTransferDestinationAccountId(input.destinationAccountId ?? "");
        setTransferOpen(true);
        setHasUnsavedTransfer(false);
      }
    } catch (reason) {
      setError((reason as Error).message);
    }
  }, [household.id, household.members]);
  const recordFunding = async (memberId: string) => {
    const line = settlement?.lines.find((row) => row.memberId === memberId);
    const amountYen = line?.amountDueYen ?? 0;
    const sourceAccountId = fundingSourceAccounts[memberId] ?? "";
    const destinationAccountId = fundingDestinationAccountId;
    setPending(true);
    setError("");
    setSuccess("");
    try {
      const existingIntent = getFundingIntent(household.id, memberId);
      if (amountYen <= 0 && !existingIntent) return;
      const idempotencyKey = makeIdempotencyKey();
      const intent = existingIntent ?? {
        idempotencyKey,
        input: {
          householdId: household.id,
          memberId,
          idempotencyKey,
          poolId:
            household.pools.find(
              (pool) =>
                pool.__typename === "SharedPool" || pool.__typename === "UnconfiguredSharedPool",
            )?.id ?? household.pools[0]?.id,
          amountYen,
          occurredAt: new Date().toISOString(),
          ...(sourceAccountId ? { sourceAccountId } : {}),
          ...(destinationAccountId ? { destinationAccountId } : {}),
        },
      };
      if (!intent.input.poolId) throw new Error("入金先のプールがありません");
      saveFundingIntent(household.id, memberId, intent);
      setPendingFunding((current) => ({ ...current, [memberId]: intent }));
      await readGraphql(recordFundingMutation, { input: intent.input });
      clearFundingIntent(household.id, memberId);
      setPendingFunding((current) => {
        const next = { ...current };
        delete next[memberId];
        return next;
      });
      setSavedFundingSourceAccounts((current) => ({
        ...current,
        [memberId]: intent.input.sourceAccountId ?? "",
      }));
      setSavedFundingDestinationAccountId(intent.input.destinationAccountId ?? "");
      const memberName = household.members.find((member) => member.id === memberId)?.displayName;
      setSuccess(
        `${memberName ?? "メンバー"}の入金${yen(intent.input.amountYen)}を${japanMonth(new Date(intent.input.occurredAt))}分として記録しました`,
      );
      await load();
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const close = async () => {
    if (!settlement || !settlementSnapshot || settlementSnapshot.period !== period) return;
    const targetHouseholdId = settlementSnapshot.householdId;
    const targetPeriod = settlementSnapshot.period;
    const expectedRevision = settlement.revision;
    setPending(true);
    setSuccess("");
    try {
      await readGraphql(closePeriodMutation, {
        input: { householdId: targetHouseholdId, period: targetPeriod, expectedRevision },
      });
      await load();
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const saveTransfer = async (event: FormEvent) => {
    event.preventDefault();
    setPending(true);
    setError("");
    setSuccess("");
    try {
      const existingIntent = getTransferIntent(household.id);
      const amountYen = Number(transferAmount);
      const selectedLoan = openLoans.find((loan) => loan.id === transferLoanId);
      if (!existingIntent) {
        if (
          !transferFrom ||
          !transferTo ||
          transferFrom === transferTo ||
          !Number.isSafeInteger(amountYen) ||
          amountYen < 1 ||
          (transferKind === "REPAYMENT" && !selectedLoan)
        ) {
          return;
        }
        if (transferKind === "REPAYMENT" && selectedLoan && amountYen > selectedLoan.balanceYen) {
          setError(`返済額は貸付残高${yen(selectedLoan.balanceYen)}以下にしてください`);
          return;
        }
      }
      const idempotencyKey = makeIdempotencyKey();
      const intent = existingIntent ?? {
        idempotencyKey,
        input: {
          householdId: household.id,
          fromMemberId: transferFrom,
          toMemberId: transferTo,
          paymentAccountId: transferPaymentAccountId || null,
          destinationAccountId: transferDestinationAccountId || null,
          amountYen,
          occurredAt: new Date().toISOString(),
          description: transferDescription.trim() || "メンバー間の送金",
          idempotencyKey,
          ...(transferKind === "REPAYMENT"
            ? { kind: "REPAYMENT" as const, loanId: transferLoanId }
            : { kind: transferKind }),
        },
      };
      saveTransferIntent(household.id, intent);
      setPendingTransfer(intent);
      if (intent.input.kind === "REPAYMENT") {
        const input = intent.input;
        await readGraphql(recordRepaymentMutation, {
          input: {
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
          },
        });
      } else {
        await readGraphql(recordTransferMutation, { input: intent.input });
      }
      clearTransferIntent(household.id);
      setPendingTransfer(null);
      setTransferAmount("");
      setTransferDescription("");
      setTransferLoanId("");
      setTransferPaymentAccountId("");
      setTransferDestinationAccountId("");
      setTransferOpen(false);
      setHasUnsavedTransfer(false);
      const fromName = household.members.find(
        (member) => member.id === intent.input.fromMemberId,
      )?.displayName;
      const toName = household.members.find(
        (member) => member.id === intent.input.toMemberId,
      )?.displayName;
      setSuccess(
        `${fromName ?? "メンバー"}から${toName ?? "メンバー"}への${yen(intent.input.amountYen)}の${ledgerKindLabels[intent.input.kind]}を記録しました`,
      );
      await load();
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const totalDue = sum(settlement?.lines.map((line) => line.amountDueYen) ?? []);
  const openLoans = loans.filter((loan) => loan.balanceYen > 0);
  const selectedLoan = openLoans.find((loan) => loan.id === transferLoanId);
  const parsedTransferAmount = Number(transferAmount);
  const hasPendingTransfer = Boolean(pendingTransfer);
  const invalidTransferAmount =
    !Number.isSafeInteger(parsedTransferAmount) ||
    parsedTransferAmount < 1 ||
    (transferKind === "REPAYMENT" &&
      (!selectedLoan || parsedTransferAmount > selectedLoan.balanceYen));
  const openTransferForLoan = (loan: LoanBalance) => {
    markTransferUnsaved();
    setTransferKind("REPAYMENT");
    setTransferLoanId(loan.id);
    setTransferFrom(loan.borrowerMemberId);
    setTransferTo(loan.lenderMemberId);
    setTransferAmount("");
    setTransferDescription("");
    setError("");
    setTransferOpen(true);
  };
  const ledgerEntries = entries.filter((entry) =>
    ["FUNDING", "TRANSFER", "ADVANCE", "LOAN", "REPAYMENT", "GIFT"].includes(entry.kind),
  );
  return (
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">入金と立て替え</span>
          <h1>精算</h1>
          <p>次回の入金額と未精算の記録</p>
        </div>
        <label className="period-picker">
          <ChevronLeft size={15} />
          <input type="month" value={period} onChange={(e) => setPeriod(e.target.value)} />
          <ChevronRight size={15} />
        </label>
      </div>
      {error && <InlineError message={error} />}
      {success && (
        <p className="settlement-confirmation" role="status">
          {success}
        </p>
      )}
      {loadError && (
        <div className="settlement-load-error">
          <InlineError message={loadError} />
          <button className="text-button" type="button" onClick={() => void load()}>
            再試行
          </button>
        </div>
      )}
      <section className="settlement-hero">
        <div>
          <span>次回の入金</span>
          <h2>{yen(totalDue)}</h2>
          <small>
            {settlement?.__typename === "ClosedSettlement"
              ? "この期間は締め済み"
              : `${period.replace("-", "年")}月分`}
          </small>
        </div>
        <div className="settlement-mark">
          <ArrowLeftRight size={32} />
        </div>
      </section>
      <section className="panel settlement-panel">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">メンバー別</span>
            <h2>入金予定</h2>
          </div>
          <span className="period-chip">
            {settlement?.__typename === "ClosedSettlement" ? "締め済み" : "未締め"}
          </span>
        </div>
        {!settlement ? (
          <div className="skeleton-list">
            <i />
            <i />
          </div>
        ) : settlement.lines.length === 0 ? (
          <div className="empty-state compact">
            <Wallet />
            <span>精算する入金はありません</span>
          </div>
        ) : (
          settlement.lines.map((line) => (
            <div className="settlement-line" key={line.memberId}>
              <div className="profile-avatar">{line.memberName.slice(0, 1)}</div>
              <div className="settlement-person">
                <b>{line.memberName}</b>
                <dl className="settlement-breakdown">
                  <div>
                    <dt>今月予定</dt>
                    <dd>{yen(line.scheduledYen)}</dd>
                  </div>
                  <div>
                    <dt>前月残高</dt>
                    <dd>{yen(line.priorBalanceYen)}</dd>
                  </div>
                  <div>
                    <dt>月次調整</dt>
                    <dd>{yen(line.monthlyAdjustmentYen)}</dd>
                  </div>
                  <div>
                    <dt>入金済み</dt>
                    <dd>{yen(line.paidYen)}</dd>
                  </div>
                  <div>
                    <dt>立替充当</dt>
                    <dd>{yen(line.advanceCreditYen)}</dd>
                  </div>
                </dl>
              </div>
              <div className="settlement-amount">
                <b className={line.amountDueYen < 0 ? "settlement-credit" : undefined}>
                  {yen(line.amountDueYen)}
                </b>
                <small>次回精算残高</small>
                <label className="settlement-account-select">
                  <span>拠出元</span>
                  <select
                    value={fundingSourceAccounts[line.memberId] ?? ""}
                    disabled={pending || Boolean(pendingFunding[line.memberId])}
                    onChange={(event) =>
                      setFundingSourceAccounts((current) => ({
                        ...current,
                        [line.memberId]: event.target.value,
                      }))
                    }
                  >
                    <option value="">口座外</option>
                    {household.accounts
                      .filter((account) => {
                        const pool = household.pools.find(
                          (candidate) => candidate.id === account.poolId,
                        );
                        return (
                          pool?.__typename === "PersonalPool" &&
                          pool.ownerMemberId === line.memberId
                        );
                      })
                      .map((account) => (
                        <option key={account.id} value={account.id}>
                          {account.name}
                        </option>
                      ))}
                  </select>
                </label>
                <label className="settlement-account-select">
                  <span>入金先</span>
                  <select
                    value={fundingDestinationAccountId}
                    disabled={pending || Boolean(pendingFunding[line.memberId])}
                    onChange={(event) => setFundingDestinationAccountId(event.target.value)}
                  >
                    <option value="">未指定</option>
                    {household.accounts
                      .filter((account) => {
                        const sharedPool = household.pools.find(
                          (pool) =>
                            pool.__typename === "SharedPool" ||
                            pool.__typename === "UnconfiguredSharedPool",
                        );
                        return sharedPool?.id === account.poolId && account.kind !== "CARD";
                      })
                      .map((account) => (
                        <option key={account.id} value={account.id}>
                          {account.name}
                        </option>
                      ))}
                  </select>
                </label>
                <button
                  className="text-button"
                  disabled={pending || (line.amountDueYen <= 0 && !pendingFunding[line.memberId])}
                  onClick={() => void recordFunding(line.memberId)}
                >
                  {pendingFunding[line.memberId]
                    ? `入金を再試行 (${yen(pendingFunding[line.memberId].input.amountYen)})`
                    : "入金を記録"}
                </button>
                {((fundingSourceAccounts[line.memberId] ?? "") !==
                  (savedFundingSourceAccounts[line.memberId] ?? "") ||
                  fundingDestinationAccountId !== savedFundingDestinationAccountId) && (
                  <button
                    className="text-button"
                    type="button"
                    disabled={pending}
                    onClick={() => {
                      setFundingSourceAccounts((current) => ({
                        ...current,
                        [line.memberId]: savedFundingSourceAccounts[line.memberId] ?? "",
                      }));
                      setFundingDestinationAccountId(savedFundingDestinationAccountId);
                    }}
                  >
                    選択を戻す
                  </button>
                )}
              </div>
            </div>
          ))
        )}
        <div className="settlement-actions">
          <button
            className="button button-soft"
            disabled={pending || settlement?.__typename === "ClosedSettlement"}
            onClick={() => void close()}
          >
            <Check size={16} />
            期間を締める
          </button>
          <button
            className="button button-primary"
            onClick={() => {
              if (transferOpen || hasPendingTransfer) setHasUnsavedTransfer(false);
              else markTransferUnsaved();
              setTransferOpen(!transferOpen);
            }}
          >
            <Plus size={16} />
            送金を記録
          </button>
        </div>
        {transferOpen && (
          <form className="transfer-form" onSubmit={saveTransfer}>
            <label>
              種別
              <select
                value={transferKind}
                disabled={pending || hasPendingTransfer}
                onChange={(event) => {
                  const nextKind = event.target.value as typeof transferKind;
                  markTransferUnsaved();
                  setTransferKind(nextKind);
                  setTransferLoanId("");
                  setTransferAmount("");
                  setError("");
                  if (transferKind === "REPAYMENT" && nextKind !== "REPAYMENT") {
                    setTransferFrom(household.members[0]?.id ?? "");
                    setTransferTo(household.members[1]?.id ?? "");
                  }
                }}
              >
                <option value="ADVANCE">立て替え</option>
                <option value="LOAN">貸し借り</option>
                <option value="REPAYMENT">返済</option>
                <option value="GIFT">贈与</option>
              </select>
            </label>
            {transferKind === "REPAYMENT" && (
              <>
                <label>
                  貸付
                  <select
                    required
                    value={transferLoanId}
                    disabled={pending || hasPendingTransfer}
                    onChange={(event) => {
                      markTransferUnsaved();
                      const loan = openLoans.find((row) => row.id === event.target.value);
                      setTransferLoanId(event.target.value);
                      if (loan) {
                        setTransferFrom(loan.borrowerMemberId);
                        setTransferTo(loan.lenderMemberId);
                      }
                      setError("");
                    }}
                  >
                    <option value="">貸付を選択</option>
                    {openLoans.map((loan) => (
                      <option key={loan.id} value={loan.id}>
                        {household.members.find((member) => member.id === loan.borrowerMemberId)
                          ?.displayName ?? "借主"}{" "}
                        →{" "}
                        {household.members.find((member) => member.id === loan.lenderMemberId)
                          ?.displayName ?? "貸主"}{" "}
                        {yen(loan.balanceYen)}
                      </option>
                    ))}
                  </select>
                </label>
                {loanHasNextPage && (
                  <button
                    className="text-button"
                    type="button"
                    disabled={loanPageLoading}
                    onClick={() => void loadMoreLoans()}
                  >
                    {loanPageLoading ? "読み込み中" : "さらに貸付を表示"}
                  </button>
                )}
              </>
            )}
            <label>
              {transferKind === "LOAN"
                ? "貸主"
                : transferKind === "REPAYMENT"
                  ? "借主"
                  : transferKind === "GIFT"
                    ? "贈与者"
                    : "立替者"}
              <select
                value={transferFrom}
                disabled={pending || hasPendingTransfer || transferKind === "REPAYMENT"}
                onChange={(event) => {
                  markTransferUnsaved();
                  setTransferFrom(event.target.value);
                }}
              >
                {household.members.map((member) => (
                  <option value={member.id} key={member.id}>
                    {member.displayName}
                  </option>
                ))}
              </select>
            </label>
            <label>
              {transferKind === "LOAN"
                ? "借主"
                : transferKind === "REPAYMENT"
                  ? "貸主"
                  : transferKind === "GIFT"
                    ? "受贈者"
                    : "受益者"}
              <select
                value={transferTo}
                disabled={pending || hasPendingTransfer || transferKind === "REPAYMENT"}
                onChange={(event) => {
                  markTransferUnsaved();
                  setTransferTo(event.target.value);
                }}
              >
                {household.members.map((member) => (
                  <option value={member.id} key={member.id}>
                    {member.displayName}
                  </option>
                ))}
              </select>
            </label>
            <label>
              資金元口座
              <select
                value={transferPaymentAccountId}
                disabled={pending || hasPendingTransfer}
                onChange={(event) => {
                  markTransferUnsaved();
                  setTransferPaymentAccountId(event.target.value);
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
                value={transferDestinationAccountId}
                disabled={pending || hasPendingTransfer}
                onChange={(event) => {
                  markTransferUnsaved();
                  setTransferDestinationAccountId(event.target.value);
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
                value={transferAmount}
                disabled={pending || hasPendingTransfer}
                max={transferKind === "REPAYMENT" ? selectedLoan?.balanceYen : undefined}
                aria-invalid={
                  transferKind === "REPAYMENT" &&
                  selectedLoan !== undefined &&
                  parsedTransferAmount > selectedLoan.balanceYen
                }
                onChange={(event) => {
                  markTransferUnsaved();
                  setTransferAmount(event.target.value.replace(/\D/g, ""));
                }}
                required
              />
              {transferKind === "REPAYMENT" && selectedLoan && (
                <small
                  className={
                    parsedTransferAmount > selectedLoan.balanceYen
                      ? "payment-unbalanced"
                      : undefined
                  }
                >
                  {parsedTransferAmount > selectedLoan.balanceYen
                    ? "返済額が残高を超えています"
                    : `返済可能額 ${yen(selectedLoan.balanceYen)}`}
                </small>
              )}
            </label>
            <label className="transfer-description">
              摘要
              <input
                value={transferDescription}
                disabled={pending || hasPendingTransfer}
                onChange={(event) => {
                  markTransferUnsaved();
                  setTransferDescription(event.target.value);
                }}
              />
            </label>
            <button
              className="button button-primary"
              disabled={
                pending ||
                household.members.length < 2 ||
                transferFrom === transferTo ||
                invalidTransferAmount ||
                (transferKind === "REPAYMENT" && !selectedLoan)
              }
            >
              <Check size={16} />
              {hasPendingTransfer ? "同じ送金を再試行" : "保存"}
            </button>
          </form>
        )}
      </section>
      <section className="panel loan-panel">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">貸付と返済</span>
            <h2>貸付残高</h2>
          </div>
          <span className="count-badge">{openLoans.length}</span>
          <button
            className="button button-soft"
            onClick={() => {
              markTransferUnsaved();
              setTransferKind("LOAN");
              setTransferLoanId("");
              setTransferAmount("");
              setTransferDescription("");
              setError("");
              if (transferKind === "REPAYMENT") {
                setTransferFrom(household.members[0]?.id ?? "");
                setTransferTo(household.members[1]?.id ?? "");
              }
              setTransferOpen(true);
            }}
          >
            <Plus size={15} />
            貸付を記録
          </button>
        </div>
        {loans.length === 0 ? (
          <div className="empty-state compact">
            <Wallet />
            <span>貸付記録はありません</span>
          </div>
        ) : (
          loans.map((loan) => (
            <div className="loan-balance-row" key={loan.id}>
              <div>
                <b>
                  {household.members.find((member) => member.id === loan.borrowerMemberId)
                    ?.displayName ?? "借主"}{" "}
                  →{" "}
                  {household.members.find((member) => member.id === loan.lenderMemberId)
                    ?.displayName ?? "貸主"}
                </b>
                <small>
                  元本 {yen(loan.principalYen)} · 返済済み {yen(loan.repaidYen)}
                </small>
              </div>
              <strong>{yen(loan.balanceYen)}</strong>
              <button
                className="text-button"
                disabled={loan.balanceYen <= 0}
                onClick={() => openTransferForLoan(loan)}
              >
                返済を記録
              </button>
            </div>
          ))
        )}
      </section>
      <div className="section-heading">
        <div>
          <span className="eyebrow">資金の記録</span>
          <h2>入金・送金</h2>
        </div>
      </div>
      <section className="panel ledger-panel">
        {ledgerEntries.length === 0 ? (
          <div className="empty-state compact">
            <ArrowLeftRight />
            <span>記録はありません</span>
          </div>
        ) : (
          ledgerEntries.map((entry) => (
            <div className="funding-entry" key={entry.id}>
              <span className="funding-icon">
                <ArrowDownLeft />
              </span>
              <div>
                <b>{entry.description}</b>
                <small>{ledgerKindLabels[entry.kind] ?? "送金"}</small>
                {entry.fromMemberId && entry.toMemberId && (
                  <small>
                    {household.members.find((member) => member.id === entry.fromMemberId)
                      ?.displayName ?? "送金元"}
                    から
                    {household.members.find((member) => member.id === entry.toMemberId)
                      ?.displayName ?? "送金先"}
                    へ
                  </small>
                )}
                <small>{new Date(entry.occurredAt).toLocaleDateString("ja-JP")}</small>
              </div>
              <strong>{yen(entry.amountYen)}</strong>
            </div>
          ))
        )}
        {ledgerMoreError && <InlineError message={ledgerMoreError} />}
        {ledgerHasNextPage && (
          <button
            className="text-button"
            type="button"
            disabled={ledgerLoadingMore}
            onClick={loadMoreLedger}
          >
            {ledgerLoadingMore ? "読み込み中" : "さらに記録を表示"}
          </button>
        )}
      </section>
    </>
  );
}
