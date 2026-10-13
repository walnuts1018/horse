import { useEffect, useState } from "react";
import { readDocumentPreview } from "../api/document-preview";
import type { DocumentReview } from "./imports";
import type { PreviewTransform } from "./evidence-geometry";

export type EvidencePreview = {
  documentId: string;
  page: number;
  src: string;
  transform: PreviewTransform;
};

export function useEvidencePreview(document: DocumentReview | undefined) {
  const [page, setPage] = useState(0);
  const [pageDraft, setPageDraft] = useState("1");
  const [preview, setPreview] = useState<EvidencePreview | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [receiptEvidenceIndex, setReceiptEvidenceIndex] = useState("0");

  useEffect(() => {
    setPage(0);
    setPageDraft("1");
  }, [document?.id]);

  useEffect(() => {
    if (
      !document?.mimeType ||
      (!document.mimeType.startsWith("image/") && document.mimeType !== "application/pdf")
    ) {
      setPreview(null);
      setError("");
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    let active = true;
    let objectUrl = "";
    setPreview(null);
    setLoading(true);
    setError("");
    void readDocumentPreview(document.id, page, controller.signal)
      .then(({ blob, transform }) => {
        if (!active) return;
        objectUrl = URL.createObjectURL(blob);
        setPreview({ documentId: document.id, page, src: objectUrl, transform });
      })
      .catch((reason) => {
        if (active && (reason as Error).name !== "AbortError") setError((reason as Error).message);
      })
      .finally(() => active && setLoading(false));
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
