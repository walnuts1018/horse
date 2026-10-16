import { yen } from "../model/format-yen";

type UnbackedPurchase = {
  id: string;
  occurredAt: string;
  merchant: string;
  totalYen: number;
};

type UnmatchedObservation = {
  id: string;
  sourceKind: string;
  merchant: string;
  occurredAt: string | null;
  amountYen: number;
  orderId: string;
  transactionId: string;
};

const sourceKindLabels: Record<string, string> = {
  BANK: "銀行",
  CARD: "カード",
  EMAIL: "メール",
  ORDER: "注文",
  RECEIPT: "レシート",
};

const dateLabel = (value: string | null) => (value ? value.slice(0, 10) : "日付不明");

export function AnalysisEvidencePanel({
  unbackedPurchases,
  unbackedCount,
  hasMoreUnbacked,
  loadingMoreUnbacked,
  loadMoreUnbacked,
  unmatchedObservations,
  unmatchedCount,
  hasMoreUnmatched,
  loadingMoreUnmatched,
  loadMoreUnmatched,
  error,
}: {
  unbackedPurchases: UnbackedPurchase[];
  unbackedCount: number;
  hasMoreUnbacked: boolean;
  loadingMoreUnbacked: boolean;
  loadMoreUnbacked: () => void;
  unmatchedObservations: UnmatchedObservation[];
  unmatchedCount: number;
  hasMoreUnmatched: boolean;
  loadingMoreUnmatched: boolean;
  loadMoreUnmatched: () => void;
  error: string;
}) {
  return (
    <section className="analysis-evidence-grid" aria-label="原本と取引の確認">
      <div className="panel analysis-evidence-panel">
        <div className="analysis-ledger-heading">
          <h2>原本のない支出</h2>
          <span>{unbackedCount}件</span>
        </div>
        {unbackedPurchases.length === 0 ? (
          <p className="analysis-ledger-empty">該当する支出はありません</p>
        ) : (
          <ul className="analysis-ledger-list">
            {unbackedPurchases.map((purchase) => (
              <li key={purchase.id}>
                <time dateTime={purchase.occurredAt}>{dateLabel(purchase.occurredAt)}</time>
                <span className="analysis-ledger-description">
                  {purchase.merchant || "店舗不明"}
                </span>
                <strong>{yen(purchase.totalYen)}</strong>
              </li>
            ))}
          </ul>
        )}
        {hasMoreUnbacked && (
          <button
            className="text-button"
            type="button"
            disabled={loadingMoreUnbacked}
            onClick={loadMoreUnbacked}
          >
            {loadingMoreUnbacked ? "取得中" : "さらに表示"}
          </button>
        )}
      </div>
      <div className="panel analysis-evidence-panel">
        <div className="analysis-ledger-heading">
          <h2>未照合の取引</h2>
          <span>{unmatchedCount}件</span>
        </div>
        {unmatchedObservations.length === 0 ? (
          <p className="analysis-ledger-empty">該当する取引はありません</p>
        ) : (
          <ul className="analysis-ledger-list">
            {unmatchedObservations.map((observation) => (
              <li key={observation.id}>
                <time dateTime={observation.occurredAt ?? undefined}>
                  {dateLabel(observation.occurredAt)}
                </time>
                <span className="analysis-ledger-description">
                  {observation.merchant ||
                    observation.orderId ||
                    observation.transactionId ||
                    sourceKindLabels[observation.sourceKind] ||
                    "取引"}
                </span>
                <strong>{yen(observation.amountYen)}</strong>
              </li>
            ))}
          </ul>
        )}
        {hasMoreUnmatched && (
          <button
            className="text-button"
            type="button"
            disabled={loadingMoreUnmatched}
            onClick={loadMoreUnmatched}
          >
            {loadingMoreUnmatched ? "取得中" : "さらに表示"}
          </button>
        )}
      </div>
      {error && <p className="analysis-ledger-empty">追加の記録を取得できませんでした</p>}
    </section>
  );
}
