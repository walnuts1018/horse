import { useEffect, useState, type Dispatch, type RefObject, type SetStateAction } from "react";

import type { Household } from "../../../shared/api";
import type { ImportReceiptItemReviewState } from "../../../shared/lib/purchase-storage";
import type {
  DocumentReview,
  ExtractedItem,
  ReceiptPaymentDraft,
  ReceiptConfirmationState,
  ReviewTarget,
  StatementEntry,
} from "./imports";

import {
  createReceiptPurchaseInput,
  effectiveReceiptItemAmount,
  uniqueConsistentReceiptCandidate,
} from "./receipt-confirmation";
import { receiptItemsNeedingReview } from "./receipt-item-review";
import { receiptPaymentAccountId, soleReceiptPaymentAccountId } from "./receipt-payment-account";
import {
  addManualReceiptItem as appendManualReceiptItem,
  deleteReceiptItemByKey,
  ensureReceiptItemKeys,
  rekeyReceiptConfirmationAfterStructureChange,
  rekeyReceiptItemValues,
  restoreReceiptItemDeletion,
  type ReceiptItemDeletionUndo,
} from "./receipt-review-draft";
import {
  createStatementExternalIncomeInput,
  createStatementPurchaseInput,
  statementConfirmationKey,
  statementOccurredAt,
} from "./statement-confirmation";
export { effectiveReceiptItemAmount } from "./receipt-confirmation";

import {
  useCorrectPurchaseFromStatementMutation,
  useCreateAndPostPurchase,
  useHouseholdPurchases,
} from "../../../entities/purchase";

function normalizeName(name: string) {
  return name.trim().replace(/\s+/g, " ").toLocaleLowerCase("ja-JP");
}

import {
  importIdempotencyKey,
  linkStatementTransfer as linkStatementTransferRequest,
  linkStatementAccountMovement as linkStatementAccountMovementRequest,
  linkStatementRefundCorrection as linkStatementRefundCorrectionRequest,
  linkStatementAccountIncome as linkStatementAccountIncomeRequest,
  recordStatementAccountMovement,
  recordStatementExternalIncome,
  recordStatementLoan,
  recordStatementRepayment,
} from "../api/documents";
import {
  receiptDateInputValue,
  receiptOccurredAt,
  receiptTimeInputValue,
  persistedStatementConfirmationKeys,
  statementConfirmationIssue,
  statementSourceReference,
} from "./imports";

export const manualAmountOverrideReasonOptions = [
  "OCRの読み間違い",
  "割引・税の修正",
  "レシートの印字が不明瞭",
  "その他",
] as const;

type ReceiptItemAllocation = { poolId: string; weight: number };

function receiptItemsChanged(document?: DocumentReview) {
  return Boolean(
    document?.result &&
    (document.result as typeof document.result & { reviewReceiptItemsChanged?: boolean })
      .reviewReceiptItemsChanged,
  );
}

