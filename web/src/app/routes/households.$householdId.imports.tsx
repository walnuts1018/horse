import { createFileRoute, useLocation } from "@tanstack/react-router";
import { useCallback, useMemo } from "react";

import { ImportsPage, parseDocumentFilters, serializeDocumentFilters } from "../../pages/imports";
import { useHouseholdRouteContext } from "../household-route-context";

export const Route = createFileRoute("/households/$householdId/imports")({
  validateSearch: (search: Record<string, unknown>) => ({
    ...serializeDocumentFilters(parseDocumentFilters(search)),
    ...(search.capture === "1" ? { capture: "1" } : {}),
  }),
  component: HouseholdImports,
});

function HouseholdImports() {
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const filterLocation = useLocation();
  const documentFilters = useMemo(() => parseDocumentFilters(search), [search]);
  const handleCaptureHandled = useCallback(
    () =>
      void navigate({
        search: (previous) => {
          const next = { ...previous };
          delete next.capture;
          return next;
        },
      }),
    [navigate],
  );
  const { household, navigateToPage, refreshHousehold, reportUnsavedChanges, showNotice } =
    useHouseholdRouteContext();
  if (!household) return null;
  return (
    <ImportsPage
      household={household}
      captureRequested={search.capture === "1"}
      onCaptureHandled={handleCaptureHandled}
      documentFilters={documentFilters}
      filterLocation={filterLocation}
      onApplyDocumentFilters={(filters) =>
        void navigate({
          search: (previous) => {
            const next = { ...previous };
            for (const key of ["store", "from", "to", "sourceKind", "confirmed"] as const) {
              delete next[key];
            }
            return { ...next, ...serializeDocumentFilters(filters) };
          },
        })
      }
      onUnsavedChanges={reportUnsavedChanges}
      onAdded={async (keepOpen) => {
        let refreshFailed = false;
        try {
          await refreshHousehold(household.id);
        } catch {
          refreshFailed = true;
        }
        if (!keepOpen) navigateToPage("ledger");
        if (refreshFailed) {
          showNotice(
            "保存しましたが家計簿を更新できませんでした。画面を再読み込みしてください",
            "error",
          );
          return;
        }
        showNotice("買い物を帳簿に追加しました", "success");
      }}
    />
  );
}
