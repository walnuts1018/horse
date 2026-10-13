import type { Household } from "../../../shared/api";
import type { PurchaseBulkEditController } from "../model/purchase-bulk-edit";

import { InlineError } from "../../../shared/ui";

import styles from "./ledger-search.module.css";

export function PurchaseBulkActions({
  accounts,
  bulkEdit,
  showSelectionControls,
}: {
  accounts: Household["accounts"];
  bulkEdit: PurchaseBulkEditController;
  showSelectionControls: boolean;
}) {
  return (
    <>
      {showSelectionControls && bulkEdit.selectedCount > 0 && (
        <section
          className={"panel ledger-bulk-actions " + styles.bulkActions}
          aria-label="選択した購入の一括編集"
        >
          <span>{bulkEdit.selectedCount}件の購入を選択中</span>
          <label>
            <input
              type="checkbox"
              checked={bulkEdit.merchantActive}
              disabled={bulkEdit.pending}
              onChange={(event) => bulkEdit.setMerchantActive(event.target.checked)}
            />
            店舗
            <input
              type="text"
              value={bulkEdit.merchant}
              disabled={!bulkEdit.merchantActive || bulkEdit.pending}
              onChange={(event) => bulkEdit.setMerchant(event.target.value)}
            />
          </label>
          <label>
            <input
              type="checkbox"
              checked={bulkEdit.occurredAtActive}
              disabled={bulkEdit.pending}
              onChange={(event) => bulkEdit.setOccurredAtActive(event.target.checked)}
            />
            日付
            <input
              type="datetime-local"
              value={bulkEdit.occurredAt}
              disabled={!bulkEdit.occurredAtActive || bulkEdit.pending}
              onChange={(event) => bulkEdit.setOccurredAt(event.target.value)}
            />
          </label>
          <label className="select-filter">
            <span>決済元</span>
            <select
              value={bulkEdit.paymentAccountId}
              disabled={bulkEdit.pending}
              onChange={(event) => bulkEdit.setPaymentAccountId(event.target.value)}
            >
              <option value="">変更なし</option>
              {accounts.map((account) => (
                <option key={account.id} value={account.id}>
                  {account.name}
                </option>
              ))}
            </select>
          </label>
          <button
            className="button button-primary"
            type="button"
            disabled={
              bulkEdit.pending ||
              (!bulkEdit.merchantActive &&
                !bulkEdit.occurredAtActive &&
                !bulkEdit.paymentAccountId) ||
              (bulkEdit.occurredAtActive && !bulkEdit.occurredAt)
            }
            onClick={() => void bulkEdit.apply()}
          >
            {bulkEdit.pending ? "更新中" : "一括変更"}
          </button>
          <button
            className="text-button"
            type="button"
            disabled={bulkEdit.pending}
            onClick={bulkEdit.clearSelection}
          >
            選択解除
          </button>
        </section>
      )}
      {bulkEdit.undo && (
        <section className={"panel ledger-bulk-actions " + styles.bulkActions} aria-live="polite">
          <span>{bulkEdit.undo.length}件の購入変更</span>
          <button
            className="text-button"
            type="button"
            disabled={bulkEdit.pending}
            onClick={() => void bulkEdit.undoChanges()}
          >
            一括変更を元に戻す
          </button>
        </section>
      )}
      {bulkEdit.error && <InlineError message={bulkEdit.error} />}
    </>
  );
}