function purchaseOccurredAtFor(document: DocumentReview) {
  return receiptOccurredAt(
    document.result?.receipt?.purchasedAt,
    document.receiptConfirmationState?.purchaseDate,
    document.receiptConfirmationState?.purchaseTime,
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
    receiptTimeInputValue(document.result?.receipt?.purchasedAt)
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
  const hasRefundStatements = selectedDocument?.result?.statements?.some(
    (entry) => entry.kind === "REFUND" && entry.amount < 0,
  );
  const { purchases: recentPurchases, refresh: refreshPurchases } = useHouseholdPurchases(
    household.id,
    100,
    { loadAll: hasRefundStatements },
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
  const hasManualAmountOverrides = (documentId: string) =>
    Object.keys(manualAmountOverrides[documentId] ?? {}).length > 0;
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
            hasManualAmountOverrides(selectedDocument.id) ||
            needsCandidateSelection ||
            (selectedCandidate
              ? !selectedCandidate.consistent
              : selectedDocument.result.balanced === false);
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
                    elementId:
                      receiptItemsChanged(selectedDocument) || !needsCandidateSelection
                        ? "receipt-review-amount-reason"
                        : "receipt-review-candidates",
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

  const editItem = (documentId: string, index: number, patch: Partial<ExtractedItem>) => {
    editedReceiptDocuments.current.add(documentId);
    markUnsaved();
    const document = selectedDocument?.id === documentId ? selectedDocument : undefined;
    const originalItems = document?.result?.receipt?.items;
    if (originalItems) {
      const keyedItems = ensureReceiptItemKeys(originalItems);
      const survivingKeys = new Set(keyedItems.flatMap((item) => (item.key ? [item.key] : [])));
      const editedItem = keyedItems[index];
      const editChangesItem =
        JSON.stringify(editedItem) !== JSON.stringify({ ...editedItem, ...patch });
      updateReceiptConfirmationState(documentId, (state) => {
        const rekeyedManualAmountOverrides = rekeyReceiptItemValues(
          state.manualAmountOverrides,
          originalItems,
          keyedItems,
          survivingKeys,
        );
        const rekeyedItemAllocations = rekeyReceiptItemValues(
          state.itemAllocations,
          originalItems,
          keyedItems,
          survivingKeys,
        );
        const rekeyedReviewStates = rekeyReceiptItemValues(
          state.receiptItemReviewStates,
          originalItems,
          keyedItems,
          survivingKeys,
        );
        const nextReviewStates =
          editChangesItem && editedItem.key
            ? { ...rekeyedReviewStates, [editedItem.key]: "CORRECTED" as const }
            : rekeyedReviewStates;
        return {
          ...state,
          ...(rekeyedManualAmountOverrides
            ? { manualAmountOverrides: rekeyedManualAmountOverrides }
            : {}),
          ...(rekeyedItemAllocations ? { itemAllocations: rekeyedItemAllocations } : {}),
          ...(nextReviewStates ? { receiptItemReviewStates: nextReviewStates } : {}),
        };
      });
    }
    setDocuments((current) =>
      current.map((entry) => {
        if (entry.id !== documentId || !entry.result?.receipt?.items) return entry;
        const items = ensureReceiptItemKeys(entry.result.receipt.items).map((item, itemIndex) => {
          if (itemIndex !== index) return item;
          const nameChanged = patch.name !== undefined && patch.name !== item.name;
          return Object.assign({}, item, patch, nameChanged ? { productVariantId: null } : {});
        });
        return {
          ...entry,
          result: { ...entry.result, receipt: { ...entry.result.receipt, items } },
        };
      }),
    );
  };

  const changeReceiptItemStructure = (
    documentId: string,
    action: "add" | { deleteKey: string },
  ) => {
    if (!selectedDocument || selectedDocument.id !== documentId) return;
    const originalItems = selectedDocument.result?.receipt?.items;
    if (!originalItems) return;
    const keyedItems = ensureReceiptItemKeys(originalItems);
    const nextItems =
      action === "add"
        ? appendManualReceiptItem(keyedItems, `manual:${crypto.randomUUID()}`)
        : deleteReceiptItemByKey(keyedItems, action.deleteKey);
    if (nextItems.length === keyedItems.length) return;
    if (action === "add") {
      setReceiptItemDeletionUndo(undefined);
    } else {
      const deletedIndex = keyedItems.findIndex((item) => item.key === action.deleteKey);
      if (deletedIndex < 0) return;
      setReceiptItemDeletionUndo({
        documentId,
        item: keyedItems[deletedIndex],
        index: deletedIndex,
        reviewTargetIndex,
        remainingItemKeys: nextItems.flatMap((item) => (item.key ? [item.key] : [])),
        remainingItems: nextItems,
        confirmationState: selectedDocument.receiptConfirmationState ?? {},
        balanced: selectedDocument.result?.balanced,
        reviewReceiptItemsChanged: receiptItemsChanged(selectedDocument),
      });
    }
    editedReceiptDocuments.current.add(documentId);
    markUnsaved();
    updateReceiptConfirmationState(documentId, (state) => {
      return rekeyReceiptConfirmationAfterStructureChange(
        state,
        originalItems,
        keyedItems,
        nextItems,
      );
    });
    setDocuments((current) =>
      current.map((entry) => {
        if (entry.id !== documentId || !entry.result?.receipt) return entry;
        const result = Object.assign(
          {
            ...entry.result,
            balanced: false,
            receipt: { ...entry.result.receipt, items: nextItems },
          },
          { reviewReceiptItemsChanged: true },
        );
        return { ...entry, result };
      }),
    );
  };

  const addReceiptItem = (documentId: string) => changeReceiptItemStructure(documentId, "add");
  const deleteReceiptItem = (documentId: string, itemKey: string) =>
    changeReceiptItemStructure(documentId, { deleteKey: itemKey });

  const undoReceiptItemDeletion = () => {
    const undo = receiptItemDeletionUndo;
    const document = selectedDocument;
    if (!undo || !document) return;
    const restored = restoreReceiptItemDeletion(document, undo);
    if (!restored) {
      setReceiptItemDeletionUndo(undefined);
      return;
    }
    editedReceiptDocuments.current.add(restored.id);
    markUnsaved();
    setDocuments((current) =>
      current.map((entry) => (entry.id === restored.id ? restored : entry)),
    );
    setReviewTargetIndex(undo.reviewTargetIndex);
    setReceiptItemDeletionUndo(undefined);
  };

  const editReceiptItemAllocations = (
    documentId: string,
    itemKey: string,
    allocations: ReceiptItemAllocation[],
  ) => {
    editedReceiptDocuments.current.add(documentId);
    markUnsaved();
    updateReceiptConfirmationState(documentId, (state) => ({
      ...state,
      itemAllocations: { ...state.itemAllocations, [itemKey]: allocations },
    }));
  };

  const editMerchant = (documentId: string, merchant: string) => {
    editedReceiptDocuments.current.add(documentId);
    markUnsaved();
    setDocuments((current) =>
      current.map((document) =>
        document.id === documentId && document.result
          ? {
              ...document,
              result: {
                ...document.result,
                receipt: { ...document.result.receipt, merchant },
              },
            }
          : document,
      ),
    );
  };

  const editPurchaseDate = (documentId: string, date: string) => {
    editedReceiptDocuments.current.add(documentId);
    markUnsaved();
    updateReceiptConfirmationState(documentId, (state) => ({ ...state, purchaseDate: date }));
    setDocuments((current) =>
      current.map((document) => {
        if (document.id !== documentId || !document.result) return document;
        const purchasedAt = receiptOccurredAt(
          document.result.receipt?.purchasedAt,
          date,
          document.receiptConfirmationState?.purchaseTime,
        );
        return purchasedAt
          ? {
              ...document,
              result: {
                ...document.result,
                receipt: { ...document.result.receipt, purchasedAt },
              },
            }
          : document;
      }),
    );
  };

  const editPurchaseTime = (documentId: string, time: string) => {
    editedReceiptDocuments.current.add(documentId);
    markUnsaved();
    updateReceiptConfirmationState(documentId, (state) => ({ ...state, purchaseTime: time }));
    setDocuments((current) =>
      current.map((document) => {
        if (document.id !== documentId || !document.result) return document;
        const purchasedAt = receiptOccurredAt(
          document.result.receipt?.purchasedAt,
          document.receiptConfirmationState?.purchaseDate ??
            receiptDateInputValue(document.result.receipt?.purchasedAt),
          time,
        );
        return purchasedAt
          ? {
              ...document,
              result: {
                ...document.result,
                receipt: { ...document.result.receipt, purchasedAt },
              },
            }
          : document;
      }),
    );
  };

  const confirm = async (document: DocumentReview) => {
    const receipt = document.result?.receipt;
    const occurredAt = purchaseOccurredAtFor(document);
    const items = receipt?.items ?? [];
    const candidateID = selectedCandidates[document.id] ?? "";
    const candidate = document.result?.candidates?.find((item) => item.id === candidateID);
    const overrideReason = manualAmountOverrideReasons[document.id]?.trim() ?? "";
    const requiresOverrideReason =
      hasManualAmountOverrides(document.id) ||
      receiptItemsChanged(document) ||
      (candidate ? !candidate.consistent : !document.result?.balanced);
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
      candidateId: candidateID,
      requiresCandidateSelection: Boolean(
        !receiptItemsChanged(document) && document.result?.candidates?.length && !candidate,
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
  };

  const confirmStatement = async (
    document: DocumentReview,
    entry: StatementEntry,
    paymentAccountId: string,
  ) => {
    if (!document.result || (entry.kind !== "CARD_PURCHASE" && entry.kind !== "EXTERNAL_INCOME"))
      return;
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    const account = household.accounts.find((candidate) => candidate.id === paymentAccountId);
    const isExternalIncome = entry.kind === "EXTERNAL_INCOME";
    if (!account || (isExternalIncome ? account.kind !== "BANK" : false)) {
      throw new Error(
        isExternalIncome ? "入金先の銀行口座を選択してください" : "決済口座を選択してください",
      );
    }
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    if (isExternalIncome) {
      await recordStatementExternalIncome(
        createStatementExternalIncomeInput({
          document,
          householdId: household.id,
          accountId: account.id,
          entry,
          idempotencyKey: await importIdempotencyKey(document.id, reference),
        }),
      );
      setConfirmedStatements((current) => new Set(current).add(key));
      setHasUnsavedChanges(false);
      onAdded(true);
      return;
    }
    await createAndPostPurchase(
      createStatementPurchaseInput({
        document,
        householdId: household.id,
        idempotencyKey: await importIdempotencyKey(document.id, reference),
        entry,
        paymentAccountId,
      }),
    );
    refreshPurchases();
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const confirmStatementRefund = async (
    document: DocumentReview,
    entry: StatementEntry,
    purchaseId: string,
    itemId?: string,
  ) => {
    if (entry.kind !== "REFUND") throw new Error("返金明細を選択してください");
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    const target = itemId ? { item: { purchaseId, itemId } } : { purchase: { purchaseId } };
    await correctPurchaseFromStatement({
      input: {
        householdId: household.id,
        sourceDocumentId: document.id,
        sourceReference: reference,
        target,
        reason: "明細の返品・返金",
        idempotencyKey: await importIdempotencyKey(document.id, reference),
      },
    });
    refreshPurchases();
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const linkStatementRefundCorrection = async (
    document: DocumentReview,
    entry: StatementEntry,
    correctionId: string,
  ) => {
    if (entry.kind !== "REFUND" || entry.amount >= 0) throw new Error("返金明細を選択してください");
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    await linkStatementRefundCorrectionRequest({
      householdId: household.id,
      correctionId,
      sourceDocumentId: document.id,
      sourceReference: reference,
    });
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const confirmStatementTransfer = async (
    document: DocumentReview,
    entry: StatementEntry,
    statementAccountId: string,
    otherAccountId: string,
  ) => {
    if (entry.kind !== "ACCOUNT_TRANSFER" && entry.kind !== "CARD_BILL_PAYMENT") {
      throw new Error("口座移動の明細を選択してください");
    }
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    const statementAccount = household.accounts.find(
      (account) => account.id === statementAccountId,
    );
    const otherAccount = household.accounts.find((account) => account.id === otherAccountId);
    const isCardBillPayment = entry.kind === "CARD_BILL_PAYMENT";
    if (!statementAccount || !otherAccount || statementAccount.id === otherAccount.id) {
      throw new Error("振替元と振替先の口座を選択してください");
    }
    if (
      (isCardBillPayment && (statementAccount.kind === "CARD" || otherAccount.kind !== "CARD")) ||
      (!isCardBillPayment && (statementAccount.kind === "CARD" || otherAccount.kind === "CARD"))
    ) {
      throw new Error("明細の口座種別に合う口座を選択してください");
    }
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    const fromAccountId =
      isCardBillPayment || entry.amount < 0 ? statementAccount.id : otherAccount.id;
    const toAccountId =
      isCardBillPayment || entry.amount < 0 ? otherAccount.id : statementAccount.id;
    await recordStatementAccountMovement({
      householdId: household.id,
      kind: isCardBillPayment ? "CARD_BILL_PAYMENT" : "TRANSFER",
      fromAccountId,
      toAccountId,
      amountYen: Math.abs(entry.amount),
      occurredAt: statementOccurredAt(entry),
      description: entry.merchant,
      idempotencyKey: await importIdempotencyKey(document.id, reference),
      sourceDocumentId: document.id,
      sourceReference: reference,
      sourceAccountId: statementAccount.id,
    });
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const confirmStatementLoan = async (
    document: DocumentReview,
    entry: StatementEntry,
    statementAccountId: string,
    fromMemberId: string,
    toMemberId: string,
    loanId?: string,
  ) => {
    if (entry.kind !== "LOAN" && entry.kind !== "REPAYMENT")
      throw new Error("貸付または返済の明細を選択してください");
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    if (
      !household.accounts.some(
        (account) => account.id === statementAccountId && account.kind === "BANK",
      )
    )
      throw new Error("銀行口座を選択してください");
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    const input = {
      householdId: household.id,
      fromMemberId,
      toMemberId,
      amountYen: Math.abs(entry.amount),
      occurredAt: statementOccurredAt(entry),
      description: entry.merchant,
      idempotencyKey: await importIdempotencyKey(document.id, reference),
      sourceDocumentId: document.id,
      sourceReference: reference,
      sourceAccountId: statementAccountId,
      ...(entry.amount < 0
        ? { paymentAccountId: statementAccountId }
        : { destinationAccountId: statementAccountId }),
    };
    if (entry.kind === "LOAN") {
      await recordStatementLoan({ ...input, kind: "LOAN" });
    } else {
      if (!loanId) throw new Error("返済対象の貸付を選択してください");
      await recordStatementRepayment({ ...input, loanId });
    }
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const linkStatementTransfer = async (
    document: DocumentReview,
    entry: StatementEntry,
    statementAccountId: string,
    transferId: string,
  ) => {
    if (entry.kind !== "LOAN" && entry.kind !== "REPAYMENT")
      throw new Error("貸付または返済の明細を選択してください");
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    if (
      !household.accounts.some(
        (account) => account.id === statementAccountId && account.kind === "BANK",
      )
    )
      throw new Error("銀行口座を選択してください");
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    await linkStatementTransferRequest({
      householdId: household.id,
      ledgerEntryId: transferId,
      sourceDocumentId: document.id,
      sourceReference: reference,
      sourceAccountId: statementAccountId,
    });
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const linkStatementAccountMovement = async (
    document: DocumentReview,
    entry: StatementEntry,
    statementAccountId: string,
    movementId: string,
  ) => {
    if (entry.kind !== "ACCOUNT_TRANSFER" && entry.kind !== "CARD_BILL_PAYMENT")
      throw new Error("口座移動の明細を選択してください");
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    if (
      !household.accounts.some(
        (account) => account.id === statementAccountId && account.kind === "BANK",
      )
    )
      throw new Error("銀行口座を選択してください");
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    await linkStatementAccountMovementRequest({
      householdId: household.id,
      accountMovementId: movementId,
      sourceDocumentId: document.id,
      sourceReference: reference,
      sourceAccountId: statementAccountId,
    });
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

  const linkStatementAccountIncome = async (
    document: DocumentReview,
    entry: StatementEntry,
    statementAccountId: string,
    incomeId: string,
  ) => {
    if (entry.kind !== "EXTERNAL_INCOME") throw new Error("外部入金の明細を選択してください");
    const issue = statementConfirmationIssue(entry);
    if (issue) throw new Error(issue);
    if (
      !household.accounts.some(
        (account) => account.id === statementAccountId && account.kind === "BANK",
      )
    )
      throw new Error("銀行口座を選択してください");
    const reference = statementSourceReference(entry);
    const key = statementConfirmationKey(document.id, entry);
    if (confirmedStatements.has(key)) return;
    await linkStatementAccountIncomeRequest({
      householdId: household.id,
      accountIncomeId: incomeId,
      sourceDocumentId: document.id,
      sourceReference: reference,
      sourceAccountId: statementAccountId,
    });
    setConfirmedStatements((current) => new Set(current).add(key));
    setHasUnsavedChanges(false);
    onAdded(true);
  };

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
    editReceiptItemAllocations,
    purchaseDateFor,
    purchaseTimeFor,
    purchaseOccurredAtFor,
    editPurchaseDate,
    editPurchaseTime,
    suggestedCategoryId,
    receiptItemAmount,
    receiptTotal,
    paymentDraftFor,
    editItem,
    addReceiptItem,
    deleteReceiptItem,
    canUndoReceiptItemDeletion: receiptItemDeletionUndo?.documentId === selectedDocumentId,
    undoReceiptItemDeletion,
    editMerchant,
    resetConfirmation,
    confirm,
    confirmStatement,
    confirmStatementRefund,
    linkStatementRefundCorrection,
    confirmStatementTransfer,
    confirmStatementLoan: (
      document: DocumentReview,
      entry: StatementEntry,
      accountId: string,
      fromMemberId: string,
      toMemberId: string,
      loanId?: string,
    ) => confirmStatementLoan(document, entry, accountId, fromMemberId, toMemberId, loanId),
    linkStatementTransfer: (
      document: DocumentReview,
      entry: StatementEntry,
      accountId: string,
      transferId: string,
    ) => linkStatementTransfer(document, entry, accountId, transferId),
    linkStatementAccountMovement: (
      document: DocumentReview,
      entry: StatementEntry,
      accountId: string,
      movementId: string,
    ) => linkStatementAccountMovement(document, entry, accountId, movementId),
    linkStatementAccountIncome: (
      document: DocumentReview,
      entry: StatementEntry,
      accountId: string,
      incomeId: string,
    ) => linkStatementAccountIncome(document, entry, accountId, incomeId),
  };
}
