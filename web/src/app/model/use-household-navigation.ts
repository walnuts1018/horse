import { useBlocker, useNavigate, useParams, useRouterState } from "@tanstack/react-router";
import { useCallback, useEffect, useRef } from "react";

import { isAppPage, type AppPage } from "../app-page";
import { hasSameNavigationTarget, type UnsavedChangesRegistry } from "../household-route-context";

export function useHouseholdNavigation(unsavedChangesRegistry: UnsavedChangesRegistry) {
  const navigate = useNavigate({ from: "/" });
  const params = useParams({ strict: false });
  const householdId = "householdId" in params ? params.householdId : undefined;
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const page = appPageFromPath(pathname);
  const currentPage = useRef(page);
  const purchaseCreatePreviousPage = useRef<AppPage>("home");

  useEffect(() => {
    currentPage.current = page;
  }, [page]);

  useBlocker({
    shouldBlockFn: ({ current, next }) => {
      if (hasSameNavigationTarget(current, next)) return false;
      return (
        unsavedChangesRegistry.hasUnsavedChanges() &&
        !window.confirm("未保存の変更を破棄しますか？")
      );
    },
    enableBeforeUnload: false,
  });

  const navigateToHouseholdPage = useCallback(
    (id: string, nextPage: AppPage, replace = false) => {
      const toByPage = {
        home: "/households/$householdId",
        ledger: "/households/$householdId/ledger",
        new: "/households/$householdId/new",
        settlement: "/households/$householdId/settlement",
        analysis: "/households/$householdId/analysis",
        imports: "/households/$householdId/imports",
        settings: "/households/$householdId/settings",
      } as const;
      void navigate({
        to: toByPage[nextPage],
        params: { householdId: id },
        search: true,
        replace,
      });
    },
    [navigate],
  );

  const navigateToPage = useCallback(
    (nextPage: AppPage) => {
      if (page === nextPage) return;
      if (nextPage === "new" && page !== "new") purchaseCreatePreviousPage.current = page;
      if (householdId) navigateToHouseholdPage(householdId, nextPage);
    },
    [page, householdId, navigateToHouseholdPage],
  );

  const getCurrentPage = useCallback(() => currentPage.current, []);
  const onNavigateToPurchase = useCallback(() => {
    if (page !== "new") purchaseCreatePreviousPage.current = page;
  }, [page]);
  const onBackFromPurchase = useCallback(
    () => navigateToPage(purchaseCreatePreviousPage.current),
    [navigateToPage],
  );

  return {
    householdId,
    page,
    pathname,
    navigateToHouseholdPage,
    navigateToPage,
    getCurrentPage,
    onNavigateToPurchase,
    onBackFromPurchase,
  };
}

function appPageFromPath(pathname: string): AppPage {
  const child = /^\/households\/[^/]+\/([^/]+)\/?$/.exec(pathname)?.[1];
  return isAppPage(child) ? child : "home";
}
