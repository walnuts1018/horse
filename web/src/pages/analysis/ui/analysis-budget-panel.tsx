import type { Budget, Household } from "../../../shared/api";

import { yen } from "../model/format-yen";

export function AnalysisBudgetPanel({
  budgetPeriods,
  household,
}: {
  budgetPeriods: Array<{ period: string; budgets: Budget[] }>;
  household: Household;
}) {
  return (
    <section className="panel analysis-budget-panel">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">選択期間</span>
          <h2>月別の予算と実績</h2>
        </div>
      </div>
      {budgetPeriods.map(({ period, budgets }) => (
        <div key={period}>
          <h3>{period}</h3>
          {budgets.length === 0 ? (
            <p className="analysis-ledger-empty">予算履歴なし</p>
          ) : (
            budgets.map((budget) => {
              const ratio =
                budget.amountYen > 0
                  ? Math.min(100, Math.max(0, (budget.actualYen / budget.amountYen) * 100))
                  : 0;
              const category = household.categories.find(
                (candidate) => candidate.id === budget.categoryId,
              );
              return (
                <div className="analysis-budget-row" key={budget.id}>
                  <div>
                    <b>{category?.name ?? budget.name}</b>
                    <small>
                      {yen(budget.actualYen)} / {yen(budget.amountYen)}
                    </small>
                  </div>
                  <progress max="100" value={ratio} aria-label={`${budget.name}の予算使用率`} />
                  <strong className={budget.remainingYen < 0 ? "settlement-credit" : undefined}>
                    {budget.remainingYen < 0
                      ? `${yen(-budget.remainingYen)}超過`
                      : `残り${yen(budget.remainingYen)}`}
                  </strong>
                </div>
              );
            })
          )}
        </div>
      ))}
    </section>
  );
}
