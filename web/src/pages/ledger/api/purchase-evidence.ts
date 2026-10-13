export type PurchaseEvidence = {
  documentId: string;
  itemId?: string | null;
  sourceKind: string;
  mimeType: string;
  region?: { page: number; top: number; left: number; bottom: number; right: number } | null;
};

export async function readPurchaseEvidence(
  purchaseId: string,
  signal?: AbortSignal,
): Promise<PurchaseEvidence[]> {
  const response = await fetch(`/purchases/${encodeURIComponent(purchaseId)}/evidence`, {
    credentials: "include",
    signal,
  });
  if (!response.ok) throw new Error(`証拠を取得できませんでした (${response.status})`);
  const payload = (await response.json()) as { evidence: PurchaseEvidence[] };
  return payload.evidence;
}
