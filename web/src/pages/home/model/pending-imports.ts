type PendingImportDocument = {
  status: string;
};

type PendingImportPage = {
  documents: PendingImportDocument[];
  nextCursor: string | null;
};

export type PendingImportStatus =
  | { kind: "review"; count: number; hasMore: boolean }
  | { kind: "processing" }
  | { kind: "none" };

function isPendingImportPage(value: unknown): value is PendingImportPage {
  if (!isRecord(value)) return false;
  const documents = value.documents;
  const nextCursor = value.nextCursor;
  return (
    Array.isArray(documents) &&
    documents.every((document) => isRecord(document) && typeof document.status === "string") &&
    (nextCursor === null || typeof nextCursor === "string")
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export async function readPendingImportStatus(
  householdId: string,
  signal?: AbortSignal,
): Promise<PendingImportStatus> {
  const query = new URLSearchParams({ householdId, confirmed: "false", first: "100" });
  const response = await fetch(`/documents?${query.toString()}`, {
    credentials: "include",
    signal,
  });
  if (!response.ok) throw new Error(`未確認の取り込みを取得できませんでした (${response.status})`);
  const value: unknown = await response.json();
  if (!isPendingImportPage(value)) throw new Error("未確認の取り込みを取得できませんでした");

  const statuses = value.documents.map((document) => document.status.toUpperCase());
  const reviewCount = statuses.filter((status) => status === "READY" || status === "REVIEW").length;
  if (reviewCount > 0) {
    return { kind: "review", count: reviewCount, hasMore: value.nextCursor !== null };
  }
  if (
    statuses.some(
      (status) => !["COMPLETED", "SUCCEEDED", "FAILED", "DUPLICATE", "DONE"].includes(status),
    )
  ) {
    return { kind: "processing" };
  }
  return { kind: "none" };
}
