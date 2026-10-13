import { useEffect, useState } from "react";
import { AlertCircle, Link2 } from "lucide-react";
import type { Purchase } from "../../../shared/api";
import { linkPurchaseEvidence, readDocumentMatches } from "../api/document-matches";
import {
  sourceKindLabel,
  yen,
  type DocumentReview,
  type ObservationCandidates,
} from "../model/imports";

export function EvidenceMatching({
  document,
  documents,
  purchases,
  receiptEvidenceIndex,
  onReceiptEvidenceIndexChange,
  pending,
  onPendingChange,
  onDirty,
  onError,
  onLinked,
}: {
  document: DocumentReview;
  documents: DocumentReview[];
  purchases: Purchase[];
  receiptEvidenceIndex: string;
  onReceiptEvidenceIndexChange: (index: string) => void;
  pending: boolean;
  onPendingChange: (pending: boolean) => void;
  onDirty: () => void;
  onError: (message: string) => void;
  onLinked: () => Promise<void> | void;
}) {
  const [observations, setObservations] = useState<ObservationCandidates[]>([]);
  const [truncated, setTruncated] = useState(false);
  const [selectedMatches, setSelectedMatches] = useState<Set<string>>(() => new Set());
  const [targetPurchaseId, setTargetPurchaseId] = useState("");
  const [targetItemId, setTargetItemId] = useState("");
  const [activeObservationRef, setActiveObservationRef] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const targetPurchase = purchases.find((purchase) => purchase.id === targetPurchaseId);
  const receiptItems = document.result?.receipt?.items ?? [];
  const hasReceiptItems = receiptItems.length > 0;

  useEffect(() => {
    if (!document.result) {
      setObservations([]);
      setTruncated(false);
      setSelectedMatches(new Set());
      return;
    }
    let active = true;
    setObservations([]);
    setTruncated(false);
    setSelectedMatches(new Set());
    void readDocumentMatches(document.id)
      .then((payload) => {
        if (!active) return;
        setObservations(payload.observations);
        setTruncated(payload.truncated);
        setActiveObservationRef(
          hasReceiptItems ? "" : (payload.observations[0]?.sourceReference ?? ""),
        );
      })
      .catch((reason) => active && onError((reason as Error).message));
    return () => {
      active = false;
    };
  }, [document.id, document.jobId, document.status, hasReceiptItems, onError]);

  const markDirty = () => {
    onDirty();
  };

  const confirm = async () => {
    if (!targetPurchaseId || (!activeObservationRef && !receiptItems.length)) return;
    const additionalSources = observations.flatMap((observation) =>
      observation.matches
        .filter((candidate) =>
          selectedMatches.has(
            `${observation.sourceReference}\u0000${candidate.documentId}\u0000${candidate.sourceReference}`,
          ),
        )
        .map((candidate) => ({
          documentId: candidate.documentId,
          sourceReference: candidate.sourceReference,
          ...(targetItemId ? { itemId: targetItemId } : {}),
        })),
    );
    const isReceiptObservation = receiptItems.length > 0 && !activeObservationRef;
    const receiptIndex = Number(receiptEvidenceIndex);
    const sources = [
      {
        documentId: document.id,
        sourceReference: activeObservationRef,
        ...(targetItemId ? { itemId: targetItemId } : {}),
        ...(isReceiptObservation && document.jobId && targetItemId
          ? { observationJobId: document.jobId, sourceItemIndex: receiptIndex }
          : {}),
      },
      ...additionalSources,
    ];
    setSubmitting(true);
    onPendingChange(true);
    try {
      await linkPurchaseEvidence(targetPurchaseId, sources);
      setSelectedMatches(new Set());
      await onLinked();
    } catch (reason) {
      const errorMessage = (reason as Error).message;
      onError(errorMessage);
    } finally {
      setSubmitting(false);
      onPendingChange(false);
    }
  };

  if (!document.result || observations.length === 0) return null;

  return (
    <section className="panel evidence-group-panel">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">照合候補</span>
          <h3>購入へまとめる証拠</h3>
        </div>
        <Link2 size={18} />
      </div>
      {truncated && (
        <small role="status">
          <AlertCircle aria-hidden size={14} /> 候補を直近100件に絞っています
        </small>
      )}
      <label className="select-filter">
        <span>照合元</span>
        <select
          value={activeObservationRef}
          onChange={(event) => {
            markDirty();
            setActiveObservationRef(event.target.value);
          }}
        >
          {receiptItems.length > 0 && <option value="">レシート全体</option>}
          {observations.map((observation) => {
            const statement = document.result?.statements?.find((entry) => {
              const reference = entry.transactionId
                ? `transaction:${entry.transactionId}`
                : `row:${entry.row}`;
              return reference === observation.sourceReference;
            });
            return (
              <option key={observation.sourceReference} value={observation.sourceReference}>
                {statement?.merchant || observation.sourceReference || "レシート"}
              </option>
            );
          })}
        </select>
      </label>
      <div className="evidence-match-list">
        {observations.map((observation) => (
          <div className="evidence-match-group" key={observation.sourceReference}>
            <small>{observation.sourceReference || "レシート全体"}</small>
            {observation.matches.map((candidate) => {
              const key = `${observation.sourceReference}\u0000${candidate.documentId}\u0000${candidate.sourceReference}`;
              const sourceDocument = documents.find((entry) => entry.id === candidate.documentId);
              return (
                <label className="evidence-match-row" key={key}>
                  <input
                    type="checkbox"
                    checked={selectedMatches.has(key)}
                    onChange={(event) => {
                      markDirty();
                      setSelectedMatches((current) => {
                        const next = new Set(current);
                        if (event.target.checked) next.add(key);
                        else next.delete(key);
                        return next;
                      });
                    }}
                  />
                  <span>
                    <b>
                      {sourceKindLabel(sourceDocument?.sourceKind)} ·{" "}
                      {candidate.sourceReference ||
                        sourceDocument?.result?.receipt?.merchant ||
                        "レシート"}
                    </b>
                    <small>
                      {candidate.state} · {candidate.reasons.join("、")}
                    </small>
                  </span>
                </label>
              );
            })}
          </div>
        ))}
      </div>
      <label className="select-filter">
        <span>購入先</span>
        <select
          value={targetPurchaseId}
          onChange={(event) => {
            markDirty();
            setTargetPurchaseId(event.target.value);
            setTargetItemId("");
          }}
        >
          <option value="">購入を選択</option>
          {purchases.map((purchase) => (
            <option key={purchase.id} value={purchase.id}>
              {purchase.occurredAt.slice(0, 10)} · {purchase.merchant} · {yen(purchase.totalYen)}
            </option>
          ))}
        </select>
      </label>
      {targetPurchase && (
        <label className="select-filter">
          <span>紐付ける品目</span>
          <select
            value={targetItemId}
            onChange={(event) => {
              markDirty();
              setTargetItemId(event.target.value);
            }}
          >
            <option value="">購入全体</option>
            {targetPurchase.items.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name} · {yen(item.amountYen)}
              </option>
            ))}
          </select>
        </label>
      )}
      {receiptItems.length > 0 && (
        <label className="select-filter">
          <span>照合するレシート品目</span>
          <select
            value={receiptEvidenceIndex}
            onChange={(event) => {
              markDirty();
              onReceiptEvidenceIndexChange(event.target.value);
            }}
          >
            {receiptItems.map((item, index) => (
              <option key={index} value={index}>
                {item.name}
              </option>
            ))}
          </select>
        </label>
      )}
      <button
        className="button button-primary button-wide"
        type="button"
        disabled={pending || submitting || !targetPurchaseId}
        onClick={() => void confirm()}
      >
        <Link2 size={15} />
        {submitting ? "紐付けています" : "選択した証拠を紐付け"}
      </button>
    </section>
  );
}
