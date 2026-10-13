import { createFileRoute } from "@tanstack/react-router";

import { SettingsPage } from "../../pages/settings";
import { useHouseholdRouteContext } from "../household-route-context";

export const Route = createFileRoute("/households/$householdId/settings")({
  component: HouseholdSettings,
});

function HouseholdSettings() {
  const { household, refreshHousehold, reportUnsavedChanges, showNotice, viewerId } =
    useHouseholdRouteContext();
  if (!household) return null;
  return (
    <SettingsPage
      household={household}
      isOwner={household.members.some(
        (member) => member.id === viewerId && member.role === "OWNER",
      )}
      onUnsavedChanges={reportUnsavedChanges}
      onRefresh={() => refreshHousehold(household.id)}
      onNotice={showNotice}
    />
  );
}
