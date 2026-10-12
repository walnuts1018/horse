import { useEffect, useState, type FormEvent } from "react";
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
  closePeriodMutation,
  ledgerQuery,
  loanBalancesQuery,
  makeIdempotencyKey,
  recordFundingMutation,
  recordTransferMutation,
  readGraphql,
  settlementQuery,
  type Connection,
  type Household,
  type LedgerEntry,
  type LoanBalance,
  type Settlement,
} from "../../../shared/api";
import { InlineError } from "../../../shared/ui";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const monthKey = () => new Date().toISOString().slice(0, 7);
const sum = (numbers: number[]) => numbers.reduce((total, value) => total + value, 0);

export function SettlementPage({ household }: { household: Household }) {
  const [period, setPeriod] = useState(monthKey());
  const [settlement, setSettlement] = useState<Settlement | null>(null);
  const [entries, setEntries] = useState<LedgerEntry[]>([]);
  const [loans, setLoans] = useState<LoanBalance[]>([]);
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const [transferOpen, setTransferOpen] = useState(false);
  const [transferFrom, setTransferFrom] = useState(household.members[0]?.id ?? "");
  const [transferTo, setTransferTo] = useState(household.members[1]?.id ?? "");
  const [transferKind, setTransferKind] = useState("ADVANCE");
  const [transferAmount, setTransferAmount] = useState("");
  const [transferDescription, setTransferDescription] = useState("");
  const [transferLoanId, setTransferLoanId] = useState("");
  const [transferPaymentAccountId, setTransferPaymentAccountId] = useState("");
  const load = async () => {
    setError("");
    try {
      const [settlementData, ledgerData, loanData] = await Promise.all([
        readGraphql<{ settlement: Settlement }>(settlementQuery, {
          householdId: household.id,
          period,
        }),
        readGraphql<{ ledger: Connection<LedgerEntry> }>(ledgerQuery, {
          householdId: household.id,
          first: 100,
        }),
        readGraphql<{ loanBalances: LoanBalance[] }>(loanBalancesQuery, {
          householdId: household.id,
        }),
      ]);
      setSettlement(settlementData.settlement);
      setEntries(ledgerData.ledger.edges.map((edge) => edge.node));
      setLoans(loanData.loanBalances);
    } catch (reason) {
      setError((reason as Error).message);
    }
  };
  useEffect(() => {
    void load();
  }, [household.id, period]);
  const recordFunding = async (memberId: string) => {
    const line = settlement?.lines.find((row) => row.memberId === memberId);
    const amountYen = line?.amountDueYen ?? 0;
    if (amountYen <= 0) return;
    setPending(true);
    try {
      await readGraphql(recordFundingMutation, {
        input: {
          householdId: household.id,
          memberId,
          poolId:
            household.pools.find((pool) => pool.kind === "SHARED")?.id ?? household.pools[0]?.id,
          amountYen,
          occurredAt: new Date().toISOString(),
          idempotencyKey: makeIdempotencyKey(),
        },
      });
      await load();
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const close = async () => {
    if (!settlement) return;
    setPending(true);
    try {
      await readGraphql(closePeriodMutation, {
        input: { householdId: household.id, period, expectedRevision: settlement.revision },
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
    const amountYen = Number(transferAmount);
    if (
      !transferFrom ||
      !transferTo ||
      transferFrom === transferTo ||
      amountYen < 1 ||
      (transferKind === "REPAYMENT" && !transferLoanId)
    )
      return;
    setPending(true);
    try {
      await readGraphql(recordTransferMutation, {
        input: {
          householdId: household.id,
          fromMemberId: transferFrom,
          toMemberId: transferTo,
          kind: transferKind,
          ...(transferKind === "REPAYMENT" ? { loanId: transferLoanId } : {}),
          paymentAccountId: transferPaymentAccountId || null,
          amountYen,
          occurredAt: new Date().toISOString(),
          description: transferDescription.trim() || "メンバー間の送金",
          idempotencyKey: makeIdempotencyKey(),
        },
      });
      setTransferAmount("");
      setTransferDescription("");
      setTransferLoanId("");
      setTransferPaymentAccountId("");
      setTransferOpen(false);
      await load();
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const totalDue = sum(settlement?.lines.map((line) => line.amountDueYen) ?? []);
  const openLoans = loans.filter((loan) => loan.balanceYen > 0);
  const openTransferForLoan = (loan: LoanBalance) => {
    setTransferKind("REPAYMENT");
    setTransferLoanId(loan.loanId);
    setTransferFrom(loan.borrowerMemberId);
    setTransferTo(loan.lenderMemberId);
    setTransferOpen(true);
  };
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
      <section className="settlement-hero">
        <div>
          <span>次回の入金</span>
          <h2>{yen(totalDue)}</h2>
          <small>
            {settlement?.status === "CLOSED"
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
            {settlement?.status === "CLOSED" ? "締め済み" : "未締め"}
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
                <button
                  className="text-button"
                  disabled={pending || line.amountDueYen <= 0}
                  onClick={() => void recordFunding(line.memberId)}
                >
                  入金を記録
                </button>
              </div>
            </div>
          ))
        )}
        <div className="settlement-actions">
          <button
            className="button button-soft"
            disabled={pending || settlement?.status === "CLOSED"}
            onClick={() => void close()}
          >
            <Check size={16} />
            期間を締める
          </button>
          <button
            className="button button-primary"
            onClick={() => setTransferOpen((open) => !open)}
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
                onChange={(event) => setTransferKind(event.target.value)}
              >
                <option value="ADVANCE">立て替え</option>
                <option value="LOAN">貸し借り</option>
                <option value="REPAYMENT">返済</option>
                <option value="GIFT">贈与</option>
              </select>
            </label>
            {transferKind === "REPAYMENT" && (
              <label>
                貸付
                <select
                  required
                  value={transferLoanId}
                  onChange={(event) => {
                    const loan = openLoans.find((row) => row.loanId === event.target.value);
                    setTransferLoanId(event.target.value);
                    if (loan) {
                      setTransferFrom(loan.borrowerMemberId);
                      setTransferTo(loan.lenderMemberId);
                    }
                  }}
                >
                  <option value="">貸付を選択</option>
                  {openLoans.map((loan) => (
                    <option key={loan.loanId} value={loan.loanId}>
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
                disabled={transferKind === "REPAYMENT"}
                onChange={(event) => setTransferFrom(event.target.value)}
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
                disabled={transferKind === "REPAYMENT"}
                onChange={(event) => setTransferTo(event.target.value)}
              >
                {household.members.map((member) => (
                  <option value={member.id} key={member.id}>
                    {member.displayName}
                  </option>
                ))}
              </select>
            </label>
            <label>
              決済口座
              <select
                value={transferPaymentAccountId}
                onChange={(event) => setTransferPaymentAccountId(event.target.value)}
              >
                <option value="">口座外</option>
                {household.accounts.map((account) => (
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
                onChange={(event) => setTransferAmount(event.target.value.replace(/\D/g, ""))}
                required
              />
            </label>
            <label className="transfer-description">
              摘要
              <input
                value={transferDescription}
                onChange={(event) => setTransferDescription(event.target.value)}
              />
            </label>
            <button
              className="button button-primary"
              disabled={pending || household.members.length < 2 || transferFrom === transferTo}
            >
              <Check size={16} />
              保存
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
              setTransferKind("LOAN");
              setTransferLoanId("");
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
            <div className="loan-balance-row" key={loan.loanId}>
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
        {entries.filter((entry) =>
          ["FUNDING", "TRANSFER", "ADVANCE", "LOAN", "REPAYMENT", "GIFT"].includes(entry.kind),
        ).length === 0 ? (
          <div className="empty-state compact">
            <ArrowLeftRight />
            <span>記録はありません</span>
          </div>
        ) : (
          entries
            .filter((entry) =>
              ["FUNDING", "TRANSFER", "ADVANCE", "LOAN", "REPAYMENT", "GIFT"].includes(entry.kind),
            )
            .map((entry) => (
              <div className="funding-entry" key={entry.id}>
                <span className="funding-icon">
                  <ArrowDownLeft />
                </span>
                <div>
                  <b>{entry.description}</b>
                  <small>{new Date(entry.occurredAt).toLocaleDateString("ja-JP")}</small>
                </div>
                <strong>{yen(entry.amountYen)}</strong>
              </div>
            ))
        )}
      </section>
    </>
  );
}
