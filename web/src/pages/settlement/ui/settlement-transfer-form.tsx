import type { SubmitEvent } from "react";

import { Check } from "lucide-react";

import type { Household } from "../../../shared/api";

import { isOneOf } from "../../../shared/lib";

import "./settlement-transfer-form.css";

const transferActions = [
  { kind: "ADVANCE", label: "立て替えた", detail: "あとで精算するお金" },
  { kind: "LOAN", label: "お金を貸した", detail: "返してもらうお金" },
  { kind: "GIFT", label: "お金をあげた", detail: "返してもらわないお金" },
] as const;

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
  onSubmit: (event: SubmitEvent<HTMLFormElement>) => void;
  onKindChange: (kind: "ADVANCE" | "LOAN" | "GIFT") => void;
  onFromChange: (memberId: string) => void;
  onToChange: (memberId: string) => void;
  onPaymentAccountChange: (accountId: string) => void;
  onDestinationAccountChange: (accountId: string) => void;
  onAmountChange: (amount: string) => void;
  onDescriptionChange: (description: string) => void;
}) {
  return (
    <form className="transfer-form settlement-transfer-entry-form" onSubmit={onSubmit}>
      <fieldset className="transfer-kind">
        <legend>どんな送金ですか？</legend>
        <div className="transfer-kind-options">
          {transferActions.map(({ kind, label, detail }) => (
            <label className="transfer-kind-option" key={kind}>
              <input
                type="radio"
                name="transfer-kind"
                value={kind}
                checked={transferKind === kind}
                disabled={pending || hasPendingTransfer}
                onChange={(event) => {
                  if (isOneOf(event.target.value, ["ADVANCE", "LOAN", "GIFT"] as const)) {
                    onKindChange(event.target.value);
                  }
                }}
              />
              <span>
                <strong>{label}</strong>
                <small>{detail}</small>
              </span>
            </label>
          ))}
        </div>
      </fieldset>
      <label>
        {transferKind === "LOAN" ? "貸主" : transferKind === "GIFT" ? "贈与者" : "立替者"}
        <select
          value={transferFrom}
          disabled={pending || hasPendingTransfer}
          onChange={(event) => onFromChange(event.target.value)}
        >
          {household.members.map((member) => (
            <option value={member.id} key={member.id} disabled={member.id === transferTo}>
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
            <option value={member.id} key={member.id} disabled={member.id === transferFrom}>
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
        type="submit"
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
