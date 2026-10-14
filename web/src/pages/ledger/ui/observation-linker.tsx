import { Link2 } from "lucide-react";
import { type SubmitEvent, useEffect, useState } from "react";

import { type Household, type Purchase, type UnmatchedObservation } from "../../../shared/api";
import { errorMessage } from "../../../shared/lib";
import { InlineError } from "../../../shared/ui";
import { readLinkablePurchases } from "../api/read-linkable-purchases";

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
  const [purchaseLoading, setPurchaseLoading] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [purchases, setPurchases] = useState<Purchase[]>([]);
  const [matches, setMatches] = useState<MatchCandidate[]>([]);
  const [purchaseSearch, setPurchaseSearch] = useState("");
  const [purchaseSearchTerm, setPurchaseSearchTerm] = useState("");
  const [purchaseAfter, setPurchaseAfter] = useState<string | null | undefined>(null);
  const [purchaseHasNextPage, setPurchaseHasNextPage] = useState(false);
  const [selectedPurchaseId, setSelectedPurchaseId] = useState("");
  const [selectedMatches, setSelectedMatches] = useState<Set<string>>(() => new Set());

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
    try {
      const [matchResponse, purchasePage] = await Promise.all([
        fetch(`/documents/${encodeURIComponent(observation.documentId)}/matches`, {
          credentials: "include",
        }),
        readLinkablePurchases(household.id, ""),
      ]);
      if (!matchResponse.ok)
        throw new Error(`照合候補を取得できませんでした (${matchResponse.status})`);
      const payload: unknown = await matchResponse.json();
      if (
        payload === null ||
        typeof payload !== "object" ||
        !("observations" in payload) ||
        !Array.isArray(payload.observations)
      )
        throw new Error("照合候補の形式が正しくありません");
      const matchesData = payload.observations.filter(
        (entry: unknown): entry is ObservationMatches =>
          entry !== null &&
          typeof entry === "object" &&
          "sourceReference" in entry &&
          typeof entry.sourceReference === "string" &&
          "matches" in entry &&
          Array.isArray(entry.matches) &&
          entry.matches.every(
            (candidate: unknown) =>
              candidate !== null &&
              typeof candidate === "object" &&
              "documentId" in candidate &&
              typeof candidate.documentId === "string" &&
              "sourceReference" in candidate &&
              typeof candidate.sourceReference === "string" &&
              "state" in candidate &&
              typeof candidate.state === "string" &&
              "reasons" in candidate &&
              Array.isArray(candidate.reasons) &&
              candidate.reasons.every((reason: unknown) => typeof reason === "string"),
          ),
      );
      const sourceMatches = matchesData.find(
        (entry) => entry.sourceReference === observation.sourceReference,
      );
      setMatches(sourceMatches?.matches ?? []);
      setPurchases(purchasePage.purchases);
      setPurchaseSearchTerm("");
      setPurchaseAfter(purchasePage.after);
      setPurchaseHasNextPage(purchasePage.hasNextPage);
    } catch (reason) {
      setError(errorMessage(reason));
    } finally {
      setLoading(false);
    }
  };

  const searchPurchases = async (search: string, after?: string | null) => {
    setPurchaseLoading(true);
    setError("");
    try {
      const page = await readLinkablePurchases(household.id, search, after);
      setPurchases((current) => {
        if (!after) return page.purchases;
        const existing = new Set(current.map(({ id }) => id));
        return [...current, ...page.purchases.filter(({ id }) => !existing.has(id))];
      });
      setPurchaseAfter(page.after);
      setPurchaseHasNextPage(page.hasNextPage);
    } catch (reason) {
      setError(errorMessage(reason));
    } finally {
      setPurchaseLoading(false);
    }
  };

  const submitPurchaseSearch = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    const search = purchaseSearch.trim();
    setPurchaseSearchTerm(search);
    setSelectedPurchaseId("");
    onDirtyChange(observationId, selectedMatches.size > 0);
    void searchPurchases(search);
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
      setError(errorMessage(reason));
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
                          onChange={(event) => {
                            const next = new Set(selectedMatches);
                            if (event.target.checked) next.add(key);
                            else next.delete(key);
                            setSelectedMatches(next);
                            onDirtyChange(
                              observationId,
                              pending || selectedPurchaseId !== "" || next.size > 0,
                            );
                          }}
                        />
                        {candidate.sourceReference || candidate.documentId} · {candidate.state}
                        {candidate.reasons.length ? ` · ${candidate.reasons.join("、")}` : ""}
                      </label>
                    );
                  })}
                </fieldset>
              )}
              <form className="ledger-filters" onSubmit={submitPurchaseSearch}>
                <label className="select-filter">
                  <span>購入を検索</span>
                  <input
                    type="search"
                    value={purchaseSearch}
                    onChange={(event) => setPurchaseSearch(event.target.value)}
                    placeholder="店舗名または品目名"
                  />
                </label>
                <button className="button button-soft" type="submit" disabled={purchaseLoading}>
                  検索
                </button>
              </form>
              <label className="select-filter">
                <span>既存購入</span>
                <select
                  value={selectedPurchaseId}
                  onChange={(event) => {
                    const purchaseId = event.target.value;
                    setSelectedPurchaseId(purchaseId);
                    onDirtyChange(
                      observationId,
                      pending || purchaseId !== "" || selectedMatches.size > 0,
                    );
                  }}
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
              {purchaseHasNextPage && (
                <button
                  className="button button-soft"
                  type="button"
                  disabled={purchaseLoading || !purchaseAfter}
                  onClick={() => void searchPurchases(purchaseSearchTerm, purchaseAfter)}
                >
                  {purchaseLoading ? "読み込み中" : "購入候補をさらに読み込む"}
                </button>
              )}
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
