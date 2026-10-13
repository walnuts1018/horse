import { createFileRoute } from "@tanstack/react-router";

import { ImportsPage } from "../../pages/imports";
import { useHouseholdRouteContext } from "../household-route-context";

export const Route = createFileRoute("/households/$householdId/imports")({
  component: HouseholdImports,
});

function HouseholdImports() {
  const { household, navigateToPage, refreshHousehold, reportUnsavedChanges, showNotice } =
    useHouseholdRouteContext();
  if (!household) return null;
  return (
    <ImportsPage
      household={household}
      onUnsavedChanges={reportUnsavedChanges}
      onAdded={async (keepOpen) => {
        await refreshHousehold(household.id);
        if (!keepOpen) navigateToPage("ledger");
        showNotice("買い物を帳簿に追加しました", "success");
      }}
    />
  );
}
