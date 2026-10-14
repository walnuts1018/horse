import { useEffect, useState } from "react";

import type { PreviewTransform } from "./evidence-geometry";
import type { DocumentReview } from "./imports";

import { toError } from "../../../shared/lib/errors";
import { readDocumentPreview } from "../api/document-preview";

export type EvidencePreview = {
  documentId: string;
  page: number;
  src: string;
  transform: PreviewTransform;
};

// 取得結果は要求キー(document.id + ページ)と一致するときだけ参照し、それ以外は読み込み中として扱う
type PreviewRequest = {
  key: string | null;
  preview: EvidencePreview | null;
  loading: boolean;
  error: string;
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
  const [requestState, setRequestState] = useState<PreviewRequest>({
    key: null,
    preview: null,
    loading: false,
    error: "",
  });
  const [receiptEvidenceIndex, setReceiptEvidenceIndex] = useState("0");

  const documentId = document?.id ?? "";
  const page = pageState.documentId === documentId ? pageState.page : 0;
  const pageDraft = pageDraftState.documentId === documentId ? pageDraftState.draft : "1";
  const previewable = Boolean(
    document?.mimeType &&
    (document.mimeType.startsWith("image/") || document.mimeType === "application/pdf"),
  );
  const requestKey = previewable && documentId ? `${documentId}:${page}` : null;
  const synced = requestKey !== null && requestState.key === requestKey;
  const preview = synced ? requestState.preview : null;
  const loading = requestKey !== null && !synced;
  const error = synced ? requestState.error : "";

  const setPage = (requestedPage: number) => {
    setPageState({ documentId, page: requestedPage });
  };
  const setPageDraft = (draft: string) => {
    setPageDraftState({ documentId, draft });
  };
  const setError = (message: string) => {
    setRequestState((current) => ({
      key: requestKey,
      preview: current.key === requestKey ? current.preview : null,
      loading: requestKey !== null && current.key !== requestKey,
      error: message,
    }));
  };

  useEffect(() => {
    if (!document?.mimeType) return;
    if (!document.mimeType.startsWith("image/") && document.mimeType !== "application/pdf") {
      return;
    }
    const controller = new AbortController();
    const key = `${document.id}:${page}`;
    let active = true;
    let objectUrl = "";
    void readDocumentPreview(document.id, page, controller.signal)
      .then(({ blob, transform }) => {
        if (!active) return;
        objectUrl = URL.createObjectURL(blob);
        setRequestState({
          key,
          preview: { documentId: document.id, page, src: objectUrl, transform },
          loading: false,
          error: "",
        });
      })
      .catch((reason) => {
        if (!active) return;
        const caught = toError(reason);
        if (caught.name === "AbortError") return;
        setRequestState({ key, preview: null, loading: false, error: caught.message });
      });
    return () => {
      active = false;
      controller.abort();
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [document?.id, document?.mimeType, page]);

  const showPage = () => {
    const requestedPage = Number(pageDraft);
    if (!Number.isInteger(requestedPage) || requestedPage < 1) {
      setError("ページ番号を入力してください");
      return;
    }
    setPage(requestedPage - 1);
  };

  const selectPage = (requestedPage: number) => {
    setPage(requestedPage);
    setPageDraft(String(requestedPage + 1));
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
