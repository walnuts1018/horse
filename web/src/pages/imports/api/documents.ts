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
  assertGraphQLMutationSuccess,
  commitGraphQLMutation,
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
  return commitGraphQLMutation<RecordAccountMovementMutation, void>(
    recordAccountMovementMutation,
    { input },
    (response) => {
      assertGraphQLMutationSuccess(
        response.recordAccountMovement,
        "RecordAccountMovementSuccess",
        "口座間振替を記録できませんでした",
        () => "口座間振替を記録できませんでした",
      );
    },
  );
}

export function linkStatementRefundCorrection(
  input: LinkStatementRefundCorrectionMutation["variables"]["input"],
) {
  return commitGraphQLMutation<LinkStatementRefundCorrectionMutation, void>(
    linkStatementRefundCorrectionMutation,
    { input },
    (response) => {
      assertGraphQLMutationSuccess(
        response.linkStatementRefundCorrection,
        "LinkStatementRefundCorrectionSuccess",
        "返金記録に紐付けられませんでした",
        () => "返金記録に紐付けられませんでした",
      );
    },
  );
}

export function recordStatementExternalIncome(
  input: RecordExternalIncomeMutation["variables"]["input"],
) {
  return commitGraphQLMutation<RecordExternalIncomeMutation, void>(
    recordExternalIncomeMutation,
    { input },
    (response) => {
      assertGraphQLMutationSuccess(
        response.recordExternalIncome,
        "RecordExternalIncomeSuccess",
        "外部入金を記録できませんでした",
        () => "外部入金を記録できませんでした",
      );
    },
  );
}

export function recordStatementLoan(input: RecordTransferMutation["variables"]["input"]) {
  return commitGraphQLMutation<RecordTransferMutation, void>(
    recordTransferMutation,
    { input },
    (response) => {
      assertGraphQLMutationSuccess(
        response.recordTransfer,
        "RecordTransferSuccess",
        "貸付を記録できませんでした",
        () => "貸付を記録できませんでした",
      );
    },
  );
}

export function recordStatementRepayment(input: RecordRepaymentMutation["variables"]["input"]) {
  return commitGraphQLMutation<RecordRepaymentMutation, void>(
    recordRepaymentMutation,
    { input },
    (response) => {
      assertGraphQLMutationSuccess(
        response.recordRepayment,
        "RecordRepaymentSuccess",
        "返済を記録できませんでした",
        () => "返済を記録できませんでした",
      );
    },
  );
}

export function linkStatementTransfer(input: LinkStatementTransferMutation["variables"]["input"]) {
  return commitGraphQLMutation<LinkStatementTransferMutation, void>(
    linkStatementTransferMutation,
    { input },
    (response) => {
      assertGraphQLMutationSuccess(
        response.linkStatementTransfer,
        "LinkStatementTransferSuccess",
        "明細を既存取引に紐付けできませんでした",
        () => "明細を既存取引に紐付けできませんでした",
      );
    },
  );
}

export function linkStatementAccountMovement(
  input: LinkStatementAccountMovementMutation["variables"]["input"],
) {
  return commitGraphQLMutation<LinkStatementAccountMovementMutation, void>(
    linkStatementAccountMovementMutation,
    { input },
    (response) => {
      assertGraphQLMutationSuccess(
        response.linkStatementAccountMovement,
        "LinkStatementAccountMovementSuccess",
        "口座移動に紐付けできませんでした",
        () => "口座移動に紐付けできませんでした",
      );
    },
  );
}

export function linkStatementAccountIncome(
  input: LinkStatementAccountIncomeMutation["variables"]["input"],
) {
  return commitGraphQLMutation<LinkStatementAccountIncomeMutation, void>(
    linkStatementAccountIncomeMutation,
    { input },
    (response) => {
      assertGraphQLMutationSuccess(
        response.linkStatementAccountIncome,
        "LinkStatementAccountIncomeSuccess",
        "入金明細を既存の入金に紐付けできませんでした",
        () => "入金明細を既存の入金に紐付けできませんでした",
      );
    },
  );
}
