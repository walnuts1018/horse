import { useCallback, useEffect, useState, type ChangeEvent } from "react";

export function useSettingsChangeTracking(onUnsavedChanges: (unsaved: boolean) => void) {
  const [dirtyForms, setDirtyForms] = useState<Set<string>>(() => new Set());
  const [pendingOperations, setPendingOperations] = useState<Set<string>>(() => new Set());

  useEffect(() => {
    onUnsavedChanges(dirtyForms.size > 0 || pendingOperations.size > 0);
    return () => onUnsavedChanges(false);
  }, [dirtyForms, onUnsavedChanges, pendingOperations]);

  const markFormDirty = useCallback((formID: string) => {
    setDirtyForms((current) => {
      if (current.has(formID)) return current;
      const next = new Set(current);
      next.add(formID);
      return next;
    });
  }, []);

  const markFormClean = useCallback((formID: string) => {
    setDirtyForms((current) => {
      if (!current.has(formID)) return current;
      const next = new Set(current);
      next.delete(formID);
      return next;
    });
  }, []);

  const setPending = useCallback((operation: string, pending: boolean) => {
    setPendingOperations((current) => {
      if (current.has(operation) === pending) return current;
      const next = new Set(current);
      if (pending) next.add(operation);
      else next.delete(operation);
      return next;
    });
  }, []);

  const isPending = useCallback(
    (operation: string) => pendingOperations.has(operation),
    [pendingOperations],
  );

  const trackFormChange = (event: ChangeEvent<HTMLDivElement>) => {
    if (!(event.target instanceof Element)) return;
    const form = event.target.closest<HTMLFormElement>("form[data-unsaved-id]");
    if (form?.dataset.unsavedId) markFormDirty(form.dataset.unsavedId);
  };

  return { isPending, markFormClean, markFormDirty, setPending, trackFormChange };
}
