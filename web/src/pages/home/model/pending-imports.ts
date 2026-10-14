type PendingImportDocument = {
  status: string;
};

type PendingImportPage = {
  documents: PendingImportDocument[];
  nextCursor: string | null;
};

export type PendingImportStatus =
  | { kind: "attention"; reviewCount: number; failedCount: number; hasMore: boolean }
  | { kind: "processing" }
  | { kind: "none" };

const reviewStatuses = new Set(["READY", "REVIEW", "COMPLETED", "SUCCEEDED", "DONE"]);
const failedStatuses = new Set(["FAILED"]);
const terminalStatuses = new Set([...reviewStatuses, ...failedStatuses, "DUPLICATE"]);

export function summarizePendingImports(statuses: string[], hasMore: boolean): PendingImportStatus {
  const normalizedStatuses = statuses.map((status) => status.toUpperCase());
  const reviewCount = normalizedStatuses.filter((status) => reviewStatuses.has(status)).length;
  const failedCount = normalizedStatuses.filter((status) => failedStatuses.has(status)).length;
  if (reviewCount > 0 || failedCount > 0) {
    return { kind: "attention", reviewCount, failedCount, hasMore };
  }
  if (normalizedStatuses.some((status) => !terminalStatuses.has(status))) {
    return { kind: "processing" };
  }
  return { kind: "none" };
}

export function pendingImportMessage(status: PendingImportStatus): string | undefined {
  if (status.kind === "none") return undefined;
  if (status.kind === "processing") return "AI解析中";
  if (status.hasMore) return "確認・再解析が必要";

  return [
    status.reviewCount > 0 ? `確認待ち ${status.reviewCount}件` : "",
    status.failedCount > 0 ? `解析失敗 ${status.failedCount}件` : "",
  ]
    .filter(Boolean)
    .join("・");
}

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

  return summarizePendingImports(
    value.documents.map((document) => document.status),
    value.nextCursor !== null,
  );
}
