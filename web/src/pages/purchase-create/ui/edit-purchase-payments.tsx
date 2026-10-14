/* oxlint-disable jsx-a11y/prefer-tag-over-role */
import { Plus, X } from "lucide-react";
import { useId, useRef, useState } from "react";

import type { Household, Purchase } from "../../../shared/api";
import type { PaymentDraft } from "../model/purchase-payments";

import { orderPaymentAccountsByUsage } from "../model/purchase-defaults";

import styles from "./edit-purchase-payments.module.css";

function PaymentAccountPicker({
  accounts,
  payment,
  index,
  invalid,
  describedBy,
  onSelect,
}: {
  accounts: Household["accounts"];
  payment: PaymentDraft;
  index: number;
  invalid: boolean;
  describedBy?: string;
  onSelect: (id: string) => void;
}) {
  const listId = useId();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const composing = useRef(false);
  const selected = accounts.find((account) => account.id === payment.paymentAccountId);
  const filtered = accounts.filter((account) =>
    account.name.toLocaleLowerCase("ja-JP").includes(query.trim().toLocaleLowerCase("ja-JP")),
  );

  const choose = (accountId: string) => {
    onSelect(accountId);
    setQuery("");
    setOpen(false);
  };

  return (
    <div
      className={styles.picker}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false);
      }}
    >
      <input
        aria-label={`決済元を検索 ${index + 1}`}
        aria-autocomplete="list"
        aria-controls={listId}
        aria-expanded={open}
        aria-activedescendant={
          open && activeIndex < filtered.length ? `${listId}-${activeIndex}` : undefined
        }
        aria-invalid={invalid}
        aria-describedby={describedBy}
        autoComplete="off"
        role="combobox"
        type="search"
        value={open ? query : (selected?.name ?? "")}
        placeholder="決済元を選択"
        onFocus={() => {
          setQuery("");
          setActiveIndex(0);
          setOpen(true);
        }}
        onChange={(event) => {
          setQuery(event.target.value);
          setActiveIndex(0);
          setOpen(true);
        }}
        onCompositionStart={() => {
          composing.current = true;
        }}
        onCompositionEnd={() => {
          composing.current = false;
        }}
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing || composing.current) return;
          if (event.key === "ArrowDown") {
            event.preventDefault();
            setActiveIndex((current) => Math.min(current + 1, Math.max(filtered.length - 1, 0)));
          } else if (event.key === "ArrowUp") {
            event.preventDefault();
            setActiveIndex((current) => Math.max(current - 1, 0));
          } else if (event.key === "Enter" && open) {
            event.preventDefault();
            if (activeIndex < filtered.length) choose(filtered[activeIndex].id);
          } else if (event.key === "Escape") {
            setOpen(false);
            setQuery("");
          }
        }}
      />
      {open && (
        <div className={styles.options} id={listId} role="listbox">
          {filtered.map((account, optionIndex) => (
            <div key={account.id}>
              <div
                aria-selected={account.id === payment.paymentAccountId}
                id={`${listId}-${optionIndex}`}
                role="option"
                tabIndex={-1}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => choose(account.id)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") choose(account.id);
                }}
              >
                {account.name}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

const yen = (amount: number) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);

export function EditPurchasePayments({
  household,
  history,
  rows,
  fieldError,
  autoTotal,
  itemTotal,
  difference,
  onSetAutoTotal,
  onAdd,
  onPatch,
  onRemove,
}: {
  household: Household;
  history: Purchase[];
  rows: PaymentDraft[];
  fieldError: { field: "paymentAccount" | "paymentAmount"; paymentKey: string } | null;
  autoTotal: boolean;
  itemTotal: number;
  difference: number;
  onSetAutoTotal: (enabled: boolean, materializedAmount?: string) => void;
  onAdd: () => void;
  onPatch: (key: string, patch: Partial<PaymentDraft>) => void;
  onRemove: (key: string) => void;
}) {
  const accounts = orderPaymentAccountsByUsage(household.accounts, history);
  return (
    <div className="field-group payment-allocation-field">
      <span className="field-legend">決済内訳</span>
      <div className="payment-allocation-list">
        {rows.map((payment, index) => {
          const isAutoRemainder = autoTotal && index === rows.length - 1;
          const autoAmount =
            itemTotal -
            rows.slice(0, index).reduce((sum, row) => sum + (Number(row.amountYen) || 0), 0);
          return (
            <div className="payment-allocation-row" key={payment.key}>
              <PaymentAccountPicker
                accounts={accounts}
                payment={payment}
                index={index}
                invalid={
                  fieldError?.field === "paymentAccount" && fieldError.paymentKey === payment.key
                }
                describedBy={
                  fieldError?.field === "paymentAccount" && fieldError.paymentKey === payment.key
                    ? `purchase-payment-account-error-${payment.key}`
                    : undefined
                }
                onSelect={(paymentAccountId) => onPatch(payment.key, { paymentAccountId })}
              />
              <div className="payment-amount-control">
                <div className="yen-input">
                  <span>¥</span>
                  <input
                    aria-label={`決済額 ${index + 1}`}
                    aria-invalid={
                      fieldError?.field === "paymentAmount" && fieldError.paymentKey === payment.key
                    }
                    aria-describedby={
                      fieldError?.field === "paymentAmount" && fieldError.paymentKey === payment.key
                        ? `purchase-payment-amount-error-${payment.key}`
                        : undefined
                    }
                    inputMode="numeric"
                    readOnly={isAutoRemainder}
                    value={isAutoRemainder ? String(autoAmount) : payment.amountYen}
                    onChange={(event) =>
                      onPatch(payment.key, { amountYen: event.target.value.replace(/\D/g, "") })
                    }
                  />
                </div>
                {index === rows.length - 1 && (
                  <button
                    className="payment-auto-toggle"
                    type="button"
                    aria-pressed={autoTotal}
                    onClick={() =>
                      onSetAutoTotal(!autoTotal, isAutoRemainder ? String(autoAmount) : undefined)
                    }
                  >
                    {autoTotal ? "残額を手入力" : "残額を自動計算"}
                  </button>
                )}
              </div>
              <button
                className="row-remove"
                type="button"
                aria-label={`決済元 ${index + 1} を削除`}
                disabled={rows.length <= 1}
                onClick={() => onRemove(payment.key)}
              >
                <X size={15} />
              </button>
              {fieldError?.paymentKey === payment.key && (
                <small
                  className="entry-error"
                  id={
                    fieldError.field === "paymentAccount"
                      ? `purchase-payment-account-error-${payment.key}`
                      : `purchase-payment-amount-error-${payment.key}`
                  }
                  role="alert"
                >
                  {fieldError.field === "paymentAccount"
                    ? "決済元を選択してください"
                    : "決済額を確認してください"}
                </small>
              )}
            </div>
          );
        })}
      </div>
      <div className="payment-allocation-footer">
        <button className="text-button" type="button" onClick={onAdd}>
          <Plus size={14} />
          決済元を追加
        </button>
        <span className={difference === 0 ? "payment-balanced" : "payment-unbalanced"}>
          {difference === 0
            ? "一致"
            : `${difference > 0 ? "不足" : "超過"} ${yen(Math.abs(difference))}`}
        </span>
      </div>
    </div>
  );
}
