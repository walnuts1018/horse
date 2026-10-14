import { useCallback, useEffect, useRef, useState } from "react";

import {
  deletePurchaseDraft,
  errorMessage,
  loadPurchaseDraft,
  savePurchaseDraft,
  toError,
  type PurchaseDraft,
} from "../../../shared/lib";

export function usePurchaseDraft({
  memberId,
  householdId,
  draft,
  restore,
  onLoadError,
  onUnsavedChanges,
}: {
  memberId: string;
  householdId: string;
  draft: PurchaseDraft;
  restore: (draft: PurchaseDraft) => void;
  onLoadError: (error: Error) => void;
  onUnsavedChanges: (unsaved: boolean) => void;
}) {
  const [storageReady, setStorageReady] = useState(false);
  const [draftScope, setDraftScope] = useState<string | null>(null);
  const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false);
  const [savedAt, setSavedAt] = useState<Date | null>(null);
  const [saveError, setSaveError] = useState("");
  const draftRef = useRef(draft);
  const restoreRef = useRef(restore);
  const onLoadErrorRef = useRef(onLoadError);
  const onUnsavedChangesRef = useRef(onUnsavedChanges);
  const timer = useRef<number | null>(null);
  const skipNextSave = useRef(false);
  const hasLocalDraft = useRef(false);
  const editVersion = useRef(0);
  const scope = JSON.stringify([memberId, householdId]);

  // refはレンダー中に読み書きすると再レンダーで巻き戻るため、コミット後に最新値を反映する
  useEffect(() => {
    draftRef.current = draft;
    restoreRef.current = restore;
    onLoadErrorRef.current = onLoadError;
    onUnsavedChangesRef.current = onUnsavedChanges;
  });

  const markUnsaved = useCallback(() => {
    editVersion.current += 1;
    hasLocalDraft.current = true;
    onUnsavedChangesRef.current(true);
    setHasUnsavedChanges(true);
  }, []);

  const persistDraft = useCallback(async () => {
    const current = draft;
    const version = editVersion.current;
    const hasContent =
      hasLocalDraft.current ||
      Boolean(
        current.merchant.trim() ||
        current.itemName.trim() ||
        current.itemAmount ||
        current.items.length,
      );
    setSaveError("");
    try {
      if (hasContent) await savePurchaseDraft(memberId, householdId, current);
      else await deletePurchaseDraft(memberId, householdId);
      setSavedAt(new Date());
      if (editVersion.current === version) {
        onUnsavedChangesRef.current(false);
        setHasUnsavedChanges(false);
      }
    } catch (reason) {
      if (editVersion.current === version) {
        onUnsavedChangesRef.current(true);
        setSaveError(errorMessage(reason));
        setHasUnsavedChanges(true);
      }
    }
  }, [memberId, householdId, draft]);

  useEffect(() => {
    let active = true;
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
      .finally(() => active && setStorageReady(true));
    return () => {
      active = false;
    };
  }, [memberId, householdId, scope]);

  useEffect(() => {
    if (!storageReady || draftScope !== scope) return;
    if (skipNextSave.current) {
      skipNextSave.current = false;
      return;
    }
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => void persistDraft(), 450);
    return () => {
      if (timer.current !== null) window.clearTimeout(timer.current);
    };
  }, [scope, storageReady, draftScope, persistDraft]);

  useEffect(
    () => () => {
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
          void savePurchaseDraft(memberId, householdId, current);
        else void deletePurchaseDraft(memberId, householdId);
      }
    },
    [memberId, householdId, storageReady, draftScope, scope],
  );

  const queued = useCallback(() => {
    skipNextSave.current = true;
    hasLocalDraft.current = false;
    setSavedAt(null);
    onUnsavedChangesRef.current(false);
    setHasUnsavedChanges(false);
  }, []);

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
