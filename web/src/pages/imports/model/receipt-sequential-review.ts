import type { DocumentReview } from "./imports";

const extractedStatuses = new Set(["READY", "REVIEW", "COMPLETED", "SUCCEEDED", "DONE"]);

export function receiptReviewProgress(
  documents: DocumentReview[],
  completedDocumentIDs: ReadonlySet<string>,
) {
  const candidates = documents.filter(
    (document) =>
      document.sourceKind?.toLowerCase() === "receipt" &&
      document.result?.receipt !== undefined &&
      extractedStatuses.has(document.status.toUpperCase()) &&
      !document.duplicateOf,
  );
  const completed = candidates.filter(
    (document) => document.confirmations?.length || completedDocumentIDs.has(document.id),
  );
  return {
    receipts: candidates.filter(
      (document) => !document.confirmations?.length && !completedDocumentIDs.has(document.id),
    ),
    completedCount: completed.length,
    totalCount: candidates.length,
  };
}

export function reviewableReceipts(
  documents: DocumentReview[],
  completedDocumentIDs: ReadonlySet<string>,
) {
  return receiptReviewProgress(documents, completedDocumentIDs).receipts;
}

export function nextReceiptToReview(
  receipts: DocumentReview[],
  currentDocumentID: string,
): DocumentReview | undefined {
  if (receipts.length === 0) return undefined;
  const currentIndex = receipts.findIndex((document) => document.id === currentDocumentID);
  return receipts[(currentIndex + 1) % receipts.length];
}
