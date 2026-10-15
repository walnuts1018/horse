import type {
  DocumentExtraction,
  DocumentExtractionVersion,
  DocumentReview,
} from "../model/imports";

import { asRecord } from "../model/imports";

function isDocumentExtractionVersion(value: unknown): value is DocumentExtractionVersion {
  const record = asRecord(value);
  return (
    record !== undefined &&
    typeof record.id === "string" &&
    typeof record.version === "number" &&
    typeof record.model === "string" &&
    typeof record.createdAt === "string" &&
    typeof record.conversationTurns === "number"
  );
}

export async function readDocumentExtractionVersions(
  documentId: string,
  signal?: AbortSignal,
): Promise<DocumentExtractionVersion[]> {
  const response = await fetch(`/documents/${encodeURIComponent(documentId)}/extractions`, {
    credentials: "include",
    signal,
  });
  if (!response.ok) throw new Error(`抽出履歴を取得できませんでした (${response.status})`);
  const record = asRecord(await response.json());
  return Array.isArray(record?.versions) ? record.versions.filter(isDocumentExtractionVersion) : [];
}

export async function readDocumentExtraction(
  documentId: string,
  versionId: string,
  signal?: AbortSignal,
): Promise<DocumentExtraction> {
  const response = await fetch(
    `/documents/${encodeURIComponent(documentId)}/extractions/${encodeURIComponent(versionId)}`,
    { credentials: "include", signal },
  );
  if (!response.ok) throw new Error(`抽出版を取得できませんでした (${response.status})`);
  const record = asRecord(await response.json());
  const result = asRecord(record?.result) as DocumentReview["result"] | undefined;
  if (
    !record ||
    typeof record.id !== "string" ||
    typeof record.version !== "number" ||
    typeof record.model !== "string" ||
    !result
  ) {
    throw new Error("抽出版を取得できませんでした");
  }
  return {
    id: record.id,
    version: record.version,
    model: record.model,
    createdAt: typeof record.createdAt === "string" ? record.createdAt : "",
    conversationTurns: 0,
    result: result as NonNullable<DocumentReview["result"]>,
  };
}
