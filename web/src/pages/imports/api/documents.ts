import { commitMutation } from "react-relay";

import type { DocumentFilters, DocumentReview } from "../model/imports";

import {
  recordAccountMovementMutation,
  recordExternalIncomeMutation,
  recordRepaymentMutation,
  recordTransferMutation,
} from "../../../entities/ledger";
import {
  getRelayEnvironment,
  type RecordAccountMovementMutation,
  type RecordExternalIncomeMutation,
  type RecordRepaymentMutation,
  type RecordTransferMutation,
} from "../../../shared/api";
import { asRecord } from "../model/imports";

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
  return new Promise<void>((resolve, reject) =>
    commitMutation<RecordAccountMovementMutation>(getRelayEnvironment(), {
      mutation: recordAccountMovementMutation,
      variables: { input },
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new Error(errors[0].message));
          return;
        }
        const result = response.recordAccountMovement;
        if (result.__typename === "RecordAccountMovementSuccess") {
          resolve();
          return;
        }
        reject(new Error(result.message || "口座間振替を記録できませんでした"));
      },
      onError: reject,
    }),
  );
}

export function recordStatementExternalIncome(
  input: RecordExternalIncomeMutation["variables"]["input"],
) {
  return new Promise<void>((resolve, reject) =>
    commitMutation<RecordExternalIncomeMutation>(getRelayEnvironment(), {
      mutation: recordExternalIncomeMutation,
      variables: { input },
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new Error(errors[0].message));
          return;
        }
        const result = response.recordExternalIncome;
        if (result.__typename === "RecordExternalIncomeSuccess") {
          resolve();
          return;
        }
        reject(new Error(result.message || "外部入金を記録できませんでした"));
      },
      onError: reject,
    }),
  );
}

export function recordStatementLoan(input: RecordTransferMutation["variables"]["input"]) {
  return new Promise<void>((resolve, reject) =>
    commitMutation<RecordTransferMutation>(getRelayEnvironment(), {
      mutation: recordTransferMutation,
      variables: { input },
      onCompleted: (response, errors) => {
        if (errors?.length) return reject(new Error(errors[0].message));
        const result = response.recordTransfer;
        if (result.__typename === "RecordTransferSuccess") return resolve();
        reject(new Error(result.message || "貸付を記録できませんでした"));
      },
      onError: reject,
    }),
  );
}

export function recordStatementRepayment(input: RecordRepaymentMutation["variables"]["input"]) {
  return new Promise<void>((resolve, reject) =>
    commitMutation<RecordRepaymentMutation>(getRelayEnvironment(), {
      mutation: recordRepaymentMutation,
      variables: { input },
      onCompleted: (response, errors) => {
        if (errors?.length) return reject(new Error(errors[0].message));
        const result = response.recordRepayment;
        if (result.__typename === "RecordRepaymentSuccess") return resolve();
        reject(new Error(result.message || "返済を記録できませんでした"));
      },
      onError: reject,
    }),
  );
}
