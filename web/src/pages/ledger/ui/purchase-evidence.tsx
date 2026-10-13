import type { Purchase } from "../../../shared/api";
import { InlineError } from "../../../shared/ui";
import type { PurchaseEvidence as PurchaseEvidenceEntry } from "../api/purchase-evidence";

export function PurchaseEvidence({
  purchase,
  evidence,
  error,
  highlightedItemId,
  onHighlightItem,
}: {
  purchase: Purchase;
  evidence: PurchaseEvidenceEntry[];
  error: string;
  highlightedItemId: string | null;
  onHighlightItem: (itemId: string | null) => void;
}) {
  if (evidence.length === 0 && !error) return null;

  return (
    <section className="panel ledger-evidence-panel">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">購入の証拠</span>
          <h3>{evidence.length}件</h3>
        </div>
      </div>
      {error && <InlineError message={error} />}
      {evidence.map((entry, index) => {
        const linkedItem = purchase.items.find((item) => item.id === entry.itemId);
        const contentUrl = `/documents/${encodeURIComponent(entry.documentId)}/content`;
        return (
          <div
            className="ledger-evidence-entry"
            key={`${entry.documentId}-${entry.itemId ?? "purchase"}-${index}`}
          >
            <a href={contentUrl} target="_blank" rel="noreferrer">
              {entry.sourceKind} · {linkedItem?.name ?? "購入全体"}を開く
            </a>
            {entry.mimeType.startsWith("image/") && (
              <div className="evidence-image-stage">
                <img src={contentUrl} alt={`${entry.sourceKind}の原本`} />
                {entry.region && (
                  <button
                    type="button"
                    className={`evidence-region ${highlightedItemId === entry.itemId ? "evidence-region-selected" : ""}`}
                    style={{
                      top: `${entry.region.top * 100}%`,
                      left: `${entry.region.left * 100}%`,
                      width: `${(entry.region.right - entry.region.left) * 100}%`,
                      height: `${(entry.region.bottom - entry.region.top) * 100}%`,
                    }}
                    aria-label={`${linkedItem?.name ?? "購入"}の原本位置`}
                    onClick={() => onHighlightItem(entry.itemId ?? null)}
                  />
                )}
              </div>
            )}
            {entry.mimeType === "application/pdf" && (
              <iframe className="evidence-pdf-preview" title="証拠原本" src={contentUrl} />
            )}
            {linkedItem && <span>品目 {linkedItem.name}</span>}
          </div>
        );
      })}
    </section>
  );
}
