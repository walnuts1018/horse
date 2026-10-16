import type { Dispatch, SetStateAction } from "react";

import type { Household } from "../../../shared/api";
import type { DocumentReview, ExtractedItem, ReceiptPaymentDraft } from "./imports";
import type { ReceiptItemDeletionUndo } from "./receipt-review-draft";
import type { ReceiptItemAllocation } from "./use-receipt-editing-workflow";

import { useCreateAndPostPurchase } from "../../../entities/purchase";
import { importIdempotencyKey } from "../api/documents";
import { purchaseDatePrecisionFor, purchaseOccurredAtFor } from "./imports";
import {
  createReceiptPurchaseInput,
  effectiveReceiptItemAmount,
  receiptOverrideReasonRequired,
} from "./receipt-confirmation";

type ReceiptPurchaseConfirmationOptions = {
  document: DocumentReview;
  household: Household;
  selectedCandidates: Record<string, string>;
  manualAmountOverrideReasons: Record<string, string | undefined>;
  manualAmountOverrides: Record<string, Record<string, number>>;
  receiptItemAllocations: Record<string, Record<string, ReceiptItemAllocation[]>>;
  paymentDraftFor: (document: DocumentReview) => ReceiptPaymentDraft[];
  suggestedCategoryId: (item: ExtractedItem) => string;
  itemsWereChanged: boolean;
  createAndPostPurchase: ReturnType<typeof useCreateAndPostPurchase>;
  clearReceiptDraft: (documentId: string) => Promise<void>;
  setReceiptItemDeletionUndo: Dispatch<SetStateAction<ReceiptItemDeletionUndo | undefined>>;
  setHasUnsavedChanges: (unsaved: boolean) => void;
  refreshPurchases: () => void;
  onAdded: (keepOpen?: boolean) => void;
};

export async function confirmReceiptPurchase({
  document,
  household,
  selectedCandidates,
  manualAmountOverrideReasons,
  manualAmountOverrides,
  receiptItemAllocations,
  paymentDraftFor,
  suggestedCategoryId,
  itemsWereChanged,
  createAndPostPurchase,
  clearReceiptDraft,
  setReceiptItemDeletionUndo,
  setHasUnsavedChanges,
  refreshPurchases,
  onAdded,
}: ReceiptPurchaseConfirmationOptions) {
  const receipt = document.result?.receipt;
  const occurredAt = purchaseOccurredAtFor(document);
  const items = receipt?.items ?? [];
  const candidateID = selectedCandidates[document.id] ?? "";
  const candidate = document.result?.candidates?.find((item) => item.id === candidateID);
  const overrideReason = manualAmountOverrideReasons[document.id]?.trim() ?? "";
  const confirmationTotal = items.reduce(
    (total, item, index) =>
      total +
      effectiveReceiptItemAmount(
        item,
        index,
        candidate?.amounts,
        manualAmountOverrides[document.id],
      ),
    0,
  );
  const requiresOverrideReason = receiptOverrideReasonRequired(receipt?.total, confirmationTotal);
  if (
    items.some(
      (item) =>
        !item.name.trim() ||
        !Number.isSafeInteger(item.grossAmount) ||
        item.grossAmount < (item.verification === "MANUAL" ? 1 : 0),
    )
  ) {
    throw new Error("品名と金額を確認してください");
  }
  const paymentDraft = paymentDraftFor(document);
  const purchaseInput = createReceiptPurchaseInput({
    document,
    householdId: household.id,
    occurredAt,
    datePrecision: purchaseDatePrecisionFor(document),
    candidateId: candidateID,
    requiresCandidateSelection: Boolean(
      !itemsWereChanged && document.result?.candidates?.length && !candidate,
    ),
    requiresOverrideReason,
    overrideReason,
    paymentDraft,
    accountCount: household.accounts.length,
    manualAmounts: manualAmountOverrides[document.id],
    allocations: receiptItemAllocations[document.id],
    suggestedCategoryIds: items.map((item) => suggestedCategoryId(item)),
  });
  await createAndPostPurchase({
    ...purchaseInput,
    idempotencyKey: await importIdempotencyKey(document.id, "receipt"),
  });
  await clearReceiptDraft(document.id);
  setReceiptItemDeletionUndo(undefined);
  setHasUnsavedChanges(false);
  refreshPurchases();
  onAdded();
}
