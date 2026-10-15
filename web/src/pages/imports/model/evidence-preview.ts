import { useCallback, useMemo, useState, useSyncExternalStore } from "react";

import type { PreviewTransform } from "./evidence-geometry";
import type { DocumentReview } from "./imports";

import { identityTransform } from "./evidence-geometry";
import {
  getReceiptEvidenceSelection,
  receiptEvidenceSelectionIndex,
  selectReceiptEvidenceItem,
  subscribeReceiptEvidenceSelection,
} from "./receipt-evidence-selection";
import { ensureReceiptItemKeys } from "./receipt-review-draft";

export type EvidencePreview = {
  documentId: string;
  page: number;
  src: string;
  transform: PreviewTransform;
};

export function useEvidencePreview(document: DocumentReview | undefined) {
  const [pageState, setPageState] = useState<{ documentId: string; page: number }>({
    documentId: "",
    page: 0,
  });
  const documentId = document?.id ?? "";
  const isImage = Boolean(document?.mimeType?.startsWith("image/"));
  const receiptItems = document?.result?.receipt?.items;
  const selection = useSyncExternalStore(
    (listener) => subscribeReceiptEvidenceSelection(documentId, listener),
    () => getReceiptEvidenceSelection(documentId),
    () => null,
  );
  const keyedReceiptItems = useMemo(
    () => ensureReceiptItemKeys(receiptItems ?? []),
    [receiptItems],
  );
  const receiptEvidenceIndex = String(
    receiptEvidenceSelectionIndex(
      selection,
      keyedReceiptItems.map((item, index) => item.key ?? `item-${index}`),
      selection?.index ?? 0,
    ),
  );
  const page = isImage ? 0 : pageState.documentId === documentId ? pageState.page : 0;
  const preview: EvidencePreview | null =
    isImage && documentId
      ? {
          documentId,
          page: 0,
          src: `/documents/${encodeURIComponent(documentId)}/content`,
          transform: identityTransform,
        }
      : null;

  const selectPage = useCallback(
    (requestedPage: number) => {
      const nextPage = isImage ? 0 : requestedPage;
      setPageState((current) =>
        current.documentId === documentId && current.page === nextPage
          ? current
          : { documentId, page: nextPage },
      );
    },
    [documentId, isImage],
  );

  const setReceiptEvidenceIndex = (index: string) => {
    const itemIndex = Number(index);
    const item = keyedReceiptItems.at(itemIndex);
    if (item) {
      selectReceiptEvidenceItem(documentId, {
        itemKey: item.key ?? `item-${itemIndex}`,
        index: itemIndex,
        focusEditor: false,
      });
    }
  };

  return {
    page,
    preview,
    selectPage,
    receiptEvidenceIndex,
    setReceiptEvidenceIndex,
  };
}

export type EvidencePreviewController = ReturnType<typeof useEvidencePreview>;
