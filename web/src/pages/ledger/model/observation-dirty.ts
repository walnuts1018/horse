import { useCallback, useEffect, useRef } from "react";

// dirty状態の集計はrefで保持し、親への通知はイベントハンドラ経由で行う(Effect経由のpushを避ける)
export function useObservationDirtyState(onUnsavedChanges: (unsaved: boolean) => void) {
  const dirtyObservationsRef = useRef<Set<string>>(new Set());
  const onUnsavedChangesRef = useRef(onUnsavedChanges);
  useEffect(() => {
    onUnsavedChangesRef.current = onUnsavedChanges;
  }, [onUnsavedChanges]);
  useEffect(() => () => onUnsavedChangesRef.current(false), []);

  return useCallback((id: string, dirty: boolean) => {
    const current = dirtyObservationsRef.current;
    if (current.has(id) === dirty) return;
    if (dirty) current.add(id);
    else current.delete(id);
    onUnsavedChangesRef.current(current.size > 0);
  }, []);
}
