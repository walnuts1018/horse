import { useEffect, useState, type Dispatch, type RefObject, type SetStateAction } from "react";

import type { Household } from "../../../shared/api";
import type { ImportReceiptItemReviewState } from "../../../shared/lib/purchase-storage";
import type {
  DocumentReview,
  ExtractedItem,
  ReceiptPaymentDraft,
  ReceiptConfirmationState,
  ReviewTarget,
} from "./imports";
import type { ReceiptItemDeletionUndo } from "./receipt-review-draft";

import { confirmReceiptPurchase } from "./confirm-receipt-purchase";
import {
  effectiveReceiptItemAmount,
  receiptOverrideReasonRequired,
  uniqueConsistentReceiptCandidate,
} from "./receipt-confirmation";
import { receiptItemsNeedingReview } from "./receipt-item-review";
import { receiptPaymentAccountId, soleReceiptPaymentAccountId } from "./receipt-payment-account";
import { useReceiptEditingWorkflow } from "./use-receipt-editing-workflow";
import { useStatementConfirmation } from "./use-statement-confirmation";
export { effectiveReceiptItemAmount } from "./receipt-confirmation";

import {
  useCreateAndPostPurchase,
  useCorrectPurchaseFromStatementMutation,
  useHouseholdPurchases,
} from "../../../entities/purchase";

function normalizeName(name: string) {
  return name.trim().replace(/\s+/g, " ").toLocaleLowerCase("ja-JP");
}

import {
  purchaseDatePrecisionFor,
  purchaseOccurredAtFor,
  receiptDateInputValue,
  receiptTimeInputValue,
  persistedStatementConfirmationKeys,
} from "./imports";

export const manualAmountOverrideReasonOptions = [
  "OCRの読み間違い",
  "割引・税の修正",
  "レシートの印字が不明瞭",
  "その他",
] as const;

function receiptItemsChanged(document?: DocumentReview) {
  return Boolean(
    document?.result &&
    (document.result as typeof document.result & { reviewReceiptItemsChanged?: boolean })
      .reviewReceiptItemsChanged,
  );
}

function purchaseDateFor(document: DocumentReview) {
  return (
    document.receiptConfirmationState?.purchaseDate ??
    receiptDateInputValue(document.result?.receipt?.purchasedAt)
  );
}

function purchaseTimeFor(document: DocumentReview) {
  return (
    document.receiptConfirmationState?.purchaseTime ??
    receiptTimeInputValue(document.result?.receipt?.purchasedAt, purchaseDatePrecisionFor(document))
  );
}

