import { Check } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type SubmitEvent } from "react";

import { readBudgets } from "../../../entities/budget";
import { type Budget, type Household } from "../../../shared/api";
import { errorMessage } from "../../../shared/lib";
import { archiveBudget, createBudget, updateBudget } from "../api/budget-management";

import styles from "./settings-page.module.css";

const currentMonth = () =>
  new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Tokyo",
    year: "numeric",
    month: "2-digit",
  }).format(new Date());

const yen = (amount: number) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);

export function ManageBudgets({
  householdId,
  categories,
  hidden,
  onNotice,
  onFormDirty,
  onFormClean,
  onPendingChange,
}: {
  householdId: string;
  categories: Household["categories"];
  hidden: boolean;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onFormDirty: (formID: string) => void;
  onFormClean: (formID: string) => void;
  onPendingChange: (pending: boolean) => void;
}) {
  const onNoticeRef = useRef(onNotice);
  const [budgetPeriod, setBudgetPeriod] = useState(currentMonth);
  const [budgets, setBudgets] = useState<Budget[]>([]);
  const [budgetName, setBudgetName] = useState("");
  const [budgetCategoryId, setBudgetCategoryId] = useState("");
  const [budgetAmount, setBudgetAmount] = useState("");
  const [editingBudget, setEditingBudget] = useState<Budget | null>(null);
  const [pending, setPending] = useState(false);

  useEffect(() => {
    onNoticeRef.current = onNotice;
  }, [onNotice]);

  const refresh = useCallback(async () => {
    setBudgets(await readBudgets(householdId, budgetPeriod));
  }, [budgetPeriod, householdId]);

  useEffect(() => {
    queueMicrotask(() => {
      void refresh().catch((reason) => onNoticeRef.current(errorMessage(reason), "error"));
    });
  }, [refresh]);

  const saveBudget = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    const amountYen = Number(budgetAmount);
    if (!amountYen || amountYen < 1) return;
    setPending(true);
    onPendingChange(true);
    try {
      const input = {
        householdId,
        categoryId: budgetCategoryId || null,
        name:
          budgetName.trim() ||
          (budgetCategoryId
            ? categories.find((category) => category.id === budgetCategoryId)?.name
            : "全体予算") ||
          "全体予算",
        amountYen,
        startsOn: editingBudget?.startsOn ?? `${budgetPeriod}-01`,
        endsOn: editingBudget?.endsOn ?? null,
      };
      if (editingBudget) {
        await updateBudget({
          ...input,
          budgetId: editingBudget.id,
          baseRevision: editingBudget.revision,
        });
      } else {
        await createBudget(input);
      }
      onFormClean("budget");
      setEditingBudget(null);
      setBudgetName("");
      setBudgetAmount("");
      await refresh();
      onNotice("予算を保存しました", "success");
    } catch (reason) {
      onNotice(errorMessage(reason), "error");
    } finally {
      setPending(false);
      onPendingChange(false);
    }
  };

  const editBudget = (budget: Budget) => {
    onFormDirty("budget");
    setEditingBudget(budget);
    setBudgetCategoryId(budget.categoryId ?? "");
    setBudgetName(budget.name);
    setBudgetAmount(String(budget.amountYen));
  };

  const toggleBudgetVisibility = async (budget: Budget) => {
    setPending(true);
    onPendingChange(true);
    try {
      await archiveBudget({
        budgetId: budget.id,
        baseRevision: budget.revision,
        archived: !budget.archived,
      });
      await refresh();
      onNotice(budget.archived ? "予算を再表示しました" : "予算を非表示にしました", "success");
    } catch (reason) {
      onNotice(errorMessage(reason), "error");
    } finally {
      setPending(false);
      onPendingChange(false);
    }
  };

  return (
    <section className={`panel ${styles.settingsSection}`} hidden={hidden}>
      <div className={`panel-heading ${styles.panelHeading}`}>
        <div>
          <span className="eyebrow">予算と実績</span>
          <h2>月次予算</h2>
        </div>
        <input
          aria-label="予算の対象月"
          type="month"
          value={budgetPeriod}
          onChange={(event) => setBudgetPeriod(event.target.value)}
        />
      </div>
      <div className={styles.settingsRows}>
        {budgets.map((budget) => {
          const ratio =
            budget.amountYen > 0 ? Math.min(100, (budget.actualYen / budget.amountYen) * 100) : 0;
          return (
            <div className={styles.settingRow} key={budget.id}>
              <div>
                <b>{budget.name}</b>
                <small>
                  {yen(budget.actualYen)} / {yen(budget.amountYen)} ·{" "}
                  {budget.remainingYen >= 0
                    ? `残り${yen(budget.remainingYen)}`
                    : `${yen(-budget.remainingYen)}超過`}
                </small>
                <progress max="100" value={ratio} aria-label={`${budget.name}の予算使用率`} />
              </div>
              <button
                type="button"
                className="text-button"
                disabled={pending}
                onClick={() => editBudget(budget)}
              >
                編集
              </button>
              <button
                type="button"
                className="text-button"
                disabled={pending}
                onClick={() => void toggleBudgetVisibility(budget)}
              >
                非表示
              </button>
            </div>
          );
        })}
      </div>
      <form
        className="inline-create"
        data-unsaved-id="budget"
        onSubmit={(event) => void saveBudget(event)}
      >
        <input
          value={budgetName}
          onChange={(event) => setBudgetName(event.target.value)}
          placeholder="予算名（省略可）"
        />
        <select
          value={budgetCategoryId}
          onChange={(event) => setBudgetCategoryId(event.target.value)}
        >
          <option value="">全体</option>
          {categories
            .filter((category) => !category.archived)
            .map((category) => (
              <option key={category.id} value={category.id}>
                {category.name}
              </option>
            ))}
        </select>
        <input
          aria-label="予算額"
          type="number"
          min="1"
          step="1"
          value={budgetAmount}
          onChange={(event) => setBudgetAmount(event.target.value)}
          placeholder="予算額"
          required
        />
        <button type="submit" className="button button-primary" disabled={pending}>
          <Check size={15} />
          {editingBudget ? "変更を保存" : "予算を追加"}
        </button>
        {editingBudget && (
          <button
            type="button"
            className="text-button"
            onClick={() => {
              setEditingBudget(null);
              setBudgetCategoryId("");
              setBudgetName("");
              setBudgetAmount("");
              onFormClean("budget");
            }}
          >
            取消
          </button>
        )}
      </form>
    </section>
  );
}
