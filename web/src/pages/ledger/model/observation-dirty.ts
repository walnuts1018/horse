import { useCallback, useEffect, useState } from "react";

export function useObservationDirtyState(onUnsavedChanges: (unsaved: boolean) => void) {
  const [dirtyObservations, setDirtyObservations] = useState<Set<string>>(() => new Set());

  useEffect(() => {
    onUnsavedChanges(dirtyObservations.size > 0);
  }, [dirtyObservations, onUnsavedChanges]);
  useEffect(() => () => onUnsavedChanges(false), [onUnsavedChanges]);

  return useCallback((id: string, dirty: boolean) => {
    setDirtyObservations((current) => {
      if (current.has(id) === dirty) return current;
      const next = new Set(current);
      if (dirty) next.add(id);
      else next.delete(id);
      return next;
    });
  }, []);
}
