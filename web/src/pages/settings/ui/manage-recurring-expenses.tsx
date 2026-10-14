import { Check } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type SubmitEvent } from "react";

import { type Household, type RecurringExpense } from "../../../shared/api";
import { errorMessage } from "../../../shared/lib";
import { isOneOf } from "../../../shared/lib/guard";
import { InlineError } from "../../../shared/ui";
import { readRecurringExpenses } from "../api/read-recurring-expenses";
import {
  archiveRecurringExpense,
  createRecurringExpense,
  updateRecurringExpense,
} from "../api/recurring-expense-management";
import {
  matchRecurringExpense,
  readRecurringExpenseMatchCandidates,
  type RecurringExpenseMatchCandidate,
} from "../api/recurring-expense-matching";

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
  const [recurring, setRecurring] = useState<RecurringExpense[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState("");
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
  const requestSequence = useRef(0);
  const [matchingExpenseId, setMatchingExpenseId] = useState<string | null>(null);
  const [matchCandidates, setMatchCandidates] = useState<RecurringExpenseMatchCandidate[]>([]);
  const [selectedPurchaseId, setSelectedPurchaseId] = useState("");
  const [today] = useState(() =>
    new Intl.DateTimeFormat("en-CA", {
      timeZone: "Asia/Tokyo",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(new Date()),
  );

  const refresh = useCallback(async () => {
    const sequence = ++requestSequence.current;
    setLoading(true);
    setLoadError("");
    try {
      const data = await readRecurringExpenses(householdId, true);
      if (requestSequence.current === sequence) setRecurring(data);
    } catch (reason) {
      if (requestSequence.current === sequence) setLoadError(errorMessage(reason));
      throw reason;
    } finally {
      if (requestSequence.current === sequence) setLoading(false);
    }
  }, [householdId]);

  useEffect(() => {
    if (hidden) return;
    queueMicrotask(() => {
      void refresh().catch(() => undefined);
    });
  }, [hidden, refresh]);

  const saveRecurring = async (event: SubmitEvent<HTMLFormElement>) => {
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
      await refresh().catch(() => undefined);
      onNotice("定期支出を保存しました", "success");
    } catch (reason) {
      onNotice(errorMessage(reason), "error");
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
      await refresh().catch(() => undefined);
      onNotice(expense.archived ? "定期支出を再表示しました" : "定期支出を終了しました", "success");
    } catch (reason) {
      onNotice(errorMessage(reason), "error");
    } finally {
      setPending(false);
      onPendingChange(false);
    }
  };

  const beginMatching = async (expense: RecurringExpense) => {
    try {
      const candidates = await readRecurringExpenseMatchCandidates(householdId, expense.merchant);
      setMatchingExpenseId(expense.id);
      setMatchCandidates(
        candidates.filter(
          (candidate) => candidate.status === "POSTED" || candidate.status === "CLOSED",
        ),
      );
      setSelectedPurchaseId("");
    } catch (reason) {
      onNotice(errorMessage(reason), "error");
    }
  };

  const confirmMatch = async (expense: RecurringExpense) => {
    if (!selectedPurchaseId) return;
    setPending(true);
    onPendingChange(true);
    try {
      await matchRecurringExpense({
        householdId,
        expenseId: expense.id,
        purchaseId: selectedPurchaseId,
        baseRevision: expense.revision,
      });
      setMatchingExpenseId(null);
      setSelectedPurchaseId("");
      await refresh().catch(() => undefined);
      onNotice("購入実績を照合しました", "success");
    } catch (reason) {
      onNotice(errorMessage(reason), "error");
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
      {loading && <output>定期支出を読み込んでいます</output>}
      {loadError && (
        <div role="alert">
          <InlineError message={loadError} />
          <button
            type="button"
            className="text-button"
            disabled={loading}
            onClick={() => void refresh().catch(() => undefined)}
          >
            再試行
          </button>
        </div>
      )}
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
                · 次回 {expense.nextDueOn} ·{" "}
                {expense.archived || (expense.endsOn && expense.endsOn < expense.nextDueOn)
                  ? "終了"
                  : expense.nextDueOn < today
                    ? "未払い"
                    : expense.nextDueOn === today
                      ? "本日期日"
                      : "予定"}
              </small>
            </div>
            {!expense.archived && (
              <button
                type="button"
                className="text-button"
                disabled={pending}
                onClick={() => void beginMatching(expense)}
              >
                実績を照合
              </button>
            )}
            <button
              type="button"
              className="text-button"
              disabled={pending || expense.archived}
              onClick={() => editRecurring(expense)}
            >
              編集
            </button>
            <button
              type="button"
              className="text-button"
              disabled={pending}
              onClick={() => void toggleRecurringExpenseVisibility(expense)}
            >
              {expense.archived ? "再表示" : "終了"}
            </button>
            {matchingExpenseId === expense.id && (
              <div className={styles.matchControls}>
                {matchCandidates.length > 0 ? (
                  <select
                    aria-label={`${expense.name}に照合する購入`}
                    value={selectedPurchaseId}
                    onChange={(event) => setSelectedPurchaseId(event.target.value)}
                  >
                    <option value="">購入を選択</option>
                    {matchCandidates.map((candidate) => (
                      <option key={candidate.id} value={candidate.id}>
                        {candidate.occurredAt.slice(0, 10)} · {candidate.merchant || "店舗なし"} ·{" "}
                        {yen(Number(candidate.totalYen))}
                      </option>
                    ))}
                  </select>
                ) : (
                  <small>照合できる購入がありません</small>
                )}
                <button
                  type="button"
                  className="button button-primary"
                  disabled={pending || !selectedPurchaseId}
                  onClick={() => void confirmMatch(expense)}
                >
                  照合
                </button>
                <button
                  type="button"
                  className="text-button"
                  onClick={() => setMatchingExpenseId(null)}
                >
                  閉じる
                </button>
              </div>
            )}
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
          onChange={(event) => {
            if (isOneOf(event.target.value, ["WEEKLY", "MONTHLY", "YEARLY"]))
              setRecurringCadence(event.target.value);
          }}
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
        <button type="submit" className="button button-primary" disabled={pending}>
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
