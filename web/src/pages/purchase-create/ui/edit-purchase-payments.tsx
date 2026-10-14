import { Plus, X } from "lucide-react";

import type { Household, Purchase } from "../../../shared/api";
import type { PaymentDraft } from "../model/purchase-payments";

import { orderPaymentAccountsByUsage } from "../model/purchase-defaults";

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
              <select
                aria-label={`決済元 ${index + 1}`}
                aria-invalid={
                  fieldError?.field === "paymentAccount" && fieldError.paymentKey === payment.key
                }
                aria-describedby={
                  fieldError?.field === "paymentAccount" && fieldError.paymentKey === payment.key
                    ? `purchase-payment-account-error-${payment.key}`
                    : undefined
                }
                value={payment.paymentAccountId}
                onChange={(event) => onPatch(payment.key, { paymentAccountId: event.target.value })}
              >
                <option value="">決済元を選択</option>
                {accounts.map((account) => (
                  <option key={account.id} value={account.id}>
                    {account.name}
                  </option>
                ))}
              </select>
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
