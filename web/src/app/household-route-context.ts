import { createContext, useContext } from "react";
import type { Household } from "../shared/api";
import type { AppPage } from "./app-page";

export type HouseholdRouteContext = {
  household: Household | null;
  viewerId: string | null;
  navigateToPage: (page: AppPage) => void;
  reportUnsavedChanges: (unsaved: boolean) => void;
  onPurchaseQueued: (idempotencyKey: string) => void;
  onBackFromPurchase: () => void;
  refreshHousehold: (id: string) => Promise<void>;
  showNotice: (message: string) => void;
};

export const householdRouteContext = createContext<HouseholdRouteContext | null>(null);

export function useHouseholdRouteContext(): HouseholdRouteContext {
  const context = useContext(householdRouteContext);
  if (!context) throw new Error("Household route context is unavailable");
  return context;
}
