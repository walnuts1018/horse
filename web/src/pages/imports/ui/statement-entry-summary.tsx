import type { StatementEntry } from "../model/imports";

export function StatementEntrySummary({
  entry,
  onSelectRegion,
}: {
  entry: StatementEntry;
  onSelectRegion: (entry: StatementEntry) => void;
}) {
  const kindLabel = (() => {
    switch (entry.kind) {
      case "REFUND":
        return "返品・返金";
      case "ACCOUNT_TRANSFER":
        return "口座間振替";
      case "CARD_BILL_PAYMENT":
        return "カード引落し";
      case "EXTERNAL_INCOME":
        return "外部入金";
      case "LOAN":
        return "貸付";
      case "REPAYMENT":
        return "貸付返済";
      case "CARD_PURCHASE":
        return "カード利用";
      default:
        return entry.kind;
    }
  })();

  return (
    <div>
      {entry.region ? (
        <button type="button" className="text-button" onClick={() => onSelectRegion(entry)}>
          {entry.merchant || "利用先未入力"}
        </button>
      ) : (
        <b>{entry.merchant || "利用先未入力"}</b>
      )}
      <small>
        {entry.occurredAt.slice(0, 10)} · {kindLabel}
        {entry.cardLast4 ? ` · •••• ${entry.cardLast4}` : ""}
      </small>
    </div>
  );
}
