import { useCallback, useEffect } from "react";

import type { UnsavedChangesRegistry } from "../household-route-context";

export function handleUnsavedChangesBeforeUnload(
  event: Pick<BeforeUnloadEvent, "preventDefault" | "returnValue">,
  hasUnsavedChanges: () => boolean,
) {
  if (!hasUnsavedChanges()) return;
  event.preventDefault();
  event.returnValue = "";
}

export function useUnsavedChanges(registry: UnsavedChangesRegistry) {
  const hasUnsavedChanges = useCallback(() => registry.hasUnsavedChanges(), [registry]);

  useEffect(() => {
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      handleUnsavedChangesBeforeUnload(event, hasUnsavedChanges);
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [hasUnsavedChanges]);

  return hasUnsavedChanges;
}
