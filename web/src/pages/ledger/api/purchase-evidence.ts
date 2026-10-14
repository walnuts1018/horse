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
  const payload: unknown = await response.json();
  if (
    payload === null ||
    typeof payload !== "object" ||
    !("evidence" in payload) ||
    !Array.isArray(payload.evidence) ||
    !payload.evidence.every(isPurchaseEvidence)
  )
    throw new Error("証拠の形式が正しくありません");
  return payload.evidence;
}

function isPurchaseEvidence(value: unknown): value is PurchaseEvidence {
  return (
    value !== null &&
    typeof value === "object" &&
    "documentId" in value &&
    typeof value.documentId === "string" &&
    "sourceKind" in value &&
    typeof value.sourceKind === "string" &&
    "mimeType" in value &&
    typeof value.mimeType === "string" &&
    (!("itemId" in value) || value.itemId === null || typeof value.itemId === "string") &&
    (!("region" in value) || value.region === null || isRegion(value.region))
  );
}

function isRegion(value: unknown): value is NonNullable<PurchaseEvidence["region"]> {
  return (
    value !== null &&
    typeof value === "object" &&
    "page" in value &&
    typeof value.page === "number" &&
    "top" in value &&
    typeof value.top === "number" &&
    "left" in value &&
    typeof value.left === "number" &&
    "bottom" in value &&
    typeof value.bottom === "number" &&
    "right" in value &&
    typeof value.right === "number"
  );
}
