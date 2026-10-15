import { createFileRoute } from "@tanstack/react-router";

import {
  defaultSettingsTab,
  isSettingsTab,
  SettingsPage,
  type SettingsTab,
} from "../../pages/settings";
import { useHouseholdRouteContext } from "../household-route-context";

export const Route = createFileRoute("/households/$householdId/settings")({
  validateSearch: (
    search: Record<string, unknown>,
  ): Record<string, unknown> & { tab?: SettingsTab } => ({
    tab: isSettingsTab(search.tab) ? search.tab : defaultSettingsTab,
  }),
  component: HouseholdSettings,
});

function HouseholdSettings() {
  const navigate = Route.useNavigate();
  const tab = Route.useSearch().tab ?? defaultSettingsTab;
  const { household, refreshHousehold, reportUnsavedChanges, showNotice, viewerId } =
    useHouseholdRouteContext();
  if (!household) return null;
  return (
    <SettingsPage
      household={household}
      viewerId={viewerId}
      onLeave={() => void navigate({ to: "/", replace: true })}
      activeTab={tab}
      onSelectTab={(nextTab) =>
        void navigate({ search: (previous) => ({ ...previous, tab: nextTab }) })
      }
      isOwner={household.members.some(
        (member) => member.id === viewerId && member.role === "OWNER",
      )}
      onUnsavedChanges={reportUnsavedChanges}
      onRefresh={() => refreshHousehold(household.id)}
      onNotice={showNotice}
    />
  );
}
