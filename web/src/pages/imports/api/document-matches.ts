import type { ObservationCandidates } from "../model/imports";

import { asRecord } from "../model/imports";

type DocumentMatches = {
  observations: ObservationCandidates[];
  truncated: boolean;
};

function isDocumentMatches(value: unknown): value is DocumentMatches {
  const record = asRecord(value);
  return (
    record !== undefined &&
    Array.isArray(record.observations) &&
    typeof record.truncated === "boolean"
  );
}

function toDocumentMatches(value: unknown): DocumentMatches {
  return isDocumentMatches(value) ? value : { observations: [], truncated: false };
}

export async function readDocumentMatches(
  documentId: string,
  sourceReference?: string,
): Promise<DocumentMatches> {
  const params = new URLSearchParams();
  if (sourceReference !== undefined) params.set("sourceReference", sourceReference);
  const query = params.size > 0 ? `?${params.toString()}` : "";
  const response = await fetch(`/documents/${encodeURIComponent(documentId)}/matches${query}`, {
    credentials: "include",
  });
  if (!response.ok) throw new Error(`照合候補を取得できませんでした (${response.status})`);
  return toDocumentMatches(await response.json());
}

export type EvidenceSource = {
  documentId: string;
  sourceReference: string;
  itemId?: string;
  observationJobId?: string;
  sourceItemIndex?: number;
};

export async function linkPurchaseEvidence(purchaseId: string, sources: EvidenceSource[]) {
  const response = await fetch(`/purchases/${encodeURIComponent(purchaseId)}/evidence`, {
    method: "POST",
    credentials: "include",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ sources }),
  });
  if (!response.ok) throw new Error(`証拠を購入に紐付けできませんでした (${response.status})`);
}
