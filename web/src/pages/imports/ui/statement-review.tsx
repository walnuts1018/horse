import { yen, type DocumentReview, type StatementEntry } from "../model/imports";

export function StatementReview({
  document,
  confirmedStatements,
  pending,
  onConfirm,
}: {
  document: DocumentReview;
  confirmedStatements: Set<string>;
  pending: boolean;
  onConfirm: (entry: StatementEntry) => void;
}) {
  const statements = document.result?.statements;
  if (!statements) return null;

  return (
    <div className="statement-review">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">明細行</span>
          <h3>{statements.length}件</h3>
        </div>
      </div>
      {statements.map((entry) => {
        const reference = entry.transactionId
          ? `transaction:${entry.transactionId}`
          : `row:${entry.row}`;
        const key = `${document.id}:${reference}`;
        const confirmed = confirmedStatements.has(key);
        return (
          <div
            className="statement-row"
            key={`${entry.row}-${entry.transactionId ?? entry.occurredAt}`}
          >
            <div>
              <b>{entry.merchant || "利用先未入力"}</b>
              <small>
                {entry.occurredAt.slice(0, 10)} ·{" "}
                {entry.kind === "BANK_MOVEMENT" ? "入出金" : "カード利用"}
                {entry.cardLast4 ? ` · •••• ${entry.cardLast4}` : ""}
              </small>
            </div>
            <div className="statement-row-action">
              <strong>{yen(entry.amount)}</strong>
              {entry.kind === "CARD_PURCHASE" && (
                <button
                  className="text-button"
                  disabled={pending || confirmed}
                  onClick={() => onConfirm(entry)}
                >
                  {confirmed ? "追加済み" : "購入に追加"}
                </button>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
