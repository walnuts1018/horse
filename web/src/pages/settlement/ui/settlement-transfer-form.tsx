import type { FormEvent } from "react";

import { Check } from "lucide-react";

import type { Household } from "../../../shared/api";

export function SettlementTransferForm({
  household,
  pending,
  hasPendingTransfer,
  transferKind,
  transferFrom,
  transferTo,
  transferPaymentAccountId,
  transferDestinationAccountId,
  transferAmount,
  transferDescription,
  invalidTransferAmount,
  onSubmit,
  onKindChange,
  onFromChange,
  onToChange,
  onPaymentAccountChange,
  onDestinationAccountChange,
  onAmountChange,
  onDescriptionChange,
}: {
  household: Household;
  pending: boolean;
  hasPendingTransfer: boolean;
  transferKind: "ADVANCE" | "LOAN" | "GIFT";
  transferFrom: string;
  transferTo: string;
  transferPaymentAccountId: string;
  transferDestinationAccountId: string;
  transferAmount: string;
  transferDescription: string;
  invalidTransferAmount: boolean;
  onSubmit: (event: FormEvent) => void;
  onKindChange: (kind: "ADVANCE" | "LOAN" | "GIFT") => void;
  onFromChange: (memberId: string) => void;
  onToChange: (memberId: string) => void;
  onPaymentAccountChange: (accountId: string) => void;
  onDestinationAccountChange: (accountId: string) => void;
  onAmountChange: (amount: string) => void;
  onDescriptionChange: (description: string) => void;
}) {
  return (
    <form className="transfer-form" onSubmit={onSubmit}>
      <label>
        種別
        <select
          value={transferKind}
          disabled={pending || hasPendingTransfer}
          onChange={(event) => onKindChange(event.target.value as typeof transferKind)}
        >
          <option value="ADVANCE">立て替え</option>
          <option value="LOAN">貸し借り</option>
          <option value="GIFT">贈与</option>
        </select>
      </label>
      <label>
        {transferKind === "LOAN" ? "貸主" : transferKind === "GIFT" ? "贈与者" : "立替者"}
        <select
          value={transferFrom}
          disabled={pending || hasPendingTransfer}
          onChange={(event) => onFromChange(event.target.value)}
        >
          {household.members.map((member) => (
            <option value={member.id} key={member.id}>
              {member.displayName}
            </option>
          ))}
        </select>
      </label>
      <label>
        {transferKind === "LOAN" ? "借主" : transferKind === "GIFT" ? "受贈者" : "受益者"}
        <select
          value={transferTo}
          disabled={pending || hasPendingTransfer}
          onChange={(event) => onToChange(event.target.value)}
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
          onChange={(event) => onPaymentAccountChange(event.target.value)}
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
          onChange={(event) => onDestinationAccountChange(event.target.value)}
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
          onChange={(event) => onAmountChange(event.target.value.replace(/\D/g, ""))}
          required
        />
      </label>
      <label className="transfer-description">
        摘要
        <input
          value={transferDescription}
          disabled={pending || hasPendingTransfer}
          onChange={(event) => onDescriptionChange(event.target.value)}
        />
      </label>
      <button
        className="button button-primary"
        disabled={
          pending ||
          household.members.length < 2 ||
          transferFrom === transferTo ||
          invalidTransferAmount
        }
      >
        <Check size={16} />
        {hasPendingTransfer ? "同じ送金を再試行" : "保存"}
      </button>
    </form>
  );
}
