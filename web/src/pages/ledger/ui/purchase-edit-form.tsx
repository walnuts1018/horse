import { Plus, X } from "lucide-react";

import type { Household, Purchase } from "../../../shared/api";
import type { PurchaseDraft } from "../model/purchase-draft";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);

export function PurchaseEditForm({
  purchase,
  draft,
  accounts,
  onChange,
  onCancel,
  onSave,
  saveState,
}: {
  purchase: Purchase;
  draft: PurchaseDraft;
  accounts: Household["accounts"];
  onChange: (draft: PurchaseDraft) => void;
  onCancel: () => void;
  onSave: (purchase: Purchase) => void | Promise<void>;
  saveState: "unsaved" | "saving" | "saved" | "conflict";
}) {
  const saveStateLabel = {
    unsaved: "未保存の変更があります",
    saving: "保存中",
    saved: "保存済み",
    conflict: "競合があります。最新の記録を確認してください",
  }[saveState];

  return (
    <form
      className="purchase-edit-form"
      onSubmit={(event) => {
        event.preventDefault();
        void onSave(purchase);
      }}
    >
      <output
        className={`purchase-edit-status purchase-edit-status-${saveState}`}
        aria-live="polite"
      >
        {saveStateLabel}
      </output>
      <input
        aria-label="店名"
        value={draft.merchant}
        onChange={(event) => onChange({ ...draft, merchant: event.target.value })}
      />
      <input
        aria-label="購入日時"
        type="datetime-local"
        value={draft.occurredAt}
        onChange={(event) => onChange({ ...draft, occurredAt: event.target.value })}
      />
      <div className="purchase-edit-payments">
        <b>決済内訳</b>
        {draft.paymentAllocations.map((payment, index) => (
          <div className="purchase-edit-payment-row" key={payment.key}>
            <select
              aria-label={"決済元 " + (index + 1)}
              value={payment.paymentAccountId}
              onChange={(event) =>
                onChange({
                  ...draft,
                  paymentAllocations: draft.paymentAllocations.map((row, rowIndex) =>
                    rowIndex === index ? { ...row, paymentAccountId: event.target.value } : row,
                  ),
                })
              }
            >
              <option value="">決済元を選択</option>
              {accounts.map((account) => (
                <option key={account.id} value={account.id}>
                  {account.name}
                </option>
              ))}
            </select>
            <input
              aria-label={"決済額 " + (index + 1)}
              inputMode="numeric"
              value={payment.amountYen}
              onChange={(event) =>
                onChange({
                  ...draft,
                  paymentAllocations: draft.paymentAllocations.map((row, rowIndex) =>
                    rowIndex === index
                      ? { ...row, amountYen: event.target.value.replace(/\D/g, "") }
                      : row,
                  ),
                })
              }
            />
            <button
              className="row-remove"
              type="button"
              aria-label={"決済元 " + (index + 1) + " を削除"}
              onClick={() =>
                onChange({
                  ...draft,
                  paymentAllocations: draft.paymentAllocations.filter(
                    (_, rowIndex) => rowIndex !== index,
                  ),
                })
              }
            >
              <X size={14} />
            </button>
          </div>
        ))}
        <button
          className="text-button"
          type="button"
          onClick={() =>
            onChange({
              ...draft,
              paymentAllocations: [
                ...draft.paymentAllocations,
                { key: crypto.randomUUID(), paymentAccountId: "", amountYen: "" },
              ],
            })
          }
        >
          <Plus size={14} />
          決済元を追加
        </button>
        <small>
          差額{" "}
          {yen(
            purchase.totalYen -
              draft.paymentAllocations.reduce(
                (total, currentPayment) => total + Number(currentPayment.amountYen || 0),
                0,
              ),
          )}
        </small>
      </div>
      <div className="edit-actions">
        <button
          type="button"
          className="button button-soft"
          onClick={onCancel}
          disabled={saveState === "saving"}
        >
          キャンセル
        </button>
        <button
          className="button button-primary"
          type="submit"
          disabled={saveState === "saving" || saveState === "conflict"}
        >
          {saveState === "saving" ? "保存中" : "買い物を保存"}
        </button>
      </div>
    </form>
  );
}
