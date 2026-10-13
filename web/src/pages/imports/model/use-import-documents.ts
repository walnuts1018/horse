import { useCallback, useEffect, useRef, useState } from "react";
import { documentListURL } from "../api/documents";
import {
  isProcessing,
  sourceKindLabel,
  type DocumentFilters,
  type DocumentReview,
} from "./imports";

type DocumentPage = {
  documents: DocumentReview[];
  nextCursor: string | null;
};

function withDocumentName(document: DocumentReview, fallback?: DocumentReview) {
  const createdAt = document.createdAt
    ? new Date(document.createdAt).toLocaleString("ja-JP", {
        month: "numeric",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      })
    : document.id.slice(0, 8);
  return {
    ...document,
    name:
      document.name ?? fallback?.name ?? `${sourceKindLabel(document.sourceKind)} · ${createdAt}`,
  };
}

export function useImportDocuments(
  householdId: string,
  filters: DocumentFilters,
  onError: (message: string) => void,
) {
  const filterScope = JSON.stringify(filters);
  const filterScopeRef = useRef(filterScope);
  const [documents, setDocuments] = useState<DocumentReview[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [nextDocumentCursor, setNextDocumentCursor] = useState<string | null>(null);
  const [loadingOlderDocuments, setLoadingOlderDocuments] = useState(false);
  const loadedOlderPages = useRef(false);
  const editedReceiptDocuments = useRef(new Set<string>());

  const refreshDocuments = useCallback(
    async (silent = false) => {
      try {
        const requestScope = filterScope;
        const response = await fetch(documentListURL(householdId, filters), {
          credentials: "include",
        });
        if (!response.ok) throw new Error(`証拠一覧を取得できませんでした (${response.status})`);
        const payload = (await response.json()) as DocumentPage;
        if (requestScope !== filterScopeRef.current) return;
        setDocuments((current) => [
          ...payload.documents.map((document) => {
            const existing = current.find((entry) => entry.id === document.id);
            return {
              ...withDocumentName(document, existing),
              ...(existing && editedReceiptDocuments.current.has(document.id)
                ? { result: existing.result }
                : {}),
            };
          }),
          ...current.filter(
            (entry) => !payload.documents.some((document) => document.id === entry.id),
          ),
        ]);
        if (!loadedOlderPages.current) setNextDocumentCursor(payload.nextCursor);
        setSelected((current) => current ?? payload.documents[0]?.id ?? null);
      } catch (reason) {
        if (!silent) onError((reason as Error).message);
      }
    },
    [filterScope, filters, householdId, onError],
  );

  useEffect(() => {
    filterScopeRef.current = filterScope;
    setDocuments([]);
    setNextDocumentCursor(null);
    loadedOlderPages.current = false;
    editedReceiptDocuments.current.clear();
    setSelected(null);
    void refreshDocuments();
  }, [filterScope, refreshDocuments]);

  const loadOlderDocuments = useCallback(async () => {
    if (!nextDocumentCursor || loadingOlderDocuments) return;
    setLoadingOlderDocuments(true);
    const requestScope = filterScope;
    try {
      const response = await fetch(documentListURL(householdId, filters, nextDocumentCursor), {
        credentials: "include",
      });
      if (!response.ok) throw new Error(`証拠一覧を取得できませんでした (${response.status})`);
      const payload = (await response.json()) as DocumentPage;
      if (requestScope !== filterScopeRef.current) return;
      setDocuments((current) => {
        const ids = new Set(current.map((document) => document.id));
        return [
          ...current,
          ...payload.documents
            .filter((document) => !ids.has(document.id))
            .map((document) => withDocumentName(document)),
        ];
      });
      setNextDocumentCursor(payload.nextCursor);
      loadedOlderPages.current = true;
    } catch (reason) {
      onError((reason as Error).message);
    } finally {
      setLoadingOlderDocuments(false);
    }
  }, [filterScope, filters, householdId, loadingOlderDocuments, nextDocumentCursor, onError]);

  const hasProcessingDocuments = documents.some((document) => isProcessing(document.status));
  useEffect(() => {
    if (!hasProcessingDocuments) return;
    const interval = window.setInterval(() => void refreshDocuments(true), 1200);
    return () => window.clearInterval(interval);
  }, [hasProcessingDocuments, refreshDocuments]);

  return {
    documents,
    setDocuments,
    selected,
    setSelected,
    nextDocumentCursor,
    loadingOlderDocuments,
    editedReceiptDocuments,
    refreshDocuments,
    loadOlderDocuments,
  };
}
