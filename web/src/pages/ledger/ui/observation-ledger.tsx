import { useEffect } from "react";
import { BookOpen } from "lucide-react";
import type { Household } from "../../../shared/api";
import { InlineError } from "../../../shared/ui";
import { useUnmatchedObservations } from "../api/observation/use-unmatched-observations";
import { useObservationDirtyState } from "../model/observation-dirty";
import type { LedgerSearch } from "../model/ledger-search";
import { ObservationLinker } from "./observation-linker";
import styles from "./ledger-search.module.css";

export function ObservationLedger({
  household,
  filters,
  onUnsavedChanges,
  onCountChange,
}: {
  household: Household;
  filters: Pick<
    LedgerSearch,
    "fromMonth" | "toMonth" | "observationSourceFilter" | "observationStateFilter"
  >;
  onUnsavedChanges: (unsaved: boolean) => void;
  onCountChange: (count: number, totalCount: number) => void;
}) {
  const unmatched = useUnmatchedObservations(household.id, {
    enabled: true,
    from: filters.fromMonth || undefined,
    to: filters.toMonth || undefined,
    sourceKind: filters.observationSourceFilter || undefined,
    state: filters.observationStateFilter || undefined,
  });
  const reportObservationDirty = useObservationDirtyState(onUnsavedChanges);
  useEffect(() => {
    onCountChange(unmatched.observations.length, unmatched.totalCount);
  }, [onCountChange, unmatched.observations.length, unmatched.totalCount]);

  const yen = (amount = 0) =>
    new Intl.NumberFormat("ja-JP", {
      style: "currency",
      currency: "JPY",
      maximumFractionDigits: 0,
    }).format(amount);

  return (
    <section className="panel ledger-list">
      {unmatched.loading ? (
        <div className="skeleton-list">
          <i />
          <i />
          <i />
        </div>
      ) : unmatched.error ? (
        <InlineError message={unmatched.error} />
      ) : unmatched.observations.length === 0 ? (
        <div className="empty-state compact">
          <BookOpen />
          <span>照合候補がありません</span>
        </div>
      ) : (
        unmatched.observations.map((observation) => (
          <div
            className="ledger-entry"
            key={`${observation.documentId}-${observation.sourceReference}`}
          >
            <div className="merchant-icon">{observation.sourceKind.slice(0, 1)}</div>
            <div className="purchase-description">
              <b>{observation.merchant || "店舗未入力"}</b>
              <span>
                {observation.sourceKind} · {observation.sourceReference}
                {observation.orderId ? ` · 注文 ${observation.orderId}` : ""}
                {observation.transactionId ? ` · 取引 ${observation.transactionId}` : ""}
              </span>
            </div>
            <div className="purchase-meta">
              <b>{yen(observation.amountYen)}</b>
              <span>
                {observation.__typename === "ConfirmedObservation" ? "照合済み" : "未照合"}
              </span>
              {observation.__typename === "ConfirmedObservation" && (
                <small>
                  {observation.purchase.merchant} ·{" "}
                  {new Date(observation.purchase.occurredAt).toLocaleDateString("ja-JP")}
                </small>
              )}
            </div>
            <a
              href={`/documents/${encodeURIComponent(observation.documentId)}/content`}
              target="_blank"
              rel="noreferrer"
            >
              原本を開く
            </a>
            {observation.__typename === "UnmatchedObservation" && (
              <ObservationLinker
                household={household}
                observation={observation}
                onConfirmed={() => void unmatched.refresh()}
                onDirtyChange={reportObservationDirty}
              />
            )}
          </div>
        ))
      )}
      {unmatched.moreError && <InlineError message={unmatched.moreError} />}
      {unmatched.hasNextPage && (
        <div className={styles.more}>
          <button
            className="button button-soft"
            disabled={unmatched.loadingMore}
            onClick={() => void unmatched.loadMore()}
          >
            {unmatched.loadingMore ? "読み込み中" : "さらに読み込む"}
          </button>
        </div>
      )}
    </section>
  );
}
