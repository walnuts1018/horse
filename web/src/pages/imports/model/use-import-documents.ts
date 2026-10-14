import { useInfiniteQuery, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";

import { errorMessage } from "../../../shared/lib/errors";
import {
  deleteImportReceiptDraft,
  loadImportReceiptDraft,
  saveImportReceiptDraft,
  type ImportReceiptDraft,
} from "../../../shared/lib/purchase-storage";
import { documentListURL } from "../api/documents";
import {
  asRecord,
  isProcessing,
  sourceKindLabel,
  withStableEvidenceKeys,
  type DocumentFilters,
  type DocumentReview,
} from "./imports";
import {
  createImportReceiptDraft,
  reconcileReceiptConfirmationState,
  reconcileReceiptReviewDraft,
} from "./receipt-review-draft";

type DocumentPage = {
  documents: DocumentReview[];
  nextCursor: string | null;
};

type DocumentQueryKey = readonly ["import-documents", string, string];

function isDocumentPage(value: unknown): value is DocumentPage {
  const record = asRecord(value);
  return (
    record !== undefined &&
    Array.isArray(record.documents) &&
    (record.nextCursor === null || typeof record.nextCursor === "string")
  );
}

function toDocumentPage(value: unknown): DocumentPage {
  return isDocumentPage(value) ? value : { documents: [], nextCursor: null };
}

function withDocumentName(document: DocumentReview, fallback?: DocumentReview) {
  const keyed = withStableEvidenceKeys(document);
  const createdAt = document.createdAt
    ? new Date(document.createdAt).toLocaleString("ja-JP", {
        month: "numeric",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      })
    : document.id.slice(0, 8);
  return {
    ...keyed,
    name:
      document.name ?? fallback?.name ?? `${sourceKindLabel(document.sourceKind)} · ${createdAt}`,
  };
}

function flattenPages(data: InfiniteData<DocumentPage> | undefined) {
  const seen = new Set<string>();
  return (data?.pages ?? []).flatMap((page) =>
    page.documents.filter((document) => {
      if (seen.has(document.id)) return false;
      seen.add(document.id);
      return true;
    }),
  );
}

async function fetchDocumentPage(
  householdId: string,
  filters: DocumentFilters,
  cursor: string | null,
  signal: AbortSignal,
) {
  const response = await fetch(documentListURL(householdId, filters, cursor ?? undefined), {
    credentials: "include",
    signal,
  });
  if (!response.ok) throw new Error(`証拠一覧を取得できませんでした (${response.status})`);
  return toDocumentPage(await response.json());
}

function hasProcessingDocuments(data: InfiniteData<DocumentPage> | undefined) {
  return flattenPages(data).some((document) => isProcessing(document.status));
}

export function useImportDocuments(
  householdId: string,
  filters: DocumentFilters,
  onError: (message: string) => void,
) {
  const queryClient = useQueryClient();
  const [selected, setSelected] = useState<string | null>(null);
  const editedReceiptDocuments = useRef(new Set<string>());
  const receiptDrafts = useRef(new Map<string, ImportReceiptDraft>());
  const notifiedDraftConflicts = useRef(new Set<string>());
  const notifiedError = useRef<unknown>(null);
  const filterScope = JSON.stringify(filters);
  const queryKey = useMemo<DocumentQueryKey>(
    () => ["import-documents", householdId, filterScope],
    [filterScope, householdId],
  );
  const query = useInfiniteQuery({
    queryKey,
    initialPageParam: null as string | null,
    queryFn: async ({ pageParam, signal }) => {
      const page = await fetchDocumentPage(householdId, filters, pageParam, signal);
      const cached = queryClient.getQueryData<InfiniteData<DocumentPage>>(queryKey);
      const cachedPageIndex = cached?.pageParams.findIndex((param) => param === pageParam) ?? -1;
      const cachedDocuments = new Map(
        (cached?.pages[cachedPageIndex]?.documents ?? []).map((document) => [
          document.id,
          document,
        ]),
      );
      const documents = await Promise.all(
        page.documents.map(async (document) => {
          const existing = cachedDocuments.get(document.id);
          let draft = receiptDrafts.current.get(document.id);
          if (!draft) {
            draft = await loadImportReceiptDraft(householdId, document.id);
            if (draft) receiptDrafts.current.set(document.id, draft);
          }
          if (draft) {
            editedReceiptDocuments.current.add(document.id);
            if (!document.result) {
              return withDocumentName(
                {
                  ...document,
                  result: draft.editedResult,
                  receiptConfirmationState: draft.confirmationState,
                },
                existing,
              );
            }
            const reconciled = reconcileReceiptReviewDraft(document, draft);
            const confirmation = reconcileReceiptConfirmationState(
              draft.baseResult,
              document.result,
              draft.confirmationState,
            );
            const conflicts = [...reconciled.conflicts, ...confirmation.conflicts];
            if (conflicts.length && !notifiedDraftConflicts.current.has(document.id)) {
              notifiedDraftConflicts.current.add(document.id);
              onError(
                `${document.name ?? "レシート"}のAI解析結果が更新され、${conflicts.join("・")}の修正を自動復元できませんでした。品目を確認してください。`,
              );
            }
            const rebasedDraft = createImportReceiptDraft(
              householdId,
              document.id,
              document.result,
              reconciled.document.result,
              confirmation.state,
            );
            if (rebasedDraft) {
              receiptDrafts.current.set(document.id, rebasedDraft);
              await saveImportReceiptDraft(rebasedDraft);
            } else {
              receiptDrafts.current.delete(document.id);
              await deleteImportReceiptDraft(householdId, document.id);
            }
            return withDocumentName(
              { ...reconciled.document, receiptConfirmationState: confirmation.state },
              existing,
            );
          }
          return {
            ...withDocumentName(document, existing),
            ...(existing && editedReceiptDocuments.current.has(document.id)
              ? { result: existing.result }
              : {}),
          };
        }),
      );
      return {
        ...page,
        documents,
      };
    },
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    refetchInterval: (currentQuery) =>
      hasProcessingDocuments(currentQuery.state.data) ? 1200 : false,
  });
  const { fetchNextPage, hasNextPage, isFetchingNextPage, refetch } = query;
  const documents = flattenPages(query.data);
  const householdScope = `${householdId}:${filterScope}`;
  const filterScopeRef = useRef(householdScope);

  useEffect(() => {
    if (filterScopeRef.current === householdScope) return;
    filterScopeRef.current = householdScope;
    editedReceiptDocuments.current.clear();
    notifiedDraftConflicts.current.clear();
    setSelected(null);
  }, [householdScope]);

  useEffect(() => {
    if (!selected && documents.length > 0) setSelected(documents[0].id);
  }, [documents, selected]);

  useEffect(() => {
    if (!query.error || query.data || notifiedError.current === query.error) return;
    notifiedError.current = query.error;
    onError(errorMessage(query.error));
  }, [onError, query.data, query.error]);

  const clearReceiptDraft = useCallback(
    async (documentId: string) => {
      editedReceiptDocuments.current.delete(documentId);
      receiptDrafts.current.delete(documentId);
      await deleteImportReceiptDraft(householdId, documentId);
    },
    [householdId],
  );

  const setDocuments: Dispatch<SetStateAction<DocumentReview[]>> = useCallback(
    (action) => {
      const changedDrafts: ImportReceiptDraft[] = [];
      const removedDocumentIDs: string[] = [];
      queryClient.setQueryData<InfiniteData<DocumentPage>>(queryKey, (current) => {
        if (!current) return current;
        const currentDocuments = flattenPages(current);
        const nextDocuments = typeof action === "function" ? action(currentDocuments) : action;
        const nextByID = new Map(nextDocuments.map((document) => [document.id, document]));
        const existingIDs = new Set(currentDocuments.map((document) => document.id));
        removedDocumentIDs.push(
          ...currentDocuments
            .filter((document) => !nextByID.has(document.id))
            .map((document) => document.id),
        );
        for (const document of currentDocuments) {
          const next = nextByID.get(document.id);
          if (!next) continue;
          const resultChanged =
            editedReceiptDocuments.current.has(document.id) &&
            !!document.result &&
            !!next.result &&
            JSON.stringify(document.result) !== JSON.stringify(next.result);
          const confirmationChanged =
            JSON.stringify(document.receiptConfirmationState) !==
            JSON.stringify(next.receiptConfirmationState);
          if (!resultChanged && !confirmationChanged) continue;
          const baseResult = receiptDrafts.current.get(document.id)?.baseResult ?? document.result;
          const editedResult = next.result ?? document.result;
          if (!baseResult || !editedResult) continue;
          const draft = createImportReceiptDraft(
            householdId,
            document.id,
            baseResult,
            editedResult,
            next.receiptConfirmationState,
          );
          if (draft) {
            receiptDrafts.current.set(document.id, draft);
            changedDrafts.push(draft);
          }
        }
        const added = nextDocuments.filter((document) => !existingIDs.has(document.id));
        return {
          ...current,
          pages: current.pages.map((page, index) => ({
            ...page,
            documents: [
              ...(index === 0 ? added : []),
              ...page.documents.flatMap((document) => {
                const next = nextByID.get(document.id);
                return next ? [next] : [];
              }),
            ],
          })),
        };
      });
      for (const draft of changedDrafts) {
        void saveImportReceiptDraft(draft).catch((reason: unknown) => {
          onError(errorMessage(reason));
        });
      }
      for (const documentId of removedDocumentIDs) {
        void clearReceiptDraft(documentId).catch((reason: unknown) => {
          onError(errorMessage(reason));
        });
      }
    },
    [clearReceiptDraft, householdId, onError, queryClient, queryKey],
  );

  const refreshDocuments = useCallback(
    async (silent = false) => {
      const result = await refetch();
      if (result.error && !silent && notifiedError.current !== result.error) {
        notifiedError.current = result.error;
        onError(errorMessage(result.error));
      }
    },
    [onError, refetch],
  );

  const loadOlderDocuments = useCallback(async () => {
    if (!hasNextPage || isFetchingNextPage) return;
    const result = await fetchNextPage();
    if (result.error) onError(errorMessage(result.error));
  }, [fetchNextPage, hasNextPage, isFetchingNextPage, onError]);

  const data = query.data;
  const nextDocumentCursor = data?.pages.at(-1)?.nextCursor ?? null;

  return {
    documents: documents.map((document) => withDocumentName(document)),
    setDocuments,
    selected,
    setSelected,
    nextDocumentCursor,
    loadingDocuments: query.isFetching,
    documentLoadFailed: query.isError,
    loadingOlderDocuments: query.isFetchingNextPage,
    editedReceiptDocuments,
    clearReceiptDraft,
    refreshDocuments,
    loadOlderDocuments,
  };
}
