import type { Dispatch, RefObject, SetStateAction } from "react";

import type { DocumentReview, ExtractedItem, ReceiptConfirmationState } from "./imports";

import { purchaseDatePrecisionFor, receiptDateInputValue, receiptOccurredAt } from "./imports";
import {
  addManualReceiptItem as appendManualReceiptItem,
  deleteReceiptItemByKey,
  ensureReceiptItemKeys,
  rekeyReceiptConfirmationAfterStructureChange,
  rekeyReceiptItemValues,
  restoreReceiptItemDeletion,
  type ReceiptItemDeletionUndo,
} from "./receipt-review-draft";

export type ReceiptItemAllocation = { poolId: string; weight: number };
type ReceiptConfirmationStateUpdater = (
  documentId: string,
  update: (state: ReceiptConfirmationState, document: DocumentReview) => ReceiptConfirmationState,
) => void;

type ReceiptEditingOptions = {
  selectedDocument: DocumentReview | undefined;
  setDocuments: Dispatch<SetStateAction<DocumentReview[]>>;
  editedReceiptDocuments: RefObject<Set<string>>;
  markUnsaved: () => void;
  updateReceiptConfirmationState: ReceiptConfirmationStateUpdater;
  reviewTargetIndex: number;
  setReviewTargetIndex: Dispatch<SetStateAction<number>>;
  receiptItemDeletionUndo: ReceiptItemDeletionUndo | undefined;
  setReceiptItemDeletionUndo: Dispatch<SetStateAction<ReceiptItemDeletionUndo | undefined>>;
  receiptItemsChanged: (document?: DocumentReview) => boolean;
};

export function useReceiptEditingWorkflow(options: ReceiptEditingOptions) {
  const {
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
  } = options;
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
        const purchaseTime = document.receiptConfirmationState?.purchaseTime;
        const purchasedAt = receiptOccurredAt(
          document.result.receipt?.purchasedAt,
          date,
          purchaseTime || undefined,
          purchaseDatePrecisionFor(document),
        );
        return purchasedAt
          ? {
              ...document,
              result: {
                ...document.result,
                receipt: {
                  ...document.result.receipt,
                  purchasedAt,
                  datePrecision: purchaseDatePrecisionFor(document),
                },
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
          time || undefined,
          time ? "DATE_TIME" : "DATE_ONLY",
        );
        return purchasedAt
          ? {
              ...document,
              result: {
                ...document.result,
                receipt: {
                  ...document.result.receipt,
                  purchasedAt,
                  datePrecision: time ? "DATE_TIME" : "DATE_ONLY",
                },
              },
            }
          : document;
      }),
    );
  };
  return {
    editItem,
    addReceiptItem,
    deleteReceiptItem,
    undoReceiptItemDeletion,
    editReceiptItemAllocations,
    editMerchant,
    editPurchaseDate,
    editPurchaseTime,
  };
}