export function useImportConfirmation({
  household,
  selectedDocument,
  setDocuments,
  editedReceiptDocuments,
  clearReceiptDraft,
  markUnsaved,
  setHasUnsavedChanges,
  onAdded,
}: {
  household: Household;
  selectedDocument: DocumentReview | undefined;
  setDocuments: Dispatch<SetStateAction<DocumentReview[]>>;
  editedReceiptDocuments: RefObject<Set<string>>;
  clearReceiptDraft: (documentId: string) => Promise<void>;
  markUnsaved: () => void;
  setHasUnsavedChanges: (unsaved: boolean) => void;
  onAdded: (keepOpen?: boolean) => void;
}) {
  const createAndPostPurchase = useCreateAndPostPurchase();
  const { mutate: correctPurchaseFromStatement } = useCorrectPurchaseFromStatementMutation();
  const { purchases: recentPurchases, refresh: refreshPurchases } = useHouseholdPurchases(
    household.id,
    100,
  );
  const [locallyConfirmedStatements, setConfirmedStatements] = useState<Set<string>>(
    () => new Set(),
  );
  const confirmedStatements = new Set([
    ...locallyConfirmedStatements,
    ...persistedStatementConfirmationKeys(selectedDocument),
  ]);
  const [reviewTargetState, setReviewTargetState] = useState<{
    documentId: string;
    index: number;
  }>({ documentId: "", index: 0 });
  const [receiptItemDeletionUndo, setReceiptItemDeletionUndo] = useState<ReceiptItemDeletionUndo>();
  const selectedDocumentId = selectedDocument?.id ?? "";
  const confirmationState = selectedDocument?.receiptConfirmationState;
  const defaultCandidate = selectedDocument
    ? receiptItemsChanged(selectedDocument)
      ? undefined
      : uniqueConsistentReceiptCandidate(selectedDocument)
    : undefined;
  const storedCandidateId = confirmationState?.selectedCandidate?.id;
  const storedCandidateIsCurrent = selectedDocument?.result?.candidates?.some(
    (candidate) => candidate.id === storedCandidateId,
  );
  const selectedCandidates = selectedDocument
    ? {
        [selectedDocument.id]:
          !receiptItemsChanged(selectedDocument) && storedCandidateIsCurrent
            ? storedCandidateId!
            : (defaultCandidate?.id ?? ""),
      }
    : {};
  const manualAmountOverrides = selectedDocument
    ? { [selectedDocument.id]: confirmationState?.manualAmountOverrides ?? {} }
    : {};
  const receiptItemReviewStates = selectedDocument
    ? { [selectedDocument.id]: confirmationState?.receiptItemReviewStates ?? {} }
    : {};
  const manualAmountOverrideReasons = selectedDocument
    ? { [selectedDocument.id]: confirmationState?.manualAmountOverrideReason }
    : {};
  const receiptPayments = selectedDocument
    ? { [selectedDocument.id]: confirmationState?.payments }
    : {};
  const receiptAmountDrafts = selectedDocument
    ? { [selectedDocument.id]: confirmationState?.amountDrafts ?? {} }
    : {};
  const receiptItemAllocations = selectedDocument
    ? { [selectedDocument.id]: confirmationState?.itemAllocations ?? {} }
    : {};
  const updateReceiptConfirmationState = (
    documentId: string,
    update: (state: ReceiptConfirmationState, document: DocumentReview) => ReceiptConfirmationState,
  ) => {
    setDocuments((current) =>
      current.map((document) => {
        if (document.id !== documentId) return document;
        const next = update(document.receiptConfirmationState ?? {}, document);
        return {
          ...document,
          receiptConfirmationState: Object.keys(next).length ? next : undefined,
        };
      }),
    );
  };
  const setSelectedCandidatesPersisted: Dispatch<SetStateAction<Record<string, string>>> = (
    action,
  ) => {
    if (!selectedDocument) return;
    if (receiptItemsChanged(selectedDocument)) return;
    updateReceiptConfirmationState(selectedDocument.id, (state, document) => {
      const current = {
        [document.id]:
          (document.result?.candidates?.some(
            (candidate) => candidate.id === state.selectedCandidate?.id,
          )
            ? state.selectedCandidate?.id
            : uniqueConsistentReceiptCandidate(document)?.id) ?? "",
      };
      const next = typeof action === "function" ? action(current) : action;
      const selectedId = next[document.id];
      const candidate = document.result?.candidates?.find((entry) => entry.id === selectedId);
      const { selectedCandidate: _selectedCandidate, ...rest } = state;
      return candidate ? { ...rest, selectedCandidate: { ...candidate } } : rest;
    });
  };
  const setManualAmountOverridesPersisted: Dispatch<
    SetStateAction<Record<string, Record<string, number>>>
  > = (action) => {
    if (!selectedDocument) return;
    updateReceiptConfirmationState(selectedDocument.id, (state, document) => {
      const current = { [document.id]: state.manualAmountOverrides ?? {} };
      const next = typeof action === "function" ? action(current) : action;
      const { manualAmountOverrides: _manualAmountOverrides, ...rest } = state;
      return Object.keys(next[document.id] ?? {}).length > 0
        ? { ...rest, manualAmountOverrides: next[document.id] }
        : rest;
    });
  };
  const setManualAmountOverrideReasonsPersisted: Dispatch<
    SetStateAction<Record<string, string | undefined>>
  > = (action) => {
    if (!selectedDocument) return;
    updateReceiptConfirmationState(selectedDocument.id, (state, document) => {
      const current = { [document.id]: state.manualAmountOverrideReason };
      const next = typeof action === "function" ? action(current) : action;
      const { manualAmountOverrideReason: _reason, ...rest } = state;
      return next[document.id] !== undefined
        ? { ...rest, manualAmountOverrideReason: next[document.id] }
        : rest;
    });
  };
  const setReceiptItemReviewStatesPersisted: Dispatch<
    SetStateAction<Record<string, Record<string, ImportReceiptItemReviewState>>>
  > = (action) => {
    if (!selectedDocument) return;
    updateReceiptConfirmationState(selectedDocument.id, (state, document) => {
      const current = { [document.id]: state.receiptItemReviewStates ?? {} };
      const next = typeof action === "function" ? action(current) : action;
      const { receiptItemReviewStates: _receiptItemReviewStates, ...rest } = state;
      return Object.keys(next[document.id] ?? {}).length > 0
        ? { ...rest, receiptItemReviewStates: next[document.id] }
        : rest;
    });
  };
  const setReceiptPaymentsPersisted: Dispatch<
    SetStateAction<Record<string, ReceiptPaymentDraft[] | undefined>>
  > = (action) => {
    if (!selectedDocument) return;
    updateReceiptConfirmationState(selectedDocument.id, (state, document) => {
      const current = { [document.id]: state.payments };
      const next = typeof action === "function" ? action(current) : action;
      const { payments: _payments, ...rest } = state;
      return next[document.id] !== undefined ? { ...rest, payments: next[document.id] } : rest;
    });
  };
  const setReceiptAmountDraftsPersisted: Dispatch<
    SetStateAction<Record<string, Record<string, string>>>
  > = (action) => {
    if (!selectedDocument) return;
    updateReceiptConfirmationState(selectedDocument.id, (state) => {
      const current = { [selectedDocument.id]: state.amountDrafts ?? {} };
      const next = typeof action === "function" ? action(current) : action;
      const { amountDrafts: _amountDrafts, ...rest } = state;
      return Object.keys(next[selectedDocument.id] ?? {}).length > 0
        ? { ...rest, amountDrafts: next[selectedDocument.id] }
        : rest;
    });
  };
  // 文書が切り替わった直後は前の文書の確認位置を引き継がない
  const reviewTargetIndex =
    reviewTargetState.documentId === selectedDocumentId ? reviewTargetState.index : 0;
  const setReviewTargetIndex: Dispatch<SetStateAction<number>> = (action) => {
    setReviewTargetState((current) => {
      const previous = current.documentId === selectedDocumentId ? current.index : 0;
      const index = typeof action === "function" ? action(previous) : action;
      return { documentId: selectedDocumentId, index };
    });
  };
  const suggestedCategoryId = (item: ExtractedItem) => {
    const categories = household.categories.filter((category) => !category.archived);
    const hinted = categories.filter(
      (category) => normalizeName(category.name) === normalizeName(item.categoryHint ?? ""),
    );
    if (hinted.length === 1) return hinted[0].id;
    const matchingCategoryIds = new Set(
      recentPurchases
        .filter((purchase) => purchase.status !== "DRAFT")
        .flatMap((purchase) => purchase.items)
        .filter((purchaseItem) => normalizeName(purchaseItem.name) === normalizeName(item.name))
        .map((purchaseItem) => purchaseItem.categoryId)
        .filter((categoryId): categoryId is string => Boolean(categoryId)),
    );
    return matchingCategoryIds.size === 1 ? [...matchingCategoryIds][0] : "";
  };
  const receiptItemAmount = (document: DocumentReview, item: ExtractedItem, index: number) => {
    const candidateID = selectedCandidates[document.id] ?? "";
    const candidate = document.result?.candidates?.find((entry) => entry.id === candidateID);
    return effectiveReceiptItemAmount(
      item,
      index,
      candidate?.amounts,
      manualAmountOverrides[document.id],
    );
  };
  const receiptTotal = (document: DocumentReview) =>
    (document.result?.receipt?.items ?? []).reduce(
      (total, item, index) => total + receiptItemAmount(document, item, index),
      0,
    );
  function paymentDraftFor(document: DocumentReview): ReceiptPaymentDraft[] {
    const storedDraft = receiptPayments[document.id];
    if (storedDraft) return storedDraft;
    const recognizedPayments = document.result?.receipt?.payments ?? [];
    if (recognizedPayments.length) {
      return recognizedPayments.map((payment, index) => {
        return {
          key: `recognized:${index}`,
          method: payment.method,
          cardLast4: payment.cardLast4,
          paymentAccountId: receiptPaymentAccountId(
            household.accounts,
            payment.method,
            payment.cardLast4,
          ),
          amountYen: payment.amount,
        };
      });
    }
    return [
      {
        key: "default",
        paymentAccountId: soleReceiptPaymentAccountId(household.accounts),
        amountYen: receiptTotal(document),
      },
    ];
  }

  const reviewTargets: ReviewTarget[] = selectedDocument?.result?.receipt?.items
    ? [
        ...receiptItemsNeedingReview(
          selectedDocument.result.receipt.items,
          receiptItemReviewStates[selectedDocument.id],
        ).map(({ item, index }) => ({
          key: `item:${item.key}`,
          label: item.name.trim() || `品目 ${index + 1}`,
          elementId: `receipt-review-item-${encodeURIComponent(item.key ?? `item-${index}`)}`,
        })),
        ...(() => {
          const needsPurchaseDate = !purchaseOccurredAtFor(selectedDocument);
          const selectedCandidate = selectedDocument.result.candidates?.find(
            (candidate) => candidate.id === selectedCandidates[selectedDocument.id],
          );
          const needsCandidateSelection =
            receiptItemsChanged(selectedDocument) ||
            ((selectedDocument.result.candidates?.length ?? 0) > 0 && !selectedCandidate);
          const paymentsTotal = paymentDraftFor(selectedDocument).reduce(
            (total, payment) => total + payment.amountYen,
            0,
          );
          const needsAmountReview =
            receiptOverrideReasonRequired(
              selectedDocument.result.receipt.total,
              receiptTotal(selectedDocument),
            ) || needsCandidateSelection;
          const needsPaymentReview =
            household.accounts.length > 0 &&
            (paymentDraftFor(selectedDocument).some((payment) => !payment.paymentAccountId) ||
              paymentsTotal !== receiptTotal(selectedDocument));
          return [
            ...(needsPurchaseDate
              ? [{ key: "date", label: "購入日", elementId: "receipt-review-purchase-date" }]
              : []),
            ...(needsAmountReview
              ? [
                  {
                    key: "amount",
                    label: "金額差異",
                    elementId: receiptItemsChanged(selectedDocument)
                      ? "receipt-review-items"
                      : needsCandidateSelection
                        ? "receipt-review-candidates"
                        : "receipt-review-amount-reason",
                  },
                ]
              : []),
            ...(needsPaymentReview
              ? [{ key: "payments", label: "決済合計", elementId: "receipt-review-payments" }]
              : []),
          ];
        })(),
      ]
    : [];
  const activeReviewIndex = Math.min(reviewTargetIndex, reviewTargets.length);
  const activeReviewTarget = reviewTargets.at(activeReviewIndex);
  const activeReviewTargetKey = activeReviewTarget?.key;
  const activeReviewTargetElementId = activeReviewTarget?.elementId;
  useEffect(() => {
    if (!activeReviewTargetKey || !activeReviewTargetElementId) return;
    if (document.activeElement?.closest(".receipt-item-review")) return;
    const element = document.getElementById(activeReviewTargetElementId);
    element?.scrollIntoView({ block: "center", behavior: "smooth" });
    if (element instanceof HTMLElement) element.focus({ preventScroll: true });
  }, [activeReviewTargetKey, activeReviewTargetElementId]);

  const resetConfirmation = () => {
    setConfirmedStatements(new Set());
    if (!selectedDocument) return;
    updateReceiptConfirmationState(selectedDocument.id, (state) => {
      const { selectedCandidate: _selectedCandidate, ...rest } = state;
      return rest;
    });
  };

  const receiptEditing = useReceiptEditingWorkflow({
    selectedDocument,
    setDocuments,
    editedReceiptDocuments,
    markUnsaved,
    updateReceiptConfirmationState,
    reviewTargetIndex,
    setReviewTargetIndex,
    receiptItemDeletionUndo,
    setReceiptItemDeletionUndo,
    receiptItemsChanged,
  });

  const confirm = (document: DocumentReview) =>
    confirmReceiptPurchase({
      document,
      household,
      selectedCandidates,
      manualAmountOverrideReasons,
      manualAmountOverrides,
      receiptItemAllocations,
      paymentDraftFor,
      suggestedCategoryId,
      itemsWereChanged: receiptItemsChanged(document),
      createAndPostPurchase,
      clearReceiptDraft,
      setReceiptItemDeletionUndo,
      setHasUnsavedChanges,
      refreshPurchases,
      onAdded,
    });

  const statementConfirmation = useStatementConfirmation({
    household,
    confirmedStatements,
    setConfirmedStatements,
    createAndPostPurchase,
    correctPurchaseFromStatement,
    refreshPurchases,
    setHasUnsavedChanges,
    onAdded,
  });
  return {
    recentPurchases,
    confirmedStatements,
    setConfirmedStatements,
    selectedCandidates,
    setSelectedCandidates: setSelectedCandidatesPersisted,
    reviewTargetIndex,
    reviewTargets,
    activeReviewIndex,
    activeReviewTarget,
    setReviewTargetIndex,
    manualAmountOverrides,
    setManualAmountOverrides: setManualAmountOverridesPersisted,
    receiptItemReviewStates,
    setReceiptItemReviewStates: setReceiptItemReviewStatesPersisted,
    manualAmountOverrideReasons,
    setManualAmountOverrideReasons: setManualAmountOverrideReasonsPersisted,
    receiptPayments,
    setReceiptPayments: setReceiptPaymentsPersisted,
    receiptAmountDrafts,
    setReceiptAmountDrafts: setReceiptAmountDraftsPersisted,
    itemAllocationsFor: (documentId: string, itemKey: string) =>
      receiptItemAllocations[documentId]?.[itemKey] ?? [],
    editReceiptItemAllocations: receiptEditing.editReceiptItemAllocations,
    purchaseDateFor,
    purchaseTimeFor,
    purchaseOccurredAtFor,
    editPurchaseDate: receiptEditing.editPurchaseDate,
    editPurchaseTime: receiptEditing.editPurchaseTime,
    suggestedCategoryId,
    receiptItemAmount,
    receiptTotal,
    paymentDraftFor,
    editItem: receiptEditing.editItem,
    addReceiptItem: receiptEditing.addReceiptItem,
    deleteReceiptItem: receiptEditing.deleteReceiptItem,
    canUndoReceiptItemDeletion: receiptItemDeletionUndo?.documentId === selectedDocumentId,
    undoReceiptItemDeletion: receiptEditing.undoReceiptItemDeletion,
    editMerchant: receiptEditing.editMerchant,
    resetConfirmation,
    confirm,
    ...statementConfirmation,
  };
}
