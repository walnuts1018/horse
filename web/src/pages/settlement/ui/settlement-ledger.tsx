import { ArrowDownLeft, ArrowLeftRight } from "lucide-react";
import type { Household, LedgerEntry } from "../../../shared/api";
import { InlineError } from "../../../shared/ui";
import { formatSettlementYen } from "../model/settlement-view";

const ledgerKindLabels: Record<string, string> = {
  FUNDING: "入金",
  ADVANCE: "立て替え",
  LOAN: "貸付",
  GIFT: "贈与",
};

export function SettlementLedger({
  household,
  entries,
  error,
  hasNextPage,
  loadingMore,
  onLoadMore,
}: {
  household: Household;
  entries: LedgerEntry[];
  error: string;
  hasNextPage: boolean;
  loadingMore: boolean;
  onLoadMore: () => void;
}) {
  const ledgerEntries = entries.filter((entry) =>
    ["FUNDING", "TRANSFER", "ADVANCE", "LOAN", "REPAYMENT", "GIFT"].includes(entry.kind),
  );

  return (
    <>
      <div className="section-heading">
        <div>
          <span className="eyebrow">資金の記録</span>
          <h2>入金・送金</h2>
        </div>
      </div>
      <section className="panel ledger-panel">
        {ledgerEntries.length === 0 ? (
          <div className="empty-state compact">
            <ArrowLeftRight />
            <span>記録はありません</span>
          </div>
        ) : (
          ledgerEntries.map((entry) => (
            <div className="funding-entry" key={entry.id}>
              <span className="funding-icon">
                <ArrowDownLeft />
              </span>
              <div>
                <b>{entry.description}</b>
                <small>{ledgerKindLabels[entry.kind] ?? "送金"}</small>
                {entry.fromMemberId && entry.toMemberId && (
                  <small>
                    {household.members.find((member) => member.id === entry.fromMemberId)
                      ?.displayName ?? "送金元"}
                    から
                    {household.members.find((member) => member.id === entry.toMemberId)
                      ?.displayName ?? "送金先"}
                    へ
                  </small>
                )}
                <small>{new Date(entry.occurredAt).toLocaleDateString("ja-JP")}</small>
              </div>
              <strong>{formatSettlementYen(entry.amountYen)}</strong>
            </div>
          ))
        )}
        {error && <InlineError message={error} />}
        {hasNextPage && (
          <button className="text-button" type="button" disabled={loadingMore} onClick={onLoadMore}>
            {loadingMore ? "読み込み中" : "さらに記録を表示"}
          </button>
        )}
      </section>
    </>
  );
}
