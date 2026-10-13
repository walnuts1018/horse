import { Check } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";

import { readRecurringExpenses } from "../../../entities/recurring-expense";
import { type Household, type RecurringExpense } from "../../../shared/api";
import {
  archiveRecurringExpense,
  createRecurringExpense,
  updateRecurringExpense,
} from "../api/recurring-expense-management";

import styles from "./settings-page.module.css";

const localDate = () => new Date().toISOString().slice(0, 10);
const yen = (amount: number) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);

export function ManageRecurringExpenses({
  householdId,
  categories,
  accounts,
  hidden,
  onNotice,
  onFormDirty,
  onFormClean,
  onPendingChange,
}: {
  householdId: string;
  categories: Household["categories"];
  accounts: Household["accounts"];
  hidden: boolean;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onFormDirty: (formID: string) => void;
  onFormClean: (formID: string) => void;
  onPendingChange: (pending: boolean) => void;
}) {
  const onNoticeRef = useRef(onNotice);
  const [recurring, setRecurring] = useState<RecurringExpense[]>([]);
  const [recurringName, setRecurringName] = useState("");
  const [recurringMerchant, setRecurringMerchant] = useState("");
  const [recurringAmount, setRecurringAmount] = useState("");
  const [recurringCategoryId, setRecurringCategoryId] = useState("");
  const [recurringAccountId, setRecurringAccountId] = useState("");
  const [recurringCadence, setRecurringCadence] = useState<RecurringExpense["cadence"]>("MONTHLY");
  const [recurringStartsOn, setRecurringStartsOn] = useState(localDate);
  const [recurringNextDueOn, setRecurringNextDueOn] = useState(localDate);
  const [recurringEndsOn, setRecurringEndsOn] = useState("");
  const [editingRecurring, setEditingRecurring] = useState<RecurringExpense | null>(null);
  const [pending, setPending] = useState(false);

  useEffect(() => {
    onNoticeRef.current = onNotice;
  }, [onNotice]);

  const refresh = useCallback(async () => {
    setRecurring(await readRecurringExpenses(householdId, true));
  }, [householdId]);

  useEffect(() => {
    void refresh().catch((reason) => onNoticeRef.current((reason as Error).message, "error"));
  }, [refresh]);

  const saveRecurring = async (event: FormEvent) => {
    event.preventDefault();
    const amountYen = Number(recurringAmount);
    if (!recurringName.trim() || !amountYen || amountYen < 1) return;
    setPending(true);
    onPendingChange(true);
    try {
      const input = {
        householdId,
        name: recurringName.trim(),
        merchant: recurringMerchant.trim(),
        amountYen,
        categoryId: recurringCategoryId || null,
        paymentAccountId: recurringAccountId || null,
        cadence: recurringCadence,
        startsOn: recurringStartsOn,
        nextDueOn: recurringNextDueOn,
        endsOn: recurringEndsOn || null,
      };
      if (editingRecurring) {
        await updateRecurringExpense({
          ...input,
          expenseId: editingRecurring.id,
          baseRevision: editingRecurring.revision,
        });
      } else {
        await createRecurringExpense(input);
      }
      onFormClean("recurring-expense");
      setEditingRecurring(null);
      setRecurringName("");
      setRecurringMerchant("");
      setRecurringAmount("");
      setRecurringEndsOn("");
      await refresh();
      onNotice("定期支出を保存しました", "success");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
      onPendingChange(false);
    }
  };

  const editRecurring = (expense: RecurringExpense) => {
    onFormDirty("recurring-expense");
    setEditingRecurring(expense);
    setRecurringName(expense.name);
    setRecurringMerchant(expense.merchant);
    setRecurringAmount(String(expense.amountYen));
    setRecurringCategoryId(expense.categoryId ?? "");
    setRecurringAccountId(expense.paymentAccountId ?? "");
    setRecurringCadence(expense.cadence);
    setRecurringStartsOn(expense.startsOn);
    setRecurringNextDueOn(expense.nextDueOn);
    setRecurringEndsOn(expense.endsOn ?? "");
  };

  const toggleRecurringExpenseVisibility = async (expense: RecurringExpense) => {
    setPending(true);
    onPendingChange(true);
    try {
      await archiveRecurringExpense({
        expenseId: expense.id,
        baseRevision: expense.revision,
        archived: !expense.archived,
      });
      await refresh();
      onNotice(expense.archived ? "定期支出を再表示しました" : "定期支出を終了しました", "success");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
      onPendingChange(false);
    }
  };

  const cancelEdit = () => {
    setEditingRecurring(null);
    setRecurringName("");
    setRecurringMerchant("");
    setRecurringAmount("");
    setRecurringCategoryId("");
    setRecurringAccountId("");
    setRecurringCadence("MONTHLY");
    setRecurringStartsOn(localDate());
    setRecurringNextDueOn(localDate());
    setRecurringEndsOn("");
    onFormClean("recurring-expense");
  };

  return (
    <section className={`panel ${styles.settingsSection}`} hidden={hidden}>
      <div className={`panel-heading ${styles.panelHeading}`}>
        <div>
          <span className="eyebrow">予定</span>
          <h2>定期支出</h2>
        </div>
        <span className="count-badge">
          {recurring.filter((expense) => !expense.archived).length}
        </span>
      </div>
      <div className={styles.settingsRows}>
        {recurring.map((expense) => (
          <div className={styles.settingRow} key={expense.id}>
            <div>
              <b>{expense.name}</b>
              <small>
                {expense.merchant} · {yen(expense.amountYen)} ·{" "}
                {expense.cadence === "WEEKLY"
                  ? "毎週"
                  : expense.cadence === "YEARLY"
                    ? "毎年"
                    : "毎月"}{" "}
                · 次回 {expense.nextDueOn}
              </small>
            </div>
            <button
              className="text-button"
              disabled={pending || expense.archived}
              onClick={() => editRecurring(expense)}
            >
              編集
            </button>
            <button
              className="text-button"
              disabled={pending}
              onClick={() => void toggleRecurringExpenseVisibility(expense)}
            >
              {expense.archived ? "再表示" : "終了"}
            </button>
          </div>
        ))}
      </div>
      <form
        className="allocation-rule-form"
        data-unsaved-id="recurring-expense"
        onSubmit={(event) => void saveRecurring(event)}
      >
        <input
          value={recurringName}
          onChange={(event) => setRecurringName(event.target.value)}
          placeholder="支出名"
          required
        />
        <input
          value={recurringMerchant}
          onChange={(event) => setRecurringMerchant(event.target.value)}
          placeholder="店舗"
        />
        <input
          aria-label="定期支出額"
          type="number"
          min="1"
          value={recurringAmount}
          onChange={(event) => setRecurringAmount(event.target.value)}
          placeholder="金額"
          required
        />
        <select
          aria-label="頻度"
          value={recurringCadence}
          onChange={(event) =>
            setRecurringCadence(event.target.value as RecurringExpense["cadence"])
          }
        >
          <option value="WEEKLY">毎週</option>
          <option value="MONTHLY">毎月</option>
          <option value="YEARLY">毎年</option>
        </select>
        <select
          aria-label="カテゴリ"
          value={recurringCategoryId}
          onChange={(event) => setRecurringCategoryId(event.target.value)}
        >
          <option value="">カテゴリなし</option>
          {categories
            .filter((category) => !category.archived)
            .map((category) => (
              <option key={category.id} value={category.id}>
                {category.name}
              </option>
            ))}
        </select>
        <select
          aria-label="決済元"
          value={recurringAccountId}
          onChange={(event) => setRecurringAccountId(event.target.value)}
        >
          <option value="">決済元なし</option>
          {accounts.map((account) => (
            <option key={account.id} value={account.id}>
              {account.name}
            </option>
          ))}
        </select>
        <label>
          開始日
          <input
            type="date"
            value={recurringStartsOn}
            onChange={(event) => setRecurringStartsOn(event.target.value)}
            required
          />
        </label>
        <label>
          次回日
          <input
            type="date"
            value={recurringNextDueOn}
            onChange={(event) => setRecurringNextDueOn(event.target.value)}
            required
          />
        </label>
        <label>
          終了日
          <input
            type="date"
            value={recurringEndsOn}
            onChange={(event) => setRecurringEndsOn(event.target.value)}
          />
        </label>
        <button className="button button-primary" disabled={pending}>
          <Check size={15} />
          {editingRecurring ? "変更を保存" : "予定を追加"}
        </button>
        {editingRecurring && (
          <button type="button" className="text-button" onClick={cancelEdit}>
            取消
          </button>
        )}
      </form>
    </section>
  );
}
