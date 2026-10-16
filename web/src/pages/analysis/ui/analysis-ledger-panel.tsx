import type { LedgerEntry } from "../../../shared/api";

import { yen } from "../model/format-yen";

const ledgerKindLabels: Partial<Record<string, string>> = {
  ADVANCE: "立て替え",
  FUNDING: "入金",
  LOAN: "貸し借り",
  REPAYMENT: "返済",
  GIFT: "贈与",
  CORRECTION: "訂正",
};

export function AnalysisLedgerPanel({
  entries,
  months,
  complete,
  error,
}: {
  entries: LedgerEntry[];
  months: string[];
  complete: boolean;
  error: string;
}) {
  const firstMonth = months[0];
  const lastMonth = months[months.length - 1];
  const history = entries
    .filter(
      (entry) =>
        entry.kind !== "PURCHASE" &&
        ledgerKindLabels[entry.kind] !== undefined &&
        entry.occurredAt.slice(0, 7) >= firstMonth &&
        entry.occurredAt.slice(0, 7) <= lastMonth,
    )
    .slice(0, 6);

  return (
    <section className="panel analysis-ledger-panel">
      <div className="analysis-ledger-heading">
        <h2>立て替え・入金・精算</h2>
        <span>{firstMonth === lastMonth ? firstMonth : `${firstMonth}〜${lastMonth}`}</span>
      </div>
      {error ? (
        <p className="analysis-ledger-empty">履歴を最後まで取得できませんでした</p>
      ) : !complete ? (
        <p className="analysis-ledger-empty">履歴を取得中</p>
      ) : history.length === 0 ? (
        <p className="analysis-ledger-empty">この期間の記録はありません</p>
      ) : (
        <ul className="analysis-ledger-list">
          {history.map((entry) => (
            <li key={entry.id}>
              <time dateTime={entry.occurredAt}>{entry.occurredAt.slice(0, 10)}</time>
              <span className="analysis-ledger-kind">{ledgerKindLabels[entry.kind]}</span>
              <span className="analysis-ledger-description">{entry.description}</span>
              <strong>{yen(entry.amountYen)}</strong>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
