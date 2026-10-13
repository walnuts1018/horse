import { Plus, X } from "lucide-react";
import type { Household } from "../../../shared/api";
import type { PaymentDraft } from "../model/purchase-payments";

const yen = (amount: number) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);

export function EditPurchasePayments({
  household,
  rows,
  autoTotal,
  itemTotal,
  difference,
  onAdd,
  onPatch,
  onRemove,
}: {
  household: Household;
  rows: PaymentDraft[];
  autoTotal: boolean;
  itemTotal: number;
  difference: number;
  onAdd: () => void;
  onPatch: (key: string, patch: Partial<PaymentDraft>) => void;
  onRemove: (key: string) => void;
}) {
  return (
    <div className="field-group payment-allocation-field">
      <label>決済内訳</label>
      <div className="payment-allocation-list">
        {rows.map((payment, index) => (
          <div className="payment-allocation-row" key={payment.key}>
            <select
              aria-label={`決済元 ${index + 1}`}
              value={payment.paymentAccountId}
              onChange={(event) => onPatch(payment.key, { paymentAccountId: event.target.value })}
            >
              <option value="">決済元を選択</option>
              {household.accounts.map((account) => (
                <option key={account.id} value={account.id}>
                  {account.name}
                </option>
              ))}
            </select>
            <div className="yen-input">
              <span>¥</span>
              <input
                aria-label={`決済額 ${index + 1}`}
                inputMode="numeric"
                value={autoTotal && rows.length === 1 ? String(itemTotal || "") : payment.amountYen}
                onChange={(event) =>
                  onPatch(payment.key, { amountYen: event.target.value.replace(/\D/g, "") })
                }
              />
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
          </div>
        ))}
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
