import { useEffect, useState } from "react";
import { AlertCircle, Link2 } from "lucide-react";
import { purchasesQuery } from "../../../entities/purchase";
import {
  readGraphql,
  type Connection,
  type Household,
  type Purchase,
  type UnmatchedObservation,
} from "../../../shared/api";
import { InlineError } from "../../../shared/ui";

type MatchCandidate = {
  documentId: string;
  sourceReference: string;
  state: string;
  reasons: string[];
};
type ObservationMatches = { sourceReference: string; matches: MatchCandidate[] };

export function ObservationLinker({
  household,
  observation,
  onConfirmed,
  onDirtyChange,
}: {
  household: Household;
  observation: UnmatchedObservation;
  onConfirmed: () => void;
  onDirtyChange: (id: string, dirty: boolean) => void;
}) {
  const observationId = `${observation.documentId}\u0000${observation.sourceReference}`;
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [purchases, setPurchases] = useState<Purchase[]>([]);
  const [matches, setMatches] = useState<MatchCandidate[]>([]);
  const [matchesTruncated, setMatchesTruncated] = useState(false);
  const [selectedPurchaseId, setSelectedPurchaseId] = useState("");
  const [selectedMatches, setSelectedMatches] = useState<Set<string>>(() => new Set());

  useEffect(() => {
    onDirtyChange(observationId, pending || selectedPurchaseId !== "" || selectedMatches.size > 0);
  }, [observationId, onDirtyChange, pending, selectedMatches, selectedPurchaseId]);
  useEffect(() => () => onDirtyChange(observationId, false), [observationId, onDirtyChange]);

  const discard = () => {
    setOpen(false);
    setSelectedPurchaseId("");
    setSelectedMatches(new Set());
    onDirtyChange(observationId, false);
  };

  const showLinker = async () => {
    if (open) {
      discard();
      return;
    }
    setOpen(true);
    if (purchases.length || loading) return;
    setLoading(true);
    setError("");
    setMatchesTruncated(false);
    try {
      const [matchResponse, purchaseResult] = await Promise.all([
        fetch(`/documents/${encodeURIComponent(observation.documentId)}/matches`, {
          credentials: "include",
        }),
        readGraphql<{ purchases: Connection<Purchase> }>(purchasesQuery, {
          householdId: household.id,
          first: 100,
        }),
      ]);
      if (!matchResponse.ok)
        throw new Error(`照合候補を取得できませんでした (${matchResponse.status})`);
      const payload = (await matchResponse.json()) as {
        observations: ObservationMatches[];
        truncated: boolean;
      };
      const sourceMatches = payload.observations.find(
        (entry) => entry.sourceReference === observation.sourceReference,
      );
      setMatches(sourceMatches?.matches ?? []);
      setMatchesTruncated(payload.truncated);
      setPurchases(purchaseResult.purchases.edges.map((edge) => edge.node));
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setLoading(false);
    }
  };

  const confirm = async () => {
    if (!selectedPurchaseId) return;
    setPending(true);
    setError("");
    try {
      const sources = [
        { documentId: observation.documentId, sourceReference: observation.sourceReference },
        ...matches
          .filter((candidate) =>
            selectedMatches.has(`${candidate.documentId}\u0000${candidate.sourceReference}`),
          )
          .map(({ documentId, sourceReference }) => ({ documentId, sourceReference })),
      ];
      const response = await fetch(
        `/purchases/${encodeURIComponent(selectedPurchaseId)}/evidence`,
        {
          method: "POST",
          credentials: "include",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ sources }),
        },
      );
      if (!response.ok) throw new Error(`原本を購入に紐付けできませんでした (${response.status})`);
      setOpen(false);
      setSelectedPurchaseId("");
      setSelectedMatches(new Set());
      onDirtyChange(observationId, false);
      onConfirmed();
    } catch (reason) {
      setError((reason as Error).message);
    } finally {
      setPending(false);
    }
  };

  return (
    <div>
      <button
        className="text-button"
        type="button"
        disabled={pending}
        onClick={() => void showLinker()}
      >
        <Link2 size={15} />
        原本を購入に紐付け
      </button>
      {open && (
        <div className="panel">
          {loading ? (
            <span>読み込み中</span>
          ) : (
            <>
              {matchesTruncated && (
                <small role="status">
                  <AlertCircle aria-hidden size={14} /> 候補を直近100件に絞っています
                </small>
              )}
              {matches.length > 0 && (
                <fieldset>
                  <legend>照合候補</legend>
                  {matches.map((candidate) => {
                    const key = `${candidate.documentId}\u0000${candidate.sourceReference}`;
                    return (
                      <label key={key}>
                        <input
                          type="checkbox"
                          checked={selectedMatches.has(key)}
                          onChange={(event) =>
                            setSelectedMatches((current) => {
                              const next = new Set(current);
                              if (event.target.checked) next.add(key);
                              else next.delete(key);
                              return next;
                            })
                          }
                        />
                        {candidate.sourceReference || candidate.documentId} · {candidate.state}
                        {candidate.reasons.length ? ` · ${candidate.reasons.join("、")}` : ""}
                      </label>
                    );
                  })}
                </fieldset>
              )}
              <label className="select-filter">
                <span>既存購入</span>
                <select
                  value={selectedPurchaseId}
                  onChange={(event) => setSelectedPurchaseId(event.target.value)}
                >
                  <option value="">購入を選択</option>
                  {purchases.map((purchase) => (
                    <option key={purchase.id} value={purchase.id}>
                      {purchase.occurredAt.slice(0, 10)} · {purchase.merchant} · {purchase.totalYen}
                      円
                    </option>
                  ))}
                </select>
              </label>
              <button
                className="button button-primary"
                type="button"
                disabled={pending || loading || !selectedPurchaseId}
                onClick={() => void confirm()}
              >
                購入に紐付け
              </button>
              <button className="text-button" type="button" disabled={pending} onClick={discard}>
                閉じる
              </button>
            </>
          )}
          {error && <InlineError message={error} />}
        </div>
      )}
    </div>
  );
}
