import { createContext, useContext } from "react";

import type { Household } from "../shared/api";
import type { NoticeKind } from "../shared/ui";
import type { AppPage } from "./app-page";

export type UnsavedChangesRegistry = {
  report: (id: string, unsaved: boolean) => void;
  clear: (id: string) => void;
  clearAll: () => void;
  getDirtyIds: () => string[];
  hasUnsavedChanges: () => boolean;
  subscribe: (listener: () => void) => () => void;
};

export function hasSameNavigationTarget(
  current: { pathname: string; params: Record<string, unknown>; search: unknown },
  next: { pathname: string; params: Record<string, unknown>; search: unknown },
): boolean {
  const currentParamKeys = Object.keys(current.params);
  const nextParamKeys = Object.keys(next.params);
  return (
    current.pathname === next.pathname &&
    currentParamKeys.length === nextParamKeys.length &&
    currentParamKeys.every(
      (key) => nextParamKeys.includes(key) && current.params[key] === next.params[key],
    ) &&
    JSON.stringify(current.search) === JSON.stringify(next.search)
  );
}

export function createUnsavedChangesRegistry(): UnsavedChangesRegistry {
  const dirtyIds = new Set<string>();
  const listeners = new Set<() => void>();
  const notify = () => listeners.forEach((listener) => listener());
  const update = (id: string, unsaved: boolean) => {
    const changed = unsaved ? !dirtyIds.has(id) : dirtyIds.has(id);
    if (!changed) return;
    if (unsaved) dirtyIds.add(id);
    else dirtyIds.delete(id);
    notify();
  };

  return {
    report: update,
    clear: (id) => update(id, false),
    clearAll: () => {
      if (dirtyIds.size === 0) return;
      dirtyIds.clear();
      notify();
    },
    getDirtyIds: () => [...dirtyIds],
    hasUnsavedChanges: () => dirtyIds.size > 0,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}

export type HouseholdRouteContext = {
  household: Household | null;
  viewerId: string | null;
  navigateToPage: (page: AppPage) => void;
  onNavigateToPurchase: () => void;
  reportUnsavedChanges: (unsaved: boolean) => void;
  onPurchaseQueued: (idempotencyKey: string) => void;
  onBackFromPurchase: () => void;
  refreshHousehold: (id: string) => Promise<void>;
  showNotice: (message: string, kind?: NoticeKind) => void;
};

export const householdRouteContext = createContext<HouseholdRouteContext | null>(null);

export function useHouseholdRouteContext(): HouseholdRouteContext {
  const context = useContext(householdRouteContext);
  if (!context) throw new Error("Household route context is unavailable");
  return context;
}
