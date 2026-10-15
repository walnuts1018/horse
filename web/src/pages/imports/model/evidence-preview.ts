import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from "react";

import type { PreviewTransform } from "./evidence-geometry";
import type { DocumentReview } from "./imports";

import { errorMessage } from "../../../shared/lib/errors";
import { readDocumentPreview } from "../api/document-preview";
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

type PreviewObjectURL = {
  key: string | null;
  src: string;
};

export function useEvidencePreview(document: DocumentReview | undefined) {
  const [pageState, setPageState] = useState<{ documentId: string; page: number }>({
    documentId: "",
    page: 0,
  });
  const [pageDraftState, setPageDraftState] = useState<{ documentId: string; draft: string }>({
    documentId: "",
    draft: "1",
  });
  const [objectURL, setObjectURL] = useState<PreviewObjectURL>({ key: null, src: "" });
  const [pageError, setPageError] = useState<{ key: string | null; message: string }>({
    key: null,
    message: "",
  });

  const documentId = document?.id ?? "";
  const isImage = Boolean(document?.mimeType?.startsWith("image/"));
  const previewable = Boolean(
    document?.mimeType && (isImage || document.mimeType === "application/pdf"),
  );
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
  const page = pageState.documentId === documentId ? pageState.page : 0;
  const pageDraft = pageDraftState.documentId === documentId ? pageDraftState.draft : "1";
  const requestKey = previewable && documentId ? `${documentId}:${page}` : null;
  const query = useQuery({
    queryKey: ["import-document-preview", documentId, page],
    queryFn: ({ signal }) => readDocumentPreview(documentId, page, signal),
    enabled: requestKey !== null,
    gcTime: 0,
  });
  const previewData = query.data && objectURL.key === requestKey ? query.data : null;
  const preview: EvidencePreview | null = previewData
    ? { documentId, page, src: objectURL.src, transform: previewData.transform }
    : isImage && documentId
      ? {
          documentId,
          page,
          src: `/documents/${encodeURIComponent(documentId)}/content`,
          transform: identityTransform,
        }
      : null;
  const error =
    (pageError.key === requestKey ? pageError.message : "") ||
    (query.error ? errorMessage(query.error) : "");
  const loading = requestKey !== null && query.isPending && !query.error;

  const selectPage = useCallback(
    (requestedPage: number) => {
      const nextPage = isImage ? 0 : requestedPage;
      setPageError({ key: `${documentId}:${nextPage}`, message: "" });
      setPageState((current) =>
        current.documentId === documentId && current.page === nextPage
          ? current
          : { documentId, page: nextPage },
      );
    },
    [documentId, isImage],
  );
  const setPageDraft = (draft: string) => setPageDraftState({ documentId, draft });
  const setError = (message: string) => setPageError({ key: requestKey, message });

  useEffect(() => {
    if (!requestKey || !query.data) return;
    const src = URL.createObjectURL(query.data.blob);
    setObjectURL({ key: requestKey, src });
    return () => URL.revokeObjectURL(src);
  }, [query.data, requestKey]);

  const showPage = () => {
    const requestedPage = Number(pageDraft);
    if (!Number.isInteger(requestedPage) || requestedPage < 1) {
      setError("ページ番号を入力してください");
      return;
    }
    selectPage(requestedPage - 1);
  };

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
    pageDraft,
    setPageDraft,
    preview,
    loading,
    error,
    setError,
    showPage,
    selectPage,
    receiptEvidenceIndex,
    setReceiptEvidenceIndex,
  };
}

export type EvidencePreviewController = ReturnType<typeof useEvidencePreview>;
