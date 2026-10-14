import { useCallback, useEffect, useRef, useState } from "react";

import {
  deletePurchaseDraft,
  errorMessage,
  loadPurchaseDraft,
  savePurchaseDraft,
  toError,
  type PurchaseDraft,
} from "../../../shared/lib";

export function createPurchaseDraftWriteQueue() {
  let tail = Promise.resolve();

  return (write: () => Promise<void>) => {
    const pending = tail.then(write);
    tail = pending.catch(() => {});
    return pending;
  };
}

export function usePurchaseDraft({
  memberId,
  householdId,
  draft,
  restore,
  onLoadError,
  onUnsavedChanges,
  mode = "local",
  initialDraft,
}: {
  memberId: string;
  householdId: string;
  draft: PurchaseDraft;
  restore: (draft: PurchaseDraft) => void;
  onLoadError: (error: Error) => void;
  onUnsavedChanges: (unsaved: boolean) => void;
  mode?: "local" | "shared";
  initialDraft?: PurchaseDraft;
}) {
  const [storageReady, setStorageReady] = useState(false);
  const [draftScope, setDraftScope] = useState<string | null>(null);
  const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false);
  const [savedAt, setSavedAt] = useState<Date | null>(null);
  const [saveError, setSaveError] = useState("");
  const scope = JSON.stringify([memberId, householdId]);
  const draftRef = useRef(draft);
  const restoreRef = useRef(restore);
  const onLoadErrorRef = useRef(onLoadError);
  const onUnsavedChangesRef = useRef(onUnsavedChanges);
  const timer = useRef<number | null>(null);
  const writeDraft = useRef(createPurchaseDraftWriteQueue());
  const skipNextSave = useRef(false);
  const hasLocalDraft = useRef(false);
  const initializedSharedScope = useRef<string | null>(null);
  const editVersion = useRef(0);
  const mounted = useRef(false);
  const scopeRef = useRef(scope);

  // refはレンダー中に読み書きすると再レンダーで巻き戻るため、コミット後に最新値を反映する
  useEffect(() => {
    draftRef.current = draft;
    scopeRef.current = scope;
    restoreRef.current = restore;
    onLoadErrorRef.current = onLoadError;
    onUnsavedChangesRef.current = onUnsavedChanges;
  });

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const markUnsaved = useCallback(() => {
    editVersion.current += 1;
    hasLocalDraft.current = true;
    onUnsavedChangesRef.current(true);
    setHasUnsavedChanges(true);
  }, []);

  const persistDraft = useCallback(
    async (current = draftRef.current) => {
      if (mode === "shared") return;
      const version = editVersion.current;
      const hasContent =
        hasLocalDraft.current ||
        Boolean(
          current.merchant.trim() ||
          current.itemName.trim() ||
          current.itemAmount ||
          current.items.length,
        );
      if (mounted.current && scopeRef.current === scope) setSaveError("");
      try {
        await writeDraft.current(() =>
          hasContent
            ? savePurchaseDraft(memberId, householdId, current)
            : deletePurchaseDraft(memberId, householdId),
        );
        if (mounted.current && scopeRef.current === scope && editVersion.current === version) {
          setSavedAt(new Date());
          onUnsavedChangesRef.current(false);
          setHasUnsavedChanges(false);
        }
      } catch (reason) {
        if (mounted.current && scopeRef.current === scope && editVersion.current === version) {
          onUnsavedChangesRef.current(true);
          setSaveError(errorMessage(reason));
          setHasUnsavedChanges(true);
        }
      }
    },
    [memberId, householdId, mode, scope],
  );

  useEffect(() => {
    if (mode !== "shared" || !initialDraft || initializedSharedScope.current === scope) return;
    initializedSharedScope.current = scope;
    restoreRef.current(initialDraft);
    setSavedAt(new Date());
    setDraftScope(scope);
    setStorageReady(true);
  }, [mode, initialDraft, scope]);

  useEffect(() => {
    if (mode === "shared") return;
    let active = true;
    queueMicrotask(() => {
      if (!active) return;
      setStorageReady(false);
      setDraftScope(null);
      void loadPurchaseDraft(memberId, householdId)
        .then((loaded) => {
          if (!active) return;
          if (loaded) {
            hasLocalDraft.current = true;
            restoreRef.current(loaded);
            setSavedAt(new Date());
          }
          setDraftScope(scope);
        })
        .catch((reason) => active && onLoadErrorRef.current(toError(reason)))
        .finally(() => {
          if (active) setStorageReady(true);
        });
    });
    return () => {
      active = false;
    };
  }, [memberId, householdId, mode, scope]);

  useEffect(() => {
    if (mode === "shared") return;
    if (!storageReady || draftScope !== scope) return;
    if (skipNextSave.current) {
      skipNextSave.current = false;
      return;
    }
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      timer.current = null;
      void persistDraft(draft);
    }, 450);
    return () => {
      if (timer.current !== null) window.clearTimeout(timer.current);
    };
  }, [mode, scope, storageReady, draftScope, draft, persistDraft]);

  useEffect(
    () => () => {
      if (mode === "shared") return;
      if (timer.current !== null) window.clearTimeout(timer.current);
      if (storageReady && draftScope === scope) {
        const current = draftRef.current;
        if (
          hasLocalDraft.current ||
          current.merchant.trim() ||
          current.itemName.trim() ||
          current.itemAmount ||
          current.items.length
        )
          void writeDraft
            .current(() => savePurchaseDraft(memberId, householdId, current))
            .catch(() => {});
        else
          void writeDraft.current(() => deletePurchaseDraft(memberId, householdId)).catch(() => {});
      }
    },
    [memberId, householdId, mode, storageReady, draftScope, scope],
  );

  const queued = useCallback(() => {
    if (mode === "shared") return;
    editVersion.current += 1;
    skipNextSave.current = true;
    hasLocalDraft.current = false;
    void writeDraft
      .current(() => deletePurchaseDraft(memberId, householdId))
      .catch((reason) => {
        if (!mounted.current) return;
        onUnsavedChangesRef.current(true);
        setSaveError(errorMessage(reason));
        setHasUnsavedChanges(true);
      });
    setSavedAt(null);
    onUnsavedChangesRef.current(false);
    setHasUnsavedChanges(false);
  }, [memberId, householdId, mode]);

  const cancelScheduledSave = useCallback(() => {
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = null;
  }, []);

  return {
    storageReady,
    hasUnsavedChanges,
    savedAt,
    saveError,
    markUnsaved,
    persistDraft,
    queued,
    cancelScheduledSave,
  };
}
