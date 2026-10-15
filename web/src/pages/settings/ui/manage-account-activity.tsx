import { ArrowLeftRight, Check, Plus } from "lucide-react";

import type { Household } from "../../../shared/api";

import { isOneOf } from "../../../shared/lib/guard";
import { InlineError } from "../../../shared/ui";
import { useAccountActivity } from "../model/account-activity";

import styles from "./settings-page.module.css";

const yen = (amount: number) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);

export function ManageAccountActivity({
  household,
  hidden,
  onNotice,
  onFormClean,
  onPendingChange,
  onRecorded,
}: {
  household: Household;
  hidden: boolean;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onFormClean: (formID: string) => void;
  onPendingChange: (pending: boolean) => void;
  onRecorded: () => void | Promise<void>;
}) {
  const activity = useAccountActivity({
    householdId: household.id,
    accounts: household.accounts,
    enabled: !hidden,
    onNotice,
    onFormClean,
    onPendingChange,
    onRecorded,
  });

  return (
    <section className={`panel ${styles.settingsSection}`} hidden={hidden}>
      <div className={`panel-heading ${styles.panelHeading}`}>
        <div>
          <span className="eyebrow">口座間の移動</span>
          <h2>資金移動</h2>
        </div>
        <ArrowLeftRight size={18} />
      </div>
      <form
        className={`allocation-rule-form ${styles.accountActivityForm}`}
        data-unsaved-id="account-income"
        onSubmit={(event) => void activity.saveIncome(event)}
      >
        <label>
          入金先
          <select
            value={activity.incomeAccountId}
            disabled={activity.pending || Boolean(activity.pendingIncomeIntent)}
            onChange={(event) => activity.setIncomeAccountId(event.target.value)}
            required
          >
            <option value="">口座を選択</option>
            {household.accounts
              .filter((account) => account.kind !== "CARD")
              .map((account) => (
                <option key={account.id} value={account.id}>
                  {account.name}
                </option>
              ))}
          </select>
        </label>
        <input
          aria-label="入金額"
          type="number"
          min="1"
          disabled={activity.pending || Boolean(activity.pendingIncomeIntent)}
          value={activity.incomeAmount}
          onChange={(event) => activity.setIncomeAmount(event.target.value)}
          placeholder="入金額"
          required
        />
        <input
          aria-label="入金日時"
          type="datetime-local"
          disabled={activity.pending || Boolean(activity.pendingIncomeIntent)}
          value={activity.incomeDate}
          onChange={(event) => activity.setIncomeDate(event.target.value)}
          required
        />
        <input
          value={activity.incomeDescription}
          disabled={activity.pending || Boolean(activity.pendingIncomeIntent)}
          onChange={(event) => activity.setIncomeDescription(event.target.value)}
          placeholder="入金の摘要"
        />
        <button
          type="submit"
          className="button button-soft"
          disabled={
            activity.pending || (!activity.pendingIncomeIntent && !activity.incomeAccountId)
          }
        >
          <Plus size={15} />
          {activity.pendingIncomeIntent ? "入金を再試行" : "入金を記録"}
        </button>
      </form>
      <form
        className={`allocation-rule-form ${styles.accountActivityForm}`}
        data-unsaved-id="account-movement"
        onSubmit={(event) => void activity.saveMovement(event)}
      >
        <select
          value={activity.movementKind}
          disabled={activity.pending || Boolean(activity.pendingMovementIntent)}
          onChange={(event) => {
            if (isOneOf(event.target.value, ["TRANSFER", "CARD_BILL_PAYMENT"]))
              activity.setMovementKind(event.target.value);
          }}
        >
          <option value="TRANSFER">口座移動</option>
          <option value="CARD_BILL_PAYMENT">カード請求の支払い</option>
        </select>
        <select
          aria-label="移動元"
          value={activity.movementFrom}
          disabled={activity.pending || Boolean(activity.pendingMovementIntent)}
          onChange={(event) => activity.setMovementFrom(event.target.value)}
          required
        >
          <option value="">移動元</option>
          {household.accounts
            .filter((account) =>
              activity.movementKind === "TRANSFER"
                ? account.kind !== "CARD"
                : account.kind === "BANK",
            )
            .map((account) => (
              <option key={account.id} value={account.id}>
                {account.name}
              </option>
            ))}
        </select>
        <select
          aria-label="移動先"
          value={activity.movementTo}
          disabled={activity.pending || Boolean(activity.pendingMovementIntent)}
          onChange={(event) => activity.setMovementTo(event.target.value)}
          required
        >
          <option value="">移動先</option>
          {household.accounts
            .filter((account) =>
              activity.movementKind === "TRANSFER"
                ? account.kind !== "CARD"
                : account.kind === "CARD",
            )
            .map((account) => (
              <option key={account.id} value={account.id}>
                {account.name}
              </option>
            ))}
        </select>
        <input
          aria-label="移動額"
          type="number"
          min="1"
          disabled={activity.pending || Boolean(activity.pendingMovementIntent)}
          value={activity.movementAmount}
          onChange={(event) => activity.setMovementAmount(event.target.value)}
          placeholder="金額"
          required
        />
        <input
          aria-label="移動日時"
          type="datetime-local"
          disabled={activity.pending || Boolean(activity.pendingMovementIntent)}
          value={activity.movementDate}
          onChange={(event) => activity.setMovementDate(event.target.value)}
          required
        />
        <input
          value={activity.movementDescription}
          disabled={activity.pending || Boolean(activity.pendingMovementIntent)}
          onChange={(event) => activity.setMovementDescription(event.target.value)}
          placeholder="摘要"
        />
        <button
          type="submit"
          className="button button-primary"
          disabled={
            activity.pending ||
            (!activity.pendingMovementIntent &&
              (!activity.movementFrom ||
                !activity.movementTo ||
                activity.movementFrom === activity.movementTo))
          }
        >
          <Check size={15} />
          {activity.pendingMovementIntent ? "同じ移動を再試行" : "記録"}
        </button>
      </form>
      <div className={styles.settingsRows}>
        {activity.incomes.map((income) => (
          <div className={styles.settingRow} key={income.id}>
            <div>
              <b>外部入金</b>
              <small>
                {new Date(income.occurredAt).toLocaleDateString("ja-JP")} ·{" "}
                {household.accounts.find((account) => account.id === income.accountId)?.name} ·{" "}
                {income.description}
              </small>
            </div>
            <strong>{yen(income.amountYen)}</strong>
          </div>
        ))}
        {activity.incomePageError && <InlineError message={activity.incomePageError} />}
        {activity.incomeHasNextPage && (
          <button
            type="button"
            className="button button-soft"
            disabled={activity.incomePageLoading}
            onClick={() => void activity.loadMoreIncomes()}
          >
            {activity.incomePageLoading ? "読み込み中" : "入金履歴をさらに表示"}
          </button>
        )}
        {activity.movements.map((movement) => (
          <div className={styles.settingRow} key={movement.id}>
            <div>
              <b>{movement.kind === "CARD_BILL_PAYMENT" ? "カード請求の支払い" : "口座移動"}</b>
              <small>
                {new Date(movement.occurredAt).toLocaleDateString("ja-JP")} ·{" "}
                {movement.fromAccount.name} → {movement.toAccount.name} · {movement.description}
              </small>
            </div>
            <strong>{yen(movement.amountYen)}</strong>
          </div>
        ))}
        {activity.movementPageError && <InlineError message={activity.movementPageError} />}
        {activity.movementHasNextPage && (
          <button
            type="button"
            className="button button-soft"
            disabled={activity.movementPageLoading}
            onClick={() => void activity.loadMoreMovements()}
          >
            {activity.movementPageLoading ? "読み込み中" : "口座移動履歴をさらに表示"}
          </button>
        )}
      </div>
    </section>
  );
}
