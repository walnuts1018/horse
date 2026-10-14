import type { CreatePurchaseInput } from "../../../shared/api";
import type { DocumentReview, StatementEntry } from "./imports";
import { statementSourceReference } from "./imports";

export function statementConfirmationKey(documentId: string, entry: StatementEntry): string {
  return `${documentId}:${statementSourceReference(entry)}`;
}

export function statementOccurredAt(entry: StatementEntry): string {
  return new Date(entry.occurredAt).toISOString();
}

export function createStatementPurchaseInput({
  document,
  householdId,
  idempotencyKey,
  entry,
  paymentAccountId,
}: {
  document: DocumentReview;
  householdId: string;
  idempotencyKey: string;
  entry: StatementEntry;
  paymentAccountId: string;
}): CreatePurchaseInput {
  return {
    idempotencyKey,
    householdId,
    origin: "AI",
    sourceDocumentId: document.id,
    sourceReference: statementSourceReference(entry),
    occurredAt: statementOccurredAt(entry),
    merchant: entry.merchant,
    paymentAllocations: [{ paymentAccountId, amountYen: entry.amount }],
    items: [
      {
        name: entry.merchant || "カード利用",
        amountYen: entry.amount,
        allocations: null,
      },
    ],
  };
}

export function createStatementExternalIncomeInput({
  document,
  householdId,
  idempotencyKey,
  entry,
  accountId,
}: {
  document: DocumentReview;
  householdId: string;
  idempotencyKey: string;
  entry: StatementEntry;
  accountId: string;
}) {
  return {
    householdId,
    accountId,
    amountYen: entry.amount,
    occurredAt: statementOccurredAt(entry),
    description: entry.merchant,
    idempotencyKey,
    sourceDocumentId: document.id,
    sourceReference: statementSourceReference(entry),
  };
}
