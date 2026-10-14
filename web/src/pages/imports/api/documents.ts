import type { DocumentFilters, DocumentReview } from "../model/imports";

import {
  recordAccountMovementMutation,
  recordExternalIncomeMutation,
  recordRepaymentMutation,
  recordTransferMutation,
  linkStatementTransferMutation,
  linkStatementAccountMovementMutation,
  linkStatementAccountIncomeMutation,
} from "../../../entities/ledger";
import {
  commitGraphQLMutationSuccess,
  type RecordAccountMovementMutation,
  type RecordExternalIncomeMutation,
  type RecordRepaymentMutation,
  type RecordTransferMutation,
  type LinkStatementTransferMutation,
  type LinkStatementAccountMovementMutation,
  type LinkStatementRefundCorrectionMutation,
  type LinkStatementAccountIncomeMutation,
} from "../../../shared/api";
import { asRecord } from "../model/imports";
import { linkStatementRefundCorrectionMutation } from "./operations";

export const documentListURL = (householdId: string, filters: DocumentFilters, after?: string) => {
  const query = new URLSearchParams({ householdId, first: "50" });
  if (filters.store.trim()) query.set("store", filters.store.trim());
  if (filters.from) query.set("from", filters.from);
  if (filters.to) query.set("to", filters.to);
  if (filters.sourceKind) query.set("sourceKind", filters.sourceKind);
  if (filters.confirmed) query.set("confirmed", filters.confirmed);
  if (after) query.set("after", after);
  return `/documents?${query.toString()}`;
};

function isDocumentReview(value: unknown): value is DocumentReview {
  const record = asRecord(value);
  return record !== undefined && typeof record.id === "string" && typeof record.status === "string";
}

function toDocumentReview(value: unknown): DocumentReview {
  return isDocumentReview(value) ? value : { id: "", status: "" };
}

export async function readDocument(
  documentId: string,
  signal?: AbortSignal,
): Promise<DocumentReview> {
  const response = await fetch(`/documents/${encodeURIComponent(documentId)}`, {
    credentials: "include",
    signal,
  });
  if (!response.ok) throw new Error(`文書を取得できませんでした (${response.status})`);
  return toDocumentReview(await response.json());
}

export async function deleteDocument(documentId: string) {
  const response = await fetch(`/documents/${encodeURIComponent(documentId)}`, {
    method: "DELETE",
    credentials: "include",
  });
  if (!response.ok) throw new Error(`原本を削除できませんでした (${response.status})`);
}

export const importIdempotencyKey = async (documentId: string, sourceReference: string) => {
  const input = new TextEncoder().encode(`${documentId}\u0000${sourceReference}`);
  const digest = await crypto.subtle.digest("SHA-256", input);
  const hash = Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
  return `import:${hash}`;
};

export function recordStatementAccountMovement(
  input: RecordAccountMovementMutation["variables"]["input"],
) {
  return commitGraphQLMutationSuccess<RecordAccountMovementMutation>(
    recordAccountMovementMutation,
    { input },
    (response) => response.recordAccountMovement,
    "RecordAccountMovementSuccess",
    "口座間振替を記録できませんでした",
  );
}

export function linkStatementRefundCorrection(
  input: LinkStatementRefundCorrectionMutation["variables"]["input"],
) {
  return commitGraphQLMutationSuccess<LinkStatementRefundCorrectionMutation>(
    linkStatementRefundCorrectionMutation,
    { input },
    (response) => response.linkStatementRefundCorrection,
    "LinkStatementRefundCorrectionSuccess",
    "返金記録に紐付けられませんでした",
  );
}

export function recordStatementExternalIncome(
  input: RecordExternalIncomeMutation["variables"]["input"],
) {
  return commitGraphQLMutationSuccess<RecordExternalIncomeMutation>(
    recordExternalIncomeMutation,
    { input },
    (response) => response.recordExternalIncome,
    "RecordExternalIncomeSuccess",
    "外部入金を記録できませんでした",
  );
}

export function recordStatementLoan(input: RecordTransferMutation["variables"]["input"]) {
  return commitGraphQLMutationSuccess<RecordTransferMutation>(
    recordTransferMutation,
    { input },
    (response) => response.recordTransfer,
    "RecordTransferSuccess",
    "貸付を記録できませんでした",
  );
}

export function recordStatementRepayment(input: RecordRepaymentMutation["variables"]["input"]) {
  return commitGraphQLMutationSuccess<RecordRepaymentMutation>(
    recordRepaymentMutation,
    { input },
    (response) => response.recordRepayment,
    "RecordRepaymentSuccess",
    "返済を記録できませんでした",
  );
}

export function linkStatementTransfer(input: LinkStatementTransferMutation["variables"]["input"]) {
  return commitGraphQLMutationSuccess<LinkStatementTransferMutation>(
    linkStatementTransferMutation,
    { input },
    (response) => response.linkStatementTransfer,
    "LinkStatementTransferSuccess",
    "明細を既存取引に紐付けできませんでした",
  );
}

export function linkStatementAccountMovement(
  input: LinkStatementAccountMovementMutation["variables"]["input"],
) {
  return commitGraphQLMutationSuccess<LinkStatementAccountMovementMutation>(
    linkStatementAccountMovementMutation,
    { input },
    (response) => response.linkStatementAccountMovement,
    "LinkStatementAccountMovementSuccess",
    "口座移動に紐付けできませんでした",
  );
}

export function linkStatementAccountIncome(
  input: LinkStatementAccountIncomeMutation["variables"]["input"],
) {
  return commitGraphQLMutationSuccess<LinkStatementAccountIncomeMutation>(
    linkStatementAccountIncomeMutation,
    { input },
    (response) => response.linkStatementAccountIncome,
    "LinkStatementAccountIncomeSuccess",
    "入金明細を既存の入金に紐付けできませんでした",
  );
}
