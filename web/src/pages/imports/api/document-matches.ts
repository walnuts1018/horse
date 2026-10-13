import type { ObservationCandidates } from "../model/imports";

export async function readDocumentMatches(documentId: string): Promise<{
  observations: ObservationCandidates[];
  truncated: boolean;
}> {
  const response = await fetch(`/documents/${encodeURIComponent(documentId)}/matches`, {
    credentials: "include",
  });
  if (!response.ok) throw new Error(`照合候補を取得できませんでした (${response.status})`);
  return (await response.json()) as {
    observations: ObservationCandidates[];
    truncated: boolean;
  };
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
