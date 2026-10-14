import { createFileRoute } from "@tanstack/react-router";

import { SettingsPage } from "../../pages/settings";
import { useHouseholdRouteContext } from "../household-route-context";

export const Route = createFileRoute("/households/$householdId/settings")({
  component: HouseholdSettings,
});

function HouseholdSettings() {
  const navigate = Route.useNavigate();
  const { household, refreshHousehold, reportUnsavedChanges, showNotice, viewerId } =
    useHouseholdRouteContext();
  if (!household) return null;
  return (
    <SettingsPage
      household={household}
      viewerId={viewerId}
      onLeave={() => void navigate({ to: "/", replace: true })}
      isOwner={household.members.some(
        (member) => member.id === viewerId && member.role === "OWNER",
      )}
      onUnsavedChanges={reportUnsavedChanges}
      onRefresh={() => refreshHousehold(household.id)}
      onNotice={showNotice}
    />
  );
}
