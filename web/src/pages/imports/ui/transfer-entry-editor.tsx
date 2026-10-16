import type { Account, Household } from "../../../shared/api";

import { yen, type StatementEntry } from "../model/imports";

type TransferDraft = {
  statementAccountId: string;
  otherAccountId: string;
  fromMemberId?: string;
  toMemberId?: string;
  loanId?: string;
  existingTransferId?: string;
  existingMovementId?: string;
};
type Loan = { id: string; lenderMemberId: string; borrowerMemberId: string; balanceYen: number };
type Candidate = { id: string; description: string; occurredAt: string };

type Props = {
  entry: StatementEntry;
  household: Household;
  accounts: Account[];
  loans: Loan[];
  draft: TransferDraft;
  matchingTransfers: Candidate[];
  matchingMovements: Candidate[];
  pending: boolean;
  confirmed: boolean;
  issue?: string;
  onChange: (update: Partial<TransferDraft>) => void;
  onSubmit: () => void;
};

export function TransferEntryEditor({
  entry,
  household,
  accounts,
  loans,
  draft,
  matchingTransfers,
  matchingMovements,
  pending,
  confirmed,
  issue,
  onChange,
  onSubmit,
}: Props) {
  const isTransfer = entry.kind === "ACCOUNT_TRANSFER";
  const isCardBillPayment = entry.kind === "CARD_BILL_PAYMENT";
  const isLoan = entry.kind === "LOAN";
  const isRepayment = entry.kind === "REPAYMENT";
  const selectedTransferId = draft.existingTransferId ?? "";
  const selectedMovementId = draft.existingMovementId ?? "";

  if (issue) return <small>{issue}</small>;
  if (confirmed) {
    return (
      <small>
        {isCardBillPayment
          ? "カード引落し登録済み"
          : isLoan
            ? "貸付登録済み"
            : isRepayment
              ? "返済登録済み"
              : "振替登録済み"}
      </small>
    );
  }

  return (
    <>
      <label>
        <span>
          {isLoan || isRepayment
            ? "明細の銀行口座"
            : isCardBillPayment
              ? "引落口座"
              : entry.amount < 0
                ? "振替元の明細口座"
                : "振替先の明細口座"}
        </span>
        <select
          value={draft.statementAccountId}
          disabled={pending}
          onChange={(event) =>
            onChange({ statementAccountId: event.target.value, otherAccountId: "" })
          }
        >
          <option value="">口座を選択</option>
          {accounts
            .filter((account) =>
              isCardBillPayment || isLoan || isRepayment
                ? account.kind === "BANK"
                : account.kind !== "CARD",
            )
            .map((account) => (
              <option key={account.id} value={account.id}>
                {account.name}
              </option>
            ))}
        </select>
      </label>
      {(isTransfer || isCardBillPayment) && (
        <label>
          <span>
            {isCardBillPayment ? "引落対象のカード" : entry.amount < 0 ? "振替先" : "振替元"}
          </span>
          <select
            value={draft.otherAccountId}
            disabled={pending || !draft.statementAccountId}
            onChange={(event) => onChange({ otherAccountId: event.target.value })}
          >
            <option value="">口座を選択</option>
            {accounts
              .filter(
                (account) =>
                  (isCardBillPayment ? account.kind === "CARD" : account.kind !== "CARD") &&
                  account.id !== draft.statementAccountId,
              )
              .map((account) => (
                <option key={account.id} value={account.id}>
                  {account.name}
                </option>
              ))}
          </select>
        </label>
      )}
      {isLoan && (
        <>
          <label>
            <span>貸主</span>
            <select
              value={draft.fromMemberId ?? ""}
              disabled={pending}
              onChange={(event) => onChange({ fromMemberId: event.target.value })}
            >
              <option value="">メンバーを選択</option>
              {household.members.map((member) => (
                <option key={member.id} value={member.id}>
                  {member.displayName}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>借主</span>
            <select
              value={draft.toMemberId ?? ""}
              disabled={pending}
              onChange={(event) => onChange({ toMemberId: event.target.value })}
            >
              <option value="">メンバーを選択</option>
              {household.members
                .filter((member) => member.id !== draft.fromMemberId)
                .map((member) => (
                  <option key={member.id} value={member.id}>
                    {member.displayName}
                  </option>
                ))}
            </select>
          </label>
        </>
      )}
      {isRepayment && (
        <label>
          <span>返済対象の貸付</span>
          <select
            value={draft.loanId ?? ""}
            disabled={pending}
            onChange={(event) => onChange({ loanId: event.target.value })}
          >
            <option value="">貸付を選択</option>
            {loans
              .filter((loan) => loan.balanceYen > 0)
              .map((loan) => (
                <option key={loan.id} value={loan.id}>
                  {
                    household.members.find((member) => member.id === loan.lenderMemberId)
                      ?.displayName
                  }{" "}
                  →{" "}
                  {
                    household.members.find((member) => member.id === loan.borrowerMemberId)
                      ?.displayName
                  }{" "}
                  · {yen(loan.balanceYen)}
                </option>
              ))}
          </select>
        </label>
      )}
      {matchingTransfers.length > 0 && (
        <label>
          <span>既存の取引</span>
          <select
            value={selectedTransferId}
            disabled={pending}
            onChange={(event) => onChange({ existingTransferId: event.target.value })}
          >
            <option value="">新しい取引として記録</option>
            {matchingTransfers.map((candidate) => (
              <option key={candidate.id} value={candidate.id}>
                {candidate.occurredAt.slice(0, 10)} · {candidate.description}
              </option>
            ))}
          </select>
        </label>
      )}
      {(isTransfer || isCardBillPayment) && matchingMovements.length > 0 && (
        <label>
          <span>既存の口座移動</span>
          <select
            value={selectedMovementId}
            disabled={pending}
            onChange={(event) => onChange({ existingMovementId: event.target.value })}
          >
            <option value="">新しい口座移動を記録</option>
            {matchingMovements.map((candidate) => (
              <option key={candidate.id} value={candidate.id}>
                {candidate.occurredAt.slice(0, 10)} · {candidate.description}
              </option>
            ))}
          </select>
        </label>
      )}
      <button
        type="button"
        className="text-button"
        disabled={
          pending ||
          !draft.statementAccountId ||
          (!selectedTransferId &&
            !selectedMovementId &&
            (isTransfer || isCardBillPayment
              ? !draft.otherAccountId
              : isLoan
                ? !draft.fromMemberId ||
                  !draft.toMemberId ||
                  draft.fromMemberId === draft.toMemberId
                : !loans.find((loan) => loan.id === draft.loanId && loan.balanceYen > 0)))
        }
        onClick={onSubmit}
      >
        {selectedMovementId
          ? "既存の口座移動に紐付け"
          : selectedTransferId
            ? "既存の取引に紐付け"
            : isCardBillPayment
              ? "カード引落しを記録"
              : isLoan
                ? "貸付を記録"
                : isRepayment
                  ? "返済を記録"
                  : "振替を記録"}
      </button>
    </>
  );
}
