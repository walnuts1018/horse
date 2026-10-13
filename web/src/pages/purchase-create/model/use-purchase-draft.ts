import { useCallback, useEffect, useRef, useState } from "react";
import {
  deletePurchaseDraft,
  loadPurchaseDraft,
  savePurchaseDraft,
  type PurchaseDraft,
} from "../../../shared/lib";

export function usePurchaseDraft({
  householdId,
  draft,
  restore,
  onLoadError,
  onUnsavedChanges,
}: {
  householdId: string;
  draft: PurchaseDraft;
  restore: (draft: PurchaseDraft) => void;
  onLoadError: (error: Error) => void;
  onUnsavedChanges: (unsaved: boolean) => void;
}) {
  const [storageReady, setStorageReady] = useState(false);
  const [draftHouseholdId, setDraftHouseholdId] = useState<string | null>(null);
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
  draftRef.current = draft;
  restoreRef.current = restore;
  onLoadErrorRef.current = onLoadError;
  onUnsavedChangesRef.current = onUnsavedChanges;

  const markUnsaved = useCallback(() => {
    editVersion.current += 1;
    hasLocalDraft.current = true;
    onUnsavedChangesRef.current(true);
    setHasUnsavedChanges(true);
  }, []);

  const persistDraft = useCallback(async () => {
    const current = draftRef.current;
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
      if (hasContent) await savePurchaseDraft(householdId, current);
      else await deletePurchaseDraft(householdId);
      setSavedAt(new Date());
      if (editVersion.current === version) {
        onUnsavedChangesRef.current(false);
        setHasUnsavedChanges(false);
      }
    } catch (reason) {
      if (editVersion.current === version) {
        onUnsavedChangesRef.current(true);
        setSaveError((reason as Error).message);
        setHasUnsavedChanges(true);
      }
    }
  }, [householdId]);

  useEffect(() => {
    let active = true;
    setStorageReady(false);
    setDraftHouseholdId(null);
    void loadPurchaseDraft(householdId)
      .then((loaded) => {
        if (!active) return;
        if (loaded) {
          hasLocalDraft.current = true;
          restoreRef.current(loaded);
          setSavedAt(new Date());
        }
        setDraftHouseholdId(householdId);
      })
      .catch((reason) => active && onLoadErrorRef.current(reason as Error))
      .finally(() => active && setStorageReady(true));
    return () => {
      active = false;
    };
  }, [householdId]);

  useEffect(() => {
    if (!storageReady || draftHouseholdId !== householdId) return;
    if (skipNextSave.current) {
      skipNextSave.current = false;
      return;
    }
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => void persistDraft(), 450);
    return () => {
      if (timer.current !== null) window.clearTimeout(timer.current);
    };
  }, [householdId, storageReady, draftHouseholdId, draft, persistDraft]);

  useEffect(
    () => () => {
      if (timer.current !== null) window.clearTimeout(timer.current);
      if (storageReady && draftHouseholdId === householdId) {
        const current = draftRef.current;
        if (
          hasLocalDraft.current ||
          current.merchant.trim() ||
          current.itemName.trim() ||
          current.itemAmount ||
          current.items.length
        )
          void savePurchaseDraft(householdId, current);
        else void deletePurchaseDraft(householdId);
      }
    },
    [householdId, storageReady, draftHouseholdId],
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
