import { BarChart3 } from "lucide-react";

import type { RecurringExpense } from "../../../shared/api";

import { yen } from "../model/format-yen";

export function RecurringExpensePanel({
  expenses,
  monthlyAmount,
}: {
  expenses: RecurringExpense[];
  monthlyAmount: number;
}) {
  return (
    <section className="panel analysis-budget-panel">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">定期支出</span>
          <h2>月額換算 {yen(monthlyAmount)}</h2>
        </div>
      </div>
      {expenses.length === 0 ? (
        <div className="empty-state compact">
          <BarChart3 />
          <span>定期支出の記録</span>
        </div>
      ) : (
        expenses.map((expense) => (
          <div className="analysis-budget-row" key={expense.id}>
            <div>
              <b>{expense.name}</b>
              <small>{expense.merchant || expense.cadence}</small>
            </div>
            <strong>{yen(expense.amountYen)}</strong>
            <small>
              {expense.cadence === "WEEKLY"
                ? "毎週"
                : expense.cadence === "YEARLY"
                  ? "毎年"
                  : "毎月"}
            </small>
          </div>
        ))
      )}
    </section>
  );
}
