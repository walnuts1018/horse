import { AlertCircle, Link2 } from "lucide-react";
import { useEffect, useState, type SetStateAction } from "react";

import type { Purchase } from "../../../shared/api";

import { errorMessage } from "../../../shared/lib/errors";
import { linkPurchaseEvidence, readDocumentMatches } from "../api/document-matches";
import {
  sourceKindLabel,
  yen,
  type DocumentReview,
  type ObservationCandidates,
} from "../model/imports";

import styles from "./imports-page.module.css";

// 取得結果は対象 document と一致するときだけ参照し、それ以外は未取得として扱う
const NO_MATCHES = new Set<string>();

type MatchingState = {
  document: DocumentReview;
  observations: ObservationCandidates[];
  truncated: boolean;
  selectedMatches: Set<string>;
};

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
  const [matchingState, setMatchingState] = useState<MatchingState>({
    document,
    observations: [],
    truncated: false,
    selectedMatches: new Set(),
  });
  const [targetPurchaseId, setTargetPurchaseId] = useState("");
  const [targetItemId, setTargetItemId] = useState("");
  const [activeObservationRef, setActiveObservationRef] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const targetPurchase = purchases.find((purchase) => purchase.id === targetPurchaseId);
  const receiptItems = document.result?.receipt?.items ?? [];
  const hasReceiptItems = receiptItems.length > 0;
  const synced = matchingState.document === document;
  const observations = synced ? matchingState.observations : [];
  const truncated = synced ? matchingState.truncated : false;
  const selectedMatches = synced ? matchingState.selectedMatches : NO_MATCHES;
  const setSelectedMatches = (action: SetStateAction<Set<string>>) => {
    setMatchingState((current) => {
      if (current.document !== document) return current;
      const keys = typeof action === "function" ? action(current.selectedMatches) : action;
      return { ...current, selectedMatches: keys };
    });
  };

  useEffect(() => {
    if (!document.result) return;
    let active = true;
    void readDocumentMatches(document.id)
      .then((payload) => {
        if (!active) return;
        setMatchingState({
          document,
          observations: payload.observations,
          truncated: payload.truncated,
          selectedMatches: new Set(),
        });
        setActiveObservationRef(
          hasReceiptItems ? "" : (payload.observations.at(0)?.sourceReference ?? ""),
        );
      })
      .catch((reason) => active && onError(errorMessage(reason)));
    return () => {
      active = false;
    };
  }, [document, hasReceiptItems, onError]);

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
        .map((candidate) =>
          Object.assign(
            {
              documentId: candidate.documentId,
              sourceReference: candidate.sourceReference,
            },
            targetItemId ? { itemId: targetItemId } : {},
          ),
        ),
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
      onError(errorMessage(reason));
    } finally {
      setSubmitting(false);
      onPendingChange(false);
    }
  };

  if (!document.result || observations.length === 0) return null;

  return (
    <section className={`panel ${styles.evidenceGroupPanel}`}>
      <div className="panel-heading">
        <div>
          <span className="eyebrow">照合候補</span>
          <h3>購入へまとめる証拠</h3>
        </div>
        <Link2 size={18} />
      </div>
      {truncated && (
        <output>
          <AlertCircle aria-hidden size={14} /> 候補を直近100件に絞っています
        </output>
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
      <div className={styles.evidenceMatchList}>
        {observations.map((observation) => (
          <div className={styles.evidenceMatchGroup} key={observation.sourceReference}>
            <small>{observation.sourceReference || "レシート全体"}</small>
            {observation.matches.map((candidate) => {
              const key = `${observation.sourceReference}\u0000${candidate.documentId}\u0000${candidate.sourceReference}`;
              const sourceDocument = documents.find((entry) => entry.id === candidate.documentId);
              return (
                <label className={styles.evidenceMatchRow} key={key}>
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
              <option key={item.key} value={index}>
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
